package scheduler

import (
	"math"
	"math/rand"
	"time"
)

// BackoffConfig represents backoff configuration
type BackoffConfig struct {
	InitialSeconds int     // Initial backoff (default: 60)
	MaxSeconds     int     // Maximum backoff (default: 3600 = 1 hour)
	Multiplier     float64 // Backoff multiplier (default: 2.0)
	Jitter         bool    // Add jitter to prevent thundering herd (default: true)
}

// DefaultBackoffConfig returns default backoff configuration
func DefaultBackoffConfig() BackoffConfig {
	return BackoffConfig{
		InitialSeconds: 60,   // 1 minute
		MaxSeconds:     3600, // 1 hour
		Multiplier:     2.0,
		Jitter:         true,
	}
}

// CalculateBackoff calculates the next backoff duration for a camera
func CalculateBackoff(state *CameraState, config BackoffConfig) int {
	if state.FailureCount == 0 {
		// No failures, no backoff
		return 0
	}

	// Calculate exponential backoff
	// backoff = initial * (multiplier ^ (failure_count - 1))
	backoff := float64(config.InitialSeconds) * math.Pow(config.Multiplier, float64(state.FailureCount-1))

	// Cap at maximum
	if backoff > float64(config.MaxSeconds) {
		backoff = float64(config.MaxSeconds)
	}

	// Add jitter if enabled (up to 20% of backoff time)
	if config.Jitter {
		jitter := backoff * 0.2 * rand.Float64()
		backoff += jitter
	}

	return int(backoff)
}

// UpdateBackoff updates the camera state after a failure
func UpdateBackoff(state *CameraState, config BackoffConfig) {
	state.FailureCount++
	state.IsBackingOff = true
	state.BackoffSeconds = CalculateBackoff(state, config)
	state.NextAttempt = time.Now().Add(time.Duration(state.BackoffSeconds) * time.Second)
}

// ResetBackoff resets the backoff state after a success
func ResetBackoff(state *CameraState) {
	state.FailureCount = 0
	state.SuccessCount++
	state.IsBackingOff = false
	state.BackoffSeconds = 0
	state.LastError = nil
	state.LastErrorTime = time.Time{}
}

// ShouldAttempt checks if a camera should attempt capture now
func ShouldAttempt(state *CameraState) bool {
	return time.Now().After(state.NextAttempt) || state.NextAttempt.IsZero()
}
