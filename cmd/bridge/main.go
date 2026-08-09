package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/bridgeapi"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/camera"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/deploy"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/image"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/logger"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/resource"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/scheduler"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/station"
	timehealth "github.com/alexwitherspoon/AviationWX.org-Bridge/internal/time"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/update"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/upload"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/web"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/pkg/health"
)

func init() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-version", "--version":
			fmt.Printf("%s (%s)\n", Version, GitCommit)
			os.Exit(0)
		}
	}

	// Resource management - dynamically set based on Docker container limits
	// GOMEMLIMIT is passed as environment variable by container startup script
	// which calculates appropriate limits based on system resources
	//
	// The startup script sets this based on total system RAM:
	//   < 1GB:  60% of RAM for Docker, 85% of that for Go (below recommended minimum)
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
	apiReporter     *bridgeapi.Reporter
	stationManager  *station.Manager
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

	statusCache *statusCache
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

// cameraRetryInterval is how often enabled cameras that failed to start at boot are retried.
const cameraRetryInterval = 15 * time.Minute

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

	log.Info("AviationWX.org Bridge starting",
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

	syncUploadSSHHostKeys(configService, configDir, log)
	go syncUploadSSHHostKeysLoop(configService, configDir, log)

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
			StaleThresholdHours:  global.SNTP.StaleThresholdHours,
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
	bridge.statusCache = newStatusCache(500*time.Millisecond, bridge.buildStatus)

	bridge.apiReporter = bridgeapi.NewReporter(bridgeapi.ReporterConfig{
		ConfigService: configService,
		Version:       Version,
		Commit:        GitCommit,
		Log:           log,
		BuildHealth:   bridge.buildAPIHealthRequest,
	})
	bridge.apiReporter.SyncFromConfig()
	if config.APIConfigured(configService.GetGlobal().API) {
		log.Info("Bridge API reporter started")
	}

	bridge.stationManager = station.NewManager(station.ManagerConfig{
		ConfigService: configService,
		Poster:        bridge.apiReporter,
		Log:           log,
	})
	bridge.stationManager.SyncFromConfig()

	// Initialize orchestrator
	if err := bridge.initOrchestrator(); err != nil {
		log.Warn("Could not initialize orchestrator - cameras disabled", "error", err)
	}

	// Create web server (no callbacks - uses ConfigService directly)
	bridge.webServer = web.NewServer(web.ServerConfig{
		ConfigService:             configService,
		GetStatus:                 bridge.getStatusCached,
		GetCaptureReadiness:       bridge.getCaptureReadiness,
		TestCamera:                bridge.testCamera,
		TestUpload:                bridge.testUpload,
		TestAPIBootstrap:          bridge.testAPIBootstrap,
		TestAPIHealth:             bridge.testAPIHealth,
		TestStationPoll:           bridge.testStationPoll,
		TestStationDiscoverStream: bridge.testStationDiscoverStream,
		GetCameraImage:            bridge.getCameraImage,
		GetWorkerStatus:           bridge.getWorkerStatus,
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

	retryStop := make(chan struct{})
	go bridge.retryFailedCamerasLoop(retryStop)

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
	if bridge.stationManager != nil {
		bridge.stationManager.Stop()
	}
	if bridge.apiReporter != nil {
		bridge.apiReporter.Stop()
	}
	if bridge.updateChecker != nil {
		bridge.updateChecker.Stop()
	}
	if bridge.orchestrator != nil {
		bridge.orchestrator.Stop()
	}
	close(retryStop)

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

	global := b.configService.GetGlobal()
	maxConcurrent := config.EffectiveMaxConcurrentUploads(global)
	maxCaptures := config.EffectiveMaxConcurrentCaptures(global)

	orch, err := scheduler.NewOrchestrator(scheduler.OrchestratorConfig{
		QueueBasePath:         queuePath,
		QueueMaxTotalMB:       100,
		QueueMaxHeapMB:        400,
		Timezone:              global.Timezone,
		MaxConcurrentUploads:  maxConcurrent,
		MaxConcurrentCaptures: maxCaptures,
		ResourceLimiter:       b.resourceLimiter,
		Logger:                b.log,
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

// retryFailedCamerasLoop periodically starts missing camera workers and restarts stale ones.
func (b *Bridge) retryFailedCamerasLoop(stop <-chan struct{}) {
	ticker := time.NewTicker(cameraRetryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			b.retryFailedCameras()
		}
	}
}

// cameraCaptureInterval returns the configured capture interval for staleness checks.
func cameraCaptureInterval(cam config.Camera) time.Duration {
	secs := cam.CaptureIntervalSeconds
	if secs <= 0 {
		secs = cam.IntervalSeconds
	}
	if secs <= 0 {
		secs = 60
	}
	return time.Duration(secs) * time.Second
}

// cameraStaleThreshold returns max(readyzMinStale, 3×capture interval), same formula as /readyz.
func cameraStaleThreshold(interval time.Duration) time.Duration {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	minStale := readyzMinStale()
	threshold := 3 * interval
	if minStale > threshold {
		return minStale
	}
	return threshold
}

// readyzMinStale is the minimum staleness window for /readyz and stale worker restart.
func readyzMinStale() time.Duration {
	return envDurationSeconds("AVIATIONWX_READYZ_STALE_SECONDS", 900)
}

// cameraWorkerIsStale reports whether a running worker should be removed and restarted.
func (b *Bridge) cameraWorkerIsStale(cam config.Camera, cs scheduler.CameraStatus, uptime time.Duration) bool {
	grace := envDurationSeconds("AVIATIONWX_READYZ_GRACE_SECONDS", 600)
	if uptime < grace {
		return false
	}
	threshold := cameraStaleThreshold(cameraCaptureInterval(cam))
	if cs.LastSuccess.IsZero() {
		return uptime > threshold
	}
	return time.Since(cs.LastSuccess) > threshold
}

// retryFailedCameras starts missing workers and restarts stale ones.
func (b *Bridge) retryFailedCameras() {
	if b.orchestrator == nil {
		return
	}
	status := b.orchestrator.GetStatus()
	for _, camConfig := range b.configService.ListCameras() {
		if !camConfig.Enabled {
			continue
		}

		var cs *scheduler.CameraStatus
		for i := range status.CameraStats {
			if status.CameraStats[i].CameraID == camConfig.ID {
				cs = &status.CameraStats[i]
				break
			}
		}

		if cs == nil {
			b.log.Info("Retrying camera worker start", "camera", camConfig.ID)
			if err := b.addCamera(camConfig); err != nil {
				b.log.Warn("Camera worker retry failed",
					"camera", camConfig.ID,
					"error", err)
			}
			continue
		}

		if !b.cameraWorkerIsStale(camConfig, *cs, status.Uptime) {
			continue
		}

		b.log.Warn("Restarting stale camera worker",
			"camera", camConfig.ID,
			"last_success", cs.LastSuccess,
			"uptime", status.Uptime)
		if err := b.orchestrator.RemoveCamera(camConfig.ID); err != nil {
			b.log.Warn("Failed to remove stale camera worker",
				"camera", camConfig.ID,
				"error", err)
			continue
		}
		if err := b.addCamera(camConfig); err != nil {
			b.log.Warn("Camera worker restart after stale failed",
				"camera", camConfig.ID,
				"error", err)
		}
	}
}

// updateTimezone updates the timezone for all camera workers
func (b *Bridge) updateTimezone(timezone string) error {
	if b.orchestrator == nil {
		return nil
	}

	b.log.Info("Updating timezone", "new_timezone", timezone)

	// Keep orchestrator config in sync so SetTimeHealth (SNTP restart) does not reset to system local TZ
	b.orchestrator.SetBridgeTimezone(timezone)

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
			StaleThresholdHours:  sntpConfig.StaleThresholdHours,
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

	// Use remote_path from config, default to "." (upload directly to base_path)
	// Each camera has unique credentials with its own chroot, so no subdirectory needed
	remotePath := camConfig.RemotePath
	if remotePath == "" {
		remotePath = "."
	}

	schedConfig := scheduler.CameraConfig{
		RemotePath:     remotePath,
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

// createUploader creates an upload client from config, applying global SFTP timeout defaults when unset.
func (b *Bridge) createUploader(uploadConfig *config.Upload) (upload.Client, error) {
	merged := *uploadConfig
	glob := b.configService.GetGlobal()
	if merged.TimeoutConnectSeconds <= 0 && glob.TimeoutConnectSeconds > 0 {
		merged.TimeoutConnectSeconds = glob.TimeoutConnectSeconds
	}
	if merged.TimeoutUploadSeconds <= 0 && glob.TimeoutUploadSeconds > 0 {
		merged.TimeoutUploadSeconds = glob.TimeoutUploadSeconds
	}
	return upload.NewClientFromConfig(merged, b.configService.SSHKnownHostsPath())
}

// refreshUploadersFromGlobal rebuilds per-camera SFTP clients so global timeout defaults take effect without restart.
func (b *Bridge) refreshUploadersFromGlobal() {
	if b.orchestrator == nil {
		return
	}
	for _, cam := range b.configService.ListCameras() {
		if !cam.Enabled || cam.Upload == nil {
			continue
		}
		client, err := b.createUploader(cam.Upload)
		if err != nil {
			b.log.Warn("Failed to refresh uploader after global config change",
				"camera", cam.ID,
				"error", err)
			continue
		}
		b.orchestrator.SetCameraUploader(cam.ID, client)
	}
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

	case "station_added", "station_updated", "station_deleted":
		if b.stationManager != nil {
			b.stationManager.SyncFromConfig()
		}

	case "global_updated":
		// Global settings changed - update services that need hot-reload
		global := b.configService.GetGlobal()

		// Sync orchestrator timezone before SNTP restart: SetTimeHealth rebuilds authority from o.config.Timezone.
		// If we only called updateTimezone after restartSNTP, SNTP would wipe the user's IANA zone (empty config).
		if b.orchestrator != nil {
			b.orchestrator.SetBridgeTimezone(global.Timezone)
		}

		// Restart SNTP service with new config (recreates time authority with NTP + timezone above)
		if err := b.restartSNTP(global.SNTP); err != nil {
			b.log.Error("Failed to restart SNTP", "error", err)
		}

		// Apply full authority to capture workers (required when SNTP is disabled; aligns state when enabled)
		if err := b.updateTimezone(global.Timezone); err != nil {
			b.log.Error("Failed to update timezone", "error", err)
		}

		if b.orchestrator != nil {
			b.orchestrator.SetMaxConcurrentUploads(config.EffectiveMaxConcurrentUploads(global))
			b.orchestrator.SetMaxConcurrentCaptures(config.EffectiveMaxConcurrentCaptures(global))
			b.refreshUploadersFromGlobal()
		}

		if b.apiReporter != nil {
			b.apiReporter.SyncFromConfig()
		}
		if b.stationManager != nil {
			// API link may have become available for weather POST.
			b.stationManager.SyncFromConfig()
		}

		b.log.Info("Global config updated",
			"timezone", global.Timezone,
			"sntp_enabled", global.SNTP != nil && global.SNTP.Enabled,
			"max_concurrent_uploads", config.EffectiveMaxConcurrentUploads(global),
			"max_concurrent_captures", config.EffectiveMaxConcurrentCaptures(global),
			"api_configured", config.APIConfigured(global.API))
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

// testAPIBootstrap probes core GET /v1/bridge/bootstrap with optional unsaved credentials.
func (b *Bridge) testAPIBootstrap(key, baseURL string) (map[string]interface{}, error) {
	global := b.configService.GetGlobal()
	key = strings.TrimSpace(key)
	baseURL = strings.TrimSpace(baseURL)
	if key == "" && global.API != nil {
		key = global.API.Key
	}
	if baseURL == "" {
		baseURL = config.EffectiveAPIBaseURL(global.API)
	}
	if key == "" {
		return nil, fmt.Errorf("api key is required")
	}
	if !config.ValidAPIKeyShape(key) {
		return nil, fmt.Errorf("api key must be awxb_ plus %d alphanumeric characters", config.APIKeySecretLength)
	}
	client, err := bridgeapi.NewClient(bridgeapi.ClientConfig{
		BaseURL: baseURL,
		APIKey:  key,
		Version: Version,
	})
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	boot, err := client.Bootstrap(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"airport_id":                 boot.Airport.ID,
		"airport_name":               boot.Airport.Name,
		"bridge_id":                  boot.BridgeID,
		"declination_deg":            boot.DeclinationDeg,
		"declination_source":         boot.DeclinationSource,
		"heartbeat_interval_seconds": boot.HeartbeatIntervalSeconds,
		"enabled_sources":            boot.EnabledSources,
	}
	if b.apiReporter != nil {
		// Refresh configured reporter bootstrap cache when testing the saved key.
		if global.API != nil && key == global.API.Key {
			_, _ = b.apiReporter.BootstrapNow(ctx)
		}
	}
	return out, nil
}

// testAPIHealth posts one health heartbeat using the running reporter.
func (b *Bridge) testAPIHealth() (map[string]interface{}, error) {
	if b.apiReporter == nil {
		return nil, fmt.Errorf("api reporter not available")
	}
	global := b.configService.GetGlobal()
	if !config.APIConfigured(global.API) {
		return nil, fmt.Errorf("api link is not configured - save an enabled key first")
	}
	b.apiReporter.SyncFromConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	boot, err := b.apiReporter.BootstrapNow(ctx)
	if err != nil {
		return nil, fmt.Errorf("bootstrap: %w", err)
	}
	snap := b.apiReporter.Snapshot()
	return map[string]interface{}{
		"bridge_id":   boot.BridgeID,
		"airport_id":  boot.Airport.ID,
		"link_status": snap.Status,
		"last_error":  snap.LastError,
		"configured":  snap.Configured,
	}, nil
}

// testStationPoll probes a LAN weather station once (Davis) or previews a
// Weather Underground-style payload (http_interceptor).
func (b *Bridge) testStationPoll(st config.Station) (map[string]interface{}, error) {
	if b.stationManager == nil {
		return nil, fmt.Errorf("station manager not available")
	}
	if strings.TrimSpace(st.Type) == "" {
		st.Type = config.StationTypeDavisWeatherLinkLive
	}
	if st.Type == config.StationTypeHTTPInterceptor {
		return b.testStationInterceptor(st)
	}
	if strings.TrimSpace(st.Host) == "" {
		return nil, fmt.Errorf("station host is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	obs, err := b.stationManager.TestPoll(ctx, st)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"provider":      obs.Provider,
		"did":           obs.DID,
		"transmitters":  obs.Transmitters,
		"provider_meta": obs.ProviderMeta,
	}
	// Missing station ts leaves ObservedAt zero; omit so the console shows
	// "no station timestamp" instead of year-1 RFC3339.
	if !obs.ObservedAt.IsZero() {
		out["observed_at"] = obs.ObservedAt.UTC().Format(time.RFC3339)
	}
	return out, nil
}

func (b *Bridge) testStationInterceptor(st config.Station) (map[string]interface{}, error) {
	config.NormalizeStationDefaults(&st)
	values := map[string]string{
		"dateutc":      "2024-06-15 12:00:00",
		"tempf":        "72.5",
		"humidity":     "55",
		"winddir":      "180",
		"windspeedmph": "5.2",
		"ID":           "TEST",
		"PASSWORD":     "x",
		"action":       "updateraw",
	}
	obs, err := b.stationManager.PreviewInterceptorRequest(st, values)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"provider":      obs.Provider,
		"provider_meta": obs.ProviderMeta,
		"listen_addr":   st.ListenAddr,
		"listen_path":   st.ListenPath,
		"dialect":       st.Dialect,
	}
	if !obs.ObservedAt.IsZero() {
		out["observed_at"] = obs.ObservedAt.UTC().Format(time.RFC3339)
	}
	return out, nil
}

func (b *Bridge) testStationDiscoverStream(ctx context.Context, stationType, subnet string, emit func(station.DiscoverEvent)) error {
	return station.DiscoverStationsStream(ctx, station.DiscoverOptions{
		Type:   stationType,
		Subnet: subnet,
	}, emit)
}

func (b *Bridge) getStatusCached() interface{} {
	if b.statusCache == nil {
		return b.buildStatus()
	}
	return b.statusCache.get()
}

// buildAPIHealthRequest assembles the outbound health payload for api.aviationwx.org.
func (b *Bridge) buildAPIHealthRequest() bridgeapi.HealthRequest {
	global := b.configService.GetGlobal()
	cameras := b.configService.ListCameras()
	stations := b.configService.ListStations()

	hostStatus := bridgeapi.StatusOperational
	ntpOK := true
	var ntpFailureSeconds int64
	if b.timeHealth != nil {
		ts := b.timeHealth.GetStatus()
		ntpOK = ts.Healthy
		if !ntpOK {
			hostStatus = bridgeapi.StatusDegraded
			if !ts.LastGoodSync.IsZero() {
				ntpFailureSeconds = int64(time.Since(ts.LastGoodSync).Seconds())
			}
		}
	}

	inv := bridgeapi.Inventory{
		Cameras:  make([]bridgeapi.InventoryCamera, 0, len(cameras)),
		Stations: make([]bridgeapi.InventoryStation, 0, len(stations)),
	}
	for _, cam := range cameras {
		inv.Cameras = append(inv.Cameras, bridgeapi.InventoryCamera{
			ID:              cam.ID,
			Name:            cam.Name,
			EnabledOnBridge: cam.Enabled,
		})
	}
	for _, st := range stations {
		inv.Stations = append(inv.Stations, bridgeapi.InventoryStation{
			ID:              st.ID,
			Name:            st.Name,
			Type:            st.Type,
			EnabledOnBridge: st.Enabled,
		})
	}

	subsystems := map[string]bridgeapi.SubsystemHealth{}
	if len(cameras) > 0 {
		camStatus := bridgeapi.StatusOperational
		uploadStatus := bridgeapi.StatusOperational
		if b.orchestrator != nil {
			orch := b.orchestrator.GetStatus()
			for _, cs := range orch.CameraStats {
				if cs.IsBackingOff || cs.LastError != nil {
					camStatus = bridgeapi.StatusDegraded
					break
				}
			}
			if orch.UploadStats.UploadsFailed > 0 {
				uploadStatus = bridgeapi.StatusDegraded
			}
		}
		subsystems["cameras"] = bridgeapi.SubsystemHealth{Status: camStatus}
		subsystems["upload"] = bridgeapi.SubsystemHealth{Status: uploadStatus}
	}
	if b.stationManager != nil {
		if wx, ok := b.stationManager.WeatherSubsystemHealth(); ok {
			subsystems["weather"] = wx
		}
	} else if len(stations) > 0 {
		subsystems["weather"] = bridgeapi.SubsystemHealth{
			Status: bridgeapi.StatusOperational,
			Detail: map[string]interface{}{"lan_ok": false},
		}
	}

	var resources *bridgeapi.HostResources
	if b.systemMonitor != nil {
		stats := b.systemMonitor.GetStats()
		queueDepth := 0
		if b.orchestrator != nil {
			for _, cs := range b.orchestrator.GetStatus().CameraStats {
				queueDepth += cs.QueueStats.ImageCount
			}
		}
		resources = &bridgeapi.HostResources{
			MemAvailableMB: int(stats.MemTotalMB - stats.MemUsedMB),
			QueuePath:      os.Getenv("AVIATIONWX_QUEUE_PATH"),
			QueueDepth:     queueDepth,
		}
		if resources.QueuePath == "" {
			resources.QueuePath = "/dev/shm/aviationwx"
		}
	}

	return bridgeapi.HealthRequest{
		ObservedAt: time.Now().UTC(),
		Host: bridgeapi.HostHealth{
			Status:            hostStatus,
			NTPOK:             ntpOK,
			NTPFailureSeconds: ntpFailureSeconds,
			Build: bridgeapi.BuildInfo{
				Version: Version,
				Commit:  GitCommit,
				Channel: getUpdateChannel(global.UpdateChannel),
			},
			Resources: resources,
		},
		Subsystems: subsystems,
		Inventory:  inv,
	}
}

// buildStatus returns the current bridge status (uncached).
func (b *Bridge) buildStatus() interface{} {
	global := b.configService.GetGlobal()
	cameras := b.configService.ListCameras()

	enabledCameras := 0
	for _, cam := range cameras {
		if cam.Enabled {
			enabledCameras++
		}
	}

	queuedImages := 0
	uploadsToday := int64(0)
	var orchStatus scheduler.OrchestratorStatus
	if b.orchestrator != nil {
		orchStatus = b.orchestrator.GetStatus()
		for _, camStatus := range orchStatus.CameraStats {
			queuedImages += camStatus.QueueStats.ImageCount
		}
		uploadsToday = orchStatus.UploadStats.UploadsToday
	}

	status := map[string]interface{}{
		"version":             Version,
		"commit":              GitCommit,
		"update_channel":      getUpdateChannel(global.UpdateChannel),
		"timezone":            global.Timezone,
		"cameras":             enabledCameras,
		"total_cameras":       len(cameras),
		"queued_images":       queuedImages,
		"uploads_today":       uploadsToday,
		"self_update_enabled": deploy.SelfUpdateEnabled(),
	}
	stations := b.configService.ListStations()
	status["total_stations"] = len(stations)
	status["first_run"] = len(cameras) == 0 && len(stations) == 0

	if tag := readHostDataLabel("configured-image-tag.txt"); tag != "" {
		status["configured_image_tag"] = tag
	}
	if lkg := readHostDataLabel("last-known-good.txt"); lkg != "" {
		status["last_known_good"] = lkg
	}

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

	if b.orchestrator != nil {
		status["orchestrator"] = orchStatus
	}

	if b.timeHealth != nil {
		timeStatus := b.timeHealth.GetStatus()
		th := map[string]interface{}{
			"healthy":    timeStatus.Healthy,
			"offset_ms":  timeStatus.Offset.Milliseconds(),
			"last_check": timeStatus.LastCheck.Format(time.RFC3339),
		}
		if !timeStatus.LastGoodSync.IsZero() {
			th["last_good_sync"] = timeStatus.LastGoodSync.UTC().Format(time.RFC3339)
		}
		status["time_health"] = th
	}

	if b.updateChecker != nil {
		updateStatus := b.updateChecker.Status()
		status["update"] = map[string]interface{}{
			"current_version":  updateStatus.CurrentVersion,
			"current_commit":   updateStatus.CurrentCommit,
			"latest_version":   updateStatus.LatestVersion,
			"latest_url":       updateStatus.LatestURL,
			"update_available": updateStatus.UpdateAvailable,
			"last_check":       updateStatus.LastCheck.Format(time.RFC3339),
		}
	}

	if b.apiReporter != nil {
		snap := b.apiReporter.Snapshot()
		apiStatus := map[string]interface{}{
			"configured": snap.Configured,
			"status":     snap.Status,
		}
		if !snap.LastHealthOK.IsZero() {
			apiStatus["last_health_ok"] = snap.LastHealthOK.UTC().Format(time.RFC3339)
		}
		if snap.LastError != "" {
			apiStatus["last_error"] = snap.LastError
		}
		if snap.Bootstrap != nil {
			apiStatus["bridge_id"] = snap.Bootstrap.BridgeID
			apiStatus["airport_id"] = snap.Bootstrap.Airport.ID
			apiStatus["airport_name"] = snap.Bootstrap.Airport.Name
			apiStatus["declination_deg"] = snap.Bootstrap.DeclinationDeg
			apiStatus["enabled_sources"] = snap.Bootstrap.EnabledSources
		}
		status["api_link"] = apiStatus
	}

	if b.stationManager != nil {
		stationsStatus := b.stationManager.StatusSnapshot()
		weatherStatus := map[string]interface{}{
			"stations": stationsStatus,
		}
		if payloads := b.stationManager.RecentPayloads(); len(payloads) > 0 {
			weatherStatus["recent_payloads"] = payloads
		}
		status["weather"] = weatherStatus
	}

	if hostRecovery := readRecoveryExhausted(); hostRecovery != nil {
		status["host_recovery"] = hostRecovery
	}

	return status
}

// getCaptureReadiness implements /readyz: reports not-ready (503) when enabled cameras have no recent successful capture.
// Uses AVIATIONWX_READYZ_GRACE_SECONDS (default 600) after orchestrator start before enforcing staleness,
// and AVIATIONWX_READYZ_STALE_SECONDS (default 900) as a minimum staleness window; per-camera threshold is
// max(stale, 3*capture interval) so long-interval cameras are not flagged incorrectly.
func (b *Bridge) getCaptureReadiness() (bool, string) {
	grace := envDurationSeconds("AVIATIONWX_READYZ_GRACE_SECONDS", 600)
	minStale := readyzMinStale()

	if b.orchestrator == nil {
		hasEnabled := false
		for _, c := range b.configService.ListCameras() {
			if c.Enabled {
				hasEnabled = true
				break
			}
		}
		return readinessWithNilOrchestrator(hasEnabled)
	}

	running, uptime, points := b.orchestrator.GetCaptureReadinessPoints()
	orch := scheduler.OrchestratorStatus{
		Running:     running,
		Uptime:      uptime,
		CameraCount: len(points),
		CameraStats: make([]scheduler.CameraStatus, len(points)),
	}
	for i, p := range points {
		orch.CameraStats[i] = scheduler.CameraStatus{
			CameraID: p.CameraID,
			CaptureStats: scheduler.CaptureStats{
				Interval: p.Interval,
			},
			LastSuccess: p.LastSuccess,
		}
	}
	return evalCaptureReadiness(grace, minStale, orch, time.Now())
}

func envDurationSeconds(key string, defaultSec int) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return time.Duration(defaultSec) * time.Second
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return time.Duration(defaultSec) * time.Second
	}
	return time.Duration(n) * time.Second
}

// getUpdateChannel normalizes the update channel value
func getUpdateChannel(channel string) string {
	if channel == "" || channel == "latest" {
		return "latest"
	}
	if channel == "edge" {
		return "edge"
	}
	// Default to latest for unknown values
	return "latest"
}

func syncUploadSSHHostKeys(configService *config.Service, configDir string, log *logger.Logger) {
	if err := update.SyncUploadSSHHostKeysForCameras(configDir, configService.ListCameras()); err != nil {
		log.Warn("Could not sync upload SSH host keys from HTTPS roster", "error", err)
	}
}

func syncUploadSSHHostKeysLoop(configService *config.Service, configDir string, log *logger.Logger) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		syncUploadSSHHostKeys(configService, configDir, log)
	}
}
