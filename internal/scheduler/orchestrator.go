package scheduler

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/camera"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/queue"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/resource"
	timepkg "github.com/alexwitherspoon/AviationWX.org-Bridge/internal/time"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/upload"
)

// Orchestrator manages capture workers, upload worker, and queues
type Orchestrator struct {
	// Components
	queueManager    *queue.Manager
	captureWorkers  map[string]*CaptureWorker
	uploadWorker    *UploadWorker
	authority       *timepkg.Authority
	exifHelper      *timepkg.ExifToolHelper
	resourceLimiter *resource.Limiter

	// Configuration
	config OrchestratorConfig

	// State
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.RWMutex
	logger Logger

	// Start time for uptime tracking
	startTime time.Time
}

// OrchestratorConfig configures the orchestrator
type OrchestratorConfig struct {
	// Queue settings
	QueueBasePath   string // Default: /dev/shm/aviationwx
	QueueMaxTotalMB int    // Default: 100
	QueueMaxHeapMB  int    // Default: 400

	// Time settings
	Timezone string // IANA timezone, e.g., "America/Los_Angeles"

	// Upload settings
	MinUploadInterval    time.Duration // Default: 1 second
	AuthBackoffSecs      int           // Default: 60
	MaxConcurrentUploads int           // Default: 2 (conservative for slow networks)

	// Resource management
	ResourceLimiter *resource.Limiter // Optional: limits concurrent CPU-intensive work

	// Logger
	Logger Logger
}

// DefaultOrchestratorConfig returns sensible defaults
func DefaultOrchestratorConfig() OrchestratorConfig {
	return OrchestratorConfig{
		QueueBasePath:        "/dev/shm/aviationwx",
		QueueMaxTotalMB:      100,
		QueueMaxHeapMB:       400,
		MinUploadInterval:    time.Second,
		AuthBackoffSecs:      60,
		MaxConcurrentUploads: 2, // Conservative for slow networks
	}
}

// NewOrchestrator creates a new orchestrator
func NewOrchestrator(config OrchestratorConfig) (*Orchestrator, error) {
	ctx, cancel := context.WithCancel(context.Background())

	logger := config.Logger
	if logger == nil {
		logger = &defaultLogger{}
	}

	// Create queue manager
	queueConfig := queue.GlobalQueueConfig{
		BasePath:           config.QueueBasePath,
		MaxTotalSizeMB:     config.QueueMaxTotalMB,
		MaxHeapMB:          config.QueueMaxHeapMB,
		MemoryCheckSeconds: 5,
		EmergencyThinRatio: 0.5,
	}

	queueManager, err := queue.NewManager(queueConfig, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create queue manager: %w", err)
	}

	// Create time authority
	var authority *timepkg.Authority
	authorityConfig := timepkg.DefaultAuthorityConfig()
	if config.Timezone != "" {
		authorityConfig.Timezone = config.Timezone
	}

	authority, err = timepkg.NewAuthority(nil, authorityConfig)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create time authority: %w", err)
	}

	// Try to create exiftool helper (optional, may not be available)
	var exifHelper *timepkg.ExifToolHelper
	exifHelper, err = timepkg.DefaultExifToolHelper()
	if err != nil {
		logger.Warn("exiftool not available, camera EXIF reading disabled",
			"error", err)
		exifHelper = nil
	} else {
		version, _ := exifHelper.GetVersion()
		logger.Info("exiftool available", "version", version)
	}

	// Use provided resource limiter or create a default one
	resourceLimiter := config.ResourceLimiter
	if resourceLimiter == nil {
		resourceLimiter = resource.DefaultLimiter()
		logger.Info("Using default resource limiter")
	}

	return &Orchestrator{
		queueManager:    queueManager,
		captureWorkers:  make(map[string]*CaptureWorker),
		authority:       authority,
		exifHelper:      exifHelper,
		resourceLimiter: resourceLimiter,
		config:          config,
		ctx:             ctx,
		cancel:          cancel,
		logger:          logger,
	}, nil
}

