package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"syscall"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/camera"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/image"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/logger"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/resource"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/scheduler"
	timehealth "github.com/alexwitherspoon/aviationwx-bridge/internal/time"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/update"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/upload"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/web"
	"github.com/alexwitherspoon/aviationwx-bridge/pkg/health"
)

func init() {
	// Resource management - dynamically set based on Docker container limits
	// GOMEMLIMIT is passed as environment variable by container startup script
	// which calculates appropriate limits based on system resources
	//
	// The startup script sets this based on total system RAM:
	//   < 1GB:  60% of RAM for Docker, 85% of that for Go (Pi Zero 2 W)
	//   1-2GB:  65% of RAM for Docker, 88% of that for Go
	//   2-4GB:  70% of RAM for Docker, 90% of that for Go
	//   > 4GB:  75%+ of RAM for Docker, 90% of that for Go
	//
	// If GOMEMLIMIT env var is not set, fall back to conservative 256MB
	// (The startup script always sets this, but we provide a safe default)

	// Note: GOMEMLIMIT env var is automatically read by Go runtime (1.19+)
	// We don't need to call debug.SetMemoryLimit() - the runtime does it for us
	// But we'll log what was detected
	goMemLimit := os.Getenv("GOMEMLIMIT")
	if goMemLimit == "" {
		// Fallback for manual docker runs without the startup script
		debug.SetMemoryLimit(256 * 1024 * 1024) // 256MB conservative default
		goMemLimit = "256MiB (default)"
	}

	log := logger.Default()
	log.Info("Resource limits initialized",
		"gomemlimit", goMemLimit,
		"num_cpu", runtime.NumCPU(),
		"gomaxprocs", runtime.GOMAXPROCS(0))

	// Note: We don't set GOMAXPROCS here anymore - let it default to NumCPU()
	// The Docker --cpus limit will constrain the actual CPU usage
	// This gives Go's scheduler more flexibility within the Docker limit
}

// Build info set at compile time via ldflags
var (
	Version   = "dev"
	GitCommit = "unknown"
)

// Bridge is the main application coordinating all services
type Bridge struct {
	configService   *config.Service
	orchestrator    *scheduler.Orchestrator
	webServer       *web.Server
	updateChecker   *update.Checker
	systemMonitor   *health.SystemMonitor
	timeHealth      *timehealth.TimeHealth
	resourceLimiter *resource.Limiter
	log             *logger.Logger

	// Preview cache (in-memory only)
	lastCaptures map[string]*CachedImage
	captureMu    sync.RWMutex

	// Worker status tracking
	cameraWorkerStatus map[string]*CameraWorkerStatus
	workerStatusMu     sync.RWMutex
}

// CameraWorkerStatus tracks the runtime status of a camera worker
type CameraWorkerStatus struct {
	CameraID           string
	Running            bool
	LastError          string
	LastAttempt        time.Time
	LastSuccess        time.Time
	NextCapture        time.Time
	ErrorCount         int
	UploadFailures     int
	QueuedImages       int
	CurrentlyCapturing bool
	CurrentlyUploading bool
}

// CachedImage holds a captured image with metadata
type CachedImage struct {
	Data       []byte
	CapturedAt time.Time
}

