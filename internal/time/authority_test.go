package time

import (
	"testing"
	"time"
)

func TestNewAuthority(t *testing.T) {
	config := DefaultAuthorityConfig()

	authority, err := NewAuthority(nil, config)
	if err != nil {
		t.Fatalf("NewAuthority failed: %v", err)
	}

	if authority == nil {
		t.Fatal("expected non-nil authority")
	}
}

func TestNewAuthority_WithTimezone(t *testing.T) {
	config := DefaultAuthorityConfig()
	config.Timezone = "America/Los_Angeles"

	authority, err := NewAuthority(nil, config)
	if err != nil {
		t.Fatalf("NewAuthority failed: %v", err)
	}

	if authority.GetTimezoneName() != "America/Los_Angeles" {
		t.Errorf("expected timezone 'America/Los_Angeles', got %q", authority.GetTimezoneName())
	}
}

func TestNewAuthority_InvalidTimezone(t *testing.T) {
	config := DefaultAuthorityConfig()
	config.Timezone = "Invalid/Timezone"

	_, err := NewAuthority(nil, config)
	if err == nil {
		t.Error("expected error for invalid timezone")
	}
}

func TestAuthority_DetermineObservationTime_NoCameraEXIF(t *testing.T) {
	config := DefaultAuthorityConfig()
	authority, _ := NewAuthority(nil, config)

	captureTime := time.Now().UTC()
	result := authority.DetermineObservationTime(captureTime, nil)

	if result.Source != SourceBridgeClock {
		t.Errorf("expected source SourceBridgeClock, got %v", result.Source)
	}

	if result.Confidence != ConfidenceHigh {
		t.Errorf("expected confidence ConfidenceHigh, got %v", result.Confidence)
	}

	if !result.Time.Equal(captureTime) {
		t.Errorf("expected time %v, got %v", captureTime, result.Time)
	}

	if result.Warning != nil {
		t.Errorf("expected no warning, got %v", result.Warning)
	}
}

func TestAuthority_DetermineObservationTime_CameraWithinTolerance(t *testing.T) {
	config := DefaultAuthorityConfig()
	config.Timezone = "UTC" // Use UTC for simplicity
	config.CameraToleranceSeconds = 5

	authority, _ := NewAuthority(nil, config)

	captureTime := time.Now().UTC()
	cameraTime := captureTime.Add(-2 * time.Second) // 2 seconds behind, within tolerance

	result := authority.DetermineObservationTime(captureTime, &cameraTime)

	if result.Source != SourceCameraEXIF {
		t.Errorf("expected source SourceCameraEXIF, got %v", result.Source)
	}

	if result.Confidence != ConfidenceHigh {
		t.Errorf("expected confidence ConfidenceHigh, got %v", result.Confidence)
	}

	if result.Warning != nil {
		t.Errorf("expected no warning for time within tolerance, got %v", result.Warning)
	}
}

func TestAuthority_DetermineObservationTime_CameraMinorDrift(t *testing.T) {
	config := DefaultAuthorityConfig()
	config.Timezone = "UTC"
	config.CameraToleranceSeconds = 5
	config.CameraWarnDriftSeconds = 30

	authority, _ := NewAuthority(nil, config)

	captureTime := time.Now().UTC()
	cameraTime := captureTime.Add(-15 * time.Second) // 15 seconds behind, should warn

	result := authority.DetermineObservationTime(captureTime, &cameraTime)

	if result.Source != SourceCameraEXIF {
		t.Errorf("expected source SourceCameraEXIF, got %v", result.Source)
	}

	if result.Warning == nil {
		t.Error("expected warning for minor drift")
	}

	if result.Warning != nil && result.Warning.Code != "camera_clock_drift" {
		t.Errorf("expected warning code 'camera_clock_drift', got %q", result.Warning.Code)
	}
}

