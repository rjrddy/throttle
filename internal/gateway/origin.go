package gateway

import (
	"net/http"
	"sync/atomic"
	"time"
)

type DemoOrigin struct {
	mode     atomic.Int32
	requests atomic.Uint64
	inflight atomic.Int64
	peak     atomic.Int64
}

func (o *DemoOrigin) SetMode(mode string) bool {
	switch mode {
	case "healthy":
		o.mode.Store(0)
	case "fail":
		o.mode.Store(1)
	case "slow":
		o.mode.Store(2)
	default:
		return false
	}
	return true
}
func (o *DemoOrigin) Mode() string {
	switch o.mode.Load() {
	case 1:
		return "fail"
	case 2:
		return "slow"
	default:
		return "healthy"
	}
}
func (o *DemoOrigin) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.requests.Add(1)
	active := o.inflight.Add(1)
	defer o.inflight.Add(-1)
	for peak := o.peak.Load(); active > peak; peak = o.peak.Load() {
		if o.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	delay := 60 * time.Millisecond
	if o.mode.Load() == 2 {
		delay = 500 * time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-r.Context().Done():
		return
	case <-timer.C:
	}
	if o.mode.Load() == 1 {
		http.Error(w, "seeded origin failure", 503)
		return
	}
	if r.URL.Path != "/v1/read" && r.URL.Path != "/v1/search" && r.URL.Path != "/v1/preview" {
		http.Error(w, "synthetic endpoint not found", 404)
		return
	}
	sendJSON(w, 200, map[string]string{"source": "synthetic-origin", "result": "ok"})
}
func (o *DemoOrigin) Counts() (uint64, int64) { return o.requests.Load(), o.peak.Load() }
