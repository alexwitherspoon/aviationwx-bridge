package time

import (
	"fmt"
	"time"
)

// TimeSource indicates where the observation time came from
type TimeSource string

const (
	SourceCameraEXIF  TimeSource = "camera_exif"
	SourceBridgeClock TimeSource = "bridge_clock"
)

// Confidence indicates how confident we are in the observation time
type Confidence string

const (
	ConfidenceHigh   Confidence = "high"   // NTP healthy, time trusted
	ConfidenceMedium Confidence = "medium" // Minor uncertainty
	ConfidenceLow    Confidence = "low"    // NTP unhealthy or large drift
)

// ObservationResult contains the determined observation time and metadata
type ObservationResult struct {
	Time       time.Time    // Observation time in UTC
	Source     TimeSource   // Where the time came from
	Confidence Confidence   // How confident we are
	Warning    *TimeWarning // Optional warning about time issues
}

// TimeWarning describes a time-related warning
type TimeWarning struct {
	Code    string                 // Machine-readable code
	Message string                 // Human-readable message
	Details map[string]interface{} // Additional details
}

// AuthorityConfig configures the time authority behavior
type AuthorityConfig struct {
	// Timezone for the bridge location (IANA format, e.g., "America/Los_Angeles")
	Timezone string `json:"timezone"`

	// Thresholds for camera clock validation
	CameraToleranceSeconds   int `json:"camera_tolerance_seconds"`    // Default: 5
	CameraWarnDriftSeconds   int `json:"camera_warn_drift_seconds"`   // Default: 30
	CameraRejectDriftSeconds int `json:"camera_reject_drift_seconds"` // Default: 300 (5 min)
}

// DefaultAuthorityConfig returns sensible defaults
func DefaultAuthorityConfig() AuthorityConfig {
	return AuthorityConfig{
		Timezone:                 "", // Use system timezone
		CameraToleranceSeconds:   5,
		CameraWarnDriftSeconds:   30,
		CameraRejectDriftSeconds: 300,
	}
}

// Authority determines observation times based on camera EXIF and bridge clock
type Authority struct {
	ntpHealth *TimeHealth
	localTZ   *time.Location
	config    AuthorityConfig
}

// NewAuthority creates a new time authority
func NewAuthority(ntpHealth *TimeHealth, config AuthorityConfig) (*Authority, error) {
	var localTZ *time.Location
	var err error

	if config.Timezone != "" {
		localTZ, err = time.LoadLocation(config.Timezone)
		if err != nil {
			return nil, fmt.Errorf("load timezone %q: %w", config.Timezone, err)
		}
	} else {
		localTZ = time.Local
	}

	return &Authority{
		ntpHealth: ntpHealth,
		localTZ:   localTZ,
		config:    config,
	}, nil
}

// DetermineObservationTime determines the observation time for a captured image
// captureStartUTC is when the bridge started the capture request
// cameraEXIF is the parsed EXIF time from the camera (may be nil)
func (a *Authority) DetermineObservationTime(captureStartUTC time.Time, cameraEXIF *time.Time) ObservationResult {
	result := ObservationResult{
		Time: captureStartUTC, // Default to bridge clock
	}

	// Check bridge NTP health first
	ntpHealthy := true
	if a.ntpHealth != nil {
		ntpHealthy = a.ntpHealth.IsHealthy()
	}

	if !ntpHealthy {
		// Bridge time is uncertain - warn user
		result.Source = SourceBridgeClock
		result.Confidence = ConfidenceLow

		var offsetMs int64
		if a.ntpHealth != nil {
			offsetMs = a.ntpHealth.GetOffset().Milliseconds()
		}

		result.Warning = &TimeWarning{
			Code:    "ntp_unhealthy",
			Message: "Bridge NTP is not synchronized. Observation times may be inaccurate.",
			Details: map[string]interface{}{
				"ntp_offset_ms": offsetMs,
			},
		}
		return result
	}

	// Bridge NTP is healthy - we are the authority
	result.Confidence = ConfidenceHigh

	// No camera EXIF? Use bridge clock
	if cameraEXIF == nil {
		result.Source = SourceBridgeClock
		return result
	}

	// Convert camera EXIF (local time) to UTC
	// Camera EXIF comes as a naive time (no timezone), interpret it in bridge's timezone
	cameraLocalTime := time.Date(
		cameraEXIF.Year(), cameraEXIF.Month(), cameraEXIF.Day(),
		cameraEXIF.Hour(), cameraEXIF.Minute(), cameraEXIF.Second(),
		cameraEXIF.Nanosecond(), a.localTZ,
	)
	cameraUTC := cameraLocalTime.UTC()

	// Calculate drift between camera and bridge
	drift := captureStartUTC.Sub(cameraUTC)
	absDrift := drift
	if absDrift < 0 {
		absDrift = -absDrift
	}

	cameraTolerance := time.Duration(a.config.CameraToleranceSeconds) * time.Second
	cameraWarnDrift := time.Duration(a.config.CameraWarnDriftSeconds) * time.Second
	cameraRejectDrift := time.Duration(a.config.CameraRejectDriftSeconds) * time.Second

	// Decision based on drift magnitude
	switch {
	case absDrift <= cameraTolerance:
		// Camera is within tolerance - use camera EXIF (more precise)
		result.Time = cameraUTC
		result.Source = SourceCameraEXIF
		result.Confidence = ConfidenceHigh

	case absDrift <= cameraWarnDrift:
		// Minor drift - use camera but warn
		result.Time = cameraUTC
		result.Source = SourceCameraEXIF
		result.Confidence = ConfidenceHigh
		result.Warning = &TimeWarning{
			Code:    "camera_clock_drift",
			Message: fmt.Sprintf("Camera clock is %.0f seconds %s. Consider adjusting camera time.", absDrift.Seconds(), driftDirection(drift)),
			Details: map[string]interface{}{
				"drift_seconds": drift.Seconds(),
				"camera_time":   cameraUTC.Format(time.RFC3339),
				"bridge_time":   captureStartUTC.Format(time.RFC3339),
			},
		}

	case absDrift <= cameraRejectDrift:
		// Significant drift - use bridge, warn strongly
		result.Time = captureStartUTC
		result.Source = SourceBridgeClock
		result.Confidence = ConfidenceHigh
		result.Warning = &TimeWarning{
			Code:    "camera_clock_rejected",
			Message: fmt.Sprintf("Camera clock is %.1f minutes %s. Using bridge time instead.", absDrift.Minutes(), driftDirection(drift)),
			Details: map[string]interface{}{
				"drift_seconds": drift.Seconds(),
				"camera_time":   cameraUTC.Format(time.RFC3339),
				"bridge_time":   captureStartUTC.Format(time.RFC3339),
				"action":        "using_bridge_time",
			},
		}

	default:
		// Extreme drift (wrong date) - use bridge, error
		result.Time = captureStartUTC
		result.Source = SourceBridgeClock
		result.Confidence = ConfidenceHigh
		result.Warning = &TimeWarning{
			Code:    "camera_clock_invalid",
			Message: fmt.Sprintf("Camera clock is incorrect (off by %.1f hours). Using bridge time.", absDrift.Hours()),
			Details: map[string]interface{}{
				"drift_hours": absDrift.Hours(),
				"camera_time": cameraUTC.Format(time.RFC3339),
				"bridge_time": captureStartUTC.Format(time.RFC3339),
				"action":      "using_bridge_time",
			},
		}
	}

	return result
}

