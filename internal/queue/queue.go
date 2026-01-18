package queue

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// NewQueue creates a new queue for a camera
func NewQueue(cameraID, directory string, config QueueConfig, logger Logger) (*Queue, error) {
	if logger == nil {
		logger = &defaultLogger{}
	}

	// Ensure directory exists
	if err := os.MkdirAll(directory, 0755); err != nil {
		return nil, fmt.Errorf("create queue directory: %w", err)
	}

	q := &Queue{
		config: config,
		state: QueueState{
			CameraID:  cameraID,
			Directory: directory,
		},
		pauseCapture:  make(chan struct{}, 1),
		resumeCapture: make(chan struct{}, 1),
		logger:        logger,
	}

	// Scan existing files to restore state
	if err := q.scanDirectory(); err != nil {
		return nil, fmt.Errorf("scan directory: %w", err)
	}

	return q, nil
}

// scanDirectory scans the queue directory and restores state
func (q *Queue) scanDirectory() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	files, err := q.listFilesSortedLocked()
	if err != nil {
		return err
	}

	q.state.ImageCount = len(files)
	q.state.TotalSizeBytes = 0

	for _, file := range files {
		q.state.TotalSizeBytes += file.Size()
	}

	if len(files) > 0 {
		q.state.OldestTimestamp = parseTimestampFromFilename(files[0].Name())
		q.state.NewestTimestamp = parseTimestampFromFilename(files[len(files)-1].Name())
	}

	q.updateHealthLevelLocked()

	q.logger.Info("Queue initialized from disk",
		"camera", q.state.CameraID,
		"images", q.state.ImageCount,
		"size_mb", float64(q.state.TotalSizeBytes)/(1024*1024),
		"health", q.state.HealthLevel.String())

	return nil
}

// Enqueue adds an image to the queue
func (q *Queue) Enqueue(imageData []byte, observationTime time.Time, timeSource, timeConfidence string) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Check if capture is paused
	if q.state.CapturePaused {
		return ErrCapturePaused
	}

	// Validate image data
	if len(imageData) < 100 {
		return ErrInvalidImage
	}

	// Validate observation time
	now := time.Now().UTC()
	if observationTime.After(now.Add(5 * time.Second)) {
		return ErrImageFromFuture
	}

	maxAge := time.Duration(q.config.MaxAgeSeconds) * time.Second
	if now.Sub(observationTime) > maxAge {
		return ErrImageExpired
	}

	imageSize := int64(len(imageData))

	// Pre-check: ensure we have space before attempting write
	// This prevents "no space" errors which are harder to recover from
	if !q.hasSpaceForImageLocked(imageSize) {
		q.logger.Warn("Low space detected, triggering preemptive cleanup",
			"camera", q.state.CameraID,
			"image_size", imageSize)
		q.emergencyThinForSpaceLocked(imageSize)
	}

	// Generate filename from observation time (milliseconds for uniqueness)
	filename := fmt.Sprintf("%d.jpg", observationTime.UnixMilli())
	filePath := filepath.Join(q.state.Directory, filename)

	// Handle duplicate timestamp (add 1ms)
	for {
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			break
		}
		observationTime = observationTime.Add(time.Millisecond)
		filename = fmt.Sprintf("%d.jpg", observationTime.UnixMilli())
		filePath = filepath.Join(q.state.Directory, filename)
	}

	// Attempt to write file (with retry on space error)
	written, err := q.writeImageWithRetry(filePath, imageData)
	if err != nil {
		return err
	}
	if !written {
		// Space couldn't be freed even after emergency cleanup
		// This is a critical situation - pause capture to prevent data loss
		q.state.CapturePaused = true
		select {
		case q.pauseCapture <- struct{}{}:
		default:
		}
		q.logger.Error("Cannot free enough space for new image, pausing capture",
			"camera", q.state.CameraID)
		return ErrQueueFull
	}

	// Update state
	q.state.ImageCount++
	q.state.TotalSizeBytes += imageSize
	q.state.ImagesQueued++

	if observationTime.After(q.state.NewestTimestamp) {
		q.state.NewestTimestamp = observationTime
	}
	if q.state.OldestTimestamp.IsZero() || observationTime.Before(q.state.OldestTimestamp) {
		q.state.OldestTimestamp = observationTime
	}

	// Check health and trigger thinning if needed
	q.updateHealthLevelLocked()
	if q.shouldThinLocked() {
		// Run thinning in background to not block enqueue
		go q.thinQueue()
	}

	q.logger.Debug("Image enqueued",
		"camera", q.state.CameraID,
		"filename", filename,
		"queue_size", q.state.ImageCount)

	return nil
}