// AddCamera adds a camera to the orchestrator
func (o *Orchestrator) AddCamera(cam camera.Camera, config CameraConfig, intervalSecs int, uploader upload.Client, onCapture func(cameraID string, imageData []byte, captureTime time.Time)) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	cameraID := cam.ID()

	// Create queue for this camera
	queueConfig := queue.DefaultQueueConfig()
	q, err := o.queueManager.CreateQueue(cameraID, queueConfig)
	if err != nil {
		return fmt.Errorf("create queue for camera %s: %w", cameraID, err)
	}

	// Create capture worker
	workerConfig := CaptureWorkerConfig{
		Camera:          cam,
		CameraConfig:    config,
		Queue:           q,
		Authority:       o.authority,
		ExifHelper:      o.exifHelper,
		ResourceLimiter: o.resourceLimiter,
		IntervalSecs:    intervalSecs,
		Logger:          o.logger,
		OnCapture:       onCapture,
	}

	worker := NewCaptureWorker(workerConfig)
	o.captureWorkers[cameraID] = worker

	// Create upload worker if it doesn't exist yet
	if o.uploadWorker == nil {
		maxConcurrent := 2 // Default
		if o.config.MaxConcurrentUploads > 0 {
			maxConcurrent = o.config.MaxConcurrentUploads
		}

		uploadConfig := UploadWorkerConfig{
			MinUploadInterval: o.config.MinUploadInterval,
			AuthBackoff:       time.Duration(o.config.AuthBackoffSecs) * time.Second,
			RetryDelay:        5 * time.Second,
			MaxConcurrent:     maxConcurrent,
			Logger:            o.logger,
		}
		o.uploadWorker = NewUploadWorker(uploadConfig)
	}

	// Add queue with camera-specific uploader
	o.uploadWorker.AddQueue(cameraID, q, config, uploader)

	// If orchestrator has already been started, start this worker immediately
	if !o.startTime.IsZero() {
		worker.Start()
		o.logger.Info("Capture worker started (hot-reload)", "camera", cameraID)
	}

	o.logger.Info("Camera added",
		"camera", cameraID,
		"interval_secs", intervalSecs)

	return nil
}

// RemoveCamera removes a camera from the orchestrator
func (o *Orchestrator) RemoveCamera(cameraID string) error {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Stop and remove capture worker
	if worker, ok := o.captureWorkers[cameraID]; ok {
		worker.Stop()
		delete(o.captureWorkers, cameraID)
		o.logger.Info("Capture worker stopped", "camera", cameraID)
	}

	// Remove queue from upload worker
	if o.uploadWorker != nil {
		o.uploadWorker.RemoveQueue(cameraID)
	}

	// Remove queue from queue manager
	if err := o.queueManager.RemoveQueue(cameraID); err != nil {
		return fmt.Errorf("remove queue: %w", err)
	}

	o.logger.Info("Camera removed", "camera", cameraID)
	return nil
}

// SetTimeHealth sets the NTP time health checker
func (o *Orchestrator) SetTimeHealth(timeHealth *timepkg.TimeHealth) {
	// Recreate authority with time health
	authorityConfig := timepkg.DefaultAuthorityConfig()
	tz := o.config.Timezone
	if tz == "" && o.authority != nil {
		// o.config.Timezone may be unset on older runs; preserve IANA zone from current authority
		tz = o.authority.GetConfiguredTimezone()
	}
	if tz != "" {
		authorityConfig.Timezone = tz
	}

	authority, err := timepkg.NewAuthority(timeHealth, authorityConfig)
	if err != nil {
		o.logger.Error("Failed to recreate authority with time health", "error", err)
		return
	}

	o.mu.Lock()
	o.authority = authority
	// Update all capture workers
	for _, worker := range o.captureWorkers {
		worker.authority = authority
	}
	o.mu.Unlock()
}

// SetMaxConcurrentUploads updates the orchestrator config and applies the new limit to the upload worker.
// n is clamped to the range allowed by the web console (1–10).
func (o *Orchestrator) SetMaxConcurrentUploads(n int) {
	if n < 1 {
		n = 1
	}
	if n > maxConcurrentUploadsCap {
		n = maxConcurrentUploadsCap
	}

	o.mu.Lock()
	o.config.MaxConcurrentUploads = n
	uw := o.uploadWorker
	o.mu.Unlock()

	if uw != nil {
		uw.SetMaxConcurrent(n)
	}
}

// SetCameraUploader replaces the upload client for a camera (hot reload of SFTP settings).
func (o *Orchestrator) SetCameraUploader(cameraID string, uploader upload.Client) {
	o.mu.RLock()
	uw := o.uploadWorker
	o.mu.RUnlock()
	if uw == nil {
		return
	}
	uw.SetUploader(cameraID, uploader)
}

