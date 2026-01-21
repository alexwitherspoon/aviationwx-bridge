package scheduler

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/queue"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/upload"
)

// UploadWorker handles uploading queued images to the server
// Supports concurrent uploads (default: 3) with connection rate limiting
// Uses newest-first (LIFO) when catching up, oldest-first (FIFO) otherwise
// Each camera has its own uploader with independent credentials
type UploadWorker struct {
	queues     map[string]*queue.Queue  // Camera ID -> Queue
	queueOrder []string                 // Order for round-robin
	configs    map[string]CameraConfig  // Camera ID -> Config
	uploaders  map[string]upload.Client // Camera ID -> Uploader (per-camera credentials)
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex
	logger     Logger

	// Concurrent upload configuration
	maxConcurrent      int        // Max concurrent uploads (default: 3)
	catchupThreshold   int        // Queue size to trigger LIFO mode (default: 20)
	activeUploads      int        // Current number of active uploads
	connectionMutex    sync.Mutex // Ensures only one connection established at a time
	lastConnectionTime time.Time  // Track last connection for rate limiting

	// Fail2ban protection
	minUploadInterval  time.Duration // Minimum time between uploads (rate limit)
	authBackoff        time.Duration // Backoff after auth failure
	retryDelay         time.Duration // Delay before single retry
	connectionInterval time.Duration // Minimum time between new connections (default: 2s)

	// Statistics
	uploadsTotal      int64
	uploadsSuccess    int64
	uploadsFailed     int64
	uploadsRetried    int64
	authFailures      int64
	lastUploadTime    time.Time
	lastSuccessTime   time.Time
	lastFailureTime   time.Time
	lastFailureReason string

	// Per-camera failure tracking (for fail2ban awareness)
	cameraFailures map[string]*uploadFailureState
}

// uploadFailureState tracks failures for a single camera
type uploadFailureState struct {
	consecutiveFailures int
	lastFailure         time.Time
	lastAuthFailure     time.Time
	backoffUntil        time.Time
}

// uploadTask represents a single upload job
type uploadTask struct {
	cameraID   string
	image      *queue.QueuedImage
	queue      *queue.Queue
	config     CameraConfig
	uploader   upload.Client
	remotePath string
}

// UploadWorkerConfig configures the upload worker
// Note: Individual uploaders are set per-camera via AddQueue
type UploadWorkerConfig struct {
	MaxConcurrent      int           // Maximum concurrent uploads (default: 3)
	CatchupThreshold   int           // Queue size to trigger LIFO mode (default: 20)
	MinUploadInterval  time.Duration // Minimum time between uploads (default: 1 second)
	AuthBackoff        time.Duration // Backoff after auth failure (default: 60 seconds)
	RetryDelay         time.Duration // Delay before single retry (default: 5 seconds)
	ConnectionInterval time.Duration // Minimum time between new connections (default: 2 seconds)
	Logger             Logger
}

// NewUploadWorker creates a new upload worker
func NewUploadWorker(cfg UploadWorkerConfig) *UploadWorker {
	ctx, cancel := context.WithCancel(context.Background())

	// Apply defaults
	maxConcurrent := cfg.MaxConcurrent
	if maxConcurrent == 0 {
		maxConcurrent = 3 // Default: 3 concurrent uploads
	}

	catchupThreshold := cfg.CatchupThreshold
	if catchupThreshold == 0 {
		catchupThreshold = 20 // Default: LIFO mode when queue > 20
	}

	minInterval := cfg.MinUploadInterval
	if minInterval == 0 {
		minInterval = time.Second
	}

	authBackoff := cfg.AuthBackoff
	if authBackoff == 0 {
		authBackoff = 60 * time.Second // Long backoff for auth failures (fail2ban protection)
	}

	retryDelay := cfg.RetryDelay
	if retryDelay == 0 {
		retryDelay = 5 * time.Second
	}

	connectionInterval := cfg.ConnectionInterval
	if connectionInterval == 0 {
		connectionInterval = 2 * time.Second // Stagger connection establishment
	}

	logger := cfg.Logger
	if logger == nil {
		logger = &defaultLogger{}
	}

	return &UploadWorker{
		queues:             make(map[string]*queue.Queue),
		queueOrder:         make([]string, 0),
		configs:            make(map[string]CameraConfig),
		uploaders:          make(map[string]upload.Client),
		ctx:                ctx,
		cancel:             cancel,
		logger:             logger,
		maxConcurrent:      maxConcurrent,
		catchupThreshold:   catchupThreshold,
		minUploadInterval:  minInterval,
		authBackoff:        authBackoff,
		retryDelay:         retryDelay,
		connectionInterval: connectionInterval,
		cameraFailures:     make(map[string]*uploadFailureState),
	}
}

