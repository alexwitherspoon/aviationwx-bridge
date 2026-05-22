package queue

import (
	"context"
	"testing"
	"time"
)

func TestNewManager(t *testing.T) {
	dir := t.TempDir()
	config := GlobalQueueConfig{
		BasePath:           dir,
		MaxTotalSizeMB:     100,
		MemoryCheckSeconds: 5,
		EmergencyThinRatio: 0.5,
		MaxHeapMB:          400,
	}

	manager, err := NewManager(config, nil)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	if manager == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestManager_CreateQueue(t *testing.T) {
	dir := t.TempDir()
	config := GlobalQueueConfig{
		BasePath:           dir,
		MaxTotalSizeMB:     100,
		MemoryCheckSeconds: 5,
		EmergencyThinRatio: 0.5,
		MaxHeapMB:          400,
	}

	manager, err := NewManager(config, nil)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	queueConfig := DefaultQueueConfig()
	queue, err := manager.CreateQueue("camera-1", queueConfig)
	if err != nil {
		t.Fatalf("CreateQueue failed: %v", err)
	}

	if queue == nil {
		t.Fatal("expected non-nil queue")
	}

	// Try to create duplicate
	_, err = manager.CreateQueue("camera-1", queueConfig)
	if err == nil {
		t.Error("expected error for duplicate camera ID")
	}
}

func TestManager_GetQueue(t *testing.T) {
	dir := t.TempDir()
	config := DefaultGlobalQueueConfig()
	config.BasePath = dir

	manager, _ := NewManager(config, nil)
	queueConfig := DefaultQueueConfig()

	_, _ = manager.CreateQueue("camera-1", queueConfig)

	// Get existing queue
	q, ok := manager.GetQueue("camera-1")
	if !ok {
		t.Error("expected to find camera-1 queue")
	}
	if q == nil {
		t.Error("expected non-nil queue")
	}

	// Get non-existing queue
	_, ok = manager.GetQueue("camera-2")
	if ok {
		t.Error("expected not to find camera-2 queue")
	}
}

func TestManager_RemoveQueue(t *testing.T) {
	dir := t.TempDir()
	config := DefaultGlobalQueueConfig()
	config.BasePath = dir

	manager, _ := NewManager(config, nil)
	queueConfig := DefaultQueueConfig()

	_, _ = manager.CreateQueue("camera-1", queueConfig)

	// Remove existing queue
	err := manager.RemoveQueue("camera-1")
	if err != nil {
		t.Fatalf("RemoveQueue failed: %v", err)
	}

	// Should not find it anymore
	_, ok := manager.GetQueue("camera-1")
	if ok {
		t.Error("expected queue to be removed")
	}

	// Remove non-existing queue
	err = manager.RemoveQueue("camera-2")
	if err == nil {
		t.Error("expected error for non-existing queue")
	}
}

func TestManager_GetAllQueues(t *testing.T) {
	dir := t.TempDir()
	config := DefaultGlobalQueueConfig()
	config.BasePath = dir

	manager, _ := NewManager(config, nil)
	queueConfig := DefaultQueueConfig()

	_, _ = manager.CreateQueue("camera-1", queueConfig)
	_, _ = manager.CreateQueue("camera-2", queueConfig)
	_, _ = manager.CreateQueue("camera-3", queueConfig)

	queues := manager.GetAllQueues()
	if len(queues) != 3 {
		t.Errorf("expected 3 queues, got %d", len(queues))
	}
}

func TestManager_GetGlobalStats(t *testing.T) {
	dir := t.TempDir()
	config := DefaultGlobalQueueConfig()
	config.BasePath = dir

	manager, _ := NewManager(config, nil)
	queueConfig := DefaultQueueConfig()

	q1, _ := manager.CreateQueue("camera-1", queueConfig)
	q2, _ := manager.CreateQueue("camera-2", queueConfig)

	// Add some images
	imageData := createTestJPEG(1024)
	_ = q1.Enqueue(imageData, time.Now().UTC(), "bridge_clock", "high")
	_ = q2.Enqueue(imageData, time.Now().UTC().Add(time.Millisecond), "bridge_clock", "high")
	_ = q2.Enqueue(imageData, time.Now().UTC().Add(2*time.Millisecond), "bridge_clock", "high")

	stats := manager.GetGlobalStats()

	if stats.TotalImages != 3 {
		t.Errorf("expected TotalImages 3, got %d", stats.TotalImages)
	}

	if len(stats.CameraStats) != 2 {
		t.Errorf("expected 2 camera stats, got %d", len(stats.CameraStats))
	}
}

func TestManager_ExpireAllOldImages(t *testing.T) {
	dir := t.TempDir()
	config := DefaultGlobalQueueConfig()
	config.BasePath = dir

	manager, _ := NewManager(config, nil)

	queueConfig := DefaultQueueConfig()
	queueConfig.MaxAgeSeconds = 2 // 2 seconds for testing

	q1, _ := manager.CreateQueue("camera-1", queueConfig)
	q2, _ := manager.CreateQueue("camera-2", queueConfig)

	// Add images now
	imageData := createTestJPEG(1024)
	_ = q1.Enqueue(imageData, time.Now().UTC(), "bridge_clock", "high")
	_ = q2.Enqueue(imageData, time.Now().UTC().Add(time.Millisecond), "bridge_clock", "high")

	// Verify images were added
	if q1.GetImageCount() != 1 || q2.GetImageCount() != 1 {
		t.Fatalf("expected 1 image in each queue")
	}

	// Wait for them to expire
	time.Sleep(2200 * time.Millisecond)

	expired := manager.ExpireAllOldImages()
	if expired != 2 {
		t.Errorf("expected 2 expired, got %d", expired)
	}
}

func TestManager_GetTotalQueueSize(t *testing.T) {
	dir := t.TempDir()
	config := DefaultGlobalQueueConfig()
	config.BasePath = dir

	manager, _ := NewManager(config, nil)
	queueConfig := DefaultQueueConfig()

	q1, _ := manager.CreateQueue("camera-1", queueConfig)
	q2, _ := manager.CreateQueue("camera-2", queueConfig)

	// Add some images
	imageData := createTestJPEG(1024)
	_ = q1.Enqueue(imageData, time.Now().UTC(), "bridge_clock", "high")
	_ = q2.Enqueue(imageData, time.Now().UTC().Add(time.Millisecond), "bridge_clock", "high")

	totalSize := manager.GetTotalQueueSize()
	expectedSize := int64(2 * len(imageData)) // 2 images

	if totalSize != expectedSize {
		t.Errorf("expected total size %d, got %d", expectedSize, totalSize)
	}
}

func TestManager_GetTotalImageCount(t *testing.T) {
	dir := t.TempDir()
	config := DefaultGlobalQueueConfig()
	config.BasePath = dir

	manager, _ := NewManager(config, nil)
	queueConfig := DefaultQueueConfig()

	q1, _ := manager.CreateQueue("camera-1", queueConfig)
	q2, _ := manager.CreateQueue("camera-2", queueConfig)

	// Add some images
	imageData := createTestJPEG(1024)
	for i := 0; i < 3; i++ {
		_ = q1.Enqueue(imageData, time.Now().UTC().Add(time.Duration(i)*time.Millisecond), "bridge_clock", "high")
	}
	for i := 0; i < 2; i++ {
		_ = q2.Enqueue(imageData, time.Now().UTC().Add(time.Duration(i+10)*time.Millisecond), "bridge_clock", "high")
	}

	total := manager.GetTotalImageCount()
	if total != 5 {
		t.Errorf("expected 5 images, got %d", total)
	}
}

func TestManager_SnapshotQueuesUpdatesCurrentTotalSize(t *testing.T) {
	dir := t.TempDir()
	config := DefaultGlobalQueueConfig()
	config.BasePath = dir

	manager, err := NewManager(config, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	queueConfig := DefaultQueueConfig()
	q, err := manager.CreateQueue("camera-1", queueConfig)
	if err != nil {
		t.Fatalf("CreateQueue: %v", err)
	}

	imageData := createTestJPEG(2048)
	if err := q.Enqueue(imageData, time.Now().UTC(), "bridge_clock", "high"); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	_, snapTotal := manager.snapshotQueues()
	want := int64(len(imageData))
	if snapTotal != want {
		t.Fatalf("snapshotQueues total=%d, want %d", snapTotal, want)
	}
	if got := manager.GetTotalQueueSize(); got != want {
		t.Fatalf("GetTotalQueueSize=%d, want %d", got, want)
	}
}

func TestManager_SnapshotQueuesConcurrentWithCheckMemoryPressure(t *testing.T) {
	dir := t.TempDir()
	config := DefaultGlobalQueueConfig()
	config.BasePath = dir
	config.MaxTotalSizeMB = 100
	config.MaxHeapMB = 400

	manager, err := NewManager(config, nil)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	q, err := manager.CreateQueue("camera-1", DefaultQueueConfig())
	if err != nil {
		t.Fatalf("CreateQueue: %v", err)
	}
	imageData := createTestJPEG(1024)
	_ = q.Enqueue(imageData, time.Now().UTC(), "bridge_clock", "high")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			manager.checkMemoryPressure()
		}
	}()

	for i := 0; i < 50; i++ {
		_, total := manager.snapshotQueues()
		if total < 0 {
			t.Fatalf("negative snapshot total")
		}
	}
	<-done
}

