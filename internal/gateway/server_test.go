package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "synthetic-only-integration-key-0001"

func setupProxy(t *testing.T, handler http.Handler, admin string) (*Server, *httptest.Server) {
	t.Helper()
	origin := httptest.NewServer(handler)
	t.Cleanup(origin.Close)
	s, err := NewServer(Config{Policy: Policy{Mode: "fixed", Algorithm: TokenBucket, Rate: 1000, Burst: 1000}, Keys: map[string]string{"neutral": testKey}, AdminToken: admin, Upstream: origin.URL, Capacity: 4, LoopbackOnly: true, Context: context.Background()})
	if err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(s)
	t.Cleanup(func() { proxy.Close(); s.Close() })
	return s, proxy
}
func call(t testing.TB, client *http.Client, method, url, key, body string) (int, string, http.Header) {
	t.Helper()
	r, err := http.NewRequest(method, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		r.Header.Set("Authorization", "Bearer "+key)
	}
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(data), response.Header
}
func TestRealProxyAuthenticationHeadersAndBodyBounds(t *testing.T) {
	var originRequests atomic.Int64
	_, proxy := setupProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		originRequests.Add(1)
		if r.Header.Get("Authorization") != "" || r.Header.Get("X-Client-ID") != "" || r.Header.Get("X-Forwarded-For") != "" {
			t.Error("untrusted/secret headers reached origin")
		}
		_, _ = io.WriteString(w, "synthetic-origin")
	}), "synthetic-admin-test-key-00001")
	status, _, _ := call(t, proxy.Client(), "GET", proxy.URL+"/proxy/v1/read", "", "")
	if status != 401 || originRequests.Load() != 0 {
		t.Fatal("anonymous bypass")
	}
	request, _ := http.NewRequest("GET", proxy.URL+"/proxy/v1/read", nil)
	request.Header.Set("X-Client-ID", "neutral")
	response, err := proxy.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("spoofed identity bypass")
	}
	request, _ = http.NewRequest("GET", proxy.URL+"/proxy/v1/read", nil)
	request.Header.Set("Authorization", "Bearer "+testKey)
	request.Header.Set("X-Client-ID", "forged")
	request.Header.Set("X-Forwarded-For", "synthetic-forwarded")
	response, err = proxy.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("valid key rejected")
	}
	status, _, _ = call(t, proxy.Client(), "POST", proxy.URL+"/proxy/v1/read", testKey, strings.Repeat("x", 65537))
	t.Logf("caught oversized body: HTTP %d; origin requests remained %d", status, originRequests.Load())
	if status != 413 || originRequests.Load() != 1 {
		t.Fatal("oversized body forwarded")
	}
	status, _, _ = call(t, proxy.Client(), "GET", proxy.URL+"/api/state", "", "")
	if status != 401 {
		t.Fatal("unprotected admin")
	}
	status, body, _ := call(t, proxy.Client(), "GET", proxy.URL+"/api/state", "synthetic-admin-test-key-00001", "")
	if status != 200 || strings.Contains(body, testKey) {
		t.Fatal("state auth or secret leak")
	}
	request, _ = http.NewRequest("PUT", proxy.URL+"/api/policy", strings.NewReader(`{"mode":"fixed","algorithm":"token","rate":20,"burst":12}`))
	request.Header.Set("Authorization", "Bearer synthetic-admin-test-key-00001")
	request.Header.Set("Origin", "https://evil.invalid")
	response, err = proxy.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("cross-origin update allowed")
	}
	request, _ = http.NewRequest("GET", proxy.URL+"/health", nil)
	request.Host = "rebinding.invalid:1234"
	response, err = proxy.Client().Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("DNS rebinding host allowed")
	}
}
func TestRealProxyTimeoutCircuitAndRecovery(t *testing.T) {
	var slow atomic.Bool
	slow.Store(true)
	s, proxy := setupProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if slow.Load() {
			select {
			case <-time.After(time.Second):
			case <-r.Context().Done():
				return
			}
		}
		_, _ = io.WriteString(w, "recovered")
	}), "")
	for i := 0; i < 3; i++ {
		status, _, _ := call(t, proxy.Client(), "GET", proxy.URL+"/proxy/v1/read", testKey, "")
		t.Logf("caught origin timeout: HTTP %d", status)
		if status != 502 {
			t.Fatal("timeout not caught", status)
		}
	}
	if s.Breaker.State(time.Now()) != "open" {
		t.Fatal("circuit did not open")
	}
	status, _, _ := call(t, proxy.Client(), "GET", proxy.URL+"/proxy/v1/read", testKey, "")
	if status != 503 {
		t.Fatal("open circuit forwarded")
	}
	slow.Store(false)
	time.Sleep(1050 * time.Millisecond)
	status, body, _ := call(t, proxy.Client(), "GET", proxy.URL+"/proxy/v1/read", testKey, "")
	if status != 200 || body != "recovered" || s.Breaker.State(time.Now()) != "closed" {
		t.Fatal("half-open recovery", status, body)
	}
	if s.Engine.Snapshot(time.Now()).Clients[0].ErrorEWMA != 0 {
		t.Fatal("server failure blamed on client")
	}
}
func TestRealCapacityBoundAndRejection(t *testing.T) {
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	s, proxy := setupProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
			_, _ = io.WriteString(w, "ok")
		case <-r.Context().Done():
		}
	}), "")
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); call(t, proxy.Client(), "GET", proxy.URL+"/proxy/v1/read", testKey, "") }()
	}
	for i := 0; i < 4; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("inflight readiness")
		}
	}
	status, _, _ := call(t, proxy.Client(), "GET", proxy.URL+"/proxy/v1/read", testKey, "")
	t.Logf("caught capacity overload: HTTP %d, active leases %d", status, len(s.capacity))
	if status != 503 || len(s.capacity) != 4 {
		t.Fatal("capacity bypass", status, len(s.capacity))
	}
	close(release)
	wg.Wait()
	if len(s.capacity) != 0 {
		t.Fatal("capacity lease leaked")
	}
}
func TestRealRateLimitAndValidatedControl(t *testing.T) {
	s, proxy := setupProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }), "")
	if err := s.Engine.Update(Policy{Mode: "fixed", Algorithm: TokenBucket, Rate: 1, Burst: 1}); err != nil {
		t.Fatal(err)
	}
	status, _, _ := call(t, proxy.Client(), "GET", proxy.URL+"/proxy/v1/read", testKey, "")
	if status != 200 {
		t.Fatal(status)
	}
	status, _, headers := call(t, proxy.Client(), "GET", proxy.URL+"/proxy/v1/read", testKey, "")
	t.Logf("caught per-client excess: HTTP %d, Retry-After %s", status, headers.Get("Retry-After"))
	if status != 429 || headers.Get("Retry-After") == "" {
		t.Fatal("limiter gate missing")
	}
	for _, body := range []string{`{"mode":"fixed","algorithm":"token","rate":0,"burst":1}`, `{"mode":"fixed","algorithm":"token","rate":1,"burst":1,"extra":true}`, `{"mode":"fixed","algorithm":"token","rate":1,"burst":1} {}`} {
		status, _, _ = call(t, proxy.Client(), "PUT", proxy.URL+"/api/policy", "", body)
		if status != 400 {
			t.Fatal("invalid policy accepted", status)
		}
	}
	status, body, _ := call(t, proxy.Client(), "GET", proxy.URL+"/api/state", "", "")
	var state map[string]json.RawMessage
	if status != 200 || json.Unmarshal([]byte(body), &state) != nil {
		t.Fatal("state JSON")
	}
	status, _, _ = call(t, proxy.Client(), "POST", proxy.URL+"/api/campaign", "", "")
	if status != 403 {
		t.Fatal("production load enabled")
	}
}
func TestServerConfigurationRejectsUnsafeOriginsAndSecrets(t *testing.T) {
	for _, upstream := range []string{"file:///tmp/no", "http://user:secret@127.0.0.1", "http://127.0.0.1/path", "http://127.0.0.1/?credential=x"} {
		_, err := NewServer(Config{Policy: Policy{Mode: "fixed", Algorithm: TokenBucket, Rate: 20, Burst: 12}, Keys: map[string]string{"neutral": testKey}, Upstream: upstream, Capacity: 4, LoopbackOnly: true})
		if err == nil {
			t.Fatal("unsafe origin accepted", upstream)
		}
	}
	_, err := NewServer(Config{Policy: Policy{Mode: "fixed", Algorithm: TokenBucket, Rate: 20, Burst: 12}, Keys: map[string]string{"neutral": testKey}, Upstream: "http://127.0.0.1", Capacity: 4, LoopbackOnly: false})
	if err == nil {
		t.Fatal("public listener without admin secret")
	}
}

func TestTruncatedOriginBodyReleasesHalfOpenProbe(t *testing.T) {
	s, proxy := setupProxy(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100")
		_, _ = io.WriteString(w, "short")
	}), "")
	s.Breaker = NewBreaker(1, time.Second)
	past := time.Now().Add(-2 * time.Second)
	permit, _ := s.Breaker.Allow(past)
	s.Breaker.Done(permit, true, past)
	request, _ := http.NewRequest("GET", proxy.URL+"/proxy/v1/read", nil)
	request.Header.Set("Authorization", "Bearer "+testKey)
	response, err := proxy.Client().Do(request)
	if err == nil {
		_, err = io.ReadAll(response.Body)
		_ = response.Body.Close()
	}
	if err == nil {
		t.Fatal("truncated response was reported as complete")
	}
	deadline := time.Now().Add(time.Second)
	for len(s.capacity) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(s.capacity) != 0 || s.Breaker.State(time.Now()) != "open" {
		t.Fatal("aborted stream leaked probe/capacity", len(s.capacity), s.Breaker.State(time.Now()))
	}
	t.Logf("caught truncated body: %v; breaker reopened and capacity released", err)
}
