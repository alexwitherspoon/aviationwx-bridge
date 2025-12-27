package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/camera"
)

// TestScheduler_EndToEnd tests the full scheduler cycle
func TestScheduler_EndToEnd(t *testing.T) {
	cameras := []camera.Camera{
		&mockCamera{id: "cam1", camType: "http", data: []byte("image1"), err: nil},
		&mockCamera{id: "cam2", camType: "onvif", data: []byte("image2"), err: nil},
	}
	configs := map[string]CameraConfig{
		"cam1": {ID: "cam1", RemotePath: "test/cam1", Enabled: true},
		"cam2": {ID: "cam2", RemotePath: "test/cam2", Enabled: true},
	}

	uploadCount := 0
	var uploadMu sync.Mutex
	uploader := &mockUploaderWithCounter{
		uploadCount: &uploadCount,
		uploadMu:    &uploadMu,
	}

	s := NewScheduler(cameras, configs, uploader, nil, Config{IntervalSeconds: 1})
	s.Start()

	// Wait for at least one cycle
	time.Sleep(1500 * time.Millisecond)

	// Check that uploads happened
	uploadMu.Lock()
	count := uploadCount
	uploadMu.Unlock()

	if count == 0 {
		t.Error("Expected at least one upload")
	}

	// Check status
	status := s.GetStatus()
	if status.CameraCount != 2 {
		t.Errorf("CameraCount = %d, want 2", status.CameraCount)
	}

	s.Stop()
}

// TestScheduler_MultipleCameras tests handling multiple cameras
func TestScheduler_MultipleCameras(t *testing.T) {
	cameras := []camera.Camera{
		&mockCamera{id: "cam1", camType: "http", data: []byte("image1"), err: nil},
		&mockCamera{id: "cam2", camType: "onvif", data: []byte("image2"), err: nil},
		&mockCamera{id: "cam3", camType: "rtsp", data: []byte("image3"), err: nil},
	}
	configs := map[string]CameraConfig{
		"cam1": {ID: "cam1", RemotePath: "test/cam1", Enabled: true},
		"cam2": {ID: "cam2", RemotePath: "test/cam2", Enabled: true},
		"cam3": {ID: "cam3", RemotePath: "test/cam3", Enabled: true},
	}

	uploader := &mockUploader{}
	s := NewScheduler(cameras, configs, uploader, nil, Config{IntervalSeconds: 1})
	s.Start()

	// Wait for processing
	time.Sleep(1500 * time.Millisecond)

	status := s.GetStatus()
	if len(status.CameraStates) != 3 {
		t.Errorf("CameraStates length = %d, want 3", len(status.CameraStates))
	}

	s.Stop()
}

// TestScheduler_FailureRecovery tests that failures are recorded and backoff is applied
func TestScheduler_FailureRecovery(t *testing.T) {
	cameras := []camera.Camera{
		&mockCamera{id: "cam1", camType: "http", data: nil, err: errors.New("capture failed")},
	}
	configs := map[string]CameraConfig{
		"cam1": {ID: "cam1", RemotePath: "test/cam1", Enabled: true},
	}

	uploader := &mockUploader{}
	s := NewScheduler(cameras, configs, uploader, nil, Config{IntervalSeconds: 1})
	s.Start()

	// Wait for failures to be recorded
	time.Sleep(2500 * time.Millisecond)

	status := s.GetStatus()
	if len(status.CameraStates) == 0 {
		t.Fatal("No camera states found")
	}
	state := status.CameraStates[0]

	// Verify failures are recorded
	if state.FailureCount == 0 {
		t.Error("FailureCount should be > 0 after failures")
	}
	if !state.IsBackingOff {
		t.Error("Should be backing off after failures")
	}
	if state.LastError == nil {
		t.Error("LastError should be set after failures")
	}

	s.Stop()
}

// TestScheduler_DegradedMode tests degraded mode activation
func TestScheduler_DegradedMode(t *testing.T) {
	cameras := []camera.Camera{
		&mockCamera{id: "cam1", camType: "http", data: nil, err: errors.New("capture failed")},
		&mockCamera{id: "cam2", camType: "onvif", data: nil, err: errors.New("capture failed")},
		&mockCamera{id: "cam3", camType: "rtsp", data: nil, err: errors.New("capture failed")},
	}
	configs := map[string]CameraConfig{
		"cam1": {ID: "cam1", RemotePath: "test/cam1", Enabled: true},
		"cam2": {ID: "cam2", RemotePath: "test/cam2", Enabled: true},
		"cam3": {ID: "cam3", RemotePath: "test/cam3", Enabled: true},
	}

	uploader := &mockUploader{}
	s := NewScheduler(cameras, configs, uploader, nil, Config{IntervalSeconds: 1})
	s.Start()

	// Wait for failures to accumulate
	time.Sleep(2000 * time.Millisecond)

	status := s.GetStatus()
	if !status.DegradedMode {
		t.Error("Degraded mode should be active after threshold failures")
	}

	// Verify concurrency limit is applied
	limit := s.degradedMode.GetConcurrencyLimit()
	if limit != 1 {
		t.Errorf("ConcurrencyLimit = %d, want 1 in degraded mode", limit)
	}

	s.Stop()
}

// mockUploaderWithCounter is a mock uploader that counts uploads
type mockUploaderWithCounter struct {
	uploadCount *int
	uploadMu    *sync.Mutex
}

func (m *mockUploaderWithCounter) Upload(remotePath string, data []byte) error {
	m.uploadMu.Lock()
	(*m.uploadCount)++
	m.uploadMu.Unlock()
	return nil
}

func (m *mockUploaderWithCounter) TestConnection() error {
	return nil
}

// mockCameraWithFailure is a mock camera that fails a certain number of times
type mockCameraWithFailure struct {
	mockCamera
	failCount *int
	failMu    *sync.Mutex
	maxFails  int
}

func (m *mockCameraWithFailure) Capture(ctx context.Context) ([]byte, error) {
	m.failMu.Lock()
	count := *m.failCount
	*m.failCount++
	m.failMu.Unlock()

	if count < m.maxFails {
		return nil, errors.New("capture failed")
	}

	// Succeed after maxFails
	return []byte("success image"), nil
}
