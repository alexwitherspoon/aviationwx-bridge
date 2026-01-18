package scheduler

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/camera"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/queue"
	timepkg "github.com/alexwitherspoon/aviationwx-bridge/internal/time"
)

// CaptureWorker handles image capture for a single camera
type CaptureWorker struct {
	camera     camera.Camera
	config     CameraConfig
	queue      *queue.Queue
	authority  *timepkg.Authority
	exifHelper *timepkg.ExifToolHelper
	state      *CameraState
	interval   time.Duration
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex
	logger     Logger
	onCapture  func(cameraID string, imageData []byte, captureTime time.Time)

	// Statistics
	capturesTotal   int64
	capturesFailed  int64
	exifReadFailed  int64
	exifWriteFailed int64
}

// CaptureWorkerConfig configures a capture worker
type CaptureWorkerConfig struct {
	Camera       camera.Camera
	CameraConfig CameraConfig
	Queue        *queue.Queue
	Authority    *timepkg.Authority
	ExifHelper   *timepkg.ExifToolHelper
	IntervalSecs int // Capture interval in seconds (1-1800, default 60)
	Logger       Logger
	OnCapture    func(cameraID string, imageData []byte, captureTime time.Time) // Called after successful capture and processing
}

// NewCaptureWorker creates a new capture worker for a camera
func NewCaptureWorker(cfg CaptureWorkerConfig) *CaptureWorker {
	ctx, cancel := context.WithCancel(context.Background())

	interval := time.Duration(cfg.IntervalSecs) * time.Second
	if interval < time.Second {
		interval = 60 * time.Second
	}
	if interval > 30*time.Minute {
		interval = 30 * time.Minute
	}

	logger := cfg.Logger
	if logger == nil {
		logger = &defaultLogger{}
	}

	return &CaptureWorker{
		camera:     cfg.Camera,
		config:     cfg.CameraConfig,
		queue:      cfg.Queue,
		authority:  cfg.Authority,
		exifHelper: cfg.ExifHelper,
		interval:   interval,
		ctx:        ctx,
		cancel:     cancel,
		logger:     logger,
		onCapture:  cfg.OnCapture,
		state: &CameraState{
			CameraID:    cfg.Camera.ID(),
			NextAttempt: time.Now(),
		},
	}
}

// Start begins the capture loop
func (w *CaptureWorker) Start() {
	go w.run()
}

// Stop stops the capture worker gracefully
func (w *CaptureWorker) Stop() {
	w.cancel()
}

// GetState returns the current camera state
func (w *CaptureWorker) GetState() CameraState {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return *w.state
}

// GetStats returns capture statistics
func (w *CaptureWorker) GetStats() CaptureStats {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return CaptureStats{
		CameraID:        w.camera.ID(),
		CapturesTotal:   w.capturesTotal,
		CapturesFailed:  w.capturesFailed,
		ExifReadFailed:  w.exifReadFailed,
		ExifWriteFailed: w.exifWriteFailed,
		Interval:        w.interval,
		QueuePaused:     w.queue.IsCapturePaused(),
	}
}

// CaptureStats provides capture statistics
type CaptureStats struct {
	CameraID        string
	CapturesTotal   int64
	CapturesFailed  int64
	ExifReadFailed  int64
	ExifWriteFailed int64
	Interval        time.Duration
	QueuePaused     bool
}

func (w *CaptureWorker) run() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Initial capture
	w.capture()

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info("Capture worker stopped", "camera", w.camera.ID())
			return

		case <-ticker.C:
			// Check if queue has paused capture due to pressure
			if w.queue.IsCapturePaused() {
				w.logger.Debug("Capture paused due to queue pressure",
					"camera", w.camera.ID())
				continue
			}

			// Check backoff
			w.mu.RLock()
			nextAttempt := w.state.NextAttempt
			w.mu.RUnlock()

			if time.Now().Before(nextAttempt) {
				continue
			}

			w.capture()

		case <-w.queue.ResumeCapture():
			w.logger.Info("Capture resumed", "camera", w.camera.ID())
		}
	}
}

