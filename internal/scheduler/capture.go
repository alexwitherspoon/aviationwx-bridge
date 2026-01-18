package scheduler

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/camera"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/queue"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/resource"
	timepkg "github.com/alexwitherspoon/aviationwx-bridge/internal/time"
)

// CaptureWorker handles image capture for a single camera
type CaptureWorker struct {
	camera          camera.Camera
	config          CameraConfig
	queue           *queue.Queue
	authority       *timepkg.Authority
	exifHelper      *timepkg.ExifToolHelper
	resourceLimiter *resource.Limiter
	state           *CameraState
	interval        time.Duration
	ctx             context.Context
	cancel          context.CancelFunc
	mu              sync.RWMutex
	logger          Logger
	onCapture       func(cameraID string, imageData []byte, captureTime time.Time)

	// Statistics
	capturesTotal      int64
	capturesFailed     int64
	exifReadFailed     int64
	exifWriteFailed    int64
	nextCaptureTime    time.Time
	currentlyCapturing bool
	lastCaptureTime    time.Time
}

// CaptureWorkerConfig configures a capture worker
type CaptureWorkerConfig struct {
	Camera          camera.Camera
	CameraConfig    CameraConfig
	Queue           *queue.Queue
	Authority       *timepkg.Authority
	ExifHelper      *timepkg.ExifToolHelper
	ResourceLimiter *resource.Limiter // Optional: limits concurrent CPU-intensive work
	IntervalSecs    int               // Capture interval in seconds (1-1800, default 60)
	Logger          Logger
	OnCapture       func(cameraID string, imageData []byte, captureTime time.Time) // Called after successful capture and processing
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
		camera:          cfg.Camera,
		config:          cfg.CameraConfig,
		queue:           cfg.Queue,
		authority:       cfg.Authority,
		exifHelper:      cfg.ExifHelper,
		resourceLimiter: cfg.ResourceLimiter,
		interval:        interval,
		ctx:             ctx,
		cancel:          cancel,
		logger:          logger,
		onCapture:       cfg.OnCapture,
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
		CameraID:           w.camera.ID(),
		CapturesTotal:      w.capturesTotal,
		CapturesFailed:     w.capturesFailed,
		ExifReadFailed:     w.exifReadFailed,
		ExifWriteFailed:    w.exifWriteFailed,
		Interval:           w.interval,
		QueuePaused:        w.queue.IsCapturePaused(),
		NextCaptureTime:    w.nextCaptureTime,
		CurrentlyCapturing: w.currentlyCapturing,
		LastCaptureTime:    w.lastCaptureTime,
	}
}

// CaptureStats provides capture statistics
type CaptureStats struct {
	CameraID           string        `json:"camera_id"`
	CapturesTotal      int64         `json:"captures_total"`
	CapturesFailed     int64         `json:"captures_failed"`
	ExifReadFailed     int64         `json:"exif_read_failed"`
	ExifWriteFailed    int64         `json:"exif_write_failed"`
	Interval           time.Duration `json:"interval"`
	QueuePaused        bool          `json:"queue_paused"`
	NextCaptureTime    time.Time     `json:"next_capture_time"`
	CurrentlyCapturing bool          `json:"currently_capturing"`
	LastCaptureTime    time.Time     `json:"last_capture_time"`
}

