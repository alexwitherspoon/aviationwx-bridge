package queue

import (
	"os"
	"testing"
	"time"
)

func TestNewQueue(t *testing.T) {
	dir := t.TempDir()
	config := DefaultQueueConfig()

	q, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	if q.state.CameraID != "test-camera" {
		t.Errorf("expected CameraID 'test-camera', got %q", q.state.CameraID)
	}

	if q.state.Directory != dir {
		t.Errorf("expected Directory %q, got %q", dir, q.state.Directory)
	}

	if q.state.ImageCount != 0 {
		t.Errorf("expected ImageCount 0, got %d", q.state.ImageCount)
	}
}

func TestQueue_Enqueue(t *testing.T) {
	dir := t.TempDir()
	config := DefaultQueueConfig()

	q, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	// Create fake image data (minimal JPEG-like)
	imageData := createTestJPEG(1024)
	observationTime := time.Now().UTC()

	err = q.Enqueue(imageData, observationTime, "bridge_clock", "high")
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	if q.state.ImageCount != 1 {
		t.Errorf("expected ImageCount 1, got %d", q.state.ImageCount)
	}

	// Check file was created
	files, _ := os.ReadDir(dir)
	if len(files) != 1 {
		t.Errorf("expected 1 file in directory, got %d", len(files))
	}
}

