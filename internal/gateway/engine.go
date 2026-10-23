package gateway

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

type Policy struct {
	Mode      string    `json:"mode"`
	Algorithm Algorithm `json:"algorithm"`
	Rate      float64   `json:"rate"`
	Burst     int       `json:"burst"`
}

func (p Policy) Validate() error {
	if p.Mode != "fixed" && p.Mode != "adaptive" {
		return errors.New("mode must be fixed or adaptive")
	}
	if !p.Algorithm.Valid() {
		return errors.New("unknown algorithm")
	}
	if math.IsNaN(p.Rate) || math.IsInf(p.Rate, 0) || p.Rate < 1 || p.Rate > 1000 || p.Burst < 1 || p.Burst > 1000 {
		return errors.New("rate/burst outside bounded range")
	}
	return nil
}

type featureBucket struct {
	tick              int64
	attempts, unknown int
}
type ClientState struct {
	ID            string  `json:"id"`
	Attempts      uint64  `json:"attempts"`
	Success       uint64  `json:"success"`
	Limited       uint64  `json:"limited"`
	Capacity      uint64  `json:"capacity_rejected"`
	ClientErrors  uint64  `json:"client_errors"`
	Circuit       uint64  `json:"circuit_rejected"`
	OriginErrors  uint64  `json:"origin_errors"`
	ErrorEWMA     float64 `json:"error_ewma"`
	Pressure      float64 `json:"pressure"`
	UnknownShare  float64 `json:"unknown_share"`
	Score         float64 `json:"score"`
	Multiplier    float64 `json:"multiplier"`
	EffectiveRate float64 `json:"effective_rate"`
	Reason        string  `json:"reason"`
	lastTick      int64
	limiter       Limiter
	buckets       [10]featureBucket
}
type Decision struct {
	Allowed bool
	Retry   time.Duration
	Rate    float64
	Score   float64
	Reason  string
}
type Event struct {
	At        int64   `json:"at"`
	Client    string  `json:"client"`
	Endpoint  string  `json:"endpoint"`
	Outcome   string  `json:"outcome"`
	Status    int     `json:"status"`
	LatencyMS float64 `json:"latency_ms"`
}
type Bucket struct {
	Second   int64 `json:"second"`
	Success  int   `json:"success"`
	Limited  int   `json:"limited"`
	Capacity int   `json:"capacity"`
	Failed   int   `json:"failed"`
}
type Snapshot struct {
	Policy   Policy        `json:"policy"`
	Version  uint64        `json:"version"`
	Clients  []ClientState `json:"clients"`
	Events   []Event       `json:"events"`
	Timeline []Bucket      `json:"timeline"`
}
type Engine struct {
	started  int64
	mu       sync.Mutex
	policy   Policy
	version  uint64
	clients  map[string]*ClientState
	events   []Event
	timeline [60]Bucket
}

