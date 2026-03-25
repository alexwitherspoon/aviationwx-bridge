package time

import (
	"context"
	"sync"
	"time"
)

// TimeHealth manages time health status and SNTP checks
type TimeHealth struct {
	healthy       bool
	offset        time.Duration // Time offset from NTP server
	lastCheck     time.Time
	lastGoodSync  time.Time // Last successful in-bounds NTP response (wall clock)
	checkInterval time.Duration
	maxOffset     time.Duration
	staleMaxAge   time.Duration // If lastGoodSync is within this age, still healthy when NTP is unreachable
	servers       []string
	queryHook     func(string) (time.Duration, error) // if set, replaces real NTP (tests)
	mu            sync.RWMutex

	// Context cancellation for clean shutdown
	ctx    context.Context
	cancel context.CancelFunc
}

// Config represents time health configuration
type Config struct {
	Enabled              bool
	Servers              []string
	CheckIntervalSeconds int
	MaxOffsetSeconds     int
	TimeoutSeconds       int
	// StaleThresholdHours: after a successful in-bounds sync, remain healthy for this many hours
	// while NTP is unreachable. 0 defaults to 24.
	StaleThresholdHours int
	// QueryHook, if non-nil, is used instead of a real NTP query. For unit tests only.
	QueryHook func(server string) (time.Duration, error)
}

// Status represents the current time health status
type Status struct {
	Healthy      bool
	Offset       time.Duration
	LastCheck    time.Time
	LastGoodSync time.Time // Zero if there has never been an in-bounds NTP sync
}

// NewTimeHealth creates a new time health manager
func NewTimeHealth(config Config) *TimeHealth {
	checkInterval := time.Duration(config.CheckIntervalSeconds) * time.Second
	if checkInterval == 0 {
		checkInterval = 300 * time.Second // Default 5 minutes
	}

	maxOffset := time.Duration(config.MaxOffsetSeconds) * time.Second
	if maxOffset == 0 {
		maxOffset = 5 * time.Second // Default 5 seconds
	}

	servers := config.Servers
	if len(servers) == 0 {
		servers = []string{"pool.ntp.org"} // Default NTP server
	}

	staleHours := config.StaleThresholdHours
	if staleHours <= 0 {
		staleHours = 24
	}
	staleMaxAge := time.Duration(staleHours) * time.Hour

	ctx, cancel := context.WithCancel(context.Background())

	return &TimeHealth{
		healthy:       false, // Unhealthy until first in-bounds sync or stale-window grace
		offset:        0,
		lastCheck:     time.Time{},
		checkInterval: checkInterval,
		maxOffset:     maxOffset,
		staleMaxAge:   staleMaxAge,
		servers:       servers,
		queryHook:     config.QueryHook,
		ctx:           ctx,
		cancel:        cancel,
	}
}

// IsHealthy returns whether time is currently considered healthy
func (th *TimeHealth) IsHealthy() bool {
	th.mu.RLock()
	defer th.mu.RUnlock()
	return th.healthy
}

// GetOffset returns the current time offset
func (th *TimeHealth) GetOffset() time.Duration {
	th.mu.RLock()
	defer th.mu.RUnlock()
	return th.offset
}

// GetStatus returns the current time health status
func (th *TimeHealth) GetStatus() Status {
	th.mu.RLock()
	defer th.mu.RUnlock()
	return Status{
		Healthy:      th.healthy,
		Offset:       th.offset,
		LastCheck:    th.lastCheck,
		LastGoodSync: th.lastGoodSync,
	}
}

// Start begins periodic SNTP health checks
func (th *TimeHealth) Start() {
	// Perform initial check
	th.check()

	// Start periodic checks
	go th.run()
}

// run performs periodic SNTP checks
func (th *TimeHealth) run() {
	ticker := time.NewTicker(th.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-th.ctx.Done():
			return
		case <-ticker.C:
			th.check()
		}
	}
}

// Stop gracefully stops the time health checker
func (th *TimeHealth) Stop() {
	if th.cancel != nil {
		th.cancel()
	}
}

// check performs a single SNTP check
func (th *TimeHealth) check() {
	// Try each server until one succeeds
	for _, server := range th.servers {
		offset, err := th.queryServer(server)
		if err != nil {
			continue // Try next server
		}

		// Update state
		now := time.Now()
		th.mu.Lock()
		th.offset = offset
		th.lastCheck = now
		inBounds := absDuration(offset) <= th.maxOffset
		th.healthy = inBounds
		if inBounds {
			th.lastGoodSync = now
		} else {
			// Out-of-bounds offset: do not keep a prior lastGoodSync, or a later all-servers-fail cycle
			// could incorrectly treat stale grace as healthy (see stale_sync_behavior_test.go).
			th.lastGoodSync = time.Time{}
		}
		th.mu.Unlock()

		return // Got a response from at least one server
	}

	// All servers failed: unhealthy only if we never synced, or last good sync is older than staleMaxAge
	th.mu.Lock()
	th.lastCheck = time.Now()
	switch {
	case th.lastGoodSync.IsZero():
		th.healthy = false
	case time.Since(th.lastGoodSync) <= th.staleMaxAge:
		th.healthy = true
	default:
		th.healthy = false
	}
	th.mu.Unlock()
}

// queryServer runs a real NTP query or Config.QueryHook when set.
func (th *TimeHealth) queryServer(server string) (time.Duration, error) {
	if th.queryHook != nil {
		return th.queryHook(server)
	}
	return th.queryNTP(server)
}

// queryNTP is declared in sntp.go to keep types.go focused on types

// absDuration returns the absolute value of a duration
func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
