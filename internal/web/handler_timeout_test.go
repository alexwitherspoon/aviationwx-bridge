package web

import (
	"testing"
	"time"
)

func TestRunWithTimeout_Completes(t *testing.T) {
	v, ok := runWithTimeout(time.Second, func() interface{} {
		return 42
	})
	if !ok || v != 42 {
		t.Fatalf("got (%v, %v), want (42, true)", v, ok)
	}
}

func TestRunWithTimeout_TimesOut(t *testing.T) {
	_, ok := runWithTimeout(20*time.Millisecond, func() interface{} {
		time.Sleep(200 * time.Millisecond)
		return nil
	})
	if ok {
		t.Fatal("expected timeout")
	}
}

func TestRunReadinessWithTimeout_Completes(t *testing.T) {
	ok, reason, completed := runReadinessWithTimeout(time.Second, func() (bool, string) {
		return true, ""
	})
	if !completed || !ok || reason != "" {
		t.Fatalf("got ok=%v reason=%q completed=%v", ok, reason, completed)
	}
}

func TestRunWithTimeout_RecoversPanic(t *testing.T) {
	start := time.Now()
	_, ok := runWithTimeout(time.Second, func() interface{} {
		panic("boom")
	})
	if ok {
		t.Fatal("expected failure after panic")
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatalf("panic path took %v, want immediate return", time.Since(start))
	}
}

func TestRunReadinessWithTimeout_RecoversPanic(t *testing.T) {
	start := time.Now()
	ok, reason, completed := runReadinessWithTimeout(time.Second, func() (bool, string) {
		panic("boom")
	})
	if !completed || ok {
		t.Fatalf("got ok=%v completed=%v after panic", ok, completed)
	}
	if reason != "readiness check failed" {
		t.Fatalf("reason=%q, want readiness check failed", reason)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatalf("panic path took %v, want immediate return", time.Since(start))
	}
}

func TestRunReadinessWithTimeout_TimesOut(t *testing.T) {
	ok, reason, completed := runReadinessWithTimeout(20*time.Millisecond, func() (bool, string) {
		time.Sleep(200 * time.Millisecond)
		return true, ""
	})
	if completed || ok || reason != "readiness check timed out" {
		t.Fatalf("got ok=%v reason=%q completed=%v", ok, reason, completed)
	}
}