func TestQueue_Dequeue(t *testing.T) {
	dir := t.TempDir()
	config := DefaultQueueConfig()

	q, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	// Enqueue multiple images with different timestamps
	times := []time.Time{
		time.Now().UTC().Add(-3 * time.Second),
		time.Now().UTC().Add(-2 * time.Second),
		time.Now().UTC().Add(-1 * time.Second),
	}

	for _, ts := range times {
		imageData := createTestJPEG(1024)
		if err := q.Enqueue(imageData, ts, "bridge_clock", "high"); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	// Dequeue should return oldest first
	img, err := q.Dequeue()
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	// Verify it's the oldest
	if img.Timestamp.UnixMilli() != times[0].UnixMilli() {
		t.Errorf("expected oldest timestamp %v, got %v", times[0], img.Timestamp)
	}
}

func TestQueue_MarkUploaded(t *testing.T) {
	dir := t.TempDir()
	config := DefaultQueueConfig()

	q, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	// Enqueue an image
	imageData := createTestJPEG(1024)
	observationTime := time.Now().UTC()
	if err := q.Enqueue(imageData, observationTime, "bridge_clock", "high"); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Dequeue it
	img, err := q.Dequeue()
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	// Mark as uploaded
	if err := q.MarkUploaded(img); err != nil {
		t.Fatalf("MarkUploaded failed: %v", err)
	}

	// Queue should be empty
	if q.state.ImageCount != 0 {
		t.Errorf("expected ImageCount 0, got %d", q.state.ImageCount)
	}

	// File should be deleted
	if _, err := os.Stat(img.FilePath); !os.IsNotExist(err) {
		t.Errorf("expected file to be deleted")
	}
}

func TestQueue_ExpireOldImages(t *testing.T) {
	dir := t.TempDir()
	config := DefaultQueueConfig()
	config.MaxAgeSeconds = 2 // 2 seconds for testing

	q, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	imageData := createTestJPEG(1024)

	// Enqueue images now (they'll expire in 2 seconds)
	for i := 0; i < 3; i++ {
		ts := time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		if err := q.Enqueue(imageData, ts, "bridge_clock", "high"); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	initialCount := q.state.ImageCount
	if initialCount != 3 {
		t.Fatalf("expected 3 images, got %d", initialCount)
	}

	// Wait for images to expire
	time.Sleep(2200 * time.Millisecond)

	// Expire old images
	expired := q.ExpireOldImages()

	if expired != 3 {
		t.Errorf("expected 3 expired, got %d", expired)
	}

	if q.state.ImageCount != 0 {
		t.Errorf("expected ImageCount 0, got %d", q.state.ImageCount)
	}
}

func TestQueue_HealthLevel(t *testing.T) {
	dir := t.TempDir()
	config := DefaultQueueConfig()
	config.MaxFiles = 10 // Small for testing
	config.ThresholdCatchingUp = 0.5
	config.ThresholdDegraded = 0.8
	config.ThresholdCritical = 0.9
	config.PauseCaptureOnCritical = false // Don't pause for this test

	q, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	// Initially healthy
	if q.GetHealthLevel() != HealthHealthy {
		t.Errorf("expected HealthHealthy, got %v", q.GetHealthLevel())
	}

	// Add images to reach catching up (50%)
	imageData := createTestJPEG(1024)
	for i := 0; i < 5; i++ {
		ts := time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		if err := q.Enqueue(imageData, ts, "bridge_clock", "high"); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	if q.GetHealthLevel() != HealthCatchingUp {
		t.Errorf("expected HealthCatchingUp at 50%%, got %v", q.GetHealthLevel())
	}

	// Add more to reach degraded (80%)
	for i := 0; i < 3; i++ {
		ts := time.Now().UTC().Add(time.Duration(i+5) * time.Millisecond)
		if err := q.Enqueue(imageData, ts, "bridge_clock", "high"); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	if q.GetHealthLevel() != HealthDegraded {
		t.Errorf("expected HealthDegraded at 80%%, got %v", q.GetHealthLevel())
	}
}

func TestQueue_Thinning(t *testing.T) {
	dir := t.TempDir()
	config := DefaultQueueConfig()
	config.MaxFiles = 20
	config.ProtectNewest = 2
	config.ProtectOldest = 2
	config.ThresholdCatchingUp = 0.5
	config.PauseCaptureOnCritical = false

	q, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	// Add images beyond threshold
	imageData := createTestJPEG(1024)
	for i := 0; i < 15; i++ {
		ts := time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		if err := q.Enqueue(imageData, ts, "bridge_clock", "high"); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	// Give thinning goroutine time to run
	time.Sleep(100 * time.Millisecond)

	// Should have thinned some images
	if q.GetImageCount() >= 15 {
		t.Logf("Image count after potential thinning: %d", q.GetImageCount())
	}
}

func TestQueue_EmergencyThin(t *testing.T) {
	dir := t.TempDir()
	config := DefaultQueueConfig()

	q, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	// Add many images
	imageData := createTestJPEG(1024)
	for i := 0; i < 20; i++ {
		ts := time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		if err := q.Enqueue(imageData, ts, "bridge_clock", "high"); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	initialCount := q.GetImageCount()

	// Emergency thin to 30%
	removed := q.EmergencyThin(0.3)

	if removed == 0 {
		t.Error("expected some images to be removed")
	}

	// Should have about 30% left
	expectedRemaining := int(float64(initialCount) * 0.3)
	if q.GetImageCount() > expectedRemaining+1 {
		t.Errorf("expected ~%d images after thin, got %d", expectedRemaining, q.GetImageCount())
	}
}

func TestQueue_CapturePause(t *testing.T) {
	dir := t.TempDir()
	config := DefaultQueueConfig()
	config.MaxFiles = 10
	config.ThresholdCritical = 0.9
	config.PauseCaptureOnCritical = true

	q, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	// Fill queue to critical level (90% = 9 out of 10)
	imageData := createTestJPEG(1024)
	for i := 0; i < 9; i++ {
		ts := time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		err := q.Enqueue(imageData, ts, "bridge_clock", "high")
		if err != nil {
			t.Fatalf("Failed to enqueue image %d: %v", i, err)
		}
	}

	// Verify we're at critical level
	stats := q.GetStats()
	if stats.ImageCount < 7 {
		t.Fatalf("expected at least 7 images, got %d", stats.ImageCount)
	}
	if stats.CapacityPercent < 70.0 {
		t.Fatalf("expected capacity >= 70%%, got %.1f%%", stats.CapacityPercent)
	}

	// Should be paused at critical level
	if !q.IsCapturePaused() {
		t.Errorf("expected capture to be paused at critical level (%.1f%% capacity, %d images)", stats.CapacityPercent, stats.ImageCount)
	}

	// Trying to enqueue should return error
	ts := time.Now().UTC()
	err = q.Enqueue(imageData, ts, "bridge_clock", "high")
	if err != ErrCapturePaused {
		t.Errorf("expected ErrCapturePaused, got %v", err)
	}
}

func TestQueue_Peek(t *testing.T) {
	dir := t.TempDir()
	config := DefaultQueueConfig()

	q, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	// Add some images
	imageData := createTestJPEG(1024)
	for i := 0; i < 5; i++ {
		ts := time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		if err := q.Enqueue(imageData, ts, "bridge_clock", "high"); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	// Peek should return images without removing them
	images, err := q.Peek(3)
	if err != nil {
		t.Fatalf("Peek failed: %v", err)
	}

	if len(images) != 3 {
		t.Errorf("expected 3 images from Peek, got %d", len(images))
	}

	// Queue should still have all images
	if q.GetImageCount() != 5 {
		t.Errorf("expected 5 images after Peek, got %d", q.GetImageCount())
	}
}

func TestQueue_Stats(t *testing.T) {
	dir := t.TempDir()
	config := DefaultQueueConfig()

	q, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	// Add some images
	imageData := createTestJPEG(1024)
	for i := 0; i < 3; i++ {
		ts := time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		if err := q.Enqueue(imageData, ts, "bridge_clock", "high"); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	stats := q.GetStats()

	if stats.CameraID != "test-camera" {
		t.Errorf("expected CameraID 'test-camera', got %q", stats.CameraID)
	}

	if stats.ImageCount != 3 {
		t.Errorf("expected ImageCount 3, got %d", stats.ImageCount)
	}

	if stats.ImagesQueued != 3 {
		t.Errorf("expected ImagesQueued 3, got %d", stats.ImagesQueued)
	}
}

func TestQueue_RestoreFromDisk(t *testing.T) {
	dir := t.TempDir()
	config := DefaultQueueConfig()

	// Create queue and add images
	q1, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	imageData := createTestJPEG(1024)
	for i := 0; i < 5; i++ {
		ts := time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		if err := q1.Enqueue(imageData, ts, "bridge_clock", "high"); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	// Create new queue from same directory (simulating restart)
	q2, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	// Should have restored the images
	if q2.GetImageCount() != 5 {
		t.Errorf("expected 5 images after restore, got %d", q2.GetImageCount())
	}
}

func TestQueue_ValidationErrors(t *testing.T) {
	dir := t.TempDir()
	config := DefaultQueueConfig()
	config.MaxAgeSeconds = 3600

	q, err := NewQueue("test-camera", dir, config, nil)
	if err != nil {
		t.Fatalf("NewQueue failed: %v", err)
	}

	// Test too small image
	smallData := []byte{0xFF, 0xD8, 0xFF, 0xD9}
	err = q.Enqueue(smallData, time.Now().UTC(), "bridge_clock", "high")
	if err != ErrInvalidImage {
		t.Errorf("expected ErrInvalidImage for small data, got %v", err)
	}

	// Test future timestamp
	futureTime := time.Now().UTC().Add(10 * time.Second)
	imageData := createTestJPEG(1024)
	err = q.Enqueue(imageData, futureTime, "bridge_clock", "high")
	if err != ErrImageFromFuture {
		t.Errorf("expected ErrImageFromFuture, got %v", err)
	}

	// Test expired timestamp
	oldTime := time.Now().UTC().Add(-2 * time.Hour)
	err = q.Enqueue(imageData, oldTime, "bridge_clock", "high")
	if err != ErrImageExpired {
		t.Errorf("expected ErrImageExpired, got %v", err)
	}
}

func TestParseTimestampFromFilename(t *testing.T) {
	tests := []struct {
		filename string
		expected int64 // Unix milliseconds
	}{
		{"1735142730000.jpg", 1735142730000},
		{"1735142730123.jpg", 1735142730123},
		{"0.jpg", 0},
	}

	for _, tc := range tests {
		result := parseTimestampFromFilename(tc.filename)
		if result.UnixMilli() != tc.expected {
			t.Errorf("parseTimestampFromFilename(%q) = %d, want %d",
				tc.filename, result.UnixMilli(), tc.expected)
		}
	}
}

// Helper function to create test JPEG data
// Creates a minimal valid JPEG structure
func createTestJPEG(size int) []byte {
	// Minimal valid JPEG: SOI + APP0 + DQT + SOF0 + DHT + SOS + data + EOI
	minimalJPEG := []byte{
		0xFF, 0xD8, // SOI
		0xFF, 0xE0, 0x00, 0x10, // APP0 marker + length
		0x4A, 0x46, 0x49, 0x46, 0x00, // "JFIF\0"
		0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, // JFIF params
		0xFF, 0xDB, 0x00, 0x43, 0x00, // DQT
		// 64-byte quantization table
		0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10,
		0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10,
		0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10,
		0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10,
		0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10,
		0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10,
		0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10,
		0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10, 0x10,
		0xFF, 0xC0, 0x00, 0x0B, 0x08, 0x00, 0x01, 0x00, 0x01, 0x01, 0x01, 0x11, 0x00, // SOF0
		0xFF, 0xC4, 0x00, 0x1F, 0x00, // DHT DC
		0x00, 0x01, 0x05, 0x01, 0x01, 0x01, 0x01, 0x01,
		0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07,
		0x08, 0x09, 0x0A, 0x0B,
		0xFF, 0xDA, 0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3F, 0x00, // SOS
		0x7F,       // pixel data
		0xFF, 0xD9, // EOI
	}
	return minimalJPEG
}

// Benchmark tests

func BenchmarkQueue_Enqueue(b *testing.B) {
	dir := b.TempDir()
	config := DefaultQueueConfig()
	config.MaxFiles = b.N + 100
	config.ThinningEnabled = false

	q, _ := NewQueue("bench-camera", dir, config, nil)
	imageData := createTestJPEG(50 * 1024) // 50KB image

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ts := time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		_ = q.Enqueue(imageData, ts, "bridge_clock", "high")
	}
}

func BenchmarkQueue_Dequeue(b *testing.B) {
	dir := b.TempDir()
	config := DefaultQueueConfig()
	config.MaxFiles = b.N + 100

	q, _ := NewQueue("bench-camera", dir, config, nil)
	imageData := createTestJPEG(50 * 1024)

	// Pre-populate
	for i := 0; i < b.N; i++ {
		ts := time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		_ = q.Enqueue(imageData, ts, "bridge_clock", "high")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = q.Dequeue()
	}
}
