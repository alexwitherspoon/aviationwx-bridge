package station

import (
	"sync"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/bridgeapi"
)

// OutboundWeatherMaxAge is how long failed weather POSTs may be retained for
// catch-up after a transient API/internet outage. Evaluation uses each
// request's ObservedAt (station time), not receive time.
const OutboundWeatherMaxAge = 10 * time.Minute

// OutboundWeatherSoftMax caps ring length if observation clocks stall (Pi
// memory guard). Prefer age-based prune; this is a backstop only.
// Sized for ~2 stations at the global ≤1 Hz emit ceiling over MaxAge.
const OutboundWeatherSoftMax = int(OutboundWeatherMaxAge/GlobalMinPollInterval) * 2

// outboundRing holds failed weather POSTs for retry. Retention is by
// ObservedAt within OutboundWeatherMaxAge (drop older). When SoftMax is hit,
// drop oldest remaining. No disk queue.
type outboundRing struct {
	mu    sync.Mutex
	items []bridgeapi.WeatherRequest
}

func newOutboundRing() *outboundRing {
	return &outboundRing{items: make([]bridgeapi.WeatherRequest, 0, 16)}
}

func (r *outboundRing) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(time.Now().UTC())
	return len(r.items)
}

func (r *outboundRing) Push(req bridgeapi.WeatherRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	r.pruneLocked(now)
	if req.ObservedAt.IsZero() || req.ObservedAt.Before(now.Add(-OutboundWeatherMaxAge)) {
		return
	}
	r.items = append(r.items, req)
	r.enforceSoftMaxLocked()
}

// PopAll returns and clears queued requests in FIFO order (oldest ObservedAt
// first among remaining). Prunes by age before returning.
func (r *outboundRing) PopAll() []bridgeapi.WeatherRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(time.Now().UTC())
	out := r.items
	r.items = make([]bridgeapi.WeatherRequest, 0, 16)
	return out
}

// PushFront restores items that failed to post (oldest first).
func (r *outboundRing) PushFront(reqs ...bridgeapi.WeatherRequest) {
	if len(reqs) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	combined := make([]bridgeapi.WeatherRequest, 0, len(reqs)+len(r.items))
	combined = append(combined, reqs...)
	combined = append(combined, r.items...)
	r.items = combined
	r.pruneLocked(now)
	r.enforceSoftMaxLocked()
}

func (r *outboundRing) pruneLocked(now time.Time) {
	cutoff := now.Add(-OutboundWeatherMaxAge)
	dst := r.items[:0]
	for _, req := range r.items {
		if req.ObservedAt.IsZero() {
			continue
		}
		if req.ObservedAt.Before(cutoff) {
			continue
		}
		dst = append(dst, req)
	}
	r.items = dst
}

func (r *outboundRing) enforceSoftMaxLocked() {
	if len(r.items) <= OutboundWeatherSoftMax {
		return
	}
	drop := len(r.items) - OutboundWeatherSoftMax
	r.items = append([]bridgeapi.WeatherRequest(nil), r.items[drop:]...)
}
