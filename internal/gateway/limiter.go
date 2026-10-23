package gateway

import (
	"math"
	"time"
)

type Algorithm string

const (
	TokenBucket   Algorithm = "token"
	SlidingWindow Algorithm = "sliding"
	GCRA          Algorithm = "gcra"
)

func (a Algorithm) Valid() bool { return a == TokenBucket || a == SlidingWindow || a == GCRA }

type Limiter struct {
	initialized  bool
	last         time.Time
	previousRate float64
	tokens       float64
	tat          time.Time
	log          []time.Time
}

// Allow is called under the owning client/gateway mutex. Rate is per second.
// Token/GCRA burst is the maximum immediate batch; sliding has ceil(rate)/second.
func (l *Limiter) Allow(now time.Time, algorithm Algorithm, rate float64, burst int) (bool, time.Duration) {
	if now.Before(l.last) {
		now = l.last
	}
	switch algorithm {
	case TokenBucket:
		if !l.initialized {
			l.tokens = float64(burst)
			l.last = now
			l.previousRate = rate
		}
		l.tokens = math.Min(float64(burst), l.tokens+now.Sub(l.last).Seconds()*l.previousRate)
		l.last, l.previousRate, l.initialized = now, rate, true
		if l.tokens >= 1 {
			l.tokens--
			return true, 0
		}
		return false, time.Duration(math.Ceil((1 - l.tokens) / rate * float64(time.Second)))
	case SlidingWindow:
		l.last = now
		cutoff := now.Add(-time.Second)
		expired := 0
		for expired < len(l.log) && !l.log[expired].After(cutoff) {
			expired++
		}
		if expired > 0 {
			copy(l.log, l.log[expired:])
			l.log = l.log[:len(l.log)-expired]
		}
		limit := int(math.Ceil(rate))
		if len(l.log) >= limit {
			return false, l.log[len(l.log)-limit].Add(time.Second).Sub(now)
		}
		l.log = append(l.log, now)
		return true, 0
	case GCRA:
		l.last = now
		interval := time.Duration(math.Ceil(float64(time.Second) / rate))
		if l.tat.IsZero() {
			l.tat = now
		}
		earliest := l.tat.Add(-time.Duration(burst-1) * interval)
		if now.Before(earliest) {
			return false, earliest.Sub(now)
		}
		if l.tat.Before(now) {
			l.tat = now
		}
		l.tat = l.tat.Add(interval)
		return true, 0
	default:
		return false, time.Second
	}
}
