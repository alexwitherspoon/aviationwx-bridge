package scheduler

import (
	"testing"
	"time"
)

func TestDefaultBackoffConfig(t *testing.T) {
	config := DefaultBackoffConfig()

	if config.InitialSeconds != 60 {
		t.Errorf("InitialSeconds = %d, want 60", config.InitialSeconds)
	}
	if config.MaxSeconds != 3600 {
		t.Errorf("MaxSeconds = %d, want 3600", config.MaxSeconds)
	}
	if config.Multiplier != 2.0 {
		t.Errorf("Multiplier = %f, want 2.0", config.Multiplier)
	}
	if !config.Jitter {
		t.Error("Jitter should default to true")
	}
}

func TestCalculateBackoff(t *testing.T) {
	config := DefaultBackoffConfig()

	tests := []struct {
		name         string
		failureCount int
		wantMin      int
		wantMax      int
	}{
		{
			name:         "no failures",
			failureCount: 0,
			wantMin:      0,
			wantMax:      0,
		},
		{
			name:         "first failure",
			failureCount: 1,
			wantMin:      60,
			wantMax:      72, // 60 + 20% jitter
		},
		{
			name:         "second failure",
			failureCount: 2,
			wantMin:      120,
			wantMax:      144, // 120 + 20% jitter
		},
		{
			name:         "third failure",
			failureCount: 3,
			wantMin:      240,
			wantMax:      288, // 240 + 20% jitter
		},
		{
			name:         "many failures (capped)",
			failureCount: 100,
			wantMin:      3600,
			wantMax:      4320, // 3600 + 20% jitter
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &CameraState{
				FailureCount: tt.failureCount,
			}

			backoff := CalculateBackoff(state, config)

			if backoff < tt.wantMin || backoff > tt.wantMax {
				t.Errorf("CalculateBackoff() = %d, want between %d and %d", backoff, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestCalculateBackoff_NoJitter(t *testing.T) {
	config := DefaultBackoffConfig()
	config.Jitter = false

	state := &CameraState{
		FailureCount: 1,
	}

	backoff := CalculateBackoff(state, config)

	if backoff != 60 {
		t.Errorf("CalculateBackoff() with no jitter = %d, want 60", backoff)
	}
}

func TestUpdateBackoff(t *testing.T) {
	config := DefaultBackoffConfig()
	state := &CameraState{
		FailureCount: 0,
	}

	UpdateBackoff(state, config)

	if state.FailureCount != 1 {
		t.Errorf("FailureCount = %d, want 1", state.FailureCount)
	}
	if !state.IsBackingOff {
		t.Error("IsBackingOff should be true")
	}
	if state.BackoffSeconds == 0 {
		t.Error("BackoffSeconds should be set")
	}
	if state.NextAttempt.IsZero() {
		t.Error("NextAttempt should be set")
	}
	if !state.NextAttempt.After(time.Now()) {
		t.Error("NextAttempt should be in the future")
	}
}

func TestResetBackoff(t *testing.T) {
	state := &CameraState{
		FailureCount:   5,
		IsBackingOff:   true,
		BackoffSeconds: 300,
		LastError:      &testError{msg: "test error"},
		LastErrorTime:  time.Now(),
	}

	ResetBackoff(state)

	if state.FailureCount != 0 {
		t.Errorf("FailureCount = %d, want 0", state.FailureCount)
	}
	if state.IsBackingOff {
		t.Error("IsBackingOff should be false")
	}
	if state.BackoffSeconds != 0 {
		t.Errorf("BackoffSeconds = %d, want 0", state.BackoffSeconds)
	}
	if state.LastError != nil {
		t.Error("LastError should be nil")
	}
	if !state.LastErrorTime.IsZero() {
		t.Error("LastErrorTime should be zero")
	}
	if state.SuccessCount != 1 {
		t.Errorf("SuccessCount = %d, want 1", state.SuccessCount)
	}
}

func TestShouldAttempt(t *testing.T) {
	tests := []struct {
		name        string
		nextAttempt time.Time
		want        bool
	}{
		{
			name:        "zero time (should attempt)",
			nextAttempt: time.Time{},
			want:        true,
		},
		{
			name:        "past time (should attempt)",
			nextAttempt: time.Now().Add(-1 * time.Minute),
			want:        true,
		},
		{
			name:        "future time (should not attempt)",
			nextAttempt: time.Now().Add(1 * time.Minute),
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &CameraState{
				NextAttempt: tt.nextAttempt,
			}

			got := ShouldAttempt(state)
			if got != tt.want {
				t.Errorf("ShouldAttempt() = %v, want %v", got, tt.want)
			}
		})
	}
}

// testError is a simple error for testing
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}