// writeImageWithRetry attempts to write an image, retrying once after cleanup if space error
func (q *Queue) writeImageWithRetry(filePath string, imageData []byte) (bool, error) {
	tmpPath := filePath + ".tmp"

	// First attempt
	err := os.WriteFile(tmpPath, imageData, 0644)
	if err == nil {
		// Write succeeded, finalize
		if renameErr := os.Rename(tmpPath, filePath); renameErr != nil {
			_ = os.Remove(tmpPath) // Best effort cleanup
			return false, fmt.Errorf("rename to final: %w", renameErr)
		}
		return true, nil
	}

	// Check if it's a space error
	if !isNoSpaceError(err) {
		return false, fmt.Errorf("write temp file: %w", err)
	}

	// Space error - attempt emergency cleanup and retry
	q.logger.Warn("Write failed due to no space, attempting emergency cleanup",
		"camera", q.state.CameraID)

	// Remove failed temp file if it exists
	_ = os.Remove(tmpPath) // Best effort cleanup

	// Emergency thin - keep only 30% of files
	q.emergencyThinLocked(0.3)

	// Retry write
	err = os.WriteFile(tmpPath, imageData, 0644)
	if err != nil {
		if isNoSpaceError(err) {
			q.logger.Error("Still no space after emergency cleanup",
				"camera", q.state.CameraID)
			return false, nil // Signal caller to pause capture
		}
		return false, fmt.Errorf("write temp file after cleanup: %w", err)
	}

	// Write succeeded after retry
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath) // Best effort cleanup
		return false, fmt.Errorf("rename to final: %w", err)
	}

	q.logger.Info("Write succeeded after emergency cleanup",
		"camera", q.state.CameraID)
	return true, nil
}

// hasSpaceForImageLocked checks if there's space (must hold lock)
func (q *Queue) hasSpaceForImageLocked(sizeBytes int64) bool {
	freeSpace, err := q.getFreeSpace()
	if err != nil {
		return true // Optimistic if we can't check
	}
	// Need 2x size for tmp file + final
	return freeSpace > sizeBytes*2
}

// emergencyThinForSpaceLocked frees up space for an incoming image
func (q *Queue) emergencyThinForSpaceLocked(neededBytes int64) {
	files, err := q.listFilesSortedLocked()
	if err != nil || len(files) <= 1 {
		return
	}

	// Always keep at least the newest file
	removed := 0
	for i := 0; i < len(files)-1; i++ {
		file := files[i]
		filePath := filepath.Join(q.state.Directory, file.Name())
		if err := os.Remove(filePath); err == nil {
			q.state.ImageCount--
			q.state.TotalSizeBytes -= file.Size()
			q.state.ImagesThinned++
			removed++
		}

		// Check if we've freed enough
		if q.hasSpaceForImageLocked(neededBytes) {
			break
		}
	}

	if removed > 0 {
		q.recalculateOldestLocked()
		q.logger.Warn("Emergency space cleanup completed",
			"camera", q.state.CameraID,
			"removed", removed,
			"remaining", q.state.ImageCount)
	}
}

// emergencyThinLocked is an internal version that assumes lock is held
func (q *Queue) emergencyThinLocked(keepRatio float64) int {
	files, err := q.listFilesSortedLocked()
	if err != nil || len(files) == 0 {
		return 0
	}

	keepCount := int(float64(len(files)) * keepRatio)
	if keepCount < 1 {
		keepCount = 1
	}

	if keepCount >= len(files) {
		return 0
	}

	toRemove := files[:len(files)-keepCount]

	removed := 0
	for _, file := range toRemove {
		filePath := filepath.Join(q.state.Directory, file.Name())
		if err := os.Remove(filePath); err == nil {
			q.state.ImageCount--
			q.state.TotalSizeBytes -= file.Size()
			q.state.ImagesThinned++
			removed++
		}
	}

	if removed > 0 {
		q.recalculateOldestLocked()
		q.updateHealthLevelLocked()
	}

	return removed
}

