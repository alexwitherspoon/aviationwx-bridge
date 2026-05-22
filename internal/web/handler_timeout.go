package web

import (
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/logger"
)

// runWithTimeout runs fn in a goroutine and returns its value, or false if timeout elapses.
func runWithTimeout(timeout time.Duration, fn func() interface{}) (interface{}, bool) {
	ch := make(chan interface{}, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Default().Error("status callback panicked", "panic", r)
			}
		}()
		ch <- fn()
	}()
	select {
	case v := <-ch:
		return v, true
	case <-time.After(timeout):
		return nil, false
	}
}

// runReadinessWithTimeout runs fn in a goroutine for /readyz (must finish within Docker health timeout).
func runReadinessWithTimeout(timeout time.Duration, fn func() (bool, string)) (ok bool, reason string, completed bool) {
	type result struct {
		ok     bool
		reason string
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Default().Error("readiness callback panicked", "panic", r)
			}
		}()
		o, r := fn()
		ch <- result{o, r}
	}()
	select {
	case res := <-ch:
		return res.ok, res.reason, true
	case <-time.After(timeout):
		return false, "readiness check timed out", false
	}
}
