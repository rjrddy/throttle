package gateway

import (
	"math"
	"math/rand"
	"testing"
	"time"
)

func TestTokenBucketBoundaries(t *testing.T) {
	now := time.Unix(100, 0)
	var l Limiter
	for i := 0; i < 3; i++ {
		ok, _ := l.Allow(now, TokenBucket, 2, 3)
		if !ok {
			t.Fatal("initial burst denied")
		}
	}
	if ok, retry := l.Allow(now, TokenBucket, 2, 3); ok || retry != 500*time.Millisecond {
		t.Fatalf("expected precise retry; %v %v", ok, retry)
	}
	if ok, _ := l.Allow(now.Add(499*time.Millisecond), TokenBucket, 2, 3); ok {
		t.Fatal("early refill")
	}
	if ok, _ := l.Allow(now.Add(500*time.Millisecond), TokenBucket, 2, 3); !ok {
		t.Fatal("refill boundary")
	}
	// Rate changes refill elapsed time at the OLD rate, not retroactively at the new rate.
	var changed Limiter
	changed.Allow(now, TokenBucket, 1, 1)
	if ok, _ := changed.Allow(now.Add(500*time.Millisecond), TokenBucket, 100, 1); ok {
		t.Fatal("rate change minted retroactive credit")
	}
	if ok, _ := changed.Allow(now.Add(505*time.Millisecond), TokenBucket, 100, 1); !ok {
		t.Fatal("new rate failed to accrue credit")
	}
}
func TestSlidingWindowBoundaryAndReducedLimit(t *testing.T) {
	now := time.Unix(100, 0)
	var l Limiter
	for i := 0; i < 10; i++ {
		if ok, _ := l.Allow(now.Add(time.Duration(i)*time.Millisecond), SlidingWindow, 10, 50); !ok {
			t.Fatal("unexpected denial")
		}
	}
	if ok, retry := l.Allow(now.Add(20*time.Millisecond), SlidingWindow, 2, 50); ok || retry != 988*time.Millisecond {
		t.Fatalf("reduced limit must wait for enough expirations: %v %v", ok, retry)
	}
	if ok, _ := l.Allow(now.Add(1008*time.Millisecond), SlidingWindow, 2, 50); !ok {
		t.Fatal("inclusive expiration boundary")
	}
}
func TestGCRABurstAndDebt(t *testing.T) {
	now := time.Unix(100, 0)
	var l Limiter
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow(now, GCRA, 2, 3); !ok {
			t.Fatal("GCRA burst")
		}
	}
	if ok, retry := l.Allow(now, GCRA, 2, 3); ok || retry != 500*time.Millisecond {
		t.Fatal("GCRA retry", ok, retry)
	}
	if ok, _ := l.Allow(now.Add(499*time.Millisecond), GCRA, 2, 3); ok {
		t.Fatal("early GCRA permit")
	}
	if ok, _ := l.Allow(now.Add(500*time.Millisecond), GCRA, 2, 3); !ok {
		t.Fatal("GCRA boundary")
	}
}
func TestRandomizedAdmissionEnvelope(t *testing.T) {
	for _, algorithm := range []Algorithm{TokenBucket, SlidingWindow, GCRA} {
		t.Run(string(algorithm), func(t *testing.T) {
			rng := rand.New(rand.NewSource(52017))
			start := time.Unix(100, 0)
			now := start
			var l Limiter
			accepted := []time.Time{}
			for i := 0; i < 20000; i++ {
				now = now.Add(time.Duration(rng.Intn(4000)) * time.Microsecond)
				ok, retry := l.Allow(now, algorithm, 20, 12)
				if !ok {
					if retry <= 0 {
						t.Fatal("nonpositive retry")
					}
					continue
				}
				accepted = append(accepted, now)
				if algorithm == SlidingWindow {
					count := 0
					for j := len(accepted) - 1; j >= 0 && accepted[j].After(now.Add(-time.Second)); j-- {
						count++
					}
					if count > 20 {
						t.Fatal("rolling window exceeded", count)
					}
				} else {
					// Check every accepted suffix, not just total from the test start.
					if !suffixEnvelope(accepted, 20, 12) {
						t.Fatalf("burst+rate envelope exceeded: %v", algorithm)
					}
				}
			}
		})
	}
}

// Check the same finite envelope criterion used above against an impossible admission.
func TestLimiterRejectNegativeControl(t *testing.T) {
	now := time.Unix(100, 0)
	broken := make([]time.Time, 13)
	for i := range broken {
		broken[i] = now
	}
	if suffixEnvelope(broken, 20, 12) {
		t.Fatal("always-admit negative control escaped the envelope checker")
	}
}
func suffixEnvelope(accepted []time.Time, rate float64, burst int) bool {
	if len(accepted) == 0 {
		return true
	}
	now := accepted[len(accepted)-1]
	for j := len(accepted) - 1; j >= 0 && accepted[j].After(now.Add(-time.Second)); j-- {
		bound := float64(burst) + rate*now.Sub(accepted[j]).Seconds()
		if float64(len(accepted)-j) > math.Floor(bound+1e-6) {
			return false
		}
	}
	return true
}
