package campaign

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rjrddy/throttle/internal/gateway"
)

type Offer struct {
	At              time.Duration
	ID, Path, Class string
}
type Receipt struct {
	ID          string  `json:"client"`
	Class       string  `json:"class"`
	Endpoint    string  `json:"path"`
	ScheduledMS float64 `json:"scheduled_ms"`
	StartedMS   float64 `json:"started_ms"`
	LatencyMS   float64 `json:"latency_ms"`
	Status      int     `json:"status"`
	Outcome     string  `json:"outcome"`
}
type ClassResult struct {
	Offered     int     `json:"offered"`
	Success     int     `json:"success"`
	Limited     int     `json:"limited"`
	Capacity    int     `json:"capacity"`
	Other       int     `json:"other"`
	SuccessRate float64 `json:"success_rate"`
	P99MS       float64 `json:"p99_ms"`
}
type Result struct {
	Mode               string                 `json:"mode"`
	Algorithm          gateway.Algorithm      `json:"algorithm"`
	Scenario           string                 `json:"scenario"`
	Trial              int                    `json:"trial"`
	Seed               int64                  `json:"seed"`
	DurationMS         int64                  `json:"duration_ms"`
	ElapsedMS          int64                  `json:"elapsed_ms"`
	Classes            map[string]ClassResult `json:"classes"`
	Receipts           []Receipt              `json:"receipts"`
	OriginRequests     uint64                 `json:"origin_requests"`
	PeakOriginInflight int64                  `json:"peak_origin_inflight"`
	FinalState         gateway.Snapshot       `json:"final_state"`
}