// SetBridgeTimezone updates the stored IANA timezone used when recreating authority (e.g. SNTP restart).
func (o *Orchestrator) SetBridgeTimezone(tz string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.config.Timezone = tz
}

// SetTimeAuthority updates the time authority for all workers
func (o *Orchestrator) SetTimeAuthority(authority *timepkg.Authority) {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.authority = authority

	// Update all capture workers
	for _, worker := range o.captureWorkers {
		worker.authority = authority
	}

	o.logger.Info("Time authority updated for all workers")
}

// Start starts all workers
func (o *Orchestrator) Start() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	o.startTime = time.Now()

	// Start queue manager background workers
	go o.queueManager.StartMemoryMonitor(o.ctx)
	go o.queueManager.StartExpirationWorker(o.ctx, time.Minute)

	// Start capture workers
	for cameraID, worker := range o.captureWorkers {
		worker.Start()
		o.logger.Info("Capture worker started", "camera", cameraID)
	}

	// Start upload worker
	if o.uploadWorker != nil {
		o.uploadWorker.Start()
		o.logger.Info("Upload worker started")
	} else {
		o.logger.Warn("No upload worker configured, images will queue but not upload")
	}

	o.logger.Info("Orchestrator started",
		"cameras", len(o.captureWorkers))

	return nil
}

// Stop stops all workers gracefully
func (o *Orchestrator) Stop() {
	o.logger.Info("Stopping orchestrator...")

	// Stop capture workers first
	o.mu.Lock()
	for cameraID, worker := range o.captureWorkers {
		worker.Stop()
		o.logger.Info("Capture worker stopped", "camera", cameraID)
	}
	o.mu.Unlock()

	// Stop upload worker
	if o.uploadWorker != nil {
		o.uploadWorker.Stop()
		o.logger.Info("Upload worker stopped")
	}

	// Cancel context (stops queue manager workers)
	o.cancel()

	o.logger.Info("Orchestrator stopped")
}

// GetStatus returns the current orchestrator status
func (o *Orchestrator) GetStatus() OrchestratorStatus {
	o.mu.RLock()
	defer o.mu.RUnlock()

	// Gather camera stats
	cameraStats := make([]CameraStatus, 0, len(o.captureWorkers))
	for cameraID, worker := range o.captureWorkers {
		q, ok := o.queueManager.GetQueue(cameraID)
		if !ok {
			continue
		}

		captureStats := worker.GetStats()
		queueStats := q.GetStats()
		state := worker.GetState()

		cameraStats = append(cameraStats, CameraStatus{
			CameraID:     cameraID,
			CaptureStats: captureStats,
			QueueStats:   queueStats,
			LastSuccess:  state.LastSuccess,
			LastError:    state.LastError,
			IsBackingOff: state.IsBackingOff,
		})
	}

	// Gather upload stats
	var uploadStats UploadStats
	if o.uploadWorker != nil {
		uploadStats = o.uploadWorker.GetStats()
	}

	// Global queue stats
	globalQueueStats := o.queueManager.GetGlobalStats()

	// Time info
	var timeInfo timepkg.TimeInfo
	if o.authority != nil {
		timeInfo = o.authority.GetTimeInfo()
	}

	return OrchestratorStatus{
		Running:          o.ctx.Err() == nil,
		Uptime:           time.Since(o.startTime),
		CameraCount:      len(o.captureWorkers),
		CameraStats:      cameraStats,
		UploadStats:      uploadStats,
		GlobalQueueStats: globalQueueStats,
		TimeInfo:         timeInfo,
	}
}

// OrchestratorStatus represents the full system status
type OrchestratorStatus struct {
	Running          bool                   `json:"running"`
	Uptime           time.Duration          `json:"uptime"`
	CameraCount      int                    `json:"camera_count"`
	CameraStats      []CameraStatus         `json:"camera_stats"`
	UploadStats      UploadStats            `json:"upload_stats"`
	GlobalQueueStats queue.GlobalQueueStats `json:"global_queue_stats"`
	TimeInfo         timepkg.TimeInfo       `json:"time_info"`
}

// CameraStatus represents status for a single camera
type CameraStatus struct {
	CameraID     string           `json:"camera_id"`
	CaptureStats CaptureStats     `json:"capture_stats"`
	QueueStats   queue.QueueStats `json:"queue_stats"`
	LastSuccess  time.Time        `json:"last_success"`
	LastError    error            `json:"last_error,omitempty"`
	IsBackingOff bool             `json:"is_backing_off"`
}
