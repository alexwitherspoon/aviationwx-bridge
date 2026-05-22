package main

import (
	"sync"
	"time"
)

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

func (c *statusCache) get() interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.data != nil && time.Since(c.at) < c.ttl {
		return c.data
	}

	for c.inflight {
		c.cond.Wait()
		if c.data != nil && time.Since(c.at) < c.ttl {
			return c.data
		}
	}

	c.inflight = true
	data := c.refresh()
	c.data = data
	c.at = time.Now()
	c.inflight = false
	c.cond.Broadcast()
	return data
}