func main() {
	// Panic recovery - log crashes and exit gracefully
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "FATAL PANIC: %v\n", r)
			fmt.Fprintf(os.Stderr, "Stack trace:\n%s\n", debug.Stack())
			os.Exit(2)
		}
	}()

	// Initialize logger
	logger.Init()
	log := logger.Default()

	log.Info("AviationWX Bridge starting",
		"version", Version,
		"commit", GitCommit,
		"pid", os.Getpid())

	// Initialize config service
	configDir := os.Getenv("AVIATIONWX_CONFIG_DIR")
	if configDir == "" {
		configDir = "/data"
	}

	legacyConfigPath := os.Getenv("AVIATIONWX_CONFIG")
	if legacyConfigPath == "" {
		legacyConfigPath = filepath.Join(configDir, "config.json")
	}

	log.Info("Initializing config service",
		"configDir", configDir,
		"legacyPath", legacyConfigPath)

	configService, err := config.InitOrMigrate(configDir, legacyConfigPath)
	if err != nil {
		log.Error("Failed to initialize config service", "error", err)
		os.Exit(1)
	}
	log.Info("Config service initialized", "dir", configDir)

	// Create update checker
	updateChecker := update.NewChecker(Version, GitCommit)
	updateChecker.Start()
	log.Info("Update checker started")

	// Get queue path
	queuePath := os.Getenv("AVIATIONWX_QUEUE_PATH")
	if queuePath == "" {
		queuePath = "/dev/shm/aviationwx"
	}

	// Initialize time health (SNTP)
	global := configService.GetGlobal()
	var timeHealth *timehealth.TimeHealth
	if global.SNTP != nil && global.SNTP.Enabled {
		timeHealth = timehealth.NewTimeHealth(timehealth.Config{
			Enabled:              global.SNTP.Enabled,
			Servers:              global.SNTP.Servers,
			CheckIntervalSeconds: global.SNTP.CheckIntervalSeconds,
			MaxOffsetSeconds:     global.SNTP.MaxOffsetSeconds,
			TimeoutSeconds:       global.SNTP.TimeoutSeconds,
		})
		timeHealth.Start()
		log.Info("Time health monitoring started", "servers", global.SNTP.Servers)
	} else {
		log.Info("Time health monitoring disabled (SNTP not configured)")
	}

	// Create resource limiter for background work throttling
	// On devices with < 1GB RAM, this will serialize image processing
	resourceConfig := resource.DefaultConfig()
	resourceLimiter := resource.NewLimiter(resourceConfig)

	log.Info("Resource limiter initialized",
		"max_image_processing", resourceConfig.MaxConcurrentImageProcessing,
		"max_exif_operations", resourceConfig.MaxConcurrentExifOperations,
		"num_cpu", runtime.NumCPU(),
		"gomaxprocs", runtime.GOMAXPROCS(0))

	// Create bridge
	bridge := &Bridge{
		configService:      configService,
		updateChecker:      updateChecker,
		systemMonitor:      health.NewSystemMonitor(queuePath),
		timeHealth:         timeHealth,
		resourceLimiter:    resourceLimiter,
		log:                log,
		lastCaptures:       make(map[string]*CachedImage),
		cameraWorkerStatus: make(map[string]*CameraWorkerStatus),
	}

	// Initialize orchestrator
	if err := bridge.initOrchestrator(); err != nil {
		log.Warn("Could not initialize orchestrator - cameras disabled", "error", err)
	}

	// Create web server (no callbacks - uses ConfigService directly)
	bridge.webServer = web.NewServer(web.ServerConfig{
		ConfigService:   configService,
		GetStatus:       bridge.getStatus,
		TestCamera:      bridge.testCamera,
		TestUpload:      bridge.testUpload,
		GetCameraImage:  bridge.getCameraImage,
		GetWorkerStatus: bridge.getWorkerStatus,
	})

	// Subscribe to config changes
	configService.Subscribe(bridge.handleConfigEvent)

	// Start orchestrator if we have cameras
	cameras := configService.ListCameras()
	if bridge.orchestrator != nil && len(cameras) > 0 {
		if err := bridge.orchestrator.Start(); err != nil {
			log.Warn("Failed to start orchestrator", "error", err)
		} else {
			log.Info("Orchestrator started", "cameras", len(cameras))
		}
	}

	// Start web server with panic recovery
	webErrChan := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Error("Web server panicked", "panic", r, "stack", string(debug.Stack()))
				webErrChan <- fmt.Errorf("web server panic: %v", r)
			}
		}()

		port := configService.GetWebPort()
		log.Info("Web console available",
			"url", fmt.Sprintf("http://localhost:%d", port),
			"password", configService.GetWebPassword())
		if err := bridge.webServer.Start(); err != nil {
			log.Error("Web server error", "error", err)
			webErrChan <- err
		}
	}()

	// Wait for shutdown signal or fatal error
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-sigChan:
		log.Info("Shutting down gracefully...")
	case err := <-webErrChan:
		log.Error("Fatal error - shutting down", "error", err)
	}

	// Stop services
	if bridge.updateChecker != nil {
		bridge.updateChecker.Stop()
	}
	if bridge.orchestrator != nil {
		bridge.orchestrator.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := bridge.webServer.Stop(ctx); err != nil {
		log.Error("Error stopping server", "error", err)
	}

	log.Info("Goodbye!")
}

