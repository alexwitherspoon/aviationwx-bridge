package web

import (
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/logger"
)

type timeoutResult struct {
	value    interface{}
	panicked bool
}

// runWithTimeout runs fn in a goroutine and returns its value, or false if timeout elapses.
func runWithTimeout(timeout time.Duration, fn func() interface{}) (interface{}, bool) {
	ch := make(chan timeoutResult, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Default().Error("status callback panicked", "panic", r)
				ch <- timeoutResult{panicked: true}
			}
		}()
		ch <- timeoutResult{value: fn()}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		if !timer.Stop() {
			<-timer.C
		}
		if res.panicked {
			return nil, false
		}
		return res.value, true
	case <-timer.C:
		return nil, false
	}
}

// runReadinessWithTimeout runs fn in a goroutine for /readyz (must finish within Docker health timeout).
func runReadinessWithTimeout(timeout time.Duration, fn func() (bool, string)) (ok bool, reason string, completed bool) {
	type result struct {
		ok       bool
		reason   string
		panicked bool
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Default().Error("readiness callback panicked", "panic", r)
				ch <- result{panicked: true}
			}
		}()
		o, r := fn()
		ch <- result{ok: o, reason: r}
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		if !timer.Stop() {
			<-timer.C
		}
		if res.panicked {
			return false, "readiness check failed", true
		}
		return res.ok, res.reason, true
	case <-timer.C:
		return false, "readiness check timed out", false
	}
}