func NewEngine(policy Policy, ids []string) (*Engine, error) {
	if err := policy.Validate(); err != nil {
		return nil, err
	}
	if len(ids) == 0 || len(ids) > 128 {
		return nil, errors.New("one to 128 registered clients required")
	}
	e := &Engine{started: time.Now().Unix(), policy: policy, version: 1, clients: make(map[string]*ClientState)}
	for _, id := range ids {
		if len(id) == 0 || len(id) > 32 {
			return nil, errors.New("client id length limit")
		}
		if _, ok := e.clients[id]; ok {
			return nil, errors.New("duplicate client id")
		}
		for _, c := range id {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return nil, errors.New("client id must be lowercase synthetic label")
			}
		}
		e.clients[id] = &ClientState{ID: id, Multiplier: 1, EffectiveRate: policy.Rate, Reason: "warming up"}
	}
	return e, nil
}
func (e *Engine) Update(policy Policy) error {
	if err := policy.Validate(); err != nil {
		return err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.policy = policy
	e.version++
	// Explicit policy edits reset algorithm/feature state; lifetime outcomes remain.
	for _, c := range e.clients {
		c.lastTick = 0
		c.limiter = Limiter{}
		c.buckets = [10]featureBucket{}
		c.ErrorEWMA = 0
		c.Score = 0
		c.Multiplier = 1
		c.EffectiveRate = policy.Rate
		c.Reason = "policy reset"
	}
	return nil
}
func endpoint(path string) string {
	switch path {
	case "/v1/read":
		return "read"
	case "/v1/search":
		return "search"
	default:
		return "unknown"
	}
}
func (e *Engine) Admit(id, path string, now time.Time) Decision {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, ok := e.clients[id]
	if !ok {
		return Decision{Reason: "unknown identity"}
	}
	c.Attempts++
	tick := now.UnixNano() / int64(100*time.Millisecond)
	if tick < c.lastTick {
		tick = c.lastTick
	}
	c.lastTick = tick
	b := &c.buckets[tick%10]
	if b.tick != tick {
		*b = featureBucket{tick: tick}
	}
	b.attempts++
	if endpoint(path) == "unknown" {
		b.unknown++
	}
	attempts, unknown := 0, 0
	for _, bucket := range c.buckets {
		if bucket.tick <= tick && bucket.tick > tick-10 {
			attempts += bucket.attempts
			unknown += bucket.unknown
		}
	}
	c.Pressure = math.Max(0, math.Min(1, (float64(attempts)/e.policy.Rate-1)/3))
	c.UnknownShare = float64(unknown) / float64(attempts)
	c.Score = .2*c.Pressure + .5*c.ErrorEWMA + .3*c.UnknownShare
	c.Multiplier = 1
	c.Reason = "fixed budget"
	if e.policy.Mode == "adaptive" {
		c.Reason = "warming up"
		if attempts >= 8 {
			c.Reason = "healthy mix; bursts alone are not penalized"
			if c.ErrorEWMA >= .25 || c.UnknownShare >= .5 {
				c.Multiplier = math.Max(.1, 1-.9*c.Score)
				c.Reason = "client errors or unknown endpoint mix"
			}
		}
	}
	c.EffectiveRate = e.policy.Rate * c.Multiplier
	burst := int(math.Max(1, math.Floor(float64(e.policy.Burst)*c.Multiplier)))
	allowed, retry := c.limiter.Allow(now, e.policy.Algorithm, c.EffectiveRate, burst)
	return Decision{allowed, retry, c.EffectiveRate, c.Score, c.Reason}
}
func (e *Engine) Observe(id, path, outcome string, status int, latency time.Duration, now time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c, ok := e.clients[id]
	if !ok {
		return
	}
	switch outcome {
	case "success":
		c.Success++
	case "limited":
		c.Limited++
	case "capacity":
		c.Capacity++
	case "client_error":
		c.ClientErrors++
	case "circuit":
		c.Circuit++
	default:
		c.OriginErrors++
	}
	// Only completed origin responses below 500 teach client-caused error behavior.
	// Origin faults/capacity/circuit/rate denials never count as a client error.
	if (outcome == "success" || outcome == "client_error") && status < 500 {
		sample := 0.0
		if status >= 400 && status != 429 {
			sample = 1
		}
		c.ErrorEWMA = .8*c.ErrorEWMA + .2*sample
	}
	event := Event{now.UnixMilli(), id, endpoint(path), outcome, status, float64(latency) / float64(time.Millisecond)}
	if len(e.events) == 200 {
		copy(e.events, e.events[1:])
		e.events = e.events[:199]
	}
	e.events = append(e.events, event)
	second := now.Unix()
	b := &e.timeline[second%60]
	if b.Second != second {
		*b = Bucket{Second: second}
	}
	switch outcome {
	case "success":
		b.Success++
	case "limited":
		b.Limited++
	case "capacity":
		b.Capacity++
	default:
		b.Failed++
	}
}
func (e *Engine) Snapshot(now time.Time) Snapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := Snapshot{Policy: e.policy, Version: e.version, Clients: make([]ClientState, 0, len(e.clients)), Events: append([]Event{}, e.events...), Timeline: []Bucket{}}
	for _, c := range e.clients {
		copy := *c
		copy.limiter = Limiter{}
		copy.buckets = [10]featureBucket{}
		out.Clients = append(out.Clients, copy)
	}
	sort.Slice(out.Clients, func(i, j int) bool { return out.Clients[i].ID < out.Clients[j].ID })
	start := now.Unix() - 59
	if start < e.started {
		start = e.started
	}
	for sec := start; sec <= now.Unix(); sec++ {
		b := e.timeline[sec%60]
		if b.Second != sec {
			b = Bucket{Second: sec}
		}
		out.Timeline = append(out.Timeline, b)
	}
	return out
}
func (e *Engine) Metrics(now time.Time) string {
	state := e.Snapshot(now)
	out := "# TYPE throttle_client_outcomes_total counter\n"
	for _, c := range state.Clients {
		for _, v := range []struct {
			name string
			n    uint64
		}{{"success", c.Success}, {"limited", c.Limited}, {"capacity", c.Capacity}, {"origin_error", c.OriginErrors}, {"client_error", c.ClientErrors}, {"circuit", c.Circuit}} {
			out += fmt.Sprintf("throttle_client_outcomes_total{client=%q,outcome=%q} %d\n", c.ID, v.name, v.n)
		}
	}
	return out
}
