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

// outboundFutureSkew matches aviationwx.org BRIDGE_WEATHER_FUTURE_SKEW_SECONDS.
// Far-future stamps are dropped - core would reject them on POST.
const outboundFutureSkew = 60 * time.Second

// OutboundWeatherSoftMax caps ring length if observation clocks stall (Pi
// memory guard). Prefer age-based prune; this is a backstop only.
// Sized for ~2 stations at the global ≤1 Hz emit ceiling over MaxAge.
const OutboundWeatherSoftMax = int(OutboundWeatherMaxAge/GlobalMinPollInterval) * 2

// outboundRing holds failed weather POSTs for retry. Retention is by
// ObservedAt within OutboundWeatherMaxAge (drop older/far-future). SoftMax
// drops from the front of the FIFO. In-memory only - no disk weather queue.
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
	if !retainOutbound(req, now) {
		return
	}
	r.items = append(r.items, req)
	r.enforceSoftMaxLocked()
}

// PopReady returns at most one due catch-up request per SourceID.
// due is last catch-up POST attempt time per SourceID (read-only here),
// including failed attempts so a down API is not retried within minInterval.
// Remaining items stay queued in FIFO order.
func (r *outboundRing) PopReady(due map[string]time.Time, now time.Time, minInterval time.Duration) []bridgeapi.WeatherRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(now)
	if len(r.items) == 0 {
		return nil
	}
	taken := make(map[string]struct{})
	var ready []bridgeapi.WeatherRequest
	kept := r.items[:0]
	for _, req := range r.items {
		id := req.SourceID
		if _, already := taken[id]; already {
			kept = append(kept, req)
			continue
		}
		if last, ok := due[id]; ok && now.Sub(last) < minInterval {
			kept = append(kept, req)
			continue
		}
		taken[id] = struct{}{}
		ready = append(ready, req)
	}
	clearRingTail(r.items, len(kept))
	r.items = kept
	return ready
}

// PushFront restores items that failed mid-flush to the front of the FIFO.
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

func retainOutbound(req bridgeapi.WeatherRequest, now time.Time) bool {
	if req.ObservedAt.IsZero() {
		return false
	}
	if req.ObservedAt.Before(now.Add(-OutboundWeatherMaxAge)) {
		return false
	}
	if req.ObservedAt.After(now.Add(outboundFutureSkew)) {
		return false
	}
	return true
}

func (r *outboundRing) pruneLocked(now time.Time) {
	dst := r.items[:0]
	for _, req := range r.items {
		if !retainOutbound(req, now) {
			continue
		}
		dst = append(dst, req)
	}
	clearRingTail(r.items, len(dst))
	r.items = dst
}

// clearRingTail zeros unused slots so dropped ProviderMeta maps can be GC'd.
func clearRingTail(items []bridgeapi.WeatherRequest, keep int) {
	var zero bridgeapi.WeatherRequest
	for i := keep; i < len(items); i++ {
		items[i] = zero
	}
}

func (r *outboundRing) enforceSoftMaxLocked() {
	if len(r.items) <= OutboundWeatherSoftMax {
		return
	}
	drop := len(r.items) - OutboundWeatherSoftMax
	n := len(r.items)
	copy(r.items, r.items[drop:])
	keep := n - drop
	clearRingTail(r.items, keep)
	r.items = r.items[:keep]
}
