package gateway

import (
	"sync"
	"time"
)

type Permit struct {
	generation uint64
	probe      bool
}
type Breaker struct {
	mu         sync.Mutex
	failures   int
	openUntil  time.Time
	probe      bool
	generation uint64
	threshold  int
	cooldown   time.Duration
}

func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	return &Breaker{threshold: threshold, cooldown: cooldown}
}
func (b *Breaker) Allow(now time.Time) (Permit, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openUntil.IsZero() {
		return Permit{generation: b.generation}, true
	}
	if now.Before(b.openUntil) || b.probe {
		return Permit{}, false
	}
	b.probe = true
	return Permit{generation: b.generation, probe: true}, true
}
func (b *Breaker) Done(p Permit, failed bool, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Epoch fencing: completions from requests admitted before opening cannot change a later state.
	if p.generation != b.generation {
		return
	}
	if !failed {
		b.failures = 0
		if p.probe {
			b.openUntil = time.Time{}
			b.probe = false
			b.generation++
		}
		return
	}
	b.failures++
	if p.probe || b.failures >= b.threshold {
		b.openUntil = now.Add(b.cooldown)
		b.probe = false
		b.generation++
	}
}
func (b *Breaker) State(now time.Time) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openUntil.IsZero() {
		return "closed"
	}
	if b.probe {
		return "half-open"
	}
	if now.Before(b.openUntil) {
		return "open"
	}
	return "probe-ready"
}

// Cancel releases only a matching half-open probe, without blaming the origin.
func (b *Breaker) Cancel(p Permit) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if p.generation == b.generation && p.probe {
		b.probe = false
	}
}
