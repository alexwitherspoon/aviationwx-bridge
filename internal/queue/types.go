package queue

import (
	"errors"
	"sync"
	"time"
)

// Errors
var (
	ErrQueueEmpty      = errors.New("queue is empty")
	ErrCapturePaused   = errors.New("capture is paused due to queue pressure")
	ErrQueueFull       = errors.New("queue is at maximum capacity")
	ErrInvalidImage    = errors.New("invalid image data")
	ErrFileTooLarge    = errors.New("file exceeds maximum size")
	ErrImageExpired    = errors.New("image exceeds maximum age")
	ErrImageFromFuture = errors.New("image timestamp is in the future")
)

// HealthLevel represents the current health state of a queue
type HealthLevel int

const (
	HealthHealthy    HealthLevel = iota // < 50% capacity
	HealthCatchingUp                    // 50-80% capacity
	HealthDegraded                      // 80-95% capacity
	HealthCritical                      // > 95% capacity
)

func (h HealthLevel) String() string {
	switch h {
	case HealthHealthy:
		return "healthy"
	case HealthCatchingUp:
		return "catching_up"
	case HealthDegraded:
		return "degraded"
	case HealthCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// QueuedImage represents an image waiting to be uploaded
type QueuedImage struct {
	Filename       string    // e.g., "1735142730000.jpg"
	Timestamp      time.Time // Observation time (UTC), parsed from filename
	FilePath       string    // Full path to file
	SizeBytes      int64     // File size in bytes
	TimeSource     string    // "camera_exif" or "bridge_clock"
	TimeConfidence string    // "high", "medium", "low"
}

// QueueState tracks the state of a single camera's queue
type QueueState struct {
	CameraID        string
	Directory       string
	ImageCount      int
	TotalSizeBytes  int64
	OldestTimestamp time.Time
	NewestTimestamp time.Time
	HealthLevel     HealthLevel
	CapturePaused   bool

	// Statistics
	ImagesQueued   int64 // Total ever queued
	ImagesUploaded int64 // Total successfully uploaded
	ImagesThinned  int64 // Total removed by thinning
	ImagesExpired  int64 // Total removed by age
}

// QueueConfig defines queue behavior for a single camera
type QueueConfig struct {
	// Capacity limits
	MaxFiles      int `json:"max_files"`       // Default: 100
	MaxSizeMB     int `json:"max_size_mb"`     // Default: 50
	MaxAgeSeconds int `json:"max_age_seconds"` // Default: 3600 (1 hour)

	// Thinning behavior
	ThinningEnabled bool `json:"thinning_enabled"` // Default: true
	ProtectNewest   int  `json:"protect_newest"`   // Default: 10
	ProtectOldest   int  `json:"protect_oldest"`   // Default: 5

	// Health thresholds (percentage of max)
	ThresholdCatchingUp float64 `json:"threshold_catching_up"` // Default: 0.50
	ThresholdDegraded   float64 `json:"threshold_degraded"`    // Default: 0.80
	ThresholdCritical   float64 `json:"threshold_critical"`    // Default: 0.95

	// Critical behavior
	PauseCaptureOnCritical bool    `json:"pause_capture_critical"` // Default: true
	ResumeThreshold        float64 `json:"resume_threshold"`       // Default: 0.70
}

// DefaultQueueConfig returns sensible defaults for queue configuration
func DefaultQueueConfig() QueueConfig {
	return QueueConfig{
		MaxFiles:               100,
		MaxSizeMB:              50,
		MaxAgeSeconds:          3600, // 1 hour
		ThinningEnabled:        true,
		ProtectNewest:          10,
		ProtectOldest:          5,
		ThresholdCatchingUp:    0.50,
		ThresholdDegraded:      0.80,
		ThresholdCritical:      0.95,
		PauseCaptureOnCritical: true,
		ResumeThreshold:        0.70,
	}
}

// GlobalQueueConfig defines global queue manager settings
type GlobalQueueConfig struct {
	BasePath           string  `json:"base_path"`            // Default: "/dev/shm/aviationwx"
	MaxTotalSizeMB     int     `json:"max_total_size_mb"`    // Default: 100 (all cameras)
	MemoryCheckSeconds int     `json:"memory_check_seconds"` // Default: 5
	EmergencyThinRatio float64 `json:"emergency_thin_ratio"` // Default: 0.5 (keep 50%)
	MaxHeapMB          int     `json:"max_heap_mb"`          // Default: 400 (for 512MB Pi)
}

// DefaultGlobalQueueConfig returns sensible defaults for global queue config
func DefaultGlobalQueueConfig() GlobalQueueConfig {
	return GlobalQueueConfig{
		BasePath:           "/dev/shm/aviationwx",
		MaxTotalSizeMB:     100,
		MemoryCheckSeconds: 5,
		EmergencyThinRatio: 0.5,
		MaxHeapMB:          400,
	}
}

// QueueStats provides statistics for monitoring
type QueueStats struct {
	CameraID        string  `json:"camera_id"`
	ImageCount      int     `json:"image_count"`
	TotalSizeMB     float64 `json:"total_size_mb"`
	OldestAge       string  `json:"oldest_age"`
	NewestAge       string  `json:"newest_age"`
	HealthLevel     string  `json:"health_level"`
	CapturePaused   bool    `json:"capture_paused"`
	CapacityPercent float64 `json:"capacity_percent"`
	ImagesQueued    int64   `json:"images_queued"`
	ImagesUploaded  int64   `json:"images_uploaded"`
	ImagesThinned   int64   `json:"images_thinned"`
	ImagesExpired   int64   `json:"images_expired"`
}

// GlobalQueueStats provides global statistics
type GlobalQueueStats struct {
	TotalImages      int          `json:"total_images"`
	TotalSizeMB      float64      `json:"total_size_mb"`
	CameraStats      []QueueStats `json:"camera_stats"`
	MemoryUsageMB    float64      `json:"memory_usage_mb"`
	MemoryLimitMB    int          `json:"memory_limit_mb"`
	FilesystemFreeMB float64      `json:"filesystem_free_mb"`
	FilesystemUsedMB float64      `json:"filesystem_used_mb"`
}

// Queue manages a single camera's image queue
type Queue struct {
	config QueueConfig
	state  QueueState
	mu     sync.RWMutex

	// Channels for coordination with capture goroutine
	pauseCapture  chan struct{}
	resumeCapture chan struct{}

	// Logger interface (optional)
	logger Logger
}

// Logger interface for dependency injection
type Logger interface {
	Debug(msg string, keysAndValues ...interface{})
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
}

// defaultLogger is a no-op logger
type defaultLogger struct{}

func (d *defaultLogger) Debug(msg string, keysAndValues ...interface{}) {}
func (d *defaultLogger) Info(msg string, keysAndValues ...interface{})  {}
func (d *defaultLogger) Warn(msg string, keysAndValues ...interface{})  {}
func (d *defaultLogger) Error(msg string, keysAndValues ...interface{}) {}
