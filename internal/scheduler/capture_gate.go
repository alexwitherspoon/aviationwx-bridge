package scheduler

import "sync"

// maxConcurrentCapturesCap is the upper bound for concurrent captures (matches web UI).
const maxConcurrentCapturesCap = 10

// CaptureGate limits how many camera capture jobs run at once across all workers.
// Each camera may queue at most one deferred capture (pending) when the gate is full;
// when a slot frees, pending cameras are woken in deterministic (sorted ID) order.
type CaptureGate struct {
	mu    sync.Mutex
	limit int
	inUse int
}

// NewCaptureGate creates a gate allowing at most limit concurrent captures (minimum 1).
func NewCaptureGate(limit int) *CaptureGate {
	if limit < 1 {
		limit = 1
	}
	return &CaptureGate{limit: limit}
}

// TryAcquire attempts to take a slot without blocking.
func (g *CaptureGate) TryAcquire() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inUse < g.limit {
		g.inUse++
		return true
	}
	return false
}

// Release returns one slot after a capture finishes.
func (g *CaptureGate) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.inUse > 0 {
		g.inUse--
	}
}

// SetLimit updates the maximum concurrent captures (minimum 1). If the new limit is
// below the number of captures currently in flight, TryAcquire blocks new work until
// Release brings inUse below the new limit; in-flight captures are not interrupted.
func (g *CaptureGate) SetLimit(n int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if n < 1 {
		n = 1
	}
	g.limit = n
}
