package main

import (
	"sync"
	"time"
)

// Max time to wait on an in-flight refresh when no cached status exists yet.
const statusCacheInflightWait = 5 * time.Second

// statusCache coalesces concurrent full status builds so UI polling cannot stack
// unbounded goroutines each blocked in orchestrator.GetStatus().
type statusCache struct {
	mu       sync.Mutex
	cond     sync.Cond
	at       time.Time
	data     interface{}
	ttl      time.Duration
	refresh  func() interface{}
	inflight bool
}

func newStatusCache(ttl time.Duration, refresh func() interface{}) *statusCache {
	c := &statusCache{ttl: ttl, refresh: refresh}
	c.cond.L = &c.mu
	return c
}

// waitForInflightOrStale waits for an in-flight refresh. Caller must hold c.mu.
func (c *statusCache) waitForInflightOrStale(wait time.Duration) (interface{}, bool) {
	deadline := time.Now().Add(wait)
	for c.inflight {
		if c.data != nil && time.Since(c.at) < c.ttl {
			return c.data, true
		}
		if c.data != nil {
			return c.data, true
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return nil, false
		}
		timer := time.AfterFunc(remaining, func() {
			c.mu.Lock()
			c.cond.Broadcast()
			c.mu.Unlock()
		})
		c.cond.Wait()
		timer.Stop()
	}
	if c.data != nil {
		return c.data, true
	}
	return nil, false
}

func (c *statusCache) get() interface{} {
	c.mu.Lock()
	if c.data != nil && time.Since(c.at) < c.ttl {
		data := c.data
		c.mu.Unlock()
		return data
	}
	if c.inflight {
		if data, ok := c.waitForInflightOrStale(statusCacheInflightWait); ok {
			c.mu.Unlock()
			return data
		}
		c.mu.Unlock()
		return nil
	}
	c.inflight = true
	c.mu.Unlock()

	var data interface{}
	func() {
		defer func() {
			if r := recover(); r != nil {
				c.mu.Lock()
				c.inflight = false
				c.cond.Broadcast()
				c.mu.Unlock()
				panic(r)
			}
		}()
		data = c.refresh()
	}()

	c.mu.Lock()
	defer c.mu.Unlock()
	c.data = data
	c.at = time.Now()
	c.inflight = false
	c.cond.Broadcast()
	return data
}