// GetTimezone returns the configured timezone
func (a *Authority) GetTimezone() *time.Location {
	return a.localTZ
}

// GetTimezoneName returns the timezone name
func (a *Authority) GetTimezoneName() string {
	return a.localTZ.String()
}

// GetConfiguredTimezone returns the IANA timezone string from bridge config (may be empty).
func (a *Authority) GetConfiguredTimezone() string {
	if a == nil {
		return ""
	}
	return a.config.Timezone
}

// FormatLocalTime formats a UTC time in the local timezone
func (a *Authority) FormatLocalTime(t time.Time) string {
	return t.In(a.localTZ).Format("2006-01-02 15:04:05 MST")
}

// FormatUTCTime formats a time in UTC
func (a *Authority) FormatUTCTime(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}

// GetCurrentTimes returns current time in both local and UTC
func (a *Authority) GetCurrentTimes() (local, utc time.Time) {
	now := time.Now()
	return now.In(a.localTZ), now.UTC()
}

// IsNTPHealthy returns whether NTP is healthy
// If NTP checking is not configured (nil), we assume system time is trusted
func (a *Authority) IsNTPHealthy() bool {
	if a.ntpHealth == nil {
		return true // No NTP checking configured, trust system time
	}
	return a.ntpHealth.IsHealthy()
}

// GetNTPOffset returns the current NTP offset
func (a *Authority) GetNTPOffset() time.Duration {
	if a.ntpHealth == nil {
		return 0
	}
	return a.ntpHealth.GetOffset()
}

func driftDirection(drift time.Duration) string {
	if drift > 0 {
		return "behind"
	}
	return "ahead"
}

// TimeInfo provides current time information for the web UI
type TimeInfo struct {
	UTC            string `json:"utc"`
	Local          string `json:"local"`
	Timezone       string `json:"timezone"`
	TimezoneAbbrev string `json:"timezone_abbrev"`
	UTCOffset      string `json:"utc_offset"`
	DSTActive      bool   `json:"dst_active"`
	TimeHealthy    bool   `json:"time_healthy"`
	NTPOffsetMs    int64  `json:"ntp_offset_ms"`
}

// GetTimeInfo returns current time information for the web UI
func (a *Authority) GetTimeInfo() TimeInfo {
	now := time.Now()
	local := now.In(a.localTZ)

	// Get timezone abbreviation and offset
	abbrev, offset := local.Zone()
	offsetHours := offset / 3600
	offsetMins := (offset % 3600) / 60
	offsetStr := fmt.Sprintf("%+03d:%02d", offsetHours, absInt(offsetMins))

	// Check if DST is active (compare with standard time)
	_, standardOffset := time.Date(local.Year(), time.January, 1, 0, 0, 0, 0, a.localTZ).Zone()
	dstActive := offset != standardOffset

	var ntpOffsetMs int64
	timeHealthy := true
	if a.ntpHealth != nil {
		timeHealthy = a.ntpHealth.IsHealthy()
		ntpOffsetMs = a.ntpHealth.GetOffset().Milliseconds()
	}

	return TimeInfo{
		UTC:            now.UTC().Format(time.RFC3339),
		Local:          local.Format(time.RFC3339),
		Timezone:       a.localTZ.String(),
		TimezoneAbbrev: abbrev,
		UTCOffset:      offsetStr,
		DSTActive:      dstActive,
		TimeHealthy:    timeHealthy,
		NTPOffsetMs:    ntpOffsetMs,
	}
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