func Plan(scenario string, duration time.Duration, seed int64) ([]Offer, error) {
	if scenario != "mixed" && scenario != "healthy-burst" && scenario != "unclassified" {
		return nil, fmt.Errorf("unknown scenario")
	}
	rng := rand.New(rand.NewSource(seed))
	offers := []Offer{}
	ids := []string{}
	for id := range gateway.DemoKeys() {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		legitimate := strings.HasPrefix(id, "legit-")
		if scenario == "unclassified" {
			if id != "legit-coral" {
				continue
			}
			for at := time.Duration(0); at < duration; at += 40 * time.Millisecond {
				offers = append(offers, Offer{at, id, "/v1/preview", "legitimate"})
			}
			continue
		}
		if !legitimate && scenario == "healthy-burst" {
			continue
		}
		phase := time.Duration(rng.Intn(180)) * time.Millisecond
		if legitimate {
			interval, batch := 300*time.Millisecond, 3
			if scenario == "healthy-burst" {
				interval, batch = 600*time.Millisecond, 5
			}
			for at := phase; at < duration; at += interval {
				for n := 0; n < batch; n++ {
					when := at + time.Duration(n)*3*time.Millisecond
					if when < duration {
						path := "/v1/read"
						if n%2 == 1 {
							path = "/v1/search"
						}
						offers = append(offers, Offer{when, id, path, "legitimate"})
					}
				}
			}
		} else {
			for at := phase; at < duration; at += 10 * time.Millisecond {
				offers = append(offers, Offer{at, id, "/v1/missing", "abuse"})
			}
		}
	}
	sort.SliceStable(offers, func(i, j int) bool { return offers[i].At < offers[j].At })
	return offers, nil
}
func percentile(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sort.Float64s(values)
	return values[int(math.Ceil(p*float64(len(values))))-1]
}
func RunHTTP(ctx context.Context, base string, scenario string, duration time.Duration, seed int64) (Result, error) {
	plan, err := Plan(scenario, duration, seed)
	if err != nil {
		return Result{}, err
	}
	result := Result{Scenario: scenario, Seed: seed, DurationMS: duration.Milliseconds(), Classes: map[string]ClassResult{}, Receipts: make([]Receipt, len(plan))}
	keys := gateway.DemoKeys()
	transport := &http.Transport{MaxIdleConns: 128, MaxIdleConnsPerHost: 128, MaxConnsPerHost: 128, IdleConnTimeout: 5 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	start := time.Now().Add(30 * time.Millisecond)
	slots := make(chan struct{}, 64)
	var wg sync.WaitGroup
	for i, offer := range plan {
		wait := time.Until(start.Add(offer.At))
		if wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				timer.Stop()
				wg.Wait()
				return result, ctx.Err()
			case <-timer.C:
			}
		}
		receipt := Receipt{ID: offer.ID, Class: offer.Class, Endpoint: offer.Path, ScheduledMS: float64(offer.At) / float64(time.Millisecond)}
		select {
		case slots <- struct{}{}:
		default:
			receipt.Outcome = "load-client-saturated"
			result.Receipts[i] = receipt
			continue
		}
		wg.Add(1)
		go func(i int, offer Offer, receipt Receipt) {
			defer wg.Done()
			defer func() { <-slots }()
			began := time.Now()
			receipt.StartedMS = float64(began.Sub(start)) / float64(time.Millisecond)
			request, err := http.NewRequestWithContext(ctx, "GET", base+"/proxy"+offer.Path, nil)
			if err != nil {
				receipt.Outcome = "client-error"
				result.Receipts[i] = receipt
				return
			}
			request.Header.Set("Authorization", "Bearer "+keys[offer.ID])
			response, err := client.Do(request)
			receipt.LatencyMS = float64(time.Since(began)) / float64(time.Millisecond)
			if err != nil {
				receipt.Outcome = "transport-error"
				result.Receipts[i] = receipt
				return
			}
			_, bodyErr := io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			receipt.LatencyMS = float64(time.Since(began)) / float64(time.Millisecond)
			if bodyErr != nil {
				receipt.Status = response.StatusCode
				receipt.Outcome = "body-error"
				result.Receipts[i] = receipt
				return
			}
			receipt.Status = response.StatusCode
			switch response.StatusCode {
			case 200:
				receipt.Outcome = "success"
			case 429:
				receipt.Outcome = "limited"
			case 503:
				receipt.Outcome = "capacity-or-circuit"
			default:
				receipt.Outcome = "origin-response"
			}
			result.Receipts[i] = receipt
		}(i, offer, receipt)
	}
	wg.Wait()
	result.ElapsedMS = time.Since(start).Milliseconds()
	for _, receipt := range result.Receipts {
		c := result.Classes[receipt.Class]
		c.Offered++
		switch {
		case receipt.Outcome == "body-error":
			c.Other++
		case receipt.Status == 200:
			c.Success++
		case receipt.Status == 429:
			c.Limited++
		case receipt.Status == 503:
			c.Capacity++
		default:
			c.Other++
		}
		result.Classes[receipt.Class] = c
	}
	for class, c := range result.Classes {
		values := []float64{}
		for _, receipt := range result.Receipts {
			if receipt.Class == class {
				values = append(values, receipt.LatencyMS)
			}
		}
		c.SuccessRate = float64(c.Success) / float64(c.Offered)
		c.P99MS = percentile(values, .99)
		result.Classes[class] = c
	}
	return result, ctx.Err()
}
func Isolated(ctx context.Context, mode string, algorithm gateway.Algorithm, scenario string, duration time.Duration, seed int64) (Result, error) {
	origin := &gateway.DemoOrigin{}
	backend := httptest.NewServer(origin)
	defer backend.Close()
	s, err := gateway.NewServer(gateway.Config{Policy: gateway.Policy{Mode: mode, Algorithm: algorithm, Rate: 20, Burst: 12}, Keys: gateway.DemoKeys(), Upstream: backend.URL, Capacity: 4, LoopbackOnly: true})
	if err != nil {
		return Result{}, err
	}
	defer s.Close()
	proxy := httptest.NewServer(s)
	defer proxy.Close()
	result, err := RunHTTP(ctx, proxy.URL, scenario, duration, seed)
	result.Mode = mode
	result.Algorithm = algorithm
	result.FinalState = s.Engine.Snapshot(time.Now())
	result.OriginRequests, result.PeakOriginInflight = origin.Counts()
	for _, receipt := range result.Receipts {
		if receipt.Status == 0 || receipt.Outcome == "body-error" {
			return result, fmt.Errorf("load generator invalidated trial: %s", receipt.Outcome)
		}
	}
	if result.PeakOriginInflight > 4 {
		return result, fmt.Errorf("capacity invariant violated")
	}
	return result, err
}
func Write(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