// AddQueue adds a camera queue to the upload worker with its own uploader
func (w *UploadWorker) AddQueue(cameraID string, q *queue.Queue, config CameraConfig, uploader upload.Client) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.queues[cameraID] = q
	w.configs[cameraID] = config
	w.uploaders[cameraID] = uploader
	w.queueOrder = append(w.queueOrder, cameraID)
	w.cameraFailures[cameraID] = &uploadFailureState{}
}

// RemoveQueue removes a camera queue from the upload worker
func (w *UploadWorker) RemoveQueue(cameraID string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	delete(w.queues, cameraID)
	delete(w.configs, cameraID)
	delete(w.uploaders, cameraID)
	delete(w.cameraFailures, cameraID)

	// Remove from queueOrder
	for i, id := range w.queueOrder {
		if id == cameraID {
			w.queueOrder = append(w.queueOrder[:i], w.queueOrder[i+1:]...)
			break
		}
	}
}

// Start begins the upload loop
func (w *UploadWorker) Start() {
	go w.run()
}

// Stop stops the upload worker gracefully
func (w *UploadWorker) Stop() {
	w.cancel()
}

// GetStats returns upload statistics
func (w *UploadWorker) GetStats() UploadStats {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var queuedTotal int
	for _, q := range w.queues {
		queuedTotal += q.GetImageCount()
	}

	uploadRate := float64(0)
	if !w.lastSuccessTime.IsZero() {
		elapsed := time.Since(w.lastSuccessTime)
		if elapsed > 0 && w.uploadsSuccess > 0 {
			// Simple rate: uploads per minute over last period
			uploadRate = float64(w.uploadsSuccess) / elapsed.Minutes()
		}
	}

	return UploadStats{
		UploadsTotal:       w.uploadsTotal,
		UploadsSuccess:     w.uploadsSuccess,
		UploadsFailed:      w.uploadsFailed,
		UploadsRetried:     w.uploadsRetried,
		AuthFailures:       w.authFailures,
		QueuedImages:       queuedTotal,
		LastUploadTime:     w.lastUploadTime,
		LastSuccessTime:    w.lastSuccessTime,
		LastFailureTime:    w.lastFailureTime,
		LastFailureReason:  w.lastFailureReason,
		UploadRatePerMin:   uploadRate,
		PerCameraFailures:  w.copyFailureStats(),
		CurrentlyUploading: w.activeUploads > 0,
		ActiveUploads:      w.activeUploads,
	}
}

// copyFailureStats creates a copy of per-camera failure counts (caller must hold lock)
func (w *UploadWorker) copyFailureStats() map[string]int64 {
	copy := make(map[string]int64, len(w.cameraFailures))
	for id, state := range w.cameraFailures {
		copy[id] = int64(state.consecutiveFailures)
	}
	return copy
}

// UploadStats provides upload statistics
type UploadStats struct {
	UploadsTotal       int64            `json:"uploads_total"`
	UploadsSuccess     int64            `json:"uploads_success"`
	UploadsFailed      int64            `json:"uploads_failed"`
	UploadsRetried     int64            `json:"uploads_retried"`
	AuthFailures       int64            `json:"auth_failures"`
	QueuedImages       int              `json:"queued_images"`
	LastUploadTime     time.Time        `json:"last_upload_time"`
	LastSuccessTime    time.Time        `json:"last_success_time"`
	LastFailureTime    time.Time        `json:"last_failure_time"`
	LastFailureReason  string           `json:"last_failure_reason"`
	UploadRatePerMin   float64          `json:"upload_rate_per_min"`
	PerCameraFailures  map[string]int64 `json:"per_camera_failures"` // Track failures per camera
	CurrentlyUploading bool             `json:"currently_uploading"`
	ActiveUploads      int              `json:"active_uploads"` // Number of concurrent uploads in progress
}

func (w *UploadWorker) run() {
	// Panic recovery: if this goroutine panics, log and restart after delay
	defer func() {
		if r := recover(); r != nil {
			w.logger.Error("Upload worker panicked, will restart",
				"panic", r,
				"stack", string(debug.Stack()))

			// Wait before restarting to avoid tight panic loop
			time.Sleep(10 * time.Second)

			// Only restart if context is still active (not explicitly stopped)
			if w.ctx.Err() == nil {
				w.logger.Info("Restarting upload worker after panic")
				go w.run()
			}
		}
	}()

	w.logger.Info("Upload worker started",
		"max_concurrent", w.maxConcurrent,
		"catchup_threshold", w.catchupThreshold)

	// Work channel for distributing upload tasks
	workChan := make(chan uploadTask, w.maxConcurrent*2)

	// Start worker goroutines
	var wg sync.WaitGroup
	for i := 0; i < w.maxConcurrent; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			w.uploadWorkerRoutine(workerID, workChan)
		}(i)
	}

	// Main coordinator loop
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			w.logger.Info("Upload worker stopping")
			close(workChan)
			wg.Wait()
			w.logger.Info("Upload worker stopped")
			return

		case <-ticker.C:
			w.scheduleUploads(workChan)
		}
	}
}

