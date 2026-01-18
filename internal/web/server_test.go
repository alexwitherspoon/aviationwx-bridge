package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

// TestTimezoneUpdate tests the PUT /api/time endpoint
func TestTimezoneUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create config service: %v", err)
	}

	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
	})

	// Test PUT /api/time
	reqBody := map[string]string{"timezone": "America/Los_Angeles"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("PUT", "/api/time", bytes.NewBuffer(body))
	req.SetBasicAuth("admin", svc.GetWebPassword())
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify timezone was updated in ConfigService
	global := svc.GetGlobal()
	if global.Timezone != "America/Los_Angeles" {
		t.Errorf("Expected timezone America/Los_Angeles, got %s", global.Timezone)
	}

	// Verify it persisted to disk
	svc2, _ := config.NewService(tmpDir)
	global2 := svc2.GetGlobal()
	if global2.Timezone != "America/Los_Angeles" {
		t.Error("Timezone did not persist to disk")
	}
}

// TestCameraAddUpdateDelete tests full camera lifecycle
func TestCameraAddUpdateDelete(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := config.NewService(tmpDir)

	workerStatus := make(map[string]map[string]interface{})

	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
		GetWorkerStatus: func(cameraID string) map[string]interface{} {
			if status, ok := workerStatus[cameraID]; ok {
				return status
			}
			return map[string]interface{}{
				"worker_running": false,
				"worker_error":   "Not started",
			}
		},
	})

	makeRequest := func(method, path string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBuffer(body))
		req.SetBasicAuth("admin", svc.GetWebPassword())
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)
		return w
	}

	// Add camera
	t.Run("AddCamera", func(t *testing.T) {
		camJSON := `{
			"id": "test-cam",
			"name": "Test Camera",
			"type": "http",
			"enabled": true,
			"snapshot_url": "http://example.com/snap.jpg",
			"capture_interval_seconds": 60,
			"upload": {
				"host": "upload.example.com",
				"port": 2121,
				"username": "user",
				"password": "pass",
				"tls": true
			}
		}`

		w := makeRequest("POST", "/api/cameras", []byte(camJSON))
		if w.Code != http.StatusCreated {
			t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
		}

		// Verify persisted
		cam, err := svc.GetCamera("test-cam")
		if err != nil {
			t.Fatalf("Camera not found: %v", err)
		}
		if cam.Name != "Test Camera" {
			t.Errorf("Expected name 'Test Camera', got %s", cam.Name)
		}
	})

	// Update camera (preserve password)
	t.Run("UpdateCamera_PreservePassword", func(t *testing.T) {
		updateJSON := `{
			"id": "test-cam",
			"name": "Updated Camera",
			"type": "http",
			"enabled": true,
			"snapshot_url": "http://example.com/snap2.jpg",
			"capture_interval_seconds": 120,
			"upload": {
				"host": "upload.example.com",
				"port": 2121,
				"username": "user",
				"password": "",
				"tls": true
			}
		}`

		w := makeRequest("PUT", "/api/cameras/test-cam", []byte(updateJSON))
		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}

		// Verify password was preserved
		cam, _ := svc.GetCamera("test-cam")
		if cam.Upload.Password != "pass" {
			t.Errorf("Expected password to be preserved, got %s", cam.Upload.Password)
		}
		if cam.Name != "Updated Camera" {
			t.Errorf("Expected name 'Updated Camera', got %s", cam.Name)
		}
	})

	// List cameras
	t.Run("ListCameras", func(t *testing.T) {
		w := makeRequest("GET", "/api/cameras", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", w.Code)
		}

		var cameras []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &cameras); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if len(cameras) != 1 {
			t.Errorf("Expected 1 camera, got %d", len(cameras))
		}
	})

	// Delete camera
	t.Run("DeleteCamera", func(t *testing.T) {
		w := makeRequest("DELETE", "/api/cameras/test-cam", nil)
		if w.Code != http.StatusNoContent {
			t.Fatalf("Expected 204, got %d", w.Code)
		}

		// Verify deleted
		_, err := svc.GetCamera("test-cam")
		if err == nil {
			t.Error("Camera should have been deleted")
		}
	})
}

// TestConfigServicePersistence tests that all changes persist to disk
func TestConfigServicePersistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create service and make changes
	svc1, _ := config.NewService(tmpDir)
	svc1.UpdateGlobal(func(g *config.GlobalSettings) error {
		g.Timezone = "America/New_York"
		return nil
	})

	cam := config.Camera{
		ID:      "persist-test",
		Name:    "Persistence Test",
		Type:    "http",
		Enabled: true,
		Upload: &config.Upload{
			Host:     "upload.example.com",
			Port:     2121,
			Username: "testuser",
			Password: "testpass",
			TLS:      true,
		},
	}
	svc1.AddCamera(cam)

	// Create new service instance (simulates restart)
	svc2, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to reload config: %v", err)
	}

	// Verify global config persisted
	global := svc2.GetGlobal()
	if global.Timezone != "America/New_York" {
		t.Errorf("Expected timezone America/New_York, got %s", global.Timezone)
	}

	// Verify camera persisted
	cam2, err := svc2.GetCamera("persist-test")
	if err != nil {
		t.Fatalf("Camera not found after reload: %v", err)
	}
	if cam2.Name != "Persistence Test" {
		t.Errorf("Expected name 'Persistence Test', got %s", cam2.Name)
	}
	if cam2.Upload.Password != "testpass" {
		t.Error("Password did not persist")
	}
}

// TestConfigServiceEventNotifications tests async event notifications
func TestConfigServiceEventNotifications(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := config.NewService(tmpDir)

	events := make(chan config.ConfigEvent, 10)
	svc.Subscribe(func(event config.ConfigEvent) {
		events <- event
	})

	// Add camera - should trigger event
	cam := config.Camera{
		ID:      "event-test",
		Name:    "Event Test",
		Type:    "http",
		Enabled: true,
	}
	svc.AddCamera(cam)

	// Wait for event
	select {
	case event := <-events:
		if event.Type != "camera_added" {
			t.Errorf("Expected camera_added, got %s", event.Type)
		}
		if event.CameraID != "event-test" {
			t.Errorf("Expected camera ID event-test, got %s", event.CameraID)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event")
	}

	// Update camera - should trigger event
	svc.UpdateCamera("event-test", func(c *config.Camera) error {
		c.Name = "Updated Name"
		return nil
	})

	select {
	case event := <-events:
		if event.Type != "camera_updated" {
			t.Errorf("Expected camera_updated, got %s", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for update event")
	}

	// Delete camera - should trigger event
	svc.DeleteCamera("event-test")

	select {
	case event := <-events:
		if event.Type != "camera_deleted" {
			t.Errorf("Expected camera_deleted, got %s", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for delete event")
	}
}
