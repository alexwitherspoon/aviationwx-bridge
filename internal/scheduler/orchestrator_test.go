package scheduler

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// TestOrchestrator_AddCamera tests adding a camera to the orchestrator
func TestOrchestrator_AddCamera(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-orchestrator-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultOrchestratorConfig()
	config.QueueBasePath = tmpDir
	orch, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	defer orch.Stop()

	cam := &mockCamera{id: "test-cam", camType: "http"}
	camConfig := CameraConfig{
		ID:         "test-cam",
		RemotePath: "test/path",
		Enabled:    true,
	}
	uploader := &mockUploader{}

	err = orch.AddCamera(cam, camConfig, 60, uploader, nil)
	if err != nil {
		t.Errorf("AddCamera() error = %v", err)
	}

	// Verify camera was added
	if len(orch.captureWorkers) != 1 {
		t.Errorf("Expected 1 capture worker, got %d", len(orch.captureWorkers))
	}
	if _, ok := orch.captureWorkers["test-cam"]; !ok {
		t.Error("Camera worker not found")
	}

	// Verify upload worker was created
	if orch.uploadWorker == nil {
		t.Error("Upload worker should be created")
	}

	// Verify queue was created
	_, ok := orch.queueManager.GetQueue("test-cam")
	if !ok {
		t.Error("Queue was not created for camera")
	}
}

// TestOrchestrator_AddMultipleCameras tests adding multiple cameras
func TestOrchestrator_AddMultipleCameras(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-orchestrator-multi-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultOrchestratorConfig()
	config.QueueBasePath = tmpDir
	orch, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	defer orch.Stop()

	// Add first camera
	cam1 := &mockCamera{id: "cam1", camType: "http"}
	uploader1 := &mockUploader{}
	err = orch.AddCamera(cam1, CameraConfig{ID: "cam1", Enabled: true}, 60, uploader1, nil)
	if err != nil {
		t.Errorf("AddCamera(cam1) error = %v", err)
	}

	// Add second camera with different uploader
	cam2 := &mockCamera{id: "cam2", camType: "rtsp"}
	uploader2 := &mockUploader{}
	err = orch.AddCamera(cam2, CameraConfig{ID: "cam2", Enabled: true}, 120, uploader2, nil)
	if err != nil {
		t.Errorf("AddCamera(cam2) error = %v", err)
	}

	// Verify both cameras were added
	if len(orch.captureWorkers) != 2 {
		t.Errorf("Expected 2 capture workers, got %d", len(orch.captureWorkers))
	}

	// Verify upload worker has both cameras
	if len(orch.uploadWorker.uploaders) != 2 {
		t.Errorf("Expected 2 uploaders in upload worker, got %d", len(orch.uploadWorker.uploaders))
	}

	// Verify each camera has its own uploader
	if orch.uploadWorker.uploaders["cam1"] != uploader1 {
		t.Error("cam1 should have uploader1")
	}
	if orch.uploadWorker.uploaders["cam2"] != uploader2 {
		t.Error("cam2 should have uploader2")
	}
}

// TestOrchestrator_RemoveCamera tests removing a camera from the orchestrator
func TestOrchestrator_RemoveCamera(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-orchestrator-remove-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultOrchestratorConfig()
	config.QueueBasePath = tmpDir
	orch, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	defer orch.Stop()

	// Add two cameras
	cam1 := &mockCamera{id: "cam1", camType: "http"}
	cam2 := &mockCamera{id: "cam2", camType: "http"}
	uploader := &mockUploader{}

	orch.AddCamera(cam1, CameraConfig{ID: "cam1", Enabled: true}, 60, uploader, nil)
	orch.AddCamera(cam2, CameraConfig{ID: "cam2", Enabled: true}, 60, uploader, nil)

	// Verify both cameras exist
	if len(orch.captureWorkers) != 2 {
		t.Fatalf("Expected 2 cameras before removal, got %d", len(orch.captureWorkers))
	}

	// Remove one camera
	err = orch.RemoveCamera("cam1")
	if err != nil {
		t.Errorf("RemoveCamera() error = %v", err)
	}

	// Verify camera was removed
	if len(orch.captureWorkers) != 1 {
		t.Errorf("Expected 1 camera after removal, got %d", len(orch.captureWorkers))
	}
	if _, ok := orch.captureWorkers["cam1"]; ok {
		t.Error("cam1 should have been removed")
	}
	if _, ok := orch.captureWorkers["cam2"]; !ok {
		t.Error("cam2 should still exist")
	}

	// Verify queue was removed
	_, ok := orch.queueManager.GetQueue("cam1")
	if ok {
		t.Error("Queue for cam1 should have been removed")
	}

	// Verify upload worker removed the camera
	if _, ok := orch.uploadWorker.uploaders["cam1"]; ok {
		t.Error("cam1 uploader should have been removed from upload worker")
	}
}

