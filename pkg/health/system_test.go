package health

import (
	"testing"
	"time"
)

func TestGetLevelFromPercent(t *testing.T) {
	tests := []struct {
		name              string
		percent           float64
		warningThreshold  float64
		criticalThreshold float64
		want              Level
	}{
		{
			name:              "healthy - well below warning",
			percent:           30.0,
			warningThreshold:  70.0,
			criticalThreshold: 90.0,
			want:              LevelHealthy,
		},
		{
			name:              "healthy - just below warning",
			percent:           69.9,
			warningThreshold:  70.0,
			criticalThreshold: 90.0,
			want:              LevelHealthy,
		},
		{
			name:              "warning - at threshold",
			percent:           70.0,
			warningThreshold:  70.0,
			criticalThreshold: 90.0,
			want:              LevelWarning,
		},
		{
			name:              "warning - between thresholds",
			percent:           80.0,
			warningThreshold:  70.0,
			criticalThreshold: 90.0,
			want:              LevelWarning,
		},
		{
			name:              "warning - just below critical",
			percent:           89.9,
			warningThreshold:  70.0,
			criticalThreshold: 90.0,
			want:              LevelWarning,
		},
		{
			name:              "critical - at threshold",
			percent:           90.0,
			warningThreshold:  70.0,
			criticalThreshold: 90.0,
			want:              LevelCritical,
		},
		{
			name:              "critical - above threshold",
			percent:           100.0,
			warningThreshold:  70.0,
			criticalThreshold: 90.0,
			want:              LevelCritical,
		},
		{
			name:              "healthy - zero percent",
			percent:           0.0,
			warningThreshold:  70.0,
			criticalThreshold: 90.0,
			want:              LevelHealthy,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getLevelFromPercent(tt.percent, tt.warningThreshold, tt.criticalThreshold)
			if got != tt.want {
				t.Errorf("getLevelFromPercent(%v, %v, %v) = %v, want %v",
					tt.percent, tt.warningThreshold, tt.criticalThreshold, got, tt.want)
			}
		})
	}
}

func TestWorstLevel(t *testing.T) {
	tests := []struct {
		name   string
		levels []Level
		want   Level
	}{
		{
			name:   "all healthy",
			levels: []Level{LevelHealthy, LevelHealthy, LevelHealthy},
			want:   LevelHealthy,
		},
		{
			name:   "one warning",
			levels: []Level{LevelHealthy, LevelWarning, LevelHealthy},
			want:   LevelWarning,
		},
		{
			name:   "one critical",
			levels: []Level{LevelHealthy, LevelCritical, LevelHealthy},
			want:   LevelCritical,
		},
		{
			name:   "warning and critical",
			levels: []Level{LevelWarning, LevelCritical, LevelHealthy},
			want:   LevelCritical,
		},
		{
			name:   "all critical",
			levels: []Level{LevelCritical, LevelCritical, LevelCritical},
			want:   LevelCritical,
		},
		{
			name:   "all warning",
			levels: []Level{LevelWarning, LevelWarning, LevelWarning},
			want:   LevelWarning,
		},
		{
			name:   "empty levels",
			levels: []Level{},
			want:   LevelHealthy,
		},
		{
			name:   "single healthy",
			levels: []Level{LevelHealthy},
			want:   LevelHealthy,
		},
		{
			name:   "single critical",
			levels: []Level{LevelCritical},
			want:   LevelCritical,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := worstLevel(tt.levels...)
			if got != tt.want {
				t.Errorf("worstLevel(%v) = %v, want %v", tt.levels, got, tt.want)
			}
		})
	}
}

func TestNewSystemMonitor(t *testing.T) {
	monitor := NewSystemMonitor("/tmp/test-queue")

	if monitor == nil {
		t.Fatal("NewSystemMonitor returned nil")
	}

	if monitor.queueBasePath != "/tmp/test-queue" {
		t.Errorf("queueBasePath = %v, want /tmp/test-queue", monitor.queueBasePath)
	}

	if monitor.startTime.IsZero() {
		t.Error("startTime should not be zero")
	}

	// Start time should be recent (within last second)
	if time.Since(monitor.startTime) > time.Second {
		t.Error("startTime should be recent")
	}
}