func (w *CaptureWorker) run() {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	// Set initial next capture time
	w.mu.Lock()
	w.nextCaptureTime = time.Now().Add(w.interval)
	w.mu.Unlock()

	// Initial capture
	w.capture()

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info("Capture worker stopped", "camera", w.camera.ID())
			return

		case <-ticker.C:
			// Check if previous capture is still running
			w.mu.RLock()
			isCapturing := w.currentlyCapturing
			w.mu.RUnlock()

			if isCapturing {
				w.logger.Warn("Skipping capture - previous job still running",
					"camera", w.camera.ID(),
					"interval", w.interval)
				continue
			}

			// Update next capture time for display
			w.mu.Lock()
			w.nextCaptureTime = time.Now().Add(w.interval)
			w.mu.Unlock()

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
	w.currentlyCapturing = true
	captureInterval := w.interval
	w.mu.Unlock()

	// Calculate maximum job time: 60s + interval
	maxJobTime := 60*time.Second + captureInterval

	// Create a timeout context for the entire capture job
	jobCtx, jobCancel := context.WithTimeout(w.ctx, maxJobTime)
	defer jobCancel()

	// Ensure we clear the flag when done
	defer func() {
		w.mu.Lock()
		w.currentlyCapturing = false
		w.lastCaptureTime = time.Now()
		w.mu.Unlock()
	}()

	// Adaptive throttling: delay if system is under pressure
	// This protects the web UI by slowing down background work
	if w.resourceLimiter != nil {
		if delay := w.resourceLimiter.GetThrottleDelay(); delay > 0 {
			w.logger.Debug("Throttling capture due to system pressure",
				"camera", w.camera.ID(),
				"delay", delay)
			select {
			case <-time.After(delay):
			case <-jobCtx.Done():
				return
			}
		}
	}

	// Record capture start time (bridge clock) for time authority
	captureStartUTC := time.Now().UTC()

	// Create context with timeout for camera capture (30s is reasonable for most cameras)
	ctx, cancel := context.WithTimeout(jobCtx, 30*time.Second)
	defer cancel()

	// Capture image from camera
	imageData, err := w.camera.Capture(ctx)
	if err != nil {
		// Check if we hit the job timeout
		if jobCtx.Err() == context.DeadlineExceeded {
			w.logger.Error("Capture job exceeded maximum time",
				"camera", w.camera.ID(),
				"max_time", maxJobTime,
				"error", err)
		}
		w.handleCaptureError(err)
		return
	}

	// Try to read camera EXIF timestamp (via exiftool)
	// Use resource limiter to serialize exiftool operations
	var cameraTime *time.Time
	if w.exifHelper != nil {
		if w.resourceLimiter != nil {
			if err := w.resourceLimiter.AcquireExifOperation(jobCtx); err != nil {
				w.logger.Debug("Skipping EXIF read due to context cancellation",
					"camera", w.camera.ID())
			} else {
				defer w.resourceLimiter.ReleaseExifOperation()
				cameraTime = w.readCameraEXIF(imageData)
			}
		} else {
			cameraTime = w.readCameraEXIF(imageData)
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
	// Use resource limiter to limit concurrent CPU-intensive work
	if w.config.ImageProcessor != nil {
		if w.resourceLimiter != nil {
			if err := w.resourceLimiter.AcquireImageProcessing(jobCtx); err != nil {
				w.logger.Warn("Image processing skipped due to context cancellation",
					"camera", w.camera.ID())
			} else {
				// Yield to let pending web requests through before heavy CPU work
				resource.YieldToHigherPriority()

				processedData, err := w.config.ImageProcessor.Process(imageData)
				w.resourceLimiter.ReleaseImageProcessing()

				if err != nil {
					w.logger.Warn("Image processing failed, using original",
						"camera", w.camera.ID(),
						"error", err)
				} else {
					imageData = processedData
				}
			}
		} else {
			processedData, err := w.config.ImageProcessor.Process(imageData)
			if err != nil {
				w.logger.Warn("Image processing failed, using original",
					"camera", w.camera.ID(),
					"error", err)
			} else {
				imageData = processedData
			}
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

// readCameraEXIF reads EXIF timestamp from image data via exiftool
func (w *CaptureWorker) readCameraEXIF(imageData []byte) *time.Time {
	// Write to temp file for exiftool to read
	tmpFile, err := os.CreateTemp("", "aviationwx-capture-*.jpg")
	if err != nil {
		return nil
	}
	tmpPath := tmpFile.Name()
	_, _ = tmpFile.Write(imageData)
	tmpFile.Close()
	defer os.Remove(tmpPath)

	result, err := w.exifHelper.ReadEXIF(tmpPath)
	if err != nil || !result.Success {
		w.mu.Lock()
		w.exifReadFailed++
		w.mu.Unlock()
		return nil
	}

	cameraTime, _ := w.exifHelper.ParseCameraTime(result)
	return cameraTime
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
