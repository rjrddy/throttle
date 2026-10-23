package gateway

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestBreakerFencesStaleCompletionsAndSingleProbe(t *testing.T) {
	b := NewBreaker(2, time.Second)
	now := time.Unix(100, 0)
	stale, _ := b.Allow(now)
	one, _ := b.Allow(now)
	two, _ := b.Allow(now)
	b.Done(one, true, now)
	b.Done(two, true, now)
	if _, ok := b.Allow(now); ok {
		t.Fatal("open circuit admitted")
	}
	b.Done(stale, false, now)
	if b.State(now) != "open" {
		t.Fatal("stale success closed breaker")
	}
	later := now.Add(time.Second)
	var permits atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, ok := b.Allow(later); ok {
				permits.Add(1)
			}
		}()
	}
	wg.Wait()
	if permits.Load() != 1 {
		t.Fatal("multiple half-open probes", permits.Load())
	}
	// Finish the sole probe using its epoch; an obsolete completion still cannot close it.
	b.Done(stale, false, later)
	if b.State(later) != "half-open" {
		t.Fatal("stale completion interfered with probe")
	}
	b.mu.Lock()
	probe := Permit{generation: b.generation, probe: true}
	b.mu.Unlock()
	b.Done(probe, false, later)
	if b.State(later) != "closed" {
		t.Fatal("successful probe did not recover")
	}
}

func TestCancelledHalfOpenProbeCanBeRetried(t *testing.T) {
	b := NewBreaker(1, time.Second)
	past := time.Unix(100, 0)
	permit, _ := b.Allow(past)
	b.Done(permit, true, past)
	now := past.Add(2 * time.Second)
	probe, ok := b.Allow(now)
	if !ok {
		t.Fatal("probe blocked")
	}
	b.Cancel(probe)
	if _, ok := b.Allow(now); !ok {
		t.Fatal("cancelled probe wedged breaker")
	}
}