// Dequeue returns the oldest image in the queue without removing it
// Call MarkUploaded after successful upload to remove it
func (q *Queue) Dequeue() (*QueuedImage, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	if q.state.ImageCount == 0 {
		return nil, ErrQueueEmpty
	}

	files, err := q.listFilesSortedLocked()
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		return nil, ErrQueueEmpty
	}

	oldest := files[0]
	timestamp := parseTimestampFromFilename(oldest.Name())

	return &QueuedImage{
		Filename:  oldest.Name(),
		Timestamp: timestamp,
		FilePath:  filepath.Join(q.state.Directory, oldest.Name()),
		SizeBytes: oldest.Size(),
	}, nil
}

// Peek returns images from the queue without removing them
// Returns up to 'count' images, oldest first
func (q *Queue) Peek(count int) ([]*QueuedImage, error) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	files, err := q.listFilesSortedLocked()
	if err != nil {
		return nil, err
	}

	if count > len(files) {
		count = len(files)
	}

	images := make([]*QueuedImage, count)
	for i := 0; i < count; i++ {
		file := files[i]
		images[i] = &QueuedImage{
			Filename:  file.Name(),
			Timestamp: parseTimestampFromFilename(file.Name()),
			FilePath:  filepath.Join(q.state.Directory, file.Name()),
			SizeBytes: file.Size(),
		}
	}

	return images, nil
}

// MarkUploaded removes a successfully uploaded image from the queue
func (q *Queue) MarkUploaded(img *QueuedImage) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if err := os.Remove(img.FilePath); err != nil {
		if os.IsNotExist(err) {
			// Already removed, update state anyway
			q.logger.Warn("Image already removed from queue",
				"camera", q.state.CameraID,
				"filename", img.Filename)
		} else {
			return fmt.Errorf("remove uploaded file: %w", err)
		}
	}

	q.state.ImageCount--
	if q.state.ImageCount < 0 {
		q.state.ImageCount = 0
	}
	q.state.TotalSizeBytes -= img.SizeBytes
	if q.state.TotalSizeBytes < 0 {
		q.state.TotalSizeBytes = 0
	}
	q.state.ImagesUploaded++

	// Recalculate oldest timestamp
	q.recalculateOldestLocked()

	// Check if we can resume capture
	q.updateHealthLevelLocked()
	if q.state.CapturePaused && q.canResumeCaptureLocked() {
		q.state.CapturePaused = false
		select {
		case q.resumeCapture <- struct{}{}:
		default:
		}
		q.logger.Info("Capture resumed",
			"camera", q.state.CameraID,
			"queue_size", q.state.ImageCount)
	}

	return nil
}

// ExpireOldImages removes images that exceed max age
func (q *Queue) ExpireOldImages() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	maxAge := time.Duration(q.config.MaxAgeSeconds) * time.Second
	cutoff := time.Now().UTC().Add(-maxAge)

	files, err := q.listFilesSortedLocked()
	if err != nil {
		return 0
	}

	expired := 0
	for _, file := range files {
		ts := parseTimestampFromFilename(file.Name())
		if ts.Before(cutoff) {
			filePath := filepath.Join(q.state.Directory, file.Name())
			if err := os.Remove(filePath); err == nil {
				q.state.ImageCount--
				q.state.TotalSizeBytes -= file.Size()
				q.state.ImagesExpired++
				expired++
			}
		} else {
			// Files are sorted by timestamp, no more expired files
			break
		}
	}

	if expired > 0 {
		q.recalculateOldestLocked()
		q.updateHealthLevelLocked()
		q.logger.Info("Expired old images",
			"camera", q.state.CameraID,
			"expired", expired,
			"max_age_seconds", q.config.MaxAgeSeconds)
	}

	return expired
}

