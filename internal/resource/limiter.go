// Package resource provides resource management and limiting for background work
// to protect interactive/admin UX on resource-constrained devices like Pi Zero 2 W.
package resource

import (
	"context"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Limiter manages resource allocation for background vs interactive work.
// It uses semaphores to limit concurrent CPU-intensive operations and
// adaptive throttling to slow down under system pressure.
type Limiter struct {
	// Semaphores for different work types (channel-based for stdlib compatibility)
	imageProcessing chan struct{}
	exifOperations  chan struct{}

	// Adaptive throttling state
	mu              sync.RWMutex
	lastCheck       time.Time
	currentPressure float64

	// Configuration
	config Config

	// Statistics
	imageAcquireCount   atomic.Int64
	imageWaitTimeNs     atomic.Int64
	exifAcquireCount    atomic.Int64
	exifWaitTimeNs      atomic.Int64
	throttleDelayCount  atomic.Int64
	throttleDelayTimeNs atomic.Int64
}

// Config configures the resource limiter
type Config struct {
	// MaxConcurrentImageProcessing limits concurrent image decode/resize/encode operations
	// Default: max(1, NumCPU/2) to leave CPU for web UI
	MaxConcurrentImageProcessing int

	// MaxConcurrentExifOperations limits concurrent exiftool subprocesses
	// Default: 1 (exiftool is heavy and serializing prevents thrashing)
	MaxConcurrentExifOperations int

	// MemoryPressureThresholdMB is the heap size above which throttling kicks in
	// Default: 200MB (suitable for Pi Zero 2 W with 512MB total)
	MemoryPressureThresholdMB int

	// GoroutinePressureThreshold is the count above which throttling kicks in
	// Default: 100
	GoroutinePressureThreshold int

	// MaxThrottleDelay is the maximum delay added under extreme pressure
	// Default: 2 seconds
	MaxThrottleDelay time.Duration

	// PressureCheckInterval is how often to recalculate system pressure
	// Default: 1 second
	PressureCheckInterval time.Duration
}

// DefaultConfig returns sensible defaults for Pi Zero 2 W
func DefaultConfig() Config {
	numCPU := runtime.NumCPU()
	totalMemoryMB := getTotalMemoryMB()

	// On devices with < 1GB RAM (like Pi Zero 2 W with 512MB),
	// serialize image processing to prevent memory exhaustion
	maxImageProcessing := 1
	if totalMemoryMB >= 1024 {
		maxImageProcessing = max(1, numCPU/2)
	}

	return Config{
		MaxConcurrentImageProcessing: maxImageProcessing,
		MaxConcurrentExifOperations:  1,
		MemoryPressureThresholdMB:    200,
		GoroutinePressureThreshold:   100,
		MaxThrottleDelay:             2 * time.Second,
		PressureCheckInterval:        time.Second,
	}
}

// NewLimiter creates a new resource limiter with the given configuration
func NewLimiter(cfg Config) *Limiter {
	// Apply defaults for zero values
	if cfg.MaxConcurrentImageProcessing <= 0 {
		cfg.MaxConcurrentImageProcessing = max(1, runtime.NumCPU()/2)
	}
	if cfg.MaxConcurrentExifOperations <= 0 {
		cfg.MaxConcurrentExifOperations = 1
	}
	if cfg.MemoryPressureThresholdMB <= 0 {
		cfg.MemoryPressureThresholdMB = 200
	}
	if cfg.GoroutinePressureThreshold <= 0 {
		cfg.GoroutinePressureThreshold = 100
	}
	if cfg.MaxThrottleDelay <= 0 {
		cfg.MaxThrottleDelay = 2 * time.Second
	}
	if cfg.PressureCheckInterval <= 0 {
		cfg.PressureCheckInterval = time.Second
	}

	return &Limiter{
		imageProcessing: make(chan struct{}, cfg.MaxConcurrentImageProcessing),
		exifOperations:  make(chan struct{}, cfg.MaxConcurrentExifOperations),
		config:          cfg,
	}
}

// DefaultLimiter creates a limiter with default configuration
func DefaultLimiter() *Limiter {
	return NewLimiter(DefaultConfig())
}

// AcquireImageProcessing blocks until a slot is available for image processing.
// Returns an error if the context is cancelled while waiting.
// Caller must call ReleaseImageProcessing when done.
func (l *Limiter) AcquireImageProcessing(ctx context.Context) error {
	start := time.Now()
	defer func() {
		l.imageAcquireCount.Add(1)
		l.imageWaitTimeNs.Add(time.Since(start).Nanoseconds())
	}()

	select {
	case l.imageProcessing <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TryAcquireImageProcessing attempts to acquire without blocking.
// Returns true if acquired, false if no slots available.
func (l *Limiter) TryAcquireImageProcessing() bool {
	select {
	case l.imageProcessing <- struct{}{}:
		l.imageAcquireCount.Add(1)
		return true
	default:
		return false
	}
}

// ReleaseImageProcessing releases an image processing slot
func (l *Limiter) ReleaseImageProcessing() {
	<-l.imageProcessing
}

// AcquireExifOperation blocks until a slot is available for exiftool operations.
// Returns an error if the context is cancelled while waiting.
// Caller must call ReleaseExifOperation when done.
func (l *Limiter) AcquireExifOperation(ctx context.Context) error {
	start := time.Now()
	defer func() {
		l.exifAcquireCount.Add(1)
		l.exifWaitTimeNs.Add(time.Since(start).Nanoseconds())
	}()

	select {
	case l.exifOperations <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TryAcquireExifOperation attempts to acquire without blocking.
// Returns true if acquired, false if no slots available.
func (l *Limiter) TryAcquireExifOperation() bool {
	select {
	case l.exifOperations <- struct{}{}:
		l.exifAcquireCount.Add(1)
		return true
	default:
		return false
	}
}

// ReleaseExifOperation releases an exiftool operation slot
func (l *Limiter) ReleaseExifOperation() {
	<-l.exifOperations
}

// GetThrottleDelay returns a delay duration based on current system pressure.
// Background workers should sleep for this duration before starting heavy work.
// Returns 0 if system is healthy.
func (l *Limiter) GetThrottleDelay() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Only recalculate periodically
	if time.Since(l.lastCheck) < l.config.PressureCheckInterval {
		delay := time.Duration(l.currentPressure * float64(l.config.MaxThrottleDelay))
		if delay > 0 {
			l.throttleDelayCount.Add(1)
			l.throttleDelayTimeNs.Add(delay.Nanoseconds())
		}
		return delay
	}

	// Calculate current pressure (0.0 - 1.0)
	pressure := l.calculatePressure()
	l.currentPressure = pressure
	l.lastCheck = time.Now()

	delay := time.Duration(pressure * float64(l.config.MaxThrottleDelay))
	if delay > 0 {
		l.throttleDelayCount.Add(1)
		l.throttleDelayTimeNs.Add(delay.Nanoseconds())
	}
	return delay
}

// calculatePressure returns a value from 0.0 (healthy) to 1.0 (critical)
func (l *Limiter) calculatePressure() float64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	heapMB := float64(m.HeapAlloc) / (1024 * 1024)
	goroutines := float64(runtime.NumGoroutine())

	pressure := 0.0

	// Memory pressure contribution (up to 0.5)
	memThreshold := float64(l.config.MemoryPressureThresholdMB)
	if heapMB > memThreshold {
		memPressure := (heapMB - memThreshold) / memThreshold
		if memPressure > 0.5 {
			memPressure = 0.5
		}
		pressure += memPressure
	}

	// Goroutine pressure contribution (up to 0.5)
	goThreshold := float64(l.config.GoroutinePressureThreshold)
	if goroutines > goThreshold {
		goPressure := (goroutines - goThreshold) / goThreshold
		if goPressure > 0.5 {
			goPressure = 0.5
		}
		pressure += goPressure
	}

	if pressure > 1.0 {
		pressure = 1.0
	}

	return pressure
}

// GetPressure returns the current system pressure (0.0 - 1.0)
func (l *Limiter) GetPressure() float64 {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.currentPressure
}

// IsUnderPressure returns true if the system is under significant pressure
func (l *Limiter) IsUnderPressure() bool {
	return l.GetPressure() > 0.3
}

// Stats holds resource limiter statistics
type Stats struct {
	// Configuration
	MaxImageProcessing int `json:"max_image_processing"`
	MaxExifOperations  int `json:"max_exif_operations"`

	// Current state
	ImageProcessingInUse int     `json:"image_processing_in_use"`
	ExifOperationsInUse  int     `json:"exif_operations_in_use"`
	CurrentPressure      float64 `json:"current_pressure"`
	IsUnderPressure      bool    `json:"is_under_pressure"`

	// Cumulative stats
	ImageAcquireCount  int64         `json:"image_acquire_count"`
	ImageTotalWaitTime time.Duration `json:"image_total_wait_time"`
	ExifAcquireCount   int64         `json:"exif_acquire_count"`
	ExifTotalWaitTime  time.Duration `json:"exif_total_wait_time"`
	ThrottleDelayCount int64         `json:"throttle_delay_count"`
	ThrottleTotalDelay time.Duration `json:"throttle_total_delay"`

	// System info
	NumCPU        int     `json:"num_cpu"`
	NumGoroutines int     `json:"num_goroutines"`
	HeapAllocMB   float64 `json:"heap_alloc_mb"`
}

// GetStats returns current limiter statistics
func (l *Limiter) GetStats() Stats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	l.mu.RLock()
	pressure := l.currentPressure
	l.mu.RUnlock()

	return Stats{
		MaxImageProcessing:   l.config.MaxConcurrentImageProcessing,
		MaxExifOperations:    l.config.MaxConcurrentExifOperations,
		ImageProcessingInUse: len(l.imageProcessing),
		ExifOperationsInUse:  len(l.exifOperations),
		CurrentPressure:      pressure,
		IsUnderPressure:      pressure > 0.3,
		ImageAcquireCount:    l.imageAcquireCount.Load(),
		ImageTotalWaitTime:   time.Duration(l.imageWaitTimeNs.Load()),
		ExifAcquireCount:     l.exifAcquireCount.Load(),
		ExifTotalWaitTime:    time.Duration(l.exifWaitTimeNs.Load()),
		ThrottleDelayCount:   l.throttleDelayCount.Load(),
		ThrottleTotalDelay:   time.Duration(l.throttleDelayTimeNs.Load()),
		NumCPU:               runtime.NumCPU(),
		NumGoroutines:        runtime.NumGoroutine(),
		HeapAllocMB:          float64(m.HeapAlloc) / (1024 * 1024),
	}
}

// YieldToHigherPriority yields the current goroutine to allow other goroutines
// (like web handlers) to run. Call this periodically in tight loops.
func YieldToHigherPriority() {
	runtime.Gosched()
}

// max returns the larger of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// getTotalMemoryMB returns the total system memory in MB by reading /proc/meminfo.
// Returns 0 if unable to detect (will default to safe low-memory behavior).
func getTotalMemoryMB() int {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}

	// Parse MemTotal from /proc/meminfo
	// Example line: "MemTotal:         416768 kB"
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				memKB, err := strconv.ParseInt(fields[1], 10, 64)
				if err == nil {
					return int(memKB / 1024) // Convert KB to MB
				}
			}
		}
	}

	return 0
}