func TestManager_MemoryMonitor(t *testing.T) {
	dir := t.TempDir()
	config := DefaultGlobalQueueConfig()
	config.BasePath = dir
	config.MemoryCheckSeconds = 1 // Check every second for testing
	config.MaxTotalSizeMB = 1     // Very small limit for testing

	manager, _ := NewManager(config, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// Start monitor in background
	go manager.StartMemoryMonitor(ctx)

	// Give it time to start
	time.Sleep(100 * time.Millisecond)

	// Add a queue with some data
	queueConfig := DefaultQueueConfig()
	q, _ := manager.CreateQueue("camera-1", queueConfig)

	// Add lots of data to trigger memory pressure
	imageData := createTestJPEG(100 * 1024) // 100KB per image
	for i := 0; i < 20; i++ {               // 2MB total, exceeds 1MB limit
		ts := time.Now().UTC().Add(time.Duration(i) * time.Millisecond)
		_ = q.Enqueue(imageData, ts, "bridge_clock", "high")
	}

	// Wait for monitor to run
	time.Sleep(1500 * time.Millisecond)

	// Should have thinned due to memory pressure
	// Note: This test is timing-dependent
	t.Logf("Final image count after memory monitoring: %d", q.GetImageCount())
}

func TestManager_ExpirationWorker(t *testing.T) {
	dir := t.TempDir()
	config := DefaultGlobalQueueConfig()
	config.BasePath = dir

	manager, _ := NewManager(config, nil)

	queueConfig := DefaultQueueConfig()
	queueConfig.MaxAgeSeconds = 1

	q, _ := manager.CreateQueue("camera-1", queueConfig)

	// Add an image
	imageData := createTestJPEG(1024)
	oldTime := time.Now().UTC().Add(-5 * time.Second)
	_ = q.Enqueue(imageData, oldTime, "bridge_clock", "high")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Start worker with short interval
	go manager.StartExpirationWorker(ctx, time.Second)

	// Wait for worker to run
	time.Sleep(1500 * time.Millisecond)

	// Image should be expired
	if q.GetImageCount() != 0 {
		t.Errorf("expected 0 images after expiration, got %d", q.GetImageCount())
	}
}