// TestOrchestrator_RemoveCamera_NonExistent tests removing a non-existent camera
func TestOrchestrator_RemoveCamera_NonExistent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-orchestrator-remove-nonexist-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultOrchestratorConfig()
	config.QueueBasePath = tmpDir
	orch, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	defer orch.Stop()

	// Try to remove non-existent camera - should error
	err = orch.RemoveCamera("nonexistent")
	if err == nil {
		t.Error("RemoveCamera() should error for non-existent camera")
	}
}

// TestOrchestrator_Start tests starting the orchestrator
func TestOrchestrator_Start(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-orchestrator-start-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultOrchestratorConfig()
	config.QueueBasePath = tmpDir
	orch, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	defer orch.Stop()

	// Add a camera
	cam := &mockCamera{id: "test-cam", camType: "http"}
	uploader := &mockUploader{}
	orch.AddCamera(cam, CameraConfig{ID: "test-cam", Enabled: true}, 60, uploader, nil)

	// Start orchestrator
	err = orch.Start()
	if err != nil {
		t.Errorf("Start() error = %v", err)
	}

	// Give it time to initialize
	time.Sleep(50 * time.Millisecond)

	// Verify status
	status := orch.GetStatus()
	if !status.Running {
		t.Error("Orchestrator should be running")
	}
	if status.CameraCount != 1 {
		t.Errorf("Expected 1 camera, got %d", status.CameraCount)
	}
}

// TestOrchestrator_Stop tests stopping the orchestrator
func TestOrchestrator_Stop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-orchestrator-stop-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultOrchestratorConfig()
	config.QueueBasePath = tmpDir
	orch, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}

	// Add and start
	cam := &mockCamera{id: "test-cam", camType: "http"}
	uploader := &mockUploader{}
	orch.AddCamera(cam, CameraConfig{ID: "test-cam", Enabled: true}, 60, uploader, nil)
	orch.Start()

	time.Sleep(50 * time.Millisecond)

	// Stop orchestrator
	orch.Stop()

	// Wait for shutdown
	time.Sleep(100 * time.Millisecond)

	// Verify context is cancelled
	select {
	case <-orch.ctx.Done():
		// Good, context is cancelled
	default:
		t.Error("Context should be cancelled after Stop()")
	}
}

// TestOrchestrator_GetStatus tests status retrieval
func TestOrchestrator_GetStatus(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-orchestrator-status-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultOrchestratorConfig()
	config.QueueBasePath = tmpDir
	orch, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	defer orch.Stop()

	// Add cameras
	cam1 := &mockCamera{id: "cam1", camType: "http"}
	cam2 := &mockCamera{id: "cam2", camType: "rtsp"}
	uploader := &mockUploader{}

	orch.AddCamera(cam1, CameraConfig{ID: "cam1", Enabled: true}, 60, uploader, nil)
	orch.AddCamera(cam2, CameraConfig{ID: "cam2", Enabled: true}, 120, uploader, nil)
	orch.Start()

	time.Sleep(50 * time.Millisecond)

	status := orch.GetStatus()

	if !status.Running {
		t.Error("Orchestrator should be running")
	}
	if status.CameraCount != 2 {
		t.Errorf("Expected 2 cameras, got %d", status.CameraCount)
	}
	if status.Uptime <= 0 {
		t.Error("Uptime should be > 0")
	}
	if len(status.CameraStats) != 2 {
		t.Errorf("Expected 2 camera stats, got %d", len(status.CameraStats))
	}
}

