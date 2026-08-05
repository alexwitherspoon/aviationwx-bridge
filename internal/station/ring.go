package station

import (
	"sync"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/bridgeapi"
)

// MaxOutboundWeatherSamples caps the in-memory weather POST ring (no disk queue).
const MaxOutboundWeatherSamples = 60

// outboundRing holds failed weather POSTs for retry. Newest samples are preferred
// when the ring is full (drop oldest).
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
	return len(r.items)
}

func (r *outboundRing) Push(req bridgeapi.WeatherRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.items) >= MaxOutboundWeatherSamples {
		copy(r.items, r.items[1:])
		r.items = r.items[:len(r.items)-1]
	}
	r.items = append(r.items, req)
}

// PopAll returns and clears queued requests in FIFO order.
func (r *outboundRing) PopAll() []bridgeapi.WeatherRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
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
	combined := make([]bridgeapi.WeatherRequest, 0, len(reqs)+len(r.items))
	combined = append(combined, reqs...)
	combined = append(combined, r.items...)
	if len(combined) > MaxOutboundWeatherSamples {
		combined = combined[len(combined)-MaxOutboundWeatherSamples:]
	}
	r.items = combined
}
