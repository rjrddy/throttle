package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Config struct {
	Policy       Policy
	Keys         map[string]string // client ID -> raw API key, never exposed in state/logs
	AdminToken   string
	Upstream     string
	Capacity     int
	WebDir       string
	LoopbackOnly bool
	Context      context.Context
	Logger       *slog.Logger
}
type CampaignRunner func(context.Context) (any, error)
type Server struct {
	Engine        *Engine
	Breaker       *Breaker
	proxy         *httputil.ReverseProxy
	keys          map[[32]byte]string
	admin         string
	capacity      chan struct{}
	web           string
	loopback      bool
	ctx           context.Context
	logger        *slog.Logger
	control       sync.Mutex
	runner        CampaignRunner
	busy          bool
	result        any
	campaignError string
	demoOrigin    *DemoOrigin
}

func DemoKeys() map[string]string {
	return map[string]string{
		"legit-amber": "synthetic-throttle-amber-key-0001", "legit-birch": "synthetic-throttle-birch-key-0001",
		"legit-coral": "synthetic-throttle-coral-key-0001", "legit-dune": "synthetic-throttle-dune-key-0001",
		"abuse-east": "synthetic-throttle-east-key-00001", "abuse-west": "synthetic-throttle-west-key-00001",
		"abuse-north": "synthetic-throttle-north-key-0001", "abuse-south": "synthetic-throttle-south-key-0001"}
}
func NewServer(c Config) (*Server, error) {
	u, err := url.Parse(c.Upstream)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return nil, errors.New("upstream must be an explicit HTTP origin without credentials/path/query")
	}
	if c.Capacity < 1 || c.Capacity > 64 {
		return nil, errors.New("capacity outside 1..64")
	}
	ids := []string{}
	keys := map[[32]byte]string{}
	for id, key := range c.Keys {
		if len(key) < 24 || len(key) > 256 {
			return nil, errors.New("client API key length limit")
		}
		hash := sha256.Sum256([]byte(key))
		if _, ok := keys[hash]; ok {
			return nil, errors.New("duplicate API key")
		}
		keys[hash] = id
		ids = append(ids, id)
	}
	engine, err := NewEngine(c.Policy, ids)
	if err != nil {
		return nil, err
	}
	if !c.LoopbackOnly && len(c.AdminToken) < 24 {
		return nil, errors.New("external listener requires admin token of at least 24 bytes")
	}
	if c.Context == nil {
		c.Context = context.Background()
	}
	if c.Logger == nil {
		c.Logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	s := &Server{Engine: engine, Breaker: NewBreaker(3, time.Second), keys: keys, admin: c.AdminToken, capacity: make(chan struct{}, c.Capacity), web: c.WebDir, loopback: c.LoopbackOnly, ctx: c.Context, logger: c.Logger}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 200 * time.Millisecond, KeepAlive: 30 * time.Second}).DialContext, MaxIdleConns: 64, MaxIdleConnsPerHost: 64, MaxConnsPerHost: 64, IdleConnTimeout: 30 * time.Second, ResponseHeaderTimeout: 250 * time.Millisecond, TLSHandshakeTimeout: 2 * time.Second}
	s.proxy = &httputil.ReverseProxy{Transport: transport, Rewrite: func(p *httputil.ProxyRequest) {
		p.SetURL(u)
		p.Out.URL.Path = strings.TrimPrefix(p.In.URL.Path, "/proxy")
		p.Out.URL.RawPath = ""
		p.Out.Header.Del("Authorization")
		p.Out.Header.Del("X-API-Key")
		p.Out.Header.Del("X-Client-ID")
		p.Out.Header.Del("X-Throttle-Client")
		p.Out.Header.Del("Forwarded")
		p.Out.Header.Del("X-Forwarded-For")
		p.Out.Header.Del("X-Forwarded-Host")
		p.Out.Header.Del("X-Forwarded-Proto")
	}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
		http.Error(w, "origin unavailable", http.StatusBadGateway)
	}}
	return s, nil
}
func (s *Server) Close() {
	if t, ok := s.proxy.Transport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
}
func (s *Server) SetCampaignRunner(runner CampaignRunner) {
	s.control.Lock()
	defer s.control.Unlock()
	s.runner = runner
}
func (s *Server) SetDemoOrigin(origin *DemoOrigin) { s.demoOrigin = origin }
func sendJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func bearer(r *http.Request) string {
	v := r.Header.Get("Authorization")
	if strings.HasPrefix(v, "Bearer ") {
		return strings.TrimPrefix(v, "Bearer ")
	}
	return ""
}
func (s *Server) isAdmin(r *http.Request) bool {
	if s.admin == "" {
		return s.loopback
	}
	a := sha256.Sum256([]byte(bearer(r)))
	b := sha256.Sum256([]byte(s.admin))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; frame-ancestors 'none'")
	if len(r.URL.RawQuery) > 2048 || len(r.URL.Path) > 256 {
		http.Error(w, "request target too large", 414)
		return
	}
	if s.loopback {
		host, _, err := net.SplitHostPort(r.Host)
		if err != nil || host != "127.0.0.1" && host != "localhost" {
			http.Error(w, "loopback Host required", 403)
			return
		}
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
		http.Error(w, "cross-origin request rejected", 403)
		return
	}
	if r.URL.Path == "/health" && r.Method == "GET" {
		sendJSON(w, 200, map[string]string{"status": "ok"})
		return
	}
	if strings.HasPrefix(r.URL.Path, "/proxy/") {
		s.forward(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/metrics" {
		if !s.isAdmin(r) {
			http.Error(w, "admin token required", 401)
			return
		}
		switch {
		case r.URL.Path == "/api/state" && r.Method == "GET":
			s.control.Lock()
			busy, result, failure := s.busy, s.result, s.campaignError
			demo := s.runner != nil
			s.control.Unlock()
			originState := "external"
			if s.demoOrigin != nil {
				originState = s.demoOrigin.Mode()
			}
			sendJSON(w, 200, map[string]any{"engine": s.Engine.Snapshot(time.Now()), "inflight": len(s.capacity), "capacity": cap(s.capacity), "breaker": s.Breaker.State(time.Now()), "campaign_busy": busy, "campaign_result": result, "campaign_error": failure, "demo_enabled": demo, "origin_mode": originState})
			return
		case r.URL.Path == "/api/policy" && r.Method == "PUT":
			var p Policy
			if err := decode(w, r, &p); err != nil {
				http.Error(w, "invalid policy JSON", 400)
				return
			}
			s.control.Lock()
			defer s.control.Unlock()
			if s.busy {
				http.Error(w, "campaign active", 409)
				return
			}
			if err := s.Engine.Update(p); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			s.logger.Info("policy_changed", "mode", p.Mode, "algorithm", p.Algorithm)
			sendJSON(w, 200, p)
			return
		case r.URL.Path == "/api/campaign" && r.Method == "POST":
			s.control.Lock()
			defer s.control.Unlock()
			if s.runner == nil {
				http.Error(w, "synthetic load disabled", 403)
				return
			}
			if s.busy {
				http.Error(w, "campaign already active", 409)
				return
			}
			s.busy = true
			s.result = nil
			s.campaignError = ""
			runner := s.runner
			go func() {
				ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
				defer cancel()
				result, err := runner(ctx)
				s.control.Lock()
				defer s.control.Unlock()
				s.busy = false
				s.result = result
				if err != nil {
					s.campaignError = "campaign failed or cancelled"
				}
				s.logger.Info("campaign_finished", "error", err != nil)
			}()
			sendJSON(w, 202, map[string]bool{"started": true})
			return
		case r.URL.Path == "/api/origin" && r.Method == "PUT":
			if s.demoOrigin == nil {
				http.Error(w, "origin control disabled", 403)
				return
			}
			var body struct {
				Mode string `json:"mode"`
			}
			if err := decode(w, r, &body); err != nil {
				http.Error(w, "invalid origin JSON", 400)
				return
			}
			s.control.Lock()
			defer s.control.Unlock()
			if s.busy {
				http.Error(w, "campaign active", 409)
				return
			}
			if !s.demoOrigin.SetMode(body.Mode) {
				http.Error(w, "invalid origin mode", 400)
				return
			}
			sendJSON(w, 200, body)
			return
		case r.URL.Path == "/metrics" && r.Method == "GET":
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			_, _ = io.WriteString(w, s.Engine.Metrics(time.Now()))
			return
		default:
			http.Error(w, "route or method not found", 404)
			return
		}
	}
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}
	// Vite emits only the fixed root and hashed asset paths. No user-controlled traversal.
	clean := filepath.Clean(r.URL.Path)
	if clean != "/" && !strings.HasPrefix(clean, "/assets/") {
		http.NotFound(w, r)
		return
	}
	if strings.Contains(r.URL.Path, "..") {
		http.NotFound(w, r)
		return
	}
	if clean == "/" {
		clean = "/index.html"
	}
	path := filepath.Join(s.web, clean)
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, path)
}
func decode(w http.ResponseWriter, r *http.Request, value any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

type capture struct {
	http.ResponseWriter
	status int
}

func (w *capture) WriteHeader(status int) {
	if status < 200 {
		w.ResponseWriter.WriteHeader(status)
		return
	}
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *capture) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(body)
}
func (w *capture) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (s *Server) forward(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	raw := bearer(r)
	id, ok := s.keys[sha256.Sum256([]byte(raw))]
	if !ok {
		http.Error(w, "client API key required", 401)
		return
	}
	if r.ContentLength > 65536 {
		http.Error(w, "body size limit", 413)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "body size limit or read failure", 413)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
	start := time.Now()
	path := strings.TrimPrefix(r.URL.Path, "/proxy")
	record := func(outcome string, status int) {
		s.Engine.Observe(id, path, outcome, status, time.Since(start), time.Now())
	}
	d := s.Engine.Admit(id, path, start)
	w.Header().Set("X-Throttle-Rate", fmt.Sprintf("%.3f", d.Rate))
	w.Header().Set("X-Throttle-Policy", d.Reason)
	if !d.Allowed {
		w.Header().Set("Retry-After", fmt.Sprint(int(math.Max(1, math.Ceil(d.Retry.Seconds())))))
		http.Error(w, "per-client budget exhausted", 429)
		record("limited", 429)
		return
	}
	select {
	case s.capacity <- struct{}{}:
		defer func() { <-s.capacity }()
	default:
		http.Error(w, "protected origin capacity exhausted", 503)
		record("capacity", 503)
		return
	}
	permit, allowed := s.Breaker.Allow(time.Now())
	if !allowed {
		http.Error(w, "origin circuit open", 503)
		record("circuit", 503)
		return
	}
	requestContext := r.Context()
	ctx, cancel := context.WithTimeout(requestContext, time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	response := &capture{ResponseWriter: w}
	finalized := false
	defer func() {
		if !finalized {
			// ReverseProxy can panic ErrAbortHandler after a truncated origin body.
			// Finalize the breaker/capacity ownership even when net/http must abort the stream.
			if errors.Is(requestContext.Err(), context.Canceled) {
				s.Breaker.Cancel(permit)
			} else {
				s.Breaker.Done(permit, true, time.Now())
			}
			record("origin_error", 502)
		}
	}()
	s.proxy.ServeHTTP(response, r)
	finalized = true
	if response.status == 0 {
		response.status = 200
	}
	failed := response.status >= 500
	if errors.Is(requestContext.Err(), context.Canceled) {
		s.Breaker.Cancel(permit)
	} else {
		s.Breaker.Done(permit, failed, time.Now())
	}
	outcome := "success"
	if failed {
		outcome = "origin_error"
	} else if response.status >= 400 {
		outcome = "client_error"
	}
	record(outcome, response.status)
}
