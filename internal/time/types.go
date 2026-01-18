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
	checkInterval time.Duration
	maxOffset     time.Duration
	servers       []string
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
}

// Status represents the current time health status
type Status struct {
	Healthy   bool
	Offset    time.Duration
	LastCheck time.Time
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

	ctx, cancel := context.WithCancel(context.Background())

	return &TimeHealth{
		healthy:       false, // Start as unhealthy until first check
		offset:        0,
		lastCheck:     time.Time{},
		checkInterval: checkInterval,
		maxOffset:     maxOffset,
		servers:       servers,
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
		Healthy:   th.healthy,
		Offset:    th.offset,
		LastCheck: th.lastCheck,
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
		offset, err := th.queryNTP(server)
		if err != nil {
			continue // Try next server
		}

		// Update state
		th.mu.Lock()
		th.offset = offset
		th.lastCheck = time.Now()
		th.healthy = absDuration(offset) <= th.maxOffset
		th.mu.Unlock()

		return // Success
	}

	// All servers failed - mark as unhealthy
	th.mu.Lock()
	th.healthy = false
	th.lastCheck = time.Now()
	th.mu.Unlock()
}

// queryNTP is declared in sntp.go to keep types.go focused on types

// absDuration returns the absolute value of a duration
func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
