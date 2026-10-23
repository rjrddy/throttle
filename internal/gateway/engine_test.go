package gateway

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestEngine(t testing.TB, mode string, algorithm Algorithm) *Engine {
	t.Helper()
	e, err := NewEngine(Policy{Mode: mode, Algorithm: algorithm, Rate: 20, Burst: 12}, []string{"neutral"})
	if err != nil {
		t.Fatal(err)
	}
	return e
}
func TestBurstAloneNeverTriggersPenalty(t *testing.T) {
	e := newTestEngine(t, "adaptive", TokenBucket)
	now := time.Unix(100, 0)
	for i := 0; i < 200; i++ {
		e.Admit("neutral", "/v1/read", now)
		e.Observe("neutral", "/v1/read", "success", 200, time.Millisecond, now)
	}
	c := e.Snapshot(now).Clients[0]
	if c.Pressure != 1 || c.Multiplier != 1 || c.EffectiveRate != 20 {
		t.Fatal(c)
	}
}
func TestExplainablePenaltyAndHealthyRecovery(t *testing.T) {
	e := newTestEngine(t, "adaptive", TokenBucket)
	now := time.Unix(100, 0)
	for i := 0; i < 100; i++ {
		e.Admit("neutral", "/v1/missing", now)
		e.Observe("neutral", "/v1/missing", "client_error", 404, time.Millisecond, now)
	}
	c := e.Snapshot(now).Clients[0]
	if c.Score < .99 || c.Multiplier > .11 {
		t.Fatal("expected causal penalty", c)
	}
	now = now.Add(2 * time.Second)
	for i := 0; i < 60; i++ {
		e.Admit("neutral", "/v1/read", now.Add(time.Duration(i)*50*time.Millisecond))
		e.Observe("neutral", "/v1/read", "success", 200, time.Millisecond, now)
	}
	c = e.Snapshot(now.Add(3 * time.Second)).Clients[0]
	if c.Multiplier != 1 || c.ErrorEWMA > .001 {
		t.Fatal("healthy recovery", c)
	}
}
func TestServerErrorsDoNotBlameClient(t *testing.T) {
	e := newTestEngine(t, "adaptive", TokenBucket)
	now := time.Unix(100, 0)
	for i := 0; i < 50; i++ {
		e.Admit("neutral", "/v1/read", now)
		e.Observe("neutral", "/v1/read", "origin_error", 503, time.Millisecond, now)
	}
	c := e.Snapshot(now).Clients[0]
	if c.ErrorEWMA != 0 || c.Multiplier != 1 {
		t.Fatal("origin fault penalized client", c)
	}
}
func TestHealthyUnclassifiedEndpointPenaltyIsVisible(t *testing.T) {
	e := newTestEngine(t, "adaptive", TokenBucket)
	now := time.Unix(100, 0)
	for i := 0; i < 40; i++ {
		e.Admit("neutral", "/v1/preview", now)
		e.Observe("neutral", "/v1/preview", "success", 200, time.Millisecond, now)
	}
	c := e.Snapshot(now).Clients[0]
	if c.Multiplier >= 1 || c.ErrorEWMA != 0 {
		t.Fatal("documented false positive disappeared", c)
	}
}
func TestFixedModeAndPolicyValidation(t *testing.T) {
	e := newTestEngine(t, "fixed", TokenBucket)
	now := time.Unix(100, 0)
	for i := 0; i < 30; i++ {
		e.Admit("neutral", "/v1/missing", now)
		e.Observe("neutral", "/v1/missing", "client_error", 404, time.Millisecond, now)
	}
	if e.Snapshot(now).Clients[0].Multiplier != 1 {
		t.Fatal("fixed mode adapted")
	}
	for _, p := range []Policy{{Mode: "mystery", Algorithm: TokenBucket, Rate: 20, Burst: 12}, {Mode: "fixed", Algorithm: "bad", Rate: 20, Burst: 12}, {Mode: "fixed", Algorithm: GCRA, Rate: 0, Burst: 12}, {Mode: "fixed", Algorithm: GCRA, Rate: 20, Burst: 1001}} {
		if e.Update(p) == nil {
			t.Fatal("invalid policy accepted")
		}
	}
	old := e.Snapshot(now)
	if old.Version != 1 {
		t.Fatal("rejected update changed version")
	}
	if err := e.Update(Policy{Mode: "adaptive", Algorithm: GCRA, Rate: 10, Burst: 5}); err != nil {
		t.Fatal(err)
	}
	current := e.Snapshot(now)
	if current.Version != 2 || current.Clients[0].Attempts != old.Clients[0].Attempts {
		t.Fatal("policy reset lost lifetime counters")
	}
}
func TestConcurrentHotIdentityNeverOvershootsBurst(t *testing.T) {
	e := newTestEngine(t, "fixed", TokenBucket)
	now := time.Unix(100, 0)
	var count atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if e.Admit("neutral", "/v1/read", now).Allowed {
				count.Add(1)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 12 {
		t.Fatal("concurrent over-admission", count.Load())
	}
}
func TestRegistryAndMetricsBounds(t *testing.T) {
	if _, err := NewEngine(Policy{Mode: "fixed", Algorithm: TokenBucket, Rate: 20, Burst: 12}, []string{"unsafe\"\n"}); err == nil {
		t.Fatal("unsafe label accepted")
	}
	e := newTestEngine(t, "fixed", TokenBucket)
	if e.Admit("unknown", "/v1/read", time.Now()).Allowed {
		t.Fatal("unregistered client admitted")
	}
	for i := 0; i < 500; i++ {
		e.Observe("neutral", "/v1/read", "success", 200, time.Millisecond, time.Now())
	}
	if len(e.Snapshot(time.Now()).Events) != 200 {
		t.Fatal("event ring unbounded")
	}
	if e.Metrics(time.Now()) == "" {
		t.Fatal("missing metrics")
	}
}
func BenchmarkAdmission(b *testing.B) {
	for _, mode := range []string{"fixed", "adaptive"} {
		for _, algorithm := range []Algorithm{TokenBucket, SlidingWindow, GCRA} {
			for _, path := range []string{"/v1/read", "/v1/missing"} {
				b.Run(fmt.Sprintf("%s/%s/%s", mode, algorithm, path), func(b *testing.B) {
					e := newTestEngine(b, mode, algorithm)
					base := time.Unix(100, 0)
					for n := 0; n < 100; n++ {
						e.Admit("neutral", path, base)
						if path == "/v1/read" {
							e.Observe("neutral", path, "success", 200, time.Millisecond, base)
						} else {
							e.Observe("neutral", path, "client_error", 404, time.Millisecond, base)
						}
					}
					b.ReportAllocs()
					b.ResetTimer()
					allowed := 0
					for i := 0; i < b.N; i++ {
						if e.Admit("neutral", path, base.Add(time.Duration(i)*time.Millisecond)).Allowed {
							allowed++
						}
					}
					b.ReportMetric(100*float64(allowed)/float64(b.N), "allowed_pct")
				})
			}
		}
	}
}
