package scheduler

import (
	"sync"
	"testing"
)

func TestCaptureGate_TryAcquireRelease(t *testing.T) {
	g := NewCaptureGate(2)
	if !g.TryAcquire() {
		t.Fatal("want first acquire")
	}
	if !g.TryAcquire() {
		t.Fatal("want second acquire")
	}
	if g.TryAcquire() {
		t.Fatal("third acquire should fail")
	}
	g.Release()
	if !g.TryAcquire() {
		t.Fatal("want acquire after release")
	}
	g.Release()
	g.Release()
}

func TestCaptureGate_SetLimitShrinkRespectsInUse(t *testing.T) {
	g := NewCaptureGate(3)
	if !g.TryAcquire() {
		t.Fatal("want first acquire")
	}
	if !g.TryAcquire() {
		t.Fatal("want second acquire")
	}
	g.SetLimit(1)
	// inUse=2, limit is clamped to >= inUse so no third slot
	if g.TryAcquire() {
		t.Fatal("should not acquire third slot while two in flight")
	}
	g.Release()
	g.Release()
	if !g.TryAcquire() {
		t.Fatal("should acquire after releases")
	}
	g.Release()
}

func TestCaptureGate_ConcurrentAcquire(t *testing.T) {
	g := NewCaptureGate(5)
	var wg sync.WaitGroup
	ok := 0
	var mu sync.Mutex
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if g.TryAcquire() {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if ok != 5 {
		t.Fatalf("want 5 acquires, got %d", ok)
	}
}