func TestAuthority_DetermineObservationTime_CameraSignificantDrift(t *testing.T) {
	config := DefaultAuthorityConfig()
	config.Timezone = "UTC"
	config.CameraWarnDriftSeconds = 30
	config.CameraRejectDriftSeconds = 300 // 5 minutes

	authority, _ := NewAuthority(nil, config)

	captureTime := time.Now().UTC()
	cameraTime := captureTime.Add(-2 * time.Minute) // 2 minutes behind, should reject

	result := authority.DetermineObservationTime(captureTime, &cameraTime)

	// Should use bridge clock, not camera
	if result.Source != SourceBridgeClock {
		t.Errorf("expected source SourceBridgeClock for significant drift, got %v", result.Source)
	}

	if result.Warning == nil {
		t.Error("expected warning for significant drift")
	}

	if result.Warning != nil && result.Warning.Code != "camera_clock_rejected" {
		t.Errorf("expected warning code 'camera_clock_rejected', got %q", result.Warning.Code)
	}
}

func TestAuthority_DetermineObservationTime_CameraInvalidTime(t *testing.T) {
	config := DefaultAuthorityConfig()
	config.Timezone = "UTC"
	config.CameraRejectDriftSeconds = 300

	authority, _ := NewAuthority(nil, config)

	captureTime := time.Now().UTC()
	cameraTime := captureTime.Add(-24 * time.Hour) // 24 hours behind, invalid

	result := authority.DetermineObservationTime(captureTime, &cameraTime)

	if result.Source != SourceBridgeClock {
		t.Errorf("expected source SourceBridgeClock for invalid time, got %v", result.Source)
	}

	if result.Warning == nil {
		t.Error("expected warning for invalid time")
	}

	if result.Warning != nil && result.Warning.Code != "camera_clock_invalid" {
		t.Errorf("expected warning code 'camera_clock_invalid', got %q", result.Warning.Code)
	}
}

func TestAuthority_DetermineObservationTime_CameraAhead(t *testing.T) {
	config := DefaultAuthorityConfig()
	config.Timezone = "UTC"
	config.CameraToleranceSeconds = 5
	config.CameraWarnDriftSeconds = 30

	authority, _ := NewAuthority(nil, config)

	captureTime := time.Now().UTC()
	cameraTime := captureTime.Add(20 * time.Second) // 20 seconds ahead, should warn

	result := authority.DetermineObservationTime(captureTime, &cameraTime)

	if result.Warning == nil {
		t.Error("expected warning for camera ahead")
	}
}

func TestAuthority_DetermineObservationTime_WithUnhealthyNTP(t *testing.T) {
	config := DefaultAuthorityConfig()

	// Create unhealthy time health
	thConfig := Config{
		Enabled:          true,
		MaxOffsetSeconds: 5,
	}
	timeHealth := NewTimeHealth(thConfig)
	// Note: timeHealth starts as unhealthy until first check

	authority, _ := NewAuthority(timeHealth, config)

	captureTime := time.Now().UTC()
	cameraTime := captureTime.Add(-1 * time.Second)

	result := authority.DetermineObservationTime(captureTime, &cameraTime)

	// Should still use bridge clock but with low confidence
	if result.Confidence != ConfidenceLow {
		t.Errorf("expected ConfidenceLow with unhealthy NTP, got %v", result.Confidence)
	}

	if result.Warning == nil {
		t.Error("expected warning for unhealthy NTP")
	}

	if result.Warning != nil && result.Warning.Code != "ntp_unhealthy" {
		t.Errorf("expected warning code 'ntp_unhealthy', got %q", result.Warning.Code)
	}
}

