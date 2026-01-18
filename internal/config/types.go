package config

// Config represents the root configuration structure
// Version 2 uses per-camera upload credentials
type Config struct {
	Version    int          `json:"version"`               // Config version, current: 2
	Timezone   string       `json:"timezone,omitempty"`    // IANA timezone, e.g., "America/Los_Angeles"
	Cameras    []Camera     `json:"cameras"`               // Camera configurations
	Global     *Global      `json:"global,omitempty"`      // Global settings
	Queue      *QueueGlobal `json:"queue,omitempty"`       // Queue settings
	SNTP       *SNTP        `json:"sntp,omitempty"`        // NTP time health settings
	WebConsole *WebConsole  `json:"web_console,omitempty"` // Web console settings

	// Deprecated: Use per-camera Upload instead
	Upload *Upload `json:"upload,omitempty"`
}

// Camera represents a camera configuration with its own upload credentials
type Camera struct {
	ID      string `json:"id"`      // Unique identifier (used for queue directory)
	Name    string `json:"name"`    // Display name
	Type    string `json:"type"`    // "http", "onvif", "rtsp"
	Enabled bool   `json:"enabled"` // Whether camera is active

	// Capture settings
	SnapshotURL            string `json:"snapshot_url,omitempty"`             // For HTTP type
	Auth                   *Auth  `json:"auth,omitempty"`                     // Camera authentication
	ONVIF                  *ONVIF `json:"onvif,omitempty"`                    // ONVIF settings
	RTSP                   *RTSP  `json:"rtsp,omitempty"`                     // RTSP settings
	CaptureIntervalSeconds int    `json:"capture_interval_seconds,omitempty"` // 1-1800, default 60

	// Image processing (bandwidth control)
	Image *ImageProcessing `json:"image,omitempty"` // Resolution/quality settings

	// Upload settings (per-camera FTP credentials)
	Upload *Upload `json:"upload"` // FTP credentials for this camera

	// Queue settings (optional, uses global defaults if not set)
	Queue *QueueCamera `json:"queue,omitempty"`

	// Deprecated fields
	RemotePath      string `json:"remote_path,omitempty"`      // Deprecated: always upload to root
	IntervalSeconds int    `json:"interval_seconds,omitempty"` // Deprecated: use CaptureIntervalSeconds
}

// ImageProcessing controls image resolution and quality for bandwidth management
// This is OPTIONAL - by default, images are uploaded exactly as received from the camera.
// Only configure this if you need to reduce bandwidth usage.
type ImageProcessing struct {
	// MaxWidth limits the image width (height scales proportionally)
	// 0 = no limit (use original resolution)
	// Common values: 1920 (1080p), 1280 (720p), 640 (480p)
	MaxWidth int `json:"max_width,omitempty"`

	// MaxHeight limits the image height (width scales proportionally)
	// 0 = no limit. If both MaxWidth and MaxHeight are set, image fits within both.
	MaxHeight int `json:"max_height,omitempty"`

	// Quality sets JPEG compression quality (1-100)
	// 0 = no re-encoding (use original)
	// Recommended: 70-90 for weather images if re-encoding is needed
	Quality int `json:"quality,omitempty"`
}

// Upload represents FTPS upload settings
type Upload struct {
	Host      string `json:"host"`                 // Default: upload.aviationwx.org
	Port      int    `json:"port,omitempty"`       // Default: 2121
	Username  string `json:"username"`             // FTP username (provided by aviationwx.org)
	Password  string `json:"password"`             // FTP password (provided by aviationwx.org)
	TLS       bool   `json:"tls,omitempty"`        // Default: true
	TLSVerify bool   `json:"tls_verify,omitempty"` // Default: true

	// Advanced settings (rarely needed)
	CABundlePath          string `json:"ca_bundle_path,omitempty"`
	TimeoutConnectSeconds int    `json:"timeout_connect_seconds,omitempty"` // Default: 30
	TimeoutUploadSeconds  int    `json:"timeout_upload_seconds,omitempty"`  // Default: 60
}

