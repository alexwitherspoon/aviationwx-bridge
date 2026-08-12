package station

import (
	"fmt"
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

func TestOutboundRingDropsByObservedAtAge(t *testing.T) {
	r := newOutboundRing()
	now := time.Now().UTC()
	r.Push(bridgeapi.WeatherRequest{
		SourceID:   "old",
		ObservedAt: now.Add(-OutboundWeatherMaxAge - time.Minute),
	})
	r.Push(bridgeapi.WeatherRequest{
		SourceID:   "fresh",
		ObservedAt: now.Add(-time.Minute),
	})
	r.Push(bridgeapi.WeatherRequest{
		SourceID:   "zero",
		ObservedAt: time.Time{},
	})
	r.Push(bridgeapi.WeatherRequest{
		SourceID:   "future",
		ObservedAt: now.Add(outboundFutureSkew + time.Minute),
	})
	if r.Len() != 1 {
		t.Fatalf("len = %d, want 1 (fresh only)", r.Len())
	}
	got := r.PopReady(map[string]time.Time{}, now, GlobalMinPollInterval)
	if len(got) != 1 || got[0].SourceID != "fresh" {
		t.Fatalf("pop = %+v", got)
	}
	if r.Len() != 0 {
		t.Fatalf("ring len after pop = %d, want 0", r.Len())
	}
}

func TestOutboundRingSoftMaxDropsOldest(t *testing.T) {
	r := newOutboundRing()
	now := time.Now().UTC()
	for i := 0; i < OutboundWeatherSoftMax+5; i++ {
		// All within the age window so SoftMax (not age) decides drops.
		r.Push(bridgeapi.WeatherRequest{
			SourceID:   string(rune('a' + (i % 26))),
			ObservedAt: now.Add(-time.Duration(i) * time.Millisecond),
			BridgeID:   fmt.Sprintf("%d", i),
		})
	}
	if r.Len() != OutboundWeatherSoftMax {
		t.Fatalf("len = %d, want soft max %d", r.Len(), OutboundWeatherSoftMax)
	}
	ready := r.PopReady(map[string]time.Time{}, now, GlobalMinPollInterval)
	if len(ready) == 0 {
		t.Fatal("expected SoftMax to keep newest-tail FIFO head for PopReady")
	}
	// SoftMax drops from the front; oldest pushes were indices 0..4 relative to overflow.
	if ready[0].BridgeID == "0" {
		t.Fatalf("SoftMax should have dropped oldest BridgeID=0, still present")
	}
}

func TestOutboundRingRetainSources(t *testing.T) {
	r := newOutboundRing()
	now := time.Now().UTC()
	r.Push(bridgeapi.WeatherRequest{SourceID: "keep", ObservedAt: now})
	r.Push(bridgeapi.WeatherRequest{SourceID: "drop", ObservedAt: now})
	r.RetainSources(map[string]struct{}{"keep": {}})
	if r.Len() != 1 {
		t.Fatalf("len = %d, want 1", r.Len())
	}
	got := r.PopReady(map[string]time.Time{}, now, GlobalMinPollInterval)
	if len(got) != 1 || got[0].SourceID != "keep" {
		t.Fatalf("got %+v", got)
	}
}

func TestOutboundRingPushFrontMidFlush(t *testing.T) {
	r := newOutboundRing()
	now := time.Now().UTC()
	r.Push(bridgeapi.WeatherRequest{SourceID: "a", ObservedAt: now.Add(-2 * time.Second), BridgeID: "1"})
	r.Push(bridgeapi.WeatherRequest{SourceID: "b", ObservedAt: now.Add(-time.Second), BridgeID: "2"})
	ready := r.PopReady(map[string]time.Time{}, now, GlobalMinPollInterval)
	if len(ready) != 2 {
		t.Fatalf("ready = %d, want 2", len(ready))
	}
	r.PushFront(ready...)
	again := r.PopReady(map[string]time.Time{}, now, GlobalMinPollInterval)
	if len(again) != 2 || again[0].BridgeID != "1" || again[1].BridgeID != "2" {
		t.Fatalf("PushFront order broken: %+v", again)
	}
}
