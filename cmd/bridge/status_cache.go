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
	if c.data != nil && time.Since(c.at) < c.ttl {
		data := c.data
		c.mu.Unlock()
		return data
	}
	// Stale-while-revalidate: do not block on a wedged refresh when cached data exists.
	if c.inflight && c.data != nil {
		data := c.data
		c.mu.Unlock()
		return data
	}
	for c.inflight {
		c.cond.Wait()
		if c.data != nil && time.Since(c.at) < c.ttl {
			data := c.data
			c.mu.Unlock()
			return data
		}
		if c.inflight && c.data != nil {
			data := c.data
			c.mu.Unlock()
			return data
		}
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
