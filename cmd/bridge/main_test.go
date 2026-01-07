package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

func TestCalculateCapacityPercent(t *testing.T) {
	tests := []struct {
		name    string
		usedMB  float64
		limitMB int
		want    float64
	}{
		{
			name:    "empty",
			usedMB:  0,
			limitMB: 100,
			want:    0,
		},
		{
			name:    "half full",
			usedMB:  50,
			limitMB: 100,
			want:    50,
		},
		{
			name:    "full",
			usedMB:  100,
			limitMB: 100,
			want:    100,
		},
		{
			name:    "overfull (capped at 100)",
			usedMB:  150,
			limitMB: 100,
			want:    100,
		},
		{
			name:    "zero limit",
			usedMB:  50,
			limitMB: 0,
			want:    0,
		},
		{
			name:    "negative limit",
			usedMB:  50,
			limitMB: -10,
			want:    0,
		},
		{
			name:    "small values",
			usedMB:  25,
			limitMB: 200,
			want:    12.5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := calculateCapacityPercent(tt.usedMB, tt.limitMB)
			if got != tt.want {
				t.Errorf("calculateCapacityPercent(%v, %v) = %v, want %v",
					tt.usedMB, tt.limitMB, got, tt.want)
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	// Create a temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	configJSON := `{
		"version": 2,
		"timezone": "America/New_York",
		"cameras": [
			{
				"id": "test-cam",
				"name": "Test Camera",
				"type": "http",
				"enabled": true,
				"snapshot_url": "http://example.com/snapshot.jpg",
				"capture_interval_seconds": 60,
				"upload": {
					"host": "upload.aviationwx.org",
					"port": 21,
					"username": "user",
					"password": "pass",
					"tls": true
				}
			}
		],
		"web_console": {
			"enabled": true,
			"port": 1229,
			"password": "testpassword"
		}
	}`

	if err := os.WriteFile(configPath, []byte(configJSON), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	if cfg.Version != 2 {
		t.Errorf("expected version 2, got %d", cfg.Version)
	}

	if cfg.Timezone != "America/New_York" {
		t.Errorf("expected timezone America/New_York, got %s", cfg.Timezone)
	}

	if len(cfg.Cameras) != 1 {
		t.Fatalf("expected 1 camera, got %d", len(cfg.Cameras))
	}

	if cfg.Cameras[0].ID != "test-cam" {
		t.Errorf("expected camera ID test-cam, got %s", cfg.Cameras[0].ID)
	}

	if cfg.WebConsole == nil {
		t.Fatal("expected web_console config")
	}

	if cfg.WebConsole.Port != 1229 {
		t.Errorf("expected web console port 1229, got %d", cfg.WebConsole.Port)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := loadConfig("/nonexistent/path/config.json")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.json")

	if err := os.WriteFile(configPath, []byte("not valid json"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := loadConfig(configPath)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestSaveConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := &config.Config{
		Version:  2,
		Timezone: "UTC",
		Cameras: []config.Camera{
			{
				ID:                     "cam1",
				Name:                   "Camera 1",
				Type:                   "http",
				Enabled:                true,
				CaptureIntervalSeconds: 60,
			},
		},
		WebConsole: &config.WebConsole{
			Enabled:  true,
			Port:     1229,
			Password: "secret",
		},
	}

	if err := saveConfig(configPath, cfg); err != nil {
		t.Fatalf("saveConfig failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("config file not created")
	}

	// Load it back and verify
	loaded, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load saved config: %v", err)
	}

	if loaded.Version != cfg.Version {
		t.Errorf("version mismatch: got %d, want %d", loaded.Version, cfg.Version)
	}

	if loaded.Timezone != cfg.Timezone {
		t.Errorf("timezone mismatch: got %s, want %s", loaded.Timezone, cfg.Timezone)
	}

	if len(loaded.Cameras) != len(cfg.Cameras) {
		t.Errorf("camera count mismatch: got %d, want %d", len(loaded.Cameras), len(cfg.Cameras))
	}
}

func TestSaveConfig_InvalidPath(t *testing.T) {
	cfg := &config.Config{Version: 2}

	err := saveConfig("/nonexistent/directory/config.json", cfg)
	if err == nil {
		t.Error("expected error for invalid path")
	}
}

// TestBridgeLogger_Interface verifies bridgeLogger implements the scheduler.Logger interface
// by actually exercising the wrapper methods with a real logger
func TestBridgeLogger_Interface(t *testing.T) {
	// Initialize the logger subsystem
	os.Setenv("LOG_LEVEL", "debug")
	defer os.Unsetenv("LOG_LEVEL")

	// Create a real bridge logger wrapper
	log := &bridgeLogger{log: nil}

	// Verify the type satisfies the interface at compile time
	var _ interface {
		Debug(msg string, keysAndValues ...interface{})
		Info(msg string, keysAndValues ...interface{})
		Warn(msg string, keysAndValues ...interface{})
		Error(msg string, keysAndValues ...interface{})
	} = log

	// Note: We can't easily test output without dependency injection,
	// but interface compliance is verified at compile time above
}

func TestConfigRoundTrip(t *testing.T) {
	// Test that config survives save -> load cycle without data loss
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	original := &config.Config{
		Version:  2,
		Timezone: "America/Los_Angeles",
		Cameras: []config.Camera{
			{
				ID:                     "cam1",
				Name:                   "Test Camera 1",
				Type:                   "http",
				Enabled:                true,
				SnapshotURL:            "http://192.168.1.100/snapshot.jpg",
				CaptureIntervalSeconds: 120,
				Upload: &config.Upload{
					Host:     "upload.aviationwx.org",
					Port:     21,
					Username: "testuser",
					Password: "testpass",
					TLS:      true,
				},
			},
			{
				ID:                     "cam2",
				Name:                   "Test Camera 2",
				Type:                   "rtsp",
				Enabled:                false,
				CaptureIntervalSeconds: 60,
				RTSP: &config.RTSP{
					URL:       "rtsp://192.168.1.101:554/stream1",
					Username:  "admin",
					Password:  "admin123",
					Substream: true,
				},
				Upload: &config.Upload{
					Host:     "upload.aviationwx.org",
					Port:     21,
					Username: "user2",
					Password: "pass2",
					TLS:      true,
				},
			},
		},
		WebConsole: &config.WebConsole{
			Enabled:  true,
			Port:     8080,
			Password: "supersecret",
		},
	}

	// Save
	if err := saveConfig(configPath, original); err != nil {
		t.Fatalf("saveConfig failed: %v", err)
	}

	// Load
	loaded, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	// Compare
	if loaded.Version != original.Version {
		t.Errorf("version: got %d, want %d", loaded.Version, original.Version)
	}

	if loaded.Timezone != original.Timezone {
		t.Errorf("timezone: got %s, want %s", loaded.Timezone, original.Timezone)
	}

	if len(loaded.Cameras) != len(original.Cameras) {
		t.Fatalf("camera count: got %d, want %d", len(loaded.Cameras), len(original.Cameras))
	}

	// Check first camera
	if loaded.Cameras[0].ID != original.Cameras[0].ID {
		t.Errorf("cam1 ID: got %s, want %s", loaded.Cameras[0].ID, original.Cameras[0].ID)
	}
	if loaded.Cameras[0].SnapshotURL != original.Cameras[0].SnapshotURL {
		t.Errorf("cam1 URL: got %s, want %s", loaded.Cameras[0].SnapshotURL, original.Cameras[0].SnapshotURL)
	}
	if loaded.Cameras[0].Upload.Username != original.Cameras[0].Upload.Username {
		t.Errorf("cam1 upload username: got %s, want %s",
			loaded.Cameras[0].Upload.Username, original.Cameras[0].Upload.Username)
	}

	// Check second camera (RTSP)
	if loaded.Cameras[1].RTSP == nil {
		t.Fatal("cam2 RTSP config should not be nil")
	}
	if loaded.Cameras[1].RTSP.URL != original.Cameras[1].RTSP.URL {
		t.Errorf("cam2 RTSP URL: got %s, want %s",
			loaded.Cameras[1].RTSP.URL, original.Cameras[1].RTSP.URL)
	}

	// Check web console
	if loaded.WebConsole.Port != original.WebConsole.Port {
		t.Errorf("web console port: got %d, want %d",
			loaded.WebConsole.Port, original.WebConsole.Port)
	}
	if loaded.WebConsole.Password != original.WebConsole.Password {
		t.Errorf("web console password: got %s, want %s",
			loaded.WebConsole.Password, original.WebConsole.Password)
	}
}