// initOrchestrator initializes the orchestrator and adds cameras
func (b *Bridge) initOrchestrator() error {
	queuePath := os.Getenv("AVIATIONWX_QUEUE_PATH")
	if queuePath == "" {
		queuePath = "/dev/shm/aviationwx"
	}

	orch, err := scheduler.NewOrchestrator(scheduler.OrchestratorConfig{
		QueueBasePath:   queuePath,
		QueueMaxTotalMB: 100,
		QueueMaxHeapMB:  400,
		ResourceLimiter: b.resourceLimiter,
		Logger:          b.log,
	})
	if err != nil {
		return fmt.Errorf("create orchestrator: %w", err)
	}
	b.orchestrator = orch

	// Add all enabled cameras
	cameras := b.configService.ListCameras()
	enabledCount := 0

	for _, camConfig := range cameras {
		if !camConfig.Enabled {
			continue
		}

		if err := b.addCamera(camConfig); err != nil {
			b.log.Warn("Failed to add camera",
				"camera", camConfig.ID,
				"error", err)
			// Don't fail - continue with other cameras
		} else {
			enabledCount++
		}
	}

	b.log.Info("Camera initialization complete",
		"total", len(cameras),
		"enabled", enabledCount)

	return nil
}

// updateTimezone updates the timezone for all camera workers
func (b *Bridge) updateTimezone(timezone string) error {
	if b.orchestrator == nil {
		return nil
	}

	b.log.Info("Updating timezone", "new_timezone", timezone)

	// Create new authority with updated timezone
	authorityConfig := timehealth.DefaultAuthorityConfig()
	authorityConfig.Timezone = timezone

	authority, err := timehealth.NewAuthority(b.timeHealth, authorityConfig)
	if err != nil {
		return fmt.Errorf("create authority: %w", err)
	}

	// Update orchestrator's authority
	b.orchestrator.SetTimeAuthority(authority)

	b.log.Info("Timezone updated successfully", "timezone", timezone)
	return nil
}

// restartSNTP restarts the SNTP time health service with new config
func (b *Bridge) restartSNTP(sntpConfig *config.SNTP) error {
	b.log.Info("Restarting SNTP service")

	// Stop existing time health
	if b.timeHealth != nil {
		b.timeHealth.Stop()
		b.timeHealth = nil
		b.log.Info("Stopped existing SNTP service")
	}

	// Start new time health if enabled
	if sntpConfig != nil && sntpConfig.Enabled {
		b.timeHealth = timehealth.NewTimeHealth(timehealth.Config{
			Enabled:              sntpConfig.Enabled,
			Servers:              sntpConfig.Servers,
			CheckIntervalSeconds: sntpConfig.CheckIntervalSeconds,
			MaxOffsetSeconds:     sntpConfig.MaxOffsetSeconds,
			TimeoutSeconds:       sntpConfig.TimeoutSeconds,
		})
		b.timeHealth.Start()

		// Update orchestrator's time health
		if b.orchestrator != nil {
			b.orchestrator.SetTimeHealth(b.timeHealth)
		}

		b.log.Info("SNTP service restarted", "servers", sntpConfig.Servers)
	} else {
		b.log.Info("SNTP service disabled")
	}

	return nil
}