// thinQueue reduces queue size by removing images from the middle
func (q *Queue) thinQueue() {
	q.mu.Lock()
	defer q.mu.Unlock()

	if !q.config.ThinningEnabled {
		return
	}

	files, err := q.listFilesSortedLocked()
	if err != nil || len(files) == 0 {
		return
	}

	// Determine target size based on health level
	var targetCount int
	switch q.state.HealthLevel {
	case HealthCatchingUp:
		targetCount = int(float64(q.config.MaxFiles) * 0.8)
	case HealthDegraded:
		targetCount = int(float64(q.config.MaxFiles) * 0.6)
	case HealthCritical:
		targetCount = int(float64(q.config.MaxFiles) * 0.4)
	default:
		return // Healthy, no thinning needed
	}

	if len(files) <= targetCount {
		return
	}

	// Protect boundaries
	protectOldest := q.config.ProtectOldest
	protectNewest := q.config.ProtectNewest

	// Ensure we don't protect more than we have
	if protectOldest+protectNewest >= len(files) {
		protectOldest = len(files) / 4
		protectNewest = len(files) / 4
	}

	// Build list of candidates (middle section)
	if protectOldest+protectNewest >= len(files) {
		return // Nothing to thin
	}

	candidateStart := protectOldest
	candidateEnd := len(files) - protectNewest
	candidates := files[candidateStart:candidateEnd]

	if len(candidates) == 0 {
		return
	}

	// Calculate how many to keep from middle
	middleTarget := targetCount - protectOldest - protectNewest
	if middleTarget < 0 {
		middleTarget = 0
	}

	toRemoveCount := len(candidates) - middleTarget
	if toRemoveCount <= 0 {
		return
	}

	// Calculate which indices to remove (evenly spaced)
	removeIndices := make(map[int]bool)
	step := float64(len(candidates)) / float64(toRemoveCount)
	for i := 0; i < toRemoveCount; i++ {
		idx := int(float64(i) * step)
		if idx < len(candidates) {
			removeIndices[idx] = true
		}
	}

	removed := 0
	for idx := range removeIndices {
		file := candidates[idx]
		filePath := filepath.Join(q.state.Directory, file.Name())
		if err := os.Remove(filePath); err == nil {
			q.state.ImageCount--
			q.state.TotalSizeBytes -= file.Size()
			q.state.ImagesThinned++
			removed++
		}
	}

	if removed > 0 {
		q.recalculateOldestLocked()
		q.updateHealthLevelLocked()
		q.logger.Info("Queue thinned",
			"camera", q.state.CameraID,
			"removed", removed,
			"remaining", q.state.ImageCount,
			"health", q.state.HealthLevel.String())
	}
}

// EmergencyThin aggressively reduces queue size, keeping only newest files
func (q *Queue) EmergencyThin(keepRatio float64) int {
	q.mu.Lock()
	defer q.mu.Unlock()

	files, err := q.listFilesSortedLocked()
	if err != nil || len(files) == 0 {
		return 0
	}

	// Keep only the newest X%
	keepCount := int(float64(len(files)) * keepRatio)
	if keepCount < 1 {
		keepCount = 1
	}

	if keepCount >= len(files) {
		return 0
	}

	// Remove oldest files
	toRemove := files[:len(files)-keepCount]

	removed := 0
	for _, file := range toRemove {
		filePath := filepath.Join(q.state.Directory, file.Name())
		if err := os.Remove(filePath); err == nil {
			q.state.ImageCount--
			q.state.TotalSizeBytes -= file.Size()
			q.state.ImagesThinned++
			removed++
		}
	}

	if removed > 0 {
		q.recalculateOldestLocked()
		q.updateHealthLevelLocked()
		q.logger.Warn("Emergency queue thin completed",
			"camera", q.state.CameraID,
			"removed", removed,
			"remaining", q.state.ImageCount)
	}

	return removed
}

// GetHealthLevel returns the current health level
func (q *Queue) GetHealthLevel() HealthLevel {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.state.HealthLevel
}

// GetImageCount returns the current number of images in queue
func (q *Queue) GetImageCount() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.state.ImageCount
}

// GetState returns a copy of the current queue state
func (q *Queue) GetState() QueueState {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.state
}

// GetStats returns queue statistics for monitoring
func (q *Queue) GetStats() QueueStats {
	q.mu.RLock()
	defer q.mu.RUnlock()

	now := time.Now().UTC()
	var oldestAge, newestAge string

	if !q.state.OldestTimestamp.IsZero() {
		oldestAge = now.Sub(q.state.OldestTimestamp).Round(time.Second).String()
	}
	if !q.state.NewestTimestamp.IsZero() {
		newestAge = now.Sub(q.state.NewestTimestamp).Round(time.Second).String()
	}

	capacityPct := q.getCapacityPercentLocked()

	return QueueStats{
		CameraID:        q.state.CameraID,
		ImageCount:      q.state.ImageCount,
		TotalSizeMB:     float64(q.state.TotalSizeBytes) / (1024 * 1024),
		OldestAge:       oldestAge,
		NewestAge:       newestAge,
		HealthLevel:     q.state.HealthLevel.String(),
		CapturePaused:   q.state.CapturePaused,
		CapacityPercent: capacityPct * 100,
		ImagesQueued:    q.state.ImagesQueued,
		ImagesUploaded:  q.state.ImagesUploaded,
		ImagesThinned:   q.state.ImagesThinned,
		ImagesExpired:   q.state.ImagesExpired,
	}
}

