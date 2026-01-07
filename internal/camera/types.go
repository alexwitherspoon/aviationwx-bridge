package camera

import (
	"context"
	"time"
)

// Camera defines the interface for camera capture backends
type Camera interface {
	// Capture fetches a fresh snapshot from the camera
	// Returns image data and any error encountered
	// Must always return fresh data - never cached or stale images
	Capture(ctx context.Context) ([]byte, error)

	// ID returns the camera identifier
	ID() string

	// Type returns the camera type ("http", "onvif", "rtsp")
	Type() string
}

// Config represents camera configuration
type Config struct {
	ID             string
	Name           string
	Type           string
	SnapshotURL    string
	Auth           *AuthConfig
	ONVIF          *ONVIFConfig
	RTSP           *RTSPConfig
	TimeoutSeconds int
}

// AuthConfig represents HTTP authentication configuration
type AuthConfig struct {
	Type     string // "basic", "digest", "bearer"
	Username string
	Password string
	Token    string
}

// ONVIFConfig represents ONVIF camera configuration
type ONVIFConfig struct {
	Endpoint     string
	Username     string
	Password     string
	ProfileToken string
}

// RTSPConfig represents RTSP camera configuration
type RTSPConfig struct {
	URL       string
	Username  string
	Password  string
	Substream bool
}

// Error types for camera operations
type (
	// TimeoutError indicates a capture operation timed out
	TimeoutError struct {
		CameraID string
		Timeout  time.Duration
	}

	// AuthError indicates authentication failed
	AuthError struct {
		CameraID string
		Message  string
	}

	// CaptureError indicates a general capture failure
	CaptureError struct {
		CameraID string
		Message  string
		Err      error
	}
)

func (e *TimeoutError) Error() string {
	return "capture timeout: " + e.CameraID
}

func (e *AuthError) Error() string {
	return "authentication failed: " + e.CameraID + ": " + e.Message
}

func (e *CaptureError) Error() string {
	if e.Err != nil {
		return "capture failed: " + e.CameraID + ": " + e.Message + ": " + e.Err.Error()
	}
	return "capture failed: " + e.CameraID + ": " + e.Message
}

func (e *CaptureError) Unwrap() error {
	return e.Err
}