// DefaultUpload returns default upload settings
func DefaultUpload() Upload {
	return Upload{
		Host:                  "upload.aviationwx.org",
		Port:                  2121,
		TLS:                   true,
		TLSVerify:             true,
		TimeoutConnectSeconds: 30,
		TimeoutUploadSeconds:  60,
	}
}

// DefaultImageProcessing returns default image processing settings
// Default is NO processing - use the original webcam image as-is
func DefaultImageProcessing() ImageProcessing {
	return ImageProcessing{
		MaxWidth:  0, // No limit - use original
		MaxHeight: 0, // No limit - use original
		Quality:   0, // 0 = no re-encoding, use original
	}
}

// GetQuality returns the quality setting
// Returns 0 if no processing should be done (use original)
func (i *ImageProcessing) GetQuality() int {
	if i == nil {
		return 0 // No processing
	}
	if i.Quality < 0 {
		return 0
	}
	if i.Quality > 100 {
		return 100
	}
	return i.Quality
}

// NeedsProcessing returns true if any image processing is configured
func (i *ImageProcessing) NeedsProcessing() bool {
	if i == nil {
		return false
	}
	return i.MaxWidth > 0 || i.MaxHeight > 0 || i.Quality > 0
}

// Auth represents HTTP authentication for camera access
type Auth struct {
	Type     string `json:"type"` // "basic", "digest", "bearer"
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	Token    string `json:"token,omitempty"` // For bearer auth
}

// ONVIF represents ONVIF camera settings
type ONVIF struct {
	Endpoint     string `json:"endpoint"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	ProfileToken string `json:"profile_token,omitempty"`
}

// RTSP represents RTSP camera settings
type RTSP struct {
	URL       string `json:"url"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	Substream bool   `json:"substream,omitempty"`
}

// Global represents global settings
type Global struct {
	CaptureTimeoutSeconds int            `json:"capture_timeout_seconds,omitempty"` // Default: 30
	RTSPTimeoutSeconds    int            `json:"rtsp_timeout_seconds,omitempty"`    // Default: 10
	Backoff               *Backoff       `json:"backoff,omitempty"`
	DegradedMode          *DegradedMode  `json:"degraded_mode,omitempty"`
	TimeAuthority         *TimeAuthority `json:"time_authority,omitempty"`
}

// Backoff represents exponential backoff settings
type Backoff struct {
	InitialSeconds int     `json:"initial_seconds,omitempty"` // Default: 5
	MaxSeconds     int     `json:"max_seconds,omitempty"`     // Default: 300
	Multiplier     float64 `json:"multiplier,omitempty"`      // Default: 2.0
	Jitter         bool    `json:"jitter,omitempty"`          // Default: true
}

// DegradedMode represents degraded mode settings
type DegradedMode struct {
	Enabled                bool    `json:"enabled,omitempty"`                  // Default: true
	FailureThreshold       int     `json:"failure_threshold,omitempty"`        // Default: 3
	ConcurrencyLimit       int     `json:"concurrency_limit,omitempty"`        // Default: 1
	SlowIntervalMultiplier float64 `json:"slow_interval_multiplier,omitempty"` // Default: 2.0
}

// SNTP represents SNTP time health check settings
type SNTP struct {
	Enabled              bool     `json:"enabled,omitempty"`                // Default: true
	Servers              []string `json:"servers,omitempty"`                // Default: pool.ntp.org, time.google.com
	CheckIntervalSeconds int      `json:"check_interval_seconds,omitempty"` // Default: 300
	MaxOffsetSeconds     int      `json:"max_offset_seconds,omitempty"`     // Default: 5
	TimeoutSeconds       int      `json:"timeout_seconds,omitempty"`        // Default: 5
}

// DefaultSNTP returns default SNTP settings
func DefaultSNTP() SNTP {
	return SNTP{
		Enabled:              true,
		Servers:              []string{"pool.ntp.org", "time.google.com"},
		CheckIntervalSeconds: 300, // 5 minutes
		MaxOffsetSeconds:     5,
		TimeoutSeconds:       5,
	}
}

