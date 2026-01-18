package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/logger"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/web"
)

// TestBridgeWithConfigService tests the Bridge with ConfigService integration
func TestBridgeWithConfigService(t *testing.T) {
	logger.Init()
	tmpDir := t.TempDir()

	// Create config service
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create config service: %v", err)
	}

	// Add a test camera
	cam := config.Camera{
		ID:                     "test-cam",
		Name:                   "Test Camera",
		Type:                   "http",
		Enabled:                false, // Disabled to avoid actual capture attempts
		SnapshotURL:            "http://example.com/snap.jpg",
		CaptureIntervalSeconds: 60,
		Upload: &config.Upload{
			Host:     "upload.example.com",
			Port:     2121,
			Username: "testuser",
			Password: "testpass",
			TLS:      true,
		},
	}

	if err := svc.AddCamera(cam); err != nil {
		t.Fatalf("Failed to add camera: %v", err)
	}

	// Verify camera was added
	cameras := svc.ListCameras()
	if len(cameras) != 1 {
		t.Errorf("Expected 1 camera, got %d", len(cameras))
	}

	// Update global config
	err = svc.UpdateGlobal(func(g *config.GlobalSettings) error {
		g.Timezone = "America/Los_Angeles"
		return nil
	})
	if err != nil {
		t.Fatalf("Failed to update global config: %v", err)
	}

	// Verify update
	global := svc.GetGlobal()
	if global.Timezone != "America/Los_Angeles" {
		t.Errorf("Expected timezone America/Los_Angeles, got %s", global.Timezone)
	}
}