// addCamera adds a camera to the orchestrator
func (b *Bridge) addCamera(camConfig config.Camera) error {
	// Track worker status
	b.workerStatusMu.Lock()
	status := &CameraWorkerStatus{
		CameraID:    camConfig.ID,
		Running:     false,
		LastAttempt: time.Now(),
	}
	b.cameraWorkerStatus[camConfig.ID] = status
	b.workerStatusMu.Unlock()

	b.log.Info("Camera added", "camera", camConfig.ID, "interval_secs", camConfig.CaptureIntervalSeconds)

	// Create camera instance
	cam, err := b.createCamera(camConfig)
	if err != nil {
		status.LastError = fmt.Sprintf("Create camera failed: %v", err)
		status.ErrorCount++
		return fmt.Errorf("create camera: %w", err)
	}

	// Create image processor
	var imgProcessor *image.Processor
	if camConfig.Image != nil && camConfig.Image.NeedsProcessing() {
		imgProcessor = image.NewProcessor(camConfig.Image)
	} else {
		imgProcessor = image.NewProcessor(nil)
	}

	schedConfig := scheduler.CameraConfig{
		RemotePath:     camConfig.ID,
		ImageProcessor: imgProcessor,
	}

	// Get capture interval
	interval := camConfig.CaptureIntervalSeconds
	if interval == 0 {
		interval = 60
	}

	// Create uploader
	var uploader upload.Client
	if camConfig.Upload != nil {
		var err error
		uploader, err = b.createUploader(camConfig.Upload)
		if err != nil {
			status.LastError = fmt.Sprintf("Create uploader failed: %v", err)
			status.ErrorCount++
			return fmt.Errorf("create uploader: %w", err)
		}
	} else {
		status.LastError = "Upload configuration missing"
		status.ErrorCount++
		return fmt.Errorf("upload configuration required for camera %s", camConfig.ID)
	}

	// Add to orchestrator
	if err := b.orchestrator.AddCamera(cam, schedConfig, interval, uploader, b.updatePreviewCache); err != nil {
		status.LastError = fmt.Sprintf("Add to orchestrator failed: %v", err)
		status.ErrorCount++
		return fmt.Errorf("add to orchestrator: %w", err)
	}

	// Success
	status.Running = true
	status.LastError = ""
	b.log.Info("Camera worker started successfully",
		"camera", camConfig.ID,
		"type", camConfig.Type,
		"interval", interval)

	return nil
}

// getWorkerStatus returns the runtime status of a camera worker
func (b *Bridge) getWorkerStatus(cameraID string) map[string]interface{} {
	b.workerStatusMu.RLock()
	defer b.workerStatusMu.RUnlock()

	status, exists := b.cameraWorkerStatus[cameraID]
	if !exists {
		return map[string]interface{}{
			"worker_running": false,
			"worker_error":   "Not started",
		}
	}

	result := map[string]interface{}{
		"worker_running": status.Running,
	}

	if status.LastError != "" {
		result["worker_error"] = status.LastError
		result["worker_error_count"] = status.ErrorCount
	}

	if !status.LastAttempt.IsZero() {
		result["worker_last_attempt"] = status.LastAttempt.Format(time.RFC3339)
	}

	return result
}

// createCamera creates a camera instance from config
func (b *Bridge) createCamera(camConfig config.Camera) (camera.Camera, error) {
	cameraConf := camera.Config{
		ID:          camConfig.ID,
		Type:        camConfig.Type,
		SnapshotURL: camConfig.SnapshotURL,
	}

	if camConfig.Auth != nil {
		cameraConf.Auth = &camera.AuthConfig{
			Type:     camConfig.Auth.Type,
			Username: camConfig.Auth.Username,
			Password: camConfig.Auth.Password,
			Token:    camConfig.Auth.Token,
		}
	}

	if camConfig.ONVIF != nil {
		cameraConf.ONVIF = &camera.ONVIFConfig{
			Endpoint:     camConfig.ONVIF.Endpoint,
			Username:     camConfig.ONVIF.Username,
			Password:     camConfig.ONVIF.Password,
			ProfileToken: camConfig.ONVIF.ProfileToken,
		}
	}

	if camConfig.RTSP != nil {
		cameraConf.RTSP = &camera.RTSPConfig{
			URL:       camConfig.RTSP.URL,
			Username:  camConfig.RTSP.Username,
			Password:  camConfig.RTSP.Password,
			Substream: camConfig.RTSP.Substream,
		}
	}

	return camera.NewCamera(cameraConf)
}