func TestSystemMonitor_GetStats(t *testing.T) {
	monitor := NewSystemMonitor("")

	// Wait a tiny bit to allow CPU measurement
	time.Sleep(150 * time.Millisecond)

	stats := monitor.GetStats()

	// Check that basic fields are populated
	if stats.NumCPU <= 0 {
		t.Errorf("NumCPU should be > 0, got %d", stats.NumCPU)
	}

	if stats.NumGoroutines <= 0 {
		t.Errorf("NumGoroutines should be > 0, got %d", stats.NumGoroutines)
	}

	if stats.Uptime == "" {
		t.Error("Uptime should not be empty")
	}

	// CPU percent should be in valid range
	if stats.CPUPercent < 0 || stats.CPUPercent > 100 {
		t.Errorf("CPUPercent should be 0-100, got %f", stats.CPUPercent)
	}

	// Memory values should be reasonable
	if stats.HeapAllocMB < 0 {
		t.Errorf("HeapAllocMB should be >= 0, got %f", stats.HeapAllocMB)
	}

	if stats.HeapSysMB < 0 {
		t.Errorf("HeapSysMB should be >= 0, got %f", stats.HeapSysMB)
	}

	// Levels should be valid
	validLevels := map[Level]bool{
		LevelHealthy:  true,
		LevelWarning:  true,
		LevelCritical: true,
	}

	if !validLevels[stats.CPULevel] {
		t.Errorf("CPULevel is invalid: %v", stats.CPULevel)
	}

	if !validLevels[stats.MemLevel] {
		t.Errorf("MemLevel is invalid: %v", stats.MemLevel)
	}

	if !validLevels[stats.DiskLevel] {
		t.Errorf("DiskLevel is invalid: %v", stats.DiskLevel)
	}

	if !validLevels[stats.OverallLevel] {
		t.Errorf("OverallLevel is invalid: %v", stats.OverallLevel)
	}
}

func TestSystemMonitor_GetStats_Concurrent(t *testing.T) {
	monitor := NewSystemMonitor("")

	// Run multiple goroutines calling GetStats concurrently
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				stats := monitor.GetStats()
				// Just verify it doesn't panic and returns valid data
				if stats.NumCPU <= 0 {
					t.Errorf("NumCPU should be > 0")
				}
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestLevelConstants(t *testing.T) {
	// Verify level string values are as expected
	if LevelHealthy != "healthy" {
		t.Errorf("LevelHealthy = %v, want healthy", LevelHealthy)
	}
	if LevelWarning != "warning" {
		t.Errorf("LevelWarning = %v, want warning", LevelWarning)
	}
	if LevelCritical != "critical" {
		t.Errorf("LevelCritical = %v, want critical", LevelCritical)
	}
}

func TestThresholdConstants(t *testing.T) {
	// Verify thresholds are in logical order
	if CPUWarningThreshold >= CPUCriticalThreshold {
		t.Errorf("CPU warning threshold (%v) should be < critical (%v)",
			CPUWarningThreshold, CPUCriticalThreshold)
	}
	if MemWarningThreshold >= MemCriticalThreshold {
		t.Errorf("Memory warning threshold (%v) should be < critical (%v)",
			MemWarningThreshold, MemCriticalThreshold)
	}
	if DiskWarningThreshold >= DiskCriticalThreshold {
		t.Errorf("Disk warning threshold (%v) should be < critical (%v)",
			DiskWarningThreshold, DiskCriticalThreshold)
	}

	// Verify thresholds are in valid percentage range
	thresholds := []float64{
		CPUWarningThreshold, CPUCriticalThreshold,
		MemWarningThreshold, MemCriticalThreshold,
		DiskWarningThreshold, DiskCriticalThreshold,
	}
	for _, th := range thresholds {
		if th < 0 || th > 100 {
			t.Errorf("Threshold %v should be in range 0-100", th)
		}
	}
}

func TestGetDiskStats_EmptyPath(t *testing.T) {
	monitor := NewSystemMonitor("")
	used, free, total := monitor.getDiskStats()

	// With empty path, should return zeros
	if used != 0 || free != 0 || total != 0 {
		t.Errorf("getDiskStats with empty path should return 0,0,0, got %f,%f,%f",
			used, free, total)
	}
}

func TestGetDiskStats_InvalidPath(t *testing.T) {
	monitor := NewSystemMonitor("/nonexistent/path/that/doesnt/exist")
	used, free, total := monitor.getDiskStats()

	// With invalid path, should return zeros
	if used != 0 || free != 0 || total != 0 {
		t.Errorf("getDiskStats with invalid path should return 0,0,0, got %f,%f,%f",
			used, free, total)
	}
}