// TestConfigServiceEvents tests event-driven architecture
func TestConfigServiceEvents(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := config.NewService(tmpDir)

	events := make(chan config.ConfigEvent, 10)
	svc.Subscribe(func(event config.ConfigEvent) {
		events <- event
	})

	// Test camera added event
	cam := config.Camera{
		ID:      "test-cam-1",
		Name:    "Test Camera 1",
		Type:    "http",
		Enabled: true,
	}
	svc.AddCamera(cam)

	select {
	case event := <-events:
		if event.Type != "camera_added" {
			t.Errorf("Expected camera_added, got %s", event.Type)
		}
		if event.CameraID != "test-cam-1" {
			t.Errorf("Expected camera ID test-cam-1, got %s", event.CameraID)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for camera_added event")
	}

	// Test camera updated event
	svc.UpdateCamera("test-cam-1", func(c *config.Camera) error {
		c.Name = "Updated Name"
		return nil
	})

	select {
	case event := <-events:
		if event.Type != "camera_updated" {
			t.Errorf("Expected camera_updated, got %s", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for camera_updated event")
	}

	// Test camera deleted event
	svc.DeleteCamera("test-cam-1")

	select {
	case event := <-events:
		if event.Type != "camera_deleted" {
			t.Errorf("Expected camera_deleted, got %s", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for camera_deleted event")
	}
}

// TestWebServerWithConfigService tests web server API with ConfigService
func TestWebServerAPIWithConfigService(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := config.NewService(tmpDir)

	// Set web console password
	svc.UpdateGlobal(func(g *config.GlobalSettings) error {
		g.WebConsole = &config.WebConsole{
			Enabled:  true,
			Port:     1229,
			Password: "testpass",
		}
		return nil
	})

	// Create bridge and web server (simplified for testing)
	bridge := &Bridge{
		configService:      svc,
		log:                logger.Default(),
		lastCaptures:       make(map[string]*CachedImage),
		cameraWorkerStatus: make(map[string]*CameraWorkerStatus),
	}

	// Create web server
	bridge.webServer = web.NewServer(web.ServerConfig{
		ConfigService:   svc,
		GetStatus:       bridge.getStatus,
		GetWorkerStatus: bridge.getWorkerStatus,
	})

	// Helper function to make authenticated requests
	makeRequest := func(method, path string, body io.Reader) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, body)
		req.SetBasicAuth("admin", "testpass")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		bridge.webServer.GetMux().ServeHTTP(w, req)
		return w
	}

	t.Run("GET /api/config", func(t *testing.T) {
		w := makeRequest("GET", "/api/config", nil)
		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", w.Code)
		}

		var global config.GlobalSettings
		if err := json.Unmarshal(w.Body.Bytes(), &global); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if global.Version != 2 {
			t.Errorf("Expected version 2, got %d", global.Version)
		}
	})

	t.Run("POST /api/cameras", func(t *testing.T) {
		camJSON := `{
			"id": "web-test-cam",
			"name": "Web Test Camera",
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

		w := makeRequest("POST", "/api/cameras", strings.NewReader(camJSON))
		if w.Code != http.StatusCreated {
			t.Errorf("Expected 201, got %d: %s", w.Code, w.Body.String())
		}

		// Verify camera was added via ConfigService
		cam, err := svc.GetCamera("web-test-cam")
		if err != nil {
			t.Errorf("Camera not found in ConfigService: %v", err)
		}
		if cam.Name != "Web Test Camera" {
			t.Errorf("Expected name 'Web Test Camera', got %s", cam.Name)
		}
	})

	t.Run("GET /api/cameras", func(t *testing.T) {
		w := makeRequest("GET", "/api/cameras", nil)
		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", w.Code)
		}

		var cameras []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &cameras); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if len(cameras) != 1 {
			t.Errorf("Expected 1 camera, got %d", len(cameras))
		}
	})

	t.Run("PUT /api/cameras/{id}", func(t *testing.T) {
		updateJSON := `{
			"id": "web-test-cam",
			"name": "Updated Camera Name",
			"type": "http",
			"enabled": false,
			"snapshot_url": "http://example.com/snap.jpg",
			"capture_interval_seconds": 120,
			"upload": {
				"host": "upload.example.com",
				"port": 2121,
				"username": "user",
				"password": "",
				"tls": true
			}
		}`

		w := makeRequest("PUT", "/api/cameras/web-test-cam", strings.NewReader(updateJSON))
		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}

		// Verify update
		cam, _ := svc.GetCamera("web-test-cam")
		if cam.Name != "Updated Camera Name" {
			t.Errorf("Expected name 'Updated Camera Name', got %s", cam.Name)
		}
		if cam.CaptureIntervalSeconds != 120 {
			t.Errorf("Expected interval 120, got %d", cam.CaptureIntervalSeconds)
		}
		// Password should be preserved (was empty in update)
		if cam.Upload.Password != "pass" {
			t.Errorf("Expected password to be preserved, got %s", cam.Upload.Password)
		}
	})

	t.Run("DELETE /api/cameras/{id}", func(t *testing.T) {
		w := makeRequest("DELETE", "/api/cameras/web-test-cam", nil)
		if w.Code != http.StatusNoContent {
			t.Errorf("Expected 204, got %d", w.Code)
		}

		// Verify deletion
		_, err := svc.GetCamera("web-test-cam")
		if err == nil {
			t.Error("Expected camera to be deleted")
		}
	})
}

// TestMigrationFromLegacy tests automatic migration from old config format
func TestMigrationFromLegacy(t *testing.T) {
	tmpDir := t.TempDir()
	legacyPath := tmpDir + "/config.json"

	// Create legacy config
	legacyConfig := config.Config{
		Version:  2,
		Timezone: "America/New_York",
		Cameras: []config.Camera{
			{
				ID:                     "legacy-cam",
				Name:                   "Legacy Camera",
				Type:                   "http",
				Enabled:                true,
				SnapshotURL:            "http://example.com/snap.jpg",
				CaptureIntervalSeconds: 60,
				Upload: &config.Upload{
					Host:     "upload.example.com",
					Port:     2121,
					Username: "legacyuser",
					Password: "legacypass",
					TLS:      true,
				},
			},
		},
		WebConsole: &config.WebConsole{
			Enabled:  true,
			Port:     1229,
			Password: "aviationwx",
		},
	}

	// Save legacy config
	data, _ := json.MarshalIndent(legacyConfig, "", "  ")
	if err := os.WriteFile(legacyPath, data, 0644); err != nil {
		t.Fatalf("Failed to write legacy config: %v", err)
	}

	// Run migration
	svc, err := config.InitOrMigrate(tmpDir, legacyPath)
	if err != nil {
		t.Fatalf("Migration failed: %v", err)
	}

	// Verify global settings migrated
	global := svc.GetGlobal()
	if global.Timezone != "America/New_York" {
		t.Errorf("Expected timezone America/New_York, got %s", global.Timezone)
	}
	if global.WebConsole.Password != "aviationwx" {
		t.Errorf("Expected password aviationwx, got %s", global.WebConsole.Password)
	}

	// Verify camera migrated
	cam, err := svc.GetCamera("legacy-cam")
	if err != nil {
		t.Fatalf("Legacy camera not found: %v", err)
	}
	if cam.Name != "Legacy Camera" {
		t.Errorf("Expected name 'Legacy Camera', got %s", cam.Name)
	}
	if cam.Upload.Username != "legacyuser" {
		t.Errorf("Expected username 'legacyuser', got %s", cam.Upload.Username)
	}

	// Verify backup was created
	backupPath := legacyPath + ".migrated"
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("Backup file was not created")
	}
}

// TestPasswordPreservation tests that empty passwords don't overwrite existing ones
func TestPasswordPreservation(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := config.NewService(tmpDir)

	// Add camera with password
	cam := config.Camera{
		ID:      "pass-test-cam",
		Name:    "Password Test",
		Type:    "http",
		Enabled: true,
		Upload: &config.Upload{
			Host:     "upload.example.com",
			Port:     2121,
			Username: "user",
			Password: "secret123",
			TLS:      true,
		},
	}
	svc.AddCamera(cam)

	// Update with empty password
	err := svc.UpdateCamera("pass-test-cam", func(c *config.Camera) error {
		c.Name = "Updated Name"
		// Password intentionally not set
		return nil
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// Verify password was NOT cleared
	updated, _ := svc.GetCamera("pass-test-cam")
	if updated.Upload.Password != "secret123" {
		t.Errorf("Expected password 'secret123', got %s", updated.Upload.Password)
	}
}
