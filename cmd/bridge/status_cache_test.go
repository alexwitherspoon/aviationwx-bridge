package main

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestStatusCache_CoalescesConcurrentRefresh(t *testing.T) {
	var calls atomic.Int32
	cache := newStatusCache(time.Second, func() interface{} {
		calls.Add(1)
		time.Sleep(50 * time.Millisecond)
		return "ok"
	})

	done := make(chan struct{}, 8)
	for i := 0; i < 8; i++ {
		go func() {
			if v := cache.get(); v != "ok" {
				t.Errorf("got %v, want ok", v)
			}
			done <- struct{}{}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	if calls.Load() != 1 {
		t.Fatalf("refresh calls = %d, want 1 (coalesced)", calls.Load())
	}
}

func TestStatusCache_UsesTTL(t *testing.T) {
	var calls atomic.Int32
	cache := newStatusCache(100*time.Millisecond, func() interface{} {
		calls.Add(1)
		return calls.Load()
	})

	_ = cache.get()
	_ = cache.get()
	if calls.Load() != 1 {
		t.Fatalf("expected 1 refresh within TTL, got %d", calls.Load())
	}

	time.Sleep(120 * time.Millisecond)
	_ = cache.get()
	if calls.Load() != 2 {
		t.Fatalf("expected refresh after TTL, got %d", calls.Load())
	}
}