// uploadWorkerRoutine is a worker goroutine that processes upload tasks
func (w *UploadWorker) uploadWorkerRoutine(workerID int, workChan <-chan uploadTask) {
	for task := range workChan {
		// Increment active uploads counter
		w.mu.Lock()
		w.activeUploads++
		w.mu.Unlock()

		// Perform upload with retry
		success := w.uploadWithRetry(task.cameraID, task.uploader, task.image, task.remotePath)

		if success {
			// Mark as uploaded (removes from queue)
			if err := task.queue.MarkUploaded(task.image); err != nil {
				w.logger.Error("Failed to mark uploaded",
					"worker", workerID,
					"camera", task.cameraID,
					"error", err)
			}

			// Reset failure state
			w.mu.Lock()
			if failState, exists := w.cameraFailures[task.cameraID]; exists {
				failState.consecutiveFailures = 0
			}
			w.mu.Unlock()
		}

		// Decrement active uploads counter
		w.mu.Lock()
		w.activeUploads--
		w.mu.Unlock()
	}
}

// scheduleUploads coordinates which images to upload next
func (w *UploadWorker) scheduleUploads(workChan chan<- uploadTask) {
	w.mu.RLock()
	if len(w.queueOrder) == 0 {
		w.mu.RUnlock()
		return
	}

	// Check how many upload slots are available
	availableSlots := w.maxConcurrent - w.activeUploads
	if availableSlots <= 0 {
		w.mu.RUnlock()
		return
	}

	// Get total queue size to determine if we're catching up
	totalQueued := 0
	for _, q := range w.queues {
		totalQueued += q.GetImageCount()
	}

	// Determine mode: LIFO (newest first) when catching up, FIFO (oldest first) otherwise
	newestFirst := totalQueued > w.catchupThreshold

	if newestFirst {
		w.logger.Debug("Catch-up mode active (LIFO)",
			"queued", totalQueued,
			"threshold", w.catchupThreshold)
	}

	// Round-robin across cameras
	cameras := make([]string, len(w.queueOrder))
	copy(cameras, w.queueOrder)
	w.mu.RUnlock()

	tasksScheduled := 0
	for _, cameraID := range cameras {
		if tasksScheduled >= availableSlots {
			break
		}

		w.mu.RLock()
		q := w.queues[cameraID]
		config := w.configs[cameraID]
		uploader := w.uploaders[cameraID]
		failState := w.cameraFailures[cameraID]
		w.mu.RUnlock()

		// Skip if camera is in backoff
		if time.Now().Before(failState.backoffUntil) {
			continue
		}

		// Skip if no uploader configured
		if uploader == nil {
			continue
		}

		// Try to get an image from this camera's queue
		img, err := q.Dequeue()
		if err == queue.ErrQueueEmpty {
			continue
		}
		if err != nil {
			w.logger.Error("Failed to dequeue",
				"camera", cameraID,
				"error", err)
			continue
		}

		// Build remote path
		remotePath := w.buildRemotePath(config.RemotePath, cameraID, img.Timestamp)

		// Send task to workers
		select {
		case workChan <- uploadTask{
			cameraID:   cameraID,
			image:      img,
			queue:      q,
			config:     config,
			uploader:   uploader,
			remotePath: remotePath,
		}:
			tasksScheduled++
		default:
			// Channel full, will try again next tick
		}
	}
}