// WebConsole represents web console settings
type WebConsole struct {
	Enabled  bool   `json:"enabled,omitempty"`  // Default: true
	Port     int    `json:"port,omitempty"`     // Default: 1229
	Password string `json:"password,omitempty"` // Default: "aviationwx"

	// Deprecated: use Password instead
	BasicAuth *BasicAuth `json:"basic_auth,omitempty"`
}

// DefaultWebConsole returns default web console settings
func DefaultWebConsole() WebConsole {
	return WebConsole{
		Enabled:  true,
		Port:     1229,
		Password: "aviationwx",
	}
}

// BasicAuth represents basic authentication settings (deprecated)
type BasicAuth struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// QueueGlobal represents global queue manager settings
type QueueGlobal struct {
	BasePath           string       `json:"base_path,omitempty"`            // Default: "/dev/shm/aviationwx"
	MaxTotalSizeMB     int          `json:"max_total_size_mb,omitempty"`    // Default: 100 (all cameras)
	MemoryCheckSeconds int          `json:"memory_check_seconds,omitempty"` // Default: 5
	EmergencyThinRatio float64      `json:"emergency_thin_ratio,omitempty"` // Default: 0.5
	MaxHeapMB          int          `json:"max_heap_mb,omitempty"`          // Default: 400 (for 512MB Pi)
	Defaults           *QueueCamera `json:"defaults,omitempty"`             // Default settings for cameras
}

// QueueCamera represents per-camera queue settings
type QueueCamera struct {
	Enabled                bool    `json:"enabled,omitempty"`                // Default: true
	MaxFiles               int     `json:"max_files,omitempty"`              // Default: 100
	MaxSizeMB              int     `json:"max_size_mb,omitempty"`            // Default: 50
	MaxAgeSeconds          int     `json:"max_age_seconds,omitempty"`        // Default: 3600 (1 hour)
	ThinningEnabled        bool    `json:"thinning_enabled,omitempty"`       // Default: true
	ProtectNewest          int     `json:"protect_newest,omitempty"`         // Default: 10
	ProtectOldest          int     `json:"protect_oldest,omitempty"`         // Default: 5
	ThresholdCatchingUp    float64 `json:"threshold_catching_up,omitempty"`  // Default: 0.50
	ThresholdDegraded      float64 `json:"threshold_degraded,omitempty"`     // Default: 0.80
	ThresholdCritical      float64 `json:"threshold_critical,omitempty"`     // Default: 0.95
	PauseCaptureOnCritical bool    `json:"pause_capture_critical,omitempty"` // Default: true
	ResumeThreshold        float64 `json:"resume_threshold,omitempty"`       // Default: 0.70
}

// TimeAuthority represents time authority settings
type TimeAuthority struct {
	CameraToleranceSeconds   int `json:"camera_tolerance_seconds,omitempty"`    // Default: 5
	CameraWarnDriftSeconds   int `json:"camera_warn_drift_seconds,omitempty"`   // Default: 30
	CameraRejectDriftSeconds int `json:"camera_reject_drift_seconds,omitempty"` // Default: 300
}

// IsFirstRun returns true if this appears to be an unconfigured installation
func (c *Config) IsFirstRun() bool {
	return len(c.Cameras) == 0
}

// GetWebPassword returns the web console password with fallback to default
func (c *Config) GetWebPassword() string {
	if c.WebConsole == nil {
		return "aviationwx"
	}
	if c.WebConsole.Password != "" {
		return c.WebConsole.Password
	}
	// Check deprecated BasicAuth
	if c.WebConsole.BasicAuth != nil && c.WebConsole.BasicAuth.Password != "" {
		return c.WebConsole.BasicAuth.Password
	}
	return "aviationwx"
}

// GetWebPort returns the web console port with fallback to default
func (c *Config) GetWebPort() int {
	if c.WebConsole == nil || c.WebConsole.Port == 0 {
		return 1229
	}
	return c.WebConsole.Port
}