func TestOrchestrator_GetCaptureReadinessPoints(t *testing.T) {
	tmpDir := t.TempDir()
	config := DefaultOrchestratorConfig()
	config.QueueBasePath = tmpDir
	orch, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	defer orch.Stop()

	cam := &mockCamera{id: "cam1", camType: "http"}
	if err := orch.AddCamera(cam, CameraConfig{ID: "cam1", Enabled: true}, 60, &mockUploader{}, nil); err != nil {
		t.Fatalf("AddCamera: %v", err)
	}
	if err := orch.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	time.Sleep(2 * time.Millisecond)

	running, uptime, points := orch.GetCaptureReadinessPoints()
	if !running {
		t.Error("expected running")
	}
	if uptime <= 0 {
		t.Errorf("expected uptime > 0, got %v", uptime)
	}
	if len(points) != 1 {
		t.Fatalf("points len = %d, want 1", len(points))
	}
	if points[0].CameraID != "cam1" {
		t.Errorf("camera id = %q, want cam1", points[0].CameraID)
	}
}

func TestNewOrchestrator_ExplicitMaxConcurrentCapturesUsesConfiguredValue(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := DefaultOrchestratorConfig()
	cfg.QueueBasePath = tmpDir
	cfg.MaxConcurrentCaptures = 7
	orch, err := NewOrchestrator(cfg)
	if err != nil {
		t.Fatalf("NewOrchestrator: %v", err)
	}
	defer orch.Stop()
	if orch.config.MaxConcurrentCaptures != 7 {
		t.Errorf("MaxConcurrentCaptures: want 7 (user config), got %d", orch.config.MaxConcurrentCaptures)
	}
}

// TestDefaultOrchestratorConfig tests the default configuration
func TestDefaultOrchestratorConfig(t *testing.T) {
	config := DefaultOrchestratorConfig()

	if config.QueueBasePath != "/dev/shm/aviationwx" {
		t.Errorf("QueueBasePath = %s, want /dev/shm/aviationwx", config.QueueBasePath)
	}
	if config.QueueMaxTotalMB != 100 {
		t.Errorf("QueueMaxTotalMB = %d, want 100", config.QueueMaxTotalMB)
	}
	if config.QueueMaxHeapMB != 400 {
		t.Errorf("QueueMaxHeapMB = %d, want 400", config.QueueMaxHeapMB)
	}
	if config.MinUploadInterval != time.Second {
		t.Errorf("MinUploadInterval = %v, want 1s", config.MinUploadInterval)
	}
	if config.AuthBackoffSecs != 60 {
		t.Errorf("AuthBackoffSecs = %d, want 60", config.AuthBackoffSecs)
	}
	if config.MaxConcurrentCaptures != 0 {
		t.Errorf("MaxConcurrentCaptures = %d, want 0 (unset)", config.MaxConcurrentCaptures)
	}
}

// TestOrchestrator_WithOnCaptureCallback tests the onCapture callback
func TestOrchestrator_WithOnCaptureCallback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-orchestrator-callback-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	config := DefaultOrchestratorConfig()
	config.QueueBasePath = tmpDir
	orch, err := NewOrchestrator(config)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}
	defer orch.Stop()

	// Track callback invocations (note: callback won't be invoked in this test
	// as we don't start the capture worker, but we verify it's stored correctly)
	callback := func(cameraID string, imageData []byte, captureTime time.Time) {
		// Callback would be called during actual capture
	}

	// Add camera with callback
	cam := &mockCamera{
		id:      "test-cam",
		camType: "http",
		data:    []byte("test data"),
	}
	uploader := &mockUploader{}

	err = orch.AddCamera(cam, CameraConfig{ID: "test-cam", Enabled: true}, 60, uploader, callback)
	if err != nil {
		t.Errorf("AddCamera() error = %v", err)
	}

	// Callback should be stored (we can't easily test invocation without starting the worker)
	if orch.captureWorkers["test-cam"].onCapture == nil {
		t.Error("Callback should be stored in capture worker")
	}
}

func TestCameraStatus_LastErrorJSONShape(t *testing.T) {
	cs := CameraStatus{
		CameraID: "cam1",
		LastError: &CameraLastError{
			Message: "connection refused",
		},
	}
	data, err := json.Marshal(cs)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	lastErr, ok := decoded["last_error"].(map[string]interface{})
	if !ok {
		t.Fatalf("last_error=%T, want object with Message", decoded["last_error"])
	}
	if lastErr["Message"] != "connection refused" {
		t.Fatalf("Message=%v, want connection refused", lastErr["Message"])
	}
}