func (w *CaptureWorker) capture() {
	w.mu.Lock()
	w.capturesTotal++
	w.mu.Unlock()

	// Record capture start time (bridge clock) for time authority
	captureStartUTC := time.Now().UTC()

	// Create context with timeout
	ctx, cancel := context.WithTimeout(w.ctx, 30*time.Second)
	defer cancel()

	// Capture image from camera
	imageData, err := w.camera.Capture(ctx)
	if err != nil {
		w.handleCaptureError(err)
		return
	}

	// Try to read camera EXIF timestamp (via exiftool)
	var cameraTime *time.Time
	if w.exifHelper != nil {
		// Write to temp file for exiftool to read
		tmpFile, err := os.CreateTemp("", "aviationwx-capture-*.jpg")
		if err == nil {
			tmpPath := tmpFile.Name()
			_, _ = tmpFile.Write(imageData)
			tmpFile.Close()
			defer os.Remove(tmpPath)

			result, err := w.exifHelper.ReadEXIF(tmpPath)
			if err == nil && result.Success {
				cameraTime, _ = w.exifHelper.ParseCameraTime(result)
			}
		}
	}

	// Determine observation time using authority
	var observation timepkg.ObservationResult
	if w.authority != nil {
		observation = w.authority.DetermineObservationTime(captureStartUTC, cameraTime)
	} else {
		observation = timepkg.ObservationResult{
			Time:       captureStartUTC,
			Source:     timepkg.SourceBridgeClock,
			Confidence: timepkg.ConfidenceHigh,
		}
	}

	// Log any time warnings
	if observation.Warning != nil {
		w.logger.Warn("Time observation warning",
			"camera", w.camera.ID(),
			"code", observation.Warning.Code,
			"message", observation.Warning.Message)
	}

	// Apply image processing if configured (resize/quality)
	if w.config.ImageProcessor != nil {
		processedData, err := w.config.ImageProcessor.Process(imageData)
		if err != nil {
			w.logger.Warn("Image processing failed, using original",
				"camera", w.camera.ID(),
				"error", err)
		} else {
			imageData = processedData
		}
	}

	// Stamp EXIF with bridge marker
	stampResult := timepkg.StampBridgeEXIF(imageData, observation)
	if !stampResult.Stamped {
		w.mu.Lock()
		w.exifWriteFailed++
		w.mu.Unlock()
		w.logger.Warn("EXIF stamp failed, using original image",
			"camera", w.camera.ID())
		// Continue with original image data
		stampResult.Data = imageData
	}

	// Enqueue for upload
	err = w.queue.Enqueue(
		stampResult.Data,
		observation.Time,
		string(observation.Source),
		string(observation.Confidence),
	)

	if err != nil {
		if err == queue.ErrCapturePaused {
			w.logger.Debug("Capture paused, image dropped",
				"camera", w.camera.ID())
		} else {
			w.logger.Error("Failed to enqueue image",
				"camera", w.camera.ID(),
				"error", err)
		}
		return
	}

	// Success
	w.mu.Lock()
	w.state.LastSuccess = time.Now()
	w.state.LastError = nil
	w.state.FailureCount = 0
	ResetBackoff(w.state)
	w.mu.Unlock()

	w.logger.Debug("Image captured and queued",
		"camera", w.camera.ID(),
		"observation_time", observation.Time.Format(time.RFC3339),
		"source", observation.Source)

	// Notify callback with processed image (before EXIF stamping for cleaner preview)
	if w.onCapture != nil {
		w.onCapture(w.camera.ID(), imageData, observation.Time)
	}
}

func (w *CaptureWorker) handleCaptureError(err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.capturesFailed++
	w.state.LastError = err
	w.state.LastErrorTime = time.Now()
	w.state.FailureCount++

	UpdateBackoff(w.state, DefaultBackoffConfig())

	w.logger.Error("Capture failed",
		"camera", w.camera.ID(),
		"error", err,
		"next_attempt", w.state.NextAttempt)
}

// Logger interface for capture worker
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
