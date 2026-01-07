package time

import (
	"testing"
	"time"
)

// TestTimeHealth_Integration tests the full time health cycle
func TestTimeHealth_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := Config{
		Enabled:              true,
		Servers:              []string{"pool.ntp.org"},
		CheckIntervalSeconds: 1, // Short for test
		MaxOffsetSeconds:     5,
		TimeoutSeconds:       5,
	}
	th := NewTimeHealth(config)

	// Start periodic checks
	th.Start()

	// Wait for initial check
	time.Sleep(2000 * time.Millisecond)

	status := th.GetStatus()
	if status.LastCheck.IsZero() {
		t.Error("LastCheck should be set after Start()")
	}

	// Health depends on actual offset - just verify check happened
	_ = status.Healthy
	_ = status.Offset
}

// TestStampEXIF_Integration tests EXIF stamping with time health
func TestStampEXIF_Integration(t *testing.T) {
	config := Config{
		Enabled: true,
	}
	th := NewTimeHealth(config)

	// Test with unhealthy time
	th.mu.Lock()
	th.healthy = false
	th.mu.Unlock()

	imageData := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00}
	captureTime := time.Now()

	result, err := StampEXIF(imageData, th, captureTime)
	if err != nil {
		t.Fatalf("StampEXIF() error = %v", err)
	}

	// Should return original data when time is unhealthy
	if len(result) != len(imageData) {
		t.Errorf("Result length = %d, want %d (should return original)", len(result), len(imageData))
	}

	// Test with healthy time
	th.mu.Lock()
	th.healthy = true
	th.mu.Unlock()

	result2, err := StampEXIF(imageData, th, captureTime)
	if err != nil {
		t.Fatalf("StampEXIF() error = %v", err)
	}

	// Should return data (may be modified when EXIF is implemented)
	if len(result2) < len(imageData) {
		t.Error("Result should not be shorter than input")
	}
}
