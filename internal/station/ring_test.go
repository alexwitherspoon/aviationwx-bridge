package station

import (
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/bridgeapi"
)

func TestPopReadyOnePerSourcePerInterval(t *testing.T) {
	r := newOutboundRing()
	now := time.Now().UTC()
	for i := 0; i < 5; i++ {
		r.Push(bridgeapi.WeatherRequest{
			SourceID:   "a",
			ObservedAt: now.Add(-time.Duration(i) * time.Second),
		})
	}
	due := map[string]time.Time{}

	ready := r.PopReady(due, now, GlobalMinPollInterval)
	if len(ready) != 1 || ready[0].SourceID != "a" {
		t.Fatalf("first pop = %+v, want one source a", ready)
	}
	if r.Len() != 4 {
		t.Fatalf("remaining = %d, want 4", r.Len())
	}

	// Same interval: not due again until caller advances due.
	due["a"] = now
	ready = r.PopReady(due, now.Add(100*time.Millisecond), GlobalMinPollInterval)
	if len(ready) != 0 {
		t.Fatalf("within interval pop = %+v, want empty", ready)
	}

	dueReady := now.Add(GlobalMinPollInterval)
	ready = r.PopReady(due, dueReady, GlobalMinPollInterval)
	if len(ready) != 1 {
		t.Fatalf("after interval pop = %+v, want one", ready)
	}
	if r.Len() != 3 {
		t.Fatalf("remaining = %d, want 3", r.Len())
	}
}

func TestPopReadyOneEachSourceSameTick(t *testing.T) {
	r := newOutboundRing()
	now := time.Now().UTC()
	r.Push(bridgeapi.WeatherRequest{SourceID: "a", ObservedAt: now.Add(-3 * time.Second)})
	r.Push(bridgeapi.WeatherRequest{SourceID: "a", ObservedAt: now.Add(-2 * time.Second)})
	r.Push(bridgeapi.WeatherRequest{SourceID: "b", ObservedAt: now.Add(-1 * time.Second)})
	r.Push(bridgeapi.WeatherRequest{SourceID: "b", ObservedAt: now})

	ready := r.PopReady(map[string]time.Time{}, now, GlobalMinPollInterval)
	if len(ready) != 2 {
		t.Fatalf("ready len = %d, want 2 (one per source)", len(ready))
	}
	seen := map[string]int{}
	for _, req := range ready {
		seen[req.SourceID]++
	}
	if seen["a"] != 1 || seen["b"] != 1 {
		t.Fatalf("ready by source = %v", seen)
	}
	if r.Len() != 2 {
		t.Fatalf("remaining = %d, want 2", r.Len())
	}
}

func TestPopReadyPreservesFIFOWithinSource(t *testing.T) {
	r := newOutboundRing()
	now := time.Now().UTC()
	r.Push(bridgeapi.WeatherRequest{SourceID: "a", ObservedAt: now.Add(-2 * time.Second), BridgeID: "first"})
	r.Push(bridgeapi.WeatherRequest{SourceID: "a", ObservedAt: now.Add(-1 * time.Second), BridgeID: "second"})

	ready := r.PopReady(map[string]time.Time{}, now, GlobalMinPollInterval)
	if len(ready) != 1 || ready[0].BridgeID != "first" {
		t.Fatalf("want oldest FIFO first, got %+v", ready)
	}
}
