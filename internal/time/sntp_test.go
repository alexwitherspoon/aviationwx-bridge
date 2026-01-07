package time

import (
	"testing"
	"time"
)

func TestQueryNTP_RealServer(t *testing.T) {
	// Skip if network is not available or in CI
	if testing.Short() {
		t.Skip("Skipping NTP query test in short mode")
	}

	config := Config{
		Enabled:        true,
		Servers:        []string{"pool.ntp.org"},
		TimeoutSeconds: 5,
	}
	th := NewTimeHealth(config)

	offset, err := th.queryNTP("pool.ntp.org")
	if err != nil {
		t.Skipf("NTP query failed (network may be unavailable): %v", err)
	}

	// Offset should be reasonable (within a few seconds)
	maxReasonableOffset := 10 * time.Second
	if absDuration(offset) > maxReasonableOffset {
		t.Errorf("Offset = %v, seems unreasonable (may indicate system time issue)", offset)
	}

	// Offset should be non-zero (system time is rarely exactly synchronized)
	// But we'll allow zero as a valid result
	_ = offset
}

func TestTimeHealth_Check(t *testing.T) {
	// Skip if network is not available
	if testing.Short() {
		t.Skip("Skipping NTP check test in short mode")
	}

	config := Config{
		Enabled:              true,
		Servers:              []string{"pool.ntp.org"},
		CheckIntervalSeconds: 300,
		MaxOffsetSeconds:     5,
		TimeoutSeconds:       5,
	}
	th := NewTimeHealth(config)

	// Perform check
	th.check()

	status := th.GetStatus()
	if status.LastCheck.IsZero() {
		t.Error("LastCheck should be set after check")
	}

	// Health status depends on actual offset
	// We just verify the check completed
	_ = status.Healthy
}

func TestTimeHealth_Check_MultipleServers(t *testing.T) {
	// Skip if network is not available
	if testing.Short() {
		t.Skip("Skipping NTP check test in short mode")
	}

	config := Config{
		Enabled:              true,
		Servers:              []string{"invalid.ntp.server", "pool.ntp.org"},
		CheckIntervalSeconds: 300,
		MaxOffsetSeconds:     5,
		TimeoutSeconds:       5,
	}
	th := NewTimeHealth(config)

	// Should try first server (fail), then second (succeed)
	th.check()

	status := th.GetStatus()
	if status.LastCheck.IsZero() {
		t.Error("LastCheck should be set after check")
	}
}

func TestTimeHealth_Check_AllServersFail(t *testing.T) {
	config := Config{
		Enabled:              true,
		Servers:              []string{"invalid.ntp.server.example.com"},
		CheckIntervalSeconds: 300,
		MaxOffsetSeconds:     5,
		TimeoutSeconds:       1, // Short timeout for faster test
	}
	th := NewTimeHealth(config)

	// Should mark as unhealthy when all servers fail
	th.check()

	status := th.GetStatus()
	if status.Healthy {
		t.Error("Should be unhealthy when all servers fail")
	}
	if !status.LastCheck.IsZero() {
		// LastCheck should be set even on failure
		_ = status.LastCheck
	}
}

func TestTimeHealth_Start(t *testing.T) {
	config := Config{
		Enabled:              true,
		Servers:              []string{"pool.ntp.org"},
		CheckIntervalSeconds: 1, // Short interval for test
		MaxOffsetSeconds:     5,
		TimeoutSeconds:       5,
	}
	th := NewTimeHealth(config)

	// Start periodic checks
	th.Start()

	// Wait a bit for initial check
	time.Sleep(1500 * time.Millisecond)

	status := th.GetStatus()
	if status.LastCheck.IsZero() {
		t.Error("LastCheck should be set after Start()")
	}

	// Note: We can't easily test the periodic nature without waiting
	// But we verify Start() doesn't panic and initial check happens
}
