package time

import (
	"testing"
	"time"
)

func TestNewTimeHealth(t *testing.T) {
	config := Config{
		Enabled:              true,
		Servers:              []string{"pool.ntp.org"},
		CheckIntervalSeconds: 300,
		MaxOffsetSeconds:     5,
	}

	th := NewTimeHealth(config)

	if th == nil {
		t.Fatal("NewTimeHealth() returned nil")
	}
	if th.healthy {
		t.Error("healthy should start as false")
	}
	if th.checkInterval != 300*time.Second {
		t.Errorf("checkInterval = %v, want 300s", th.checkInterval)
	}
	if th.maxOffset != 5*time.Second {
		t.Errorf("maxOffset = %v, want 5s", th.maxOffset)
	}
}

func TestNewTimeHealth_Defaults(t *testing.T) {
	config := Config{
		Enabled: true,
	}

	th := NewTimeHealth(config)

	if th.checkInterval != 300*time.Second {
		t.Errorf("checkInterval = %v, want 300s (default)", th.checkInterval)
	}
	if th.maxOffset != 5*time.Second {
		t.Errorf("maxOffset = %v, want 5s (default)", th.maxOffset)
	}
	if len(th.servers) == 0 {
		t.Error("servers should have default value")
	}
}

func TestTimeHealth_IsHealthy(t *testing.T) {
	config := Config{
		Enabled: true,
	}
	th := NewTimeHealth(config)

	// Should start as unhealthy
	if th.IsHealthy() {
		t.Error("Should start as unhealthy")
	}

	// Manually set healthy
	th.mu.Lock()
	th.healthy = true
	th.mu.Unlock()

	if !th.IsHealthy() {
		t.Error("Should be healthy after setting")
	}
}

func TestTimeHealth_GetOffset(t *testing.T) {
	config := Config{
		Enabled: true,
	}
	th := NewTimeHealth(config)

	offset := th.GetOffset()
	if offset != 0 {
		t.Errorf("Initial offset = %v, want 0", offset)
	}

	// Manually set offset
	th.mu.Lock()
	th.offset = 2 * time.Second
	th.mu.Unlock()

	offset = th.GetOffset()
	if offset != 2*time.Second {
		t.Errorf("Offset = %v, want 2s", offset)
	}
}

func TestTimeHealth_GetStatus(t *testing.T) {
	config := Config{
		Enabled: true,
	}
	th := NewTimeHealth(config)

	status := th.GetStatus()
	if status.Healthy {
		t.Error("Status should start as unhealthy")
	}
	if status.Offset != 0 {
		t.Errorf("Status offset = %v, want 0", status.Offset)
	}
	if !status.LastCheck.IsZero() {
		t.Error("LastCheck should be zero initially")
	}
}

func TestAbsDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    time.Duration
		expected time.Duration
	}{
		{
			name:     "positive",
			input:    5 * time.Second,
			expected: 5 * time.Second,
		},
		{
			name:     "negative",
			input:    -5 * time.Second,
			expected: 5 * time.Second,
		},
		{
			name:     "zero",
			input:    0,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := absDuration(tt.input)
			if result != tt.expected {
				t.Errorf("absDuration(%v) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