func (w *UploadWorker) uploadWithRetry(cameraID string, uploader upload.Client, img *queue.QueuedImage, remotePath string) bool {
	w.mu.Lock()
	w.uploadsTotal++
	w.lastUploadTime = time.Now()
	w.mu.Unlock()

	// Connection rate limiting: only one new connection at a time
	// This prevents triggering fail2ban with multiple simultaneous logins
	w.connectionMutex.Lock()
	if !w.lastConnectionTime.IsZero() {
		elapsed := time.Since(w.lastConnectionTime)
		if elapsed < w.connectionInterval {
			time.Sleep(w.connectionInterval - elapsed)
		}
	}
	w.lastConnectionTime = time.Now()
	w.connectionMutex.Unlock()

	// Maximum upload time: 120 seconds (generous for slow connections)
	maxUploadTime := 120 * time.Second
	uploadDeadline := time.After(maxUploadTime)

	// Channel for upload result
	type uploadResult struct {
		success bool
		err     error
	}
	resultCh := make(chan uploadResult, 1)

	// Run upload in goroutine with timeout protection
	go func() {
		// Read image data from file
		imageData, err := readImageFile(img.FilePath)
		if err != nil {
			w.logger.Error("Failed to read image file",
				"camera", cameraID,
				"path", img.FilePath,
				"error", err)
			resultCh <- uploadResult{false, err}
			return
		}

		// First attempt
		err = uploader.Upload(remotePath, imageData)
		if err == nil {
			w.logger.Debug("Upload successful",
				"camera", cameraID,
				"path", remotePath,
				"size", len(imageData))
			resultCh <- uploadResult{true, nil}
			return
		}

		// First attempt failed - analyze error
		w.logger.Warn("Upload failed, will retry once",
			"camera", cameraID,
			"error", err)

		// Check if auth error (fail2ban sensitive)
		if w.isAuthError(err) {
			resultCh <- uploadResult{false, err}
			return
		}

		// Wait before retry
		time.Sleep(w.retryDelay)

		// Second (and final) attempt
		w.mu.Lock()
		w.uploadsRetried++
		w.mu.Unlock()

		err = uploader.Upload(remotePath, imageData)
		if err == nil {
			w.logger.Info("Upload succeeded on retry",
				"camera", cameraID,
				"path", remotePath)
			resultCh <- uploadResult{true, nil}
			return
		}

		w.logger.Error("Upload failed after retry",
			"camera", cameraID,
			"error", err)
		resultCh <- uploadResult{false, err}
	}()

	// Wait for result or timeout
	select {
	case result := <-resultCh:
		if result.success {
			w.recordSuccess()
			return true
		}
		w.recordFailure(cameraID, result.err)
		if w.isAuthError(result.err) {
			w.handleAuthFailure(cameraID)
		}
		return false

	case <-uploadDeadline:
		w.logger.Error("Upload exceeded maximum time",
			"camera", cameraID,
			"max_time", maxUploadTime)
		w.recordFailure(cameraID, fmt.Errorf("upload timeout after %v", maxUploadTime))
		return false
	}
}

func (w *UploadWorker) buildRemotePath(basePath, cameraID string, timestamp time.Time) string {
	if basePath == "" {
		basePath = cameraID
	}

	// Ensure path doesn't end with /
	basePath = strings.TrimSuffix(basePath, "/")

	// Use millisecond timestamp for filename
	filename := fmt.Sprintf("%d.jpg", timestamp.UnixMilli())

	return fmt.Sprintf("%s/%s", basePath, filename)
}

func (w *UploadWorker) isAuthError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "auth") ||
		strings.Contains(errStr, "401") ||
		strings.Contains(errStr, "403") ||
		strings.Contains(errStr, "login") ||
		strings.Contains(errStr, "credential") ||
		strings.Contains(errStr, "permission") ||
		strings.Contains(errStr, "access denied")
}

func (w *UploadWorker) handleAuthFailure(cameraID string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.authFailures++
	w.uploadsFailed++
	w.lastFailureTime = time.Now()

	failState := w.cameraFailures[cameraID]
	failState.lastAuthFailure = time.Now()
	failState.backoffUntil = time.Now().Add(w.authBackoff)
	failState.consecutiveFailures++

	w.logger.Warn("Auth failure - backing off to avoid fail2ban",
		"camera", cameraID,
		"backoff_until", failState.backoffUntil,
		"consecutive_failures", failState.consecutiveFailures)
}

func (w *UploadWorker) recordSuccess() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.uploadsSuccess++
	w.lastSuccessTime = time.Now()
}

func (w *UploadWorker) recordFailure(cameraID string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.uploadsFailed++
	w.lastFailureTime = time.Now()
	w.lastFailureReason = err.Error()

	failState := w.cameraFailures[cameraID]
	failState.lastFailure = time.Now()
	failState.consecutiveFailures++

	// Exponential backoff for repeated failures (but less aggressive than auth)
	if failState.consecutiveFailures > 3 {
		backoffDuration := time.Duration(failState.consecutiveFailures*5) * time.Second
		if backoffDuration > 30*time.Second {
			backoffDuration = 30 * time.Second
		}
		failState.backoffUntil = time.Now().Add(backoffDuration)
	}
}

// readImageFile reads image data from a file
func readImageFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