// PauseCapture returns the channel to receive pause signals
func (q *Queue) PauseCapture() <-chan struct{} {
	return q.pauseCapture
}

// ResumeCapture returns the channel to receive resume signals
func (q *Queue) ResumeCapture() <-chan struct{} {
	return q.resumeCapture
}

// IsCapturePaused returns whether capture is currently paused
func (q *Queue) IsCapturePaused() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.state.CapturePaused
}

// Internal methods

func (q *Queue) listFilesSortedLocked() ([]os.FileInfo, error) {
	entries, err := os.ReadDir(q.state.Directory)
	if err != nil {
		return nil, fmt.Errorf("read directory: %w", err)
	}

	var files []os.FileInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Only include .jpg files with numeric names (timestamps)
		if !strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".tmp") {
			continue
		}
		baseName := strings.TrimSuffix(name, ".jpg")
		if _, err := strconv.ParseInt(baseName, 10, 64); err != nil {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}
		files = append(files, info)
	}

	// Sort by filename (which is timestamp)
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	return files, nil
}

func (q *Queue) updateHealthLevelLocked() {
	capacityPct := q.getCapacityPercentLocked()
	prevLevel := q.state.HealthLevel

	switch {
	case capacityPct >= q.config.ThresholdCritical:
		q.state.HealthLevel = HealthCritical
		if q.config.PauseCaptureOnCritical && !q.state.CapturePaused {
			q.state.CapturePaused = true
			select {
			case q.pauseCapture <- struct{}{}:
			default:
			}
			q.logger.Warn("Queue critical - pausing capture",
				"camera", q.state.CameraID,
				"capacity_percent", capacityPct*100)
		}

	case capacityPct >= q.config.ThresholdDegraded:
		q.state.HealthLevel = HealthDegraded

	case capacityPct >= q.config.ThresholdCatchingUp:
		q.state.HealthLevel = HealthCatchingUp

	default:
		q.state.HealthLevel = HealthHealthy
	}

	if q.state.HealthLevel != prevLevel {
		q.logger.Info("Queue health changed",
			"camera", q.state.CameraID,
			"from", prevLevel.String(),
			"to", q.state.HealthLevel.String(),
			"capacity_percent", capacityPct*100)
	}
}

func (q *Queue) getCapacityPercentLocked() float64 {
	countPct := float64(q.state.ImageCount) / float64(q.config.MaxFiles)
	sizePct := float64(q.state.TotalSizeBytes) / float64(q.config.MaxSizeMB*1024*1024)

	if countPct > sizePct {
		return countPct
	}
	return sizePct
}

func (q *Queue) shouldThinLocked() bool {
	return q.state.HealthLevel >= HealthCatchingUp && q.config.ThinningEnabled
}

func (q *Queue) canResumeCaptureLocked() bool {
	capacityPct := q.getCapacityPercentLocked()
	return capacityPct < q.config.ResumeThreshold
}

func (q *Queue) recalculateOldestLocked() {
	files, err := q.listFilesSortedLocked()
	if err != nil || len(files) == 0 {
		q.state.OldestTimestamp = time.Time{}
		q.state.NewestTimestamp = time.Time{}
		return
	}

	q.state.OldestTimestamp = parseTimestampFromFilename(files[0].Name())
	q.state.NewestTimestamp = parseTimestampFromFilename(files[len(files)-1].Name())
}

// parseTimestampFromFilename extracts UTC time from filename like "1735142730000.jpg"
func parseTimestampFromFilename(filename string) time.Time {
	baseName := strings.TrimSuffix(filename, ".jpg")
	ms, err := strconv.ParseInt(baseName, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

// getFreeSpace returns the available bytes in the queue directory filesystem
func (q *Queue) getFreeSpace() (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(q.state.Directory, &stat); err != nil {
		return 0, err
	}
	// Available blocks * block size
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

// isNoSpaceError checks if an error is a "no space left on device" error
func isNoSpaceError(err error) bool {
	if err == nil {
		return false
	}
	// Check for ENOSPC
	if pathErr, ok := err.(*os.PathError); ok {
		if errno, ok := pathErr.Err.(syscall.Errno); ok {
			return errno == syscall.ENOSPC
		}
	}
	// Also check error message as fallback
	return strings.Contains(err.Error(), "no space left")
}
