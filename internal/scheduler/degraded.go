package scheduler

import (
	"sync"
	"time"
)

// DegradedMode manages degraded mode state
type DegradedMode struct {
	enabled                bool
	failureThreshold       int     // Number of failures to trigger degraded mode
	concurrencyLimit       int     // Max concurrent operations in degraded mode
	slowIntervalMultiplier float64 // Multiply interval by this in degraded mode
	failureCount           int     // Current failure count across all cameras
	active                 bool    // Whether degraded mode is currently active
	lastFailureTime        time.Time
	mu                     sync.RWMutex
}

// DegradedConfig represents degraded mode configuration
type DegradedConfig struct {
	Enabled                bool
	FailureThreshold       int
	ConcurrencyLimit       int
	SlowIntervalMultiplier float64
}

// DefaultDegradedConfig returns default degraded mode configuration
func DefaultDegradedConfig() DegradedConfig {
	return DegradedConfig{
		Enabled:                true,
		FailureThreshold:       3,   // Trigger after 3 failures
		ConcurrencyLimit:       1,   // Only 1 concurrent operation
		SlowIntervalMultiplier: 2.0, // Double the interval
	}
}

// NewDegradedMode creates a new degraded mode manager
func NewDegradedMode(config DegradedConfig) *DegradedMode {
	return &DegradedMode{
		enabled:                config.Enabled,
		failureThreshold:       config.FailureThreshold,
		concurrencyLimit:       config.ConcurrencyLimit,
		slowIntervalMultiplier: config.SlowIntervalMultiplier,
	}
}

// RecordFailure records a failure and checks if degraded mode should activate
func (dm *DegradedMode) RecordFailure() {
	if !dm.enabled {
		return
	}

	dm.mu.Lock()
	defer dm.mu.Unlock()

	dm.failureCount++
	dm.lastFailureTime = time.Now()

	// Activate degraded mode if threshold reached
	if dm.failureCount >= dm.failureThreshold && !dm.active {
		dm.active = true
	}
}

// RecordSuccess records a success and checks if degraded mode should deactivate
func (dm *DegradedMode) RecordSuccess() {
	if !dm.enabled {
		return
	}

	dm.mu.Lock()
	defer dm.mu.Unlock()

	// Reset failure count on success
	dm.failureCount = 0

	// Deactivate degraded mode if active
	if dm.active {
		dm.active = false
	}
}

// IsActive returns whether degraded mode is currently active
func (dm *DegradedMode) IsActive() bool {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return dm.active
}

// GetConcurrencyLimit returns the maximum concurrent operations
func (dm *DegradedMode) GetConcurrencyLimit() int {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	if !dm.active {
		return -1 // No limit when not in degraded mode
	}
	return dm.concurrencyLimit
}

// GetIntervalMultiplier returns the interval multiplier for degraded mode
func (dm *DegradedMode) GetIntervalMultiplier() float64 {
	dm.mu.RLock()
	defer dm.mu.RUnlock()

	if !dm.active {
		return 1.0 // No multiplier when not in degraded mode
	}
	return dm.slowIntervalMultiplier
}

// GetFailureCount returns the current failure count
func (dm *DegradedMode) GetFailureCount() int {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	return dm.failureCount
}