// createUploader creates an upload client from config
func (b *Bridge) createUploader(uploadConfig *config.Upload) (upload.Client, error) {
	return upload.NewFTPSClient(upload.Config{
		Host:                  uploadConfig.Host,
		Port:                  uploadConfig.Port,
		Username:              uploadConfig.Username,
		Password:              uploadConfig.Password,
		TLS:                   uploadConfig.TLS,
		TLSVerify:             true,
		TimeoutConnectSeconds: 10,
		TimeoutUploadSeconds:  30,
	})
}

// handleConfigEvent handles config change events from ConfigService
func (b *Bridge) handleConfigEvent(event config.ConfigEvent) {
	b.log.Info("Config event received", "type", event.Type, "camera", event.CameraID)

	switch event.Type {
	case "camera_added":
		// Get the camera config
		camConfig, err := b.configService.GetCamera(event.CameraID)
		if err != nil {
			b.log.Error("Failed to get camera config", "camera", event.CameraID, "error", err)
			return
		}

		if camConfig.Enabled {
			if err := b.addCamera(*camConfig); err != nil {
				b.log.Error("Failed to add camera worker", "camera", event.CameraID, "error", err)
			}
		}

	case "camera_updated":
		// Get the updated config
		camConfig, err := b.configService.GetCamera(event.CameraID)
		if err != nil {
			b.log.Error("Failed to get camera config", "camera", event.CameraID, "error", err)
			return
		}

		// Remove old worker
		if b.orchestrator != nil {
			if err := b.orchestrator.RemoveCamera(event.CameraID); err != nil {
				b.log.Error("Failed to remove camera worker during update",
					"camera", event.CameraID,
					"error", err)
			}
		}

		// Clean up status
		b.workerStatusMu.Lock()
		delete(b.cameraWorkerStatus, event.CameraID)
		b.workerStatusMu.Unlock()

		// Add new worker if enabled
		if camConfig.Enabled {
			if err := b.addCamera(*camConfig); err != nil {
				b.log.Error("Failed to update camera worker", "camera", event.CameraID, "error", err)
			}
		}

	case "camera_deleted":
		// Remove worker
		if b.orchestrator != nil {
			if err := b.orchestrator.RemoveCamera(event.CameraID); err != nil {
				b.log.Error("Failed to remove camera worker during delete",
					"camera", event.CameraID,
					"error", err)
			}
		}

		// Clean up caches
		b.captureMu.Lock()
		delete(b.lastCaptures, event.CameraID)
		b.captureMu.Unlock()

		b.workerStatusMu.Lock()
		delete(b.cameraWorkerStatus, event.CameraID)
		b.workerStatusMu.Unlock()

		b.log.Info("Camera removed", "camera", event.CameraID)

	case "global_updated":
		// Global settings changed - update services that need hot-reload
		global := b.configService.GetGlobal()

		// Update timezone for all camera workers
		if err := b.updateTimezone(global.Timezone); err != nil {
			b.log.Error("Failed to update timezone", "error", err)
		}

		// Restart SNTP service with new config
		if err := b.restartSNTP(global.SNTP); err != nil {
			b.log.Error("Failed to restart SNTP", "error", err)
		}

		b.log.Info("Global config updated",
			"timezone", global.Timezone,
			"sntp_enabled", global.SNTP != nil && global.SNTP.Enabled)
	}
}

// updatePreviewCache stores the last captured image for preview
func (b *Bridge) updatePreviewCache(cameraID string, imageData []byte, captureTime time.Time) {
	b.captureMu.Lock()
	defer b.captureMu.Unlock()
	b.lastCaptures[cameraID] = &CachedImage{
		Data:       imageData,
		CapturedAt: captureTime,
	}
	b.log.Debug("Preview cache updated", "camera", cameraID, "size", len(imageData))
}

