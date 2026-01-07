package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/camera"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/image"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/logger"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/scheduler"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/update"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/upload"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/web"
	"github.com/alexwitherspoon/aviationwx-bridge/pkg/health"
)

// Build info set at compile time via ldflags
var (
	Version   = "dev"
	GitCommit = "unknown"
)

// Bridge is the main application struct
type Bridge struct {
	config        *config.Config
	configPath    string
	orchestrator  *scheduler.Orchestrator
	webServer     *web.Server
	updateChecker *update.Checker
	systemMonitor *health.SystemMonitor
	log           *logger.Logger
}

func main() {
	// Initialize logger from environment
	logger.Init()
	log := logger.Default()

	log.Info("AviationWX Bridge starting",
		"version", Version,
		"commit", GitCommit)

	// Load configuration
	configPath := os.Getenv("AVIATIONWX_CONFIG")
	if configPath == "" {
		configPath = "/data/config.json"
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		log.Warn("Could not load config, using defaults",
			"path", configPath,
			"error", err)
		cfg = &config.Config{
			Version:  2,
			Timezone: "UTC",
			Cameras:  []config.Camera{},
			WebConsole: &config.WebConsole{
				Enabled:  true,
				Port:     1229,
				Password: "aviationwx",
			},
		}
	}

	// Create update checker
	updateChecker := update.NewChecker(Version, GitCommit)
	updateChecker.Start()
	log.Info("Update checker started")

	// Get queue path for system monitor
	queuePath := os.Getenv("AVIATIONWX_QUEUE_PATH")
	if queuePath == "" {
		queuePath = "/dev/shm/aviationwx"
	}

	// Create bridge
	bridge := &Bridge{
		config:        cfg,
		configPath:    configPath,
		updateChecker: updateChecker,
		systemMonitor: health.NewSystemMonitor(queuePath),
		log:           log,
	}

	// Initialize orchestrator
	if err := bridge.initOrchestrator(); err != nil {
		log.Warn("Could not initialize orchestrator", "error", err)
		// Continue without orchestrator - web UI will still work
	}

	// Create web server with callbacks
	bridge.webServer = web.NewServer(web.ServerConfig{
		Config:         cfg,
		ConfigPath:     configPath,
		OnConfigChange: bridge.handleConfigChange,
		GetStatus:      bridge.getStatus,
		TestCamera:     bridge.testCamera,
		TestUpload:     bridge.testUpload,
		GetCameraImage: bridge.getCameraImage,
	})

	// Start orchestrator if we have cameras
	if bridge.orchestrator != nil && len(cfg.Cameras) > 0 {
		if err := bridge.orchestrator.Start(); err != nil {
			log.Warn("Failed to start orchestrator", "error", err)
		} else {
			log.Info("Orchestrator started", "cameras", len(cfg.Cameras))
		}
	}

	// Start web server in goroutine
	go func() {
		port := cfg.GetWebPort()
		log.Info("Web console available",
			"url", fmt.Sprintf("http://localhost:%d", port),
			"password", cfg.GetWebPassword())
		if err := bridge.webServer.Start(); err != nil {
			log.Error("Web server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Info("Shutting down...")

	// Stop update checker
	if bridge.updateChecker != nil {
		bridge.updateChecker.Stop()
	}

	// Stop orchestrator
	if bridge.orchestrator != nil {
		bridge.orchestrator.Stop()
	}

	// Stop web server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := bridge.webServer.Stop(ctx); err != nil {
		log.Error("Error stopping server", "error", err)
	}

	log.Info("Goodbye!")
}

// initOrchestrator initializes the orchestrator and adds cameras
func (b *Bridge) initOrchestrator() error {
	// Get queue path from environment or use default
	queuePath := os.Getenv("AVIATIONWX_QUEUE_PATH")
	if queuePath == "" {
		queuePath = "/dev/shm/aviationwx"
	}

	// Create orchestrator
	orchConfig := scheduler.DefaultOrchestratorConfig()
	orchConfig.QueueBasePath = queuePath
	orchConfig.Timezone = b.config.Timezone
	orchConfig.Logger = newBridgeLogger(b.log)
	// Note: exiftool is used for EXIF operations (auto-detected from PATH)

	orch, err := scheduler.NewOrchestrator(orchConfig)
	if err != nil {
		return fmt.Errorf("create orchestrator: %w", err)
	}
	b.orchestrator = orch

	// Add cameras from config
	for _, camConfig := range b.config.Cameras {
		if !camConfig.Enabled {
			b.log.Info("Camera disabled, skipping", "camera", camConfig.ID)
			continue
		}

		if err := b.addCamera(camConfig); err != nil {
			b.log.Error("Failed to add camera", "camera", camConfig.ID, "error", err)
			// Continue with other cameras
		}
	}

	return nil
}

// addCamera creates and adds a camera to the orchestrator
func (b *Bridge) addCamera(camConfig config.Camera) error {
	// Create camera instance
	cam, err := b.createCamera(camConfig)
	if err != nil {
		return fmt.Errorf("create camera: %w", err)
	}

	// Create image processor if configured
	var imgProcessor *image.Processor
	if camConfig.Image != nil && camConfig.Image.NeedsProcessing() {
		imgProcessor = image.NewProcessor(camConfig.Image)
	}

	// Create scheduler camera config
	schedConfig := scheduler.CameraConfig{
		RemotePath:     camConfig.ID, // Use camera ID as remote path
		ImageProcessor: imgProcessor,
	}

	// Get capture interval
	interval := camConfig.CaptureIntervalSeconds
	if interval == 0 {
		interval = 60 // Default 60 seconds
	}

	// Add to orchestrator
	if err := b.orchestrator.AddCamera(cam, schedConfig, interval); err != nil {
		return fmt.Errorf("add to orchestrator: %w", err)
	}

	// Create and set uploader for this camera
	if camConfig.Upload != nil {
		uploader, err := b.createUploader(camConfig.Upload)
		if err != nil {
			b.log.Warn("Failed to create uploader, uploads disabled",
				"camera", camConfig.ID, "error", err)
		} else {
			b.orchestrator.SetUploader(uploader)
		}
	}

	b.log.Info("Camera added successfully",
		"camera", camConfig.ID,
		"type", camConfig.Type,
		"interval", interval)

	return nil
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

// handleConfigChange is called when config is updated via web UI
func (b *Bridge) handleConfigChange(newCfg *config.Config) error {
	// Save config to file
	if err := saveConfig(b.configPath, newCfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	// Update internal config reference
	b.config = newCfg

	// Update web server config
	b.webServer.UpdateConfig(newCfg)

	// TODO: Hot-reload cameras (for now, requires restart)
	b.log.Info("Config saved. Some changes may require restart to take effect.")

	return nil
}

// getStatus returns the current system status
func (b *Bridge) getStatus() interface{} {
	result := map[string]interface{}{
		"version":    Version,
		"git_commit": GitCommit,
	}

	// Add update status
	if b.updateChecker != nil {
		updateStatus := b.updateChecker.Status()
		result["update"] = map[string]interface{}{
			"available":      updateStatus.UpdateAvailable,
			"latest_version": updateStatus.LatestVersion,
			"latest_url":     updateStatus.LatestURL,
		}
	}

	if b.orchestrator != nil {
		status := b.orchestrator.GetStatus()
		result["running"] = status.Running
		result["uptime_seconds"] = int(status.Uptime.Seconds())
		result["uploads_today"] = status.UploadStats.UploadsSuccess
		result["queue_size"] = status.UploadStats.QueuedImages
		result["ntp_healthy"] = status.TimeInfo.TimeHealthy

		// Camera stats
		cameras := make([]map[string]interface{}, len(status.CameraStats))
		for i, cam := range status.CameraStats {
			cameras[i] = map[string]interface{}{
				"id":              cam.CameraID,
				"captures_total":  cam.CaptureStats.CapturesTotal,
				"captures_failed": cam.CaptureStats.CapturesFailed,
				"queue_count":     cam.QueueStats.ImageCount,
				"queue_size_mb":   cam.QueueStats.TotalSizeMB,
				"is_backing_off":  cam.IsBackingOff,
			}
			if !cam.LastSuccess.IsZero() {
				cameras[i]["last_success"] = cam.LastSuccess.Format(time.RFC3339)
			}
			if cam.LastError != nil {
				cameras[i]["last_error"] = cam.LastError.Error()
			}
		}
		result["cameras"] = cameras

		// Upload stats
		result["upload_stats"] = map[string]interface{}{
			"total":         status.UploadStats.UploadsTotal,
			"success":       status.UploadStats.UploadsSuccess,
			"failed":        status.UploadStats.UploadsFailed,
			"auth_failures": status.UploadStats.AuthFailures,
		}

		// Queue storage stats (for UI visibility)
		result["queue_storage"] = map[string]interface{}{
			"total_images":       status.GlobalQueueStats.TotalImages,
			"total_size_mb":      status.GlobalQueueStats.TotalSizeMB,
			"memory_usage_mb":    status.GlobalQueueStats.MemoryUsageMB,
			"memory_limit_mb":    status.GlobalQueueStats.MemoryLimitMB,
			"capacity_percent":   calculateCapacityPercent(status.GlobalQueueStats.TotalSizeMB, status.GlobalQueueStats.MemoryLimitMB),
			"filesystem_free_mb": status.GlobalQueueStats.FilesystemFreeMB,
			"filesystem_used_mb": status.GlobalQueueStats.FilesystemUsedMB,
		}
	} else {
		result["running"] = false
		result["uploads_today"] = 0
		result["queue_size"] = 0
		result["ntp_healthy"] = true
		result["queue_storage"] = map[string]interface{}{
			"total_images":       0,
			"total_size_mb":      0.0,
			"memory_usage_mb":    0.0,
			"memory_limit_mb":    100,
			"capacity_percent":   0.0,
			"filesystem_free_mb": 0.0,
			"filesystem_used_mb": 0.0,
		}
	}

	// Add system resource stats
	if b.systemMonitor != nil {
		sysStats := b.systemMonitor.GetStats()
		result["system"] = map[string]interface{}{
			"cpu_percent":    sysStats.CPUPercent,
			"cpu_level":      sysStats.CPULevel,
			"num_goroutines": sysStats.NumGoroutines,
			"num_cpu":        sysStats.NumCPU,
			"mem_used_mb":    sysStats.MemUsedMB,
			"mem_total_mb":   sysStats.MemTotalMB,
			"mem_percent":    sysStats.MemPercent,
			"mem_level":      sysStats.MemLevel,
			"heap_alloc_mb":  sysStats.HeapAllocMB,
			"disk_used_mb":   sysStats.DiskUsedMB,
			"disk_free_mb":   sysStats.DiskFreeMB,
			"disk_total_mb":  sysStats.DiskTotalMB,
			"disk_percent":   sysStats.DiskPercent,
			"disk_level":     sysStats.DiskLevel,
			"overall_level":  sysStats.OverallLevel,
			"uptime":         sysStats.Uptime,
		}
	}

	return result
}

// testCamera tests camera capture
func (b *Bridge) testCamera(camConfig config.Camera) ([]byte, error) {
	cam, err := b.createCamera(camConfig)
	if err != nil {
		return nil, fmt.Errorf("create camera: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	imageData, err := cam.Capture(ctx)
	if err != nil {
		return nil, fmt.Errorf("capture: %w", err)
	}

	// Apply image processing if configured
	if camConfig.Image != nil && camConfig.Image.NeedsProcessing() {
		processor := image.NewProcessor(camConfig.Image)
		imageData, err = processor.Process(imageData)
		if err != nil {
			return nil, fmt.Errorf("process image: %w", err)
		}
	}

	return imageData, nil
}

// testUpload tests upload connection
func (b *Bridge) testUpload(uploadConfig config.Upload) error {
	uploader, err := b.createUploader(&uploadConfig)
	if err != nil {
		return fmt.Errorf("create uploader: %w", err)
	}

	return uploader.TestConnection()
}

// getCameraImage returns the latest captured image for a camera
func (b *Bridge) getCameraImage(cameraID string) ([]byte, error) {
	// Find the camera config
	var camConfig *config.Camera
	for i := range b.config.Cameras {
		if b.config.Cameras[i].ID == cameraID {
			camConfig = &b.config.Cameras[i]
			break
		}
	}

	if camConfig == nil {
		return nil, fmt.Errorf("camera not found: %s", cameraID)
	}

	// If orchestrator is running, try to peek at queue
	// For now, just capture a fresh image
	cam, err := b.createCamera(*camConfig)
	if err != nil {
		return nil, fmt.Errorf("create camera: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	imageData, err := cam.Capture(ctx)
	if err != nil {
		return nil, fmt.Errorf("capture: %w", err)
	}

	// Apply image processing if configured
	if camConfig.Image != nil && camConfig.Image.NeedsProcessing() {
		processor := image.NewProcessor(camConfig.Image)
		imageData, err = processor.Process(imageData)
		if err != nil {
			return nil, fmt.Errorf("process image: %w", err)
		}
	}

	return imageData, nil
}

// bridgeLogger wraps logger.Logger to implement scheduler.Logger
type bridgeLogger struct {
	log *logger.Logger
}

func newBridgeLogger(log *logger.Logger) *bridgeLogger {
	return &bridgeLogger{log: log}
}

func (l *bridgeLogger) Debug(msg string, keysAndValues ...interface{}) {
	l.log.Debug(msg, keysAndValues...)
}

func (l *bridgeLogger) Info(msg string, keysAndValues ...interface{}) {
	l.log.Info(msg, keysAndValues...)
}

func (l *bridgeLogger) Warn(msg string, keysAndValues ...interface{}) {
	l.log.Warn(msg, keysAndValues...)
}

func (l *bridgeLogger) Error(msg string, keysAndValues ...interface{}) {
	l.log.Error(msg, keysAndValues...)
}

func loadConfig(path string) (*config.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func saveConfig(path string, cfg *config.Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// calculateCapacityPercent calculates percentage of queue capacity used
func calculateCapacityPercent(usedMB float64, limitMB int) float64 {
	if limitMB <= 0 {
		return 0
	}
	percent := (usedMB / float64(limitMB)) * 100
	if percent > 100 {
		percent = 100
	}
	return percent
}