func TestAuthority_TimezoneConversion(t *testing.T) {
	config := DefaultAuthorityConfig()
	config.Timezone = "America/Los_Angeles" // PST = UTC-8
	config.CameraToleranceSeconds = 5

	authority, _ := NewAuthority(nil, config)

	// Simulate capture at 10:00:00 UTC
	captureTime := time.Date(2024, 12, 25, 10, 0, 0, 0, time.UTC)

	// Camera reports 02:00:00 (local time, which is PST = UTC-8)
	// This should convert to 10:00:00 UTC
	cameraLocalTime := time.Date(2024, 12, 25, 2, 0, 0, 0, time.UTC)

	result := authority.DetermineObservationTime(captureTime, &cameraLocalTime)

	// After conversion, should be within tolerance
	if result.Source != SourceCameraEXIF {
		t.Errorf("expected source SourceCameraEXIF after timezone conversion, got %v", result.Source)
	}

	// Verify the converted time is correct
	expectedUTC := time.Date(2024, 12, 25, 10, 0, 0, 0, time.UTC)
	if !result.Time.Equal(expectedUTC) {
		t.Errorf("expected converted time %v, got %v", expectedUTC, result.Time)
	}
}

func TestAuthority_GetTimeInfo(t *testing.T) {
	config := DefaultAuthorityConfig()
	config.Timezone = "America/New_York"

	authority, _ := NewAuthority(nil, config)

	info := authority.GetTimeInfo()

	if info.Timezone != "America/New_York" {
		t.Errorf("expected timezone 'America/New_York', got %q", info.Timezone)
	}

	if info.UTC == "" {
		t.Error("expected non-empty UTC time")
	}

	if info.Local == "" {
		t.Error("expected non-empty local time")
	}

	// TimezoneAbbrev should be EST or EDT depending on time of year
	if info.TimezoneAbbrev != "EST" && info.TimezoneAbbrev != "EDT" {
		t.Logf("timezone abbrev: %s (may vary by date)", info.TimezoneAbbrev)
	}
}

func TestAuthority_FormatTimes(t *testing.T) {
	config := DefaultAuthorityConfig()
	config.Timezone = "America/Los_Angeles"

	authority, _ := NewAuthority(nil, config)

	testTime := time.Date(2024, 12, 25, 16, 30, 0, 0, time.UTC)

	utcStr := authority.FormatUTCTime(testTime)
	if utcStr != "2024-12-25 16:30:00 UTC" {
		t.Errorf("expected '2024-12-25 16:30:00 UTC', got %q", utcStr)
	}

	localStr := authority.FormatLocalTime(testTime)
	// Should be 08:30:00 PST (UTC-8)
	if localStr != "2024-12-25 08:30:00 PST" {
		t.Errorf("expected '2024-12-25 08:30:00 PST', got %q", localStr)
	}
}

func TestAuthority_GetCurrentTimes(t *testing.T) {
	config := DefaultAuthorityConfig()
	config.Timezone = "UTC"

	authority, _ := NewAuthority(nil, config)

	local, utc := authority.GetCurrentTimes()

	// Local and UTC should be equal when timezone is UTC
	if !local.Equal(utc) {
		t.Errorf("expected local == utc for UTC timezone, got local=%v, utc=%v", local, utc)
	}
}

func TestDriftDirection(t *testing.T) {
	tests := []struct {
		drift    time.Duration
		expected string
	}{
		{5 * time.Second, "behind"},
		{-5 * time.Second, "ahead"},
		{0, "ahead"}, // Zero is treated as ahead (negative side)
	}

	for _, tc := range tests {
		result := driftDirection(tc.drift)
		if result != tc.expected {
			t.Errorf("driftDirection(%v) = %q, want %q", tc.drift, result, tc.expected)
		}
	}
}

func TestDefaultAuthorityConfig(t *testing.T) {
	config := DefaultAuthorityConfig()

	if config.CameraToleranceSeconds != 5 {
		t.Errorf("expected CameraToleranceSeconds 5, got %d", config.CameraToleranceSeconds)
	}

	if config.CameraWarnDriftSeconds != 30 {
		t.Errorf("expected CameraWarnDriftSeconds 30, got %d", config.CameraWarnDriftSeconds)
	}

	if config.CameraRejectDriftSeconds != 300 {
		t.Errorf("expected CameraRejectDriftSeconds 300, got %d", config.CameraRejectDriftSeconds)
	}
}