// getCameraImage returns the cached preview image for a camera
func (b *Bridge) getCameraImage(cameraID string) ([]byte, error) {
	b.captureMu.RLock()
	defer b.captureMu.RUnlock()

	cached, found := b.lastCaptures[cameraID]
	if !found {
		return nil, fmt.Errorf("no image available yet")
	}

	// Return cached image if it's recent (< 5 minutes old)
	if time.Since(cached.CapturedAt) < 5*time.Minute {
		return cached.Data, nil
	}

	return nil, fmt.Errorf("cached image too old")
}

// testCamera tests a camera configuration
func (b *Bridge) testCamera(camConfig config.Camera) ([]byte, error) {
	cam, err := b.createCamera(camConfig)
	if err != nil {
		return nil, fmt.Errorf("create camera: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	image, err := cam.Capture(ctx)
	if err != nil {
		return nil, fmt.Errorf("capture image: %w", err)
	}

	return image, nil
}

// testUpload tests an upload configuration
func (b *Bridge) testUpload(uploadConfig config.Upload) error {
	client, err := b.createUploader(&uploadConfig)
	if err != nil {
		return fmt.Errorf("create uploader: %w", err)
	}

	// Test connection only (don't upload a file)
	if err := client.TestConnection(); err != nil {
		return fmt.Errorf("test connection: %w", err)
	}

	return nil
}

// getStatus returns the current bridge status
func (b *Bridge) getStatus() interface{} {
	global := b.configService.GetGlobal()
	cameras := b.configService.ListCameras()

	enabledCameras := 0
	for _, cam := range cameras {
		if cam.Enabled {
			enabledCameras++
		}
	}

	queuedImages := 0
	if b.orchestrator != nil {
		orchStatus := b.orchestrator.GetStatus()
		for _, camStatus := range orchStatus.CameraStats {
			queuedImages += camStatus.QueueStats.ImageCount
		}
	}

	status := map[string]interface{}{
		"version":       Version,
		"commit":        GitCommit,
		"timezone":      global.Timezone,
		"cameras":       enabledCameras,
		"total_cameras": len(cameras),
		"queued_images": queuedImages,
	}

	// Add system health if available
	if b.systemMonitor != nil {
		sysStats := b.systemMonitor.GetStats()
		status["system"] = map[string]interface{}{
			"cpu_percent":   sysStats.CPUPercent,
			"mem_percent":   sysStats.MemPercent,
			"mem_used_mb":   sysStats.MemUsedMB,
			"mem_total_mb":  sysStats.MemTotalMB,
			"disk_percent":  sysStats.DiskPercent,
			"disk_used_mb":  sysStats.DiskUsedMB,
			"disk_total_mb": sysStats.DiskTotalMB,
			"uptime":        sysStats.Uptime,
		}
	}

	// Add orchestrator status with detailed camera stats
	if b.orchestrator != nil {
		orchStatus := b.orchestrator.GetStatus()
		status["orchestrator"] = orchStatus
	}

	// Add time health if available
	if b.timeHealth != nil {
		timeStatus := b.timeHealth.GetStatus()
		status["time_health"] = map[string]interface{}{
			"healthy":    timeStatus.Healthy,
			"offset_ms":  timeStatus.Offset.Milliseconds(),
			"last_check": timeStatus.LastCheck.Format(time.RFC3339),
		}
	}

	// Add update checker status if available
	if b.updateChecker != nil {
		updateStatus := b.updateChecker.Status()
		status["update"] = map[string]interface{}{
			"current_version":  updateStatus.CurrentVersion,
			"current_commit":   updateStatus.CurrentCommit,
			"latest_version":   updateStatus.LatestVersion,
			"update_available": updateStatus.UpdateAvailable,
			"last_check":       updateStatus.LastCheck.Format(time.RFC3339),
		}
	}

	return status
}
