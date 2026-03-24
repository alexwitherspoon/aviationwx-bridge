package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/hardware"
)

func TestNewService(t *testing.T) {
	tmpDir := t.TempDir()

	svc, err := NewService(tmpDir)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	// Verify default global config
	global := svc.GetGlobal()
	if global.Version != 2 {
		t.Errorf("Expected version 2, got %d", global.Version)
	}
	if global.Timezone != "UTC" {
		t.Errorf("Expected timezone UTC, got %s", global.Timezone)
	}
	if global.WebConsole == nil || global.WebConsole.Port != 1229 {
		t.Error("Expected default web console config")
	}
	if global.MaxConcurrentCaptures != 0 {
		t.Errorf("Expected MaxConcurrentCaptures 0 (unset, profiled each boot), got %d", global.MaxConcurrentCaptures)
	}
	if n := EffectiveMaxConcurrentCaptures(global); n != hardware.DefaultMaxConcurrentCaptures() {
		t.Errorf("EffectiveMaxConcurrentCaptures: want profiled default %d, got %d", hardware.DefaultMaxConcurrentCaptures(), n)
	}
	raw, err := os.ReadFile(filepath.Join(tmpDir, "global.json"))
	if err != nil {
		t.Fatalf("read global.json: %v", err)
	}
	if strings.Contains(string(raw), "max_concurrent_captures") {
		t.Error("first-time global.json should omit max_concurrent_captures so profiling applies each boot")
	}

	// Verify global.json was created
	if _, err := os.Stat(filepath.Join(tmpDir, "global.json")); os.IsNotExist(err) {
		t.Error("global.json was not created")
	}

	// Verify cameras directory was created
	if _, err := os.Stat(filepath.Join(tmpDir, "cameras")); os.IsNotExist(err) {
		t.Error("cameras directory was not created")
	}
}

func TestAddCamera(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)

	cam := Camera{
		ID:      "test-cam-1",
		Name:    "Test Camera",
		Type:    "http",
		Enabled: true,
		Upload: &Upload{
			Host:     "example.com",
			Port:     21,
			Username: "test",
			Password: "pass",
		},
	}

	_, err := svc.AddCamera(cam)
	if err != nil {
		t.Fatalf("AddCamera failed: %v", err)
	}

	// Verify file was created
	camPath := filepath.Join(tmpDir, "cameras", "test-cam-1.json")
	if _, err := os.Stat(camPath); os.IsNotExist(err) {
		t.Error("Camera file was not created")
	}

	// Verify we can retrieve it
	retrieved, err := svc.GetCamera("test-cam-1")
	if err != nil {
		t.Fatalf("GetCamera failed: %v", err)
	}
	if retrieved.ID != "test-cam-1" {
		t.Errorf("Expected ID test-cam-1, got %s", retrieved.ID)
	}
	if retrieved.Name != "Test Camera" {
		t.Errorf("Expected name 'Test Camera', got %s", retrieved.Name)
	}
}

func TestAddCamera_ReturnsPersistedCameraWithID(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)
	added, err := svc.AddCamera(Camera{
		Name: "Hello Cam",
		Type: "http",
		Upload: &Upload{
			Host:     "example.com",
			Port:     2222,
			Username: "u1",
			Password: "p",
		},
	})
	if err != nil {
		t.Fatalf("AddCamera: %v", err)
	}
	if added.ID != "hello-cam" {
		t.Fatalf("returned ID: want hello-cam, got %q", added.ID)
	}
}

func TestAddCamera_DuplicateUploadCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)
	u := &Upload{
		Protocol: "sftp",
		Host:     "upload.aviationwx.org",
		Port:     2222,
		Username: "acct-one",
		Password: "secret",
	}
	if _, err := svc.AddCamera(Camera{
		ID:     "cam-1",
		Name:   "One",
		Type:   "http",
		Upload: u,
	}); err != nil {
		t.Fatalf("first AddCamera: %v", err)
	}
	_, err := svc.AddCamera(Camera{
		ID:   "cam-2",
		Name: "Two",
		Type: "http",
		Upload: &Upload{
			Protocol: "sftp",
			Host:     "upload.aviationwx.org",
			Port:     2222,
			Username: "acct-one",
			Password: "different",
		},
	})
	if err == nil {
		t.Fatal("expected error for duplicate SFTP identity")
	}
	if !errors.Is(err, ErrDuplicateUploadCredentials) {
		t.Fatalf("want ErrDuplicateUploadCredentials, got %v", err)
	}
}

func TestUpdateCamera_AllowedWhenUploadUnchanged(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)
	if _, err := svc.AddCamera(Camera{
		ID:   "cam-1",
		Name: "One",
		Type: "http",
		Upload: &Upload{
			Protocol: "sftp",
			Host:     "upload.aviationwx.org",
			Port:     2222,
			Username: "u1",
			Password: "p",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddCamera(Camera{
		ID:   "cam-2",
		Name: "Two",
		Type: "http",
		Upload: &Upload{
			Protocol: "sftp",
			Host:     "upload.aviationwx.org",
			Port:     2222,
			Username: "u2",
			Password: "p",
		},
	}); err != nil {
		t.Fatal(err)
	}
	err := svc.UpdateCamera("cam-2", func(c *Camera) error {
		c.Name = "Renamed"
		return nil
	})
	if err != nil {
		t.Fatalf("update with same upload identity: %v", err)
	}
}

func TestUpdateCamera_DuplicateUploadCredentials(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)
	if _, err := svc.AddCamera(Camera{
		ID:   "cam-1",
		Name: "One",
		Type: "http",
		Upload: &Upload{
			Protocol: "sftp",
			Host:     "upload.aviationwx.org",
			Port:     2222,
			Username: "u1",
			Password: "p",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddCamera(Camera{
		ID:   "cam-2",
		Name: "Two",
		Type: "http",
		Upload: &Upload{
			Protocol: "sftp",
			Host:     "upload.aviationwx.org",
			Port:     2222,
			Username: "u2",
			Password: "p",
		},
	}); err != nil {
		t.Fatal(err)
	}
	err := svc.UpdateCamera("cam-2", func(c *Camera) error {
		c.Upload = &Upload{
			Protocol: "sftp",
			Host:     "upload.aviationwx.org",
			Port:     2222,
			Username: "u1",
			Password: "p",
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected duplicate upload error")
	}
	if !errors.Is(err, ErrDuplicateUploadCredentials) {
		t.Fatalf("want ErrDuplicateUploadCredentials, got %v", err)
	}
}

func TestAddCamera_Duplicate(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)

	cam := Camera{
		ID:      "test-cam-1",
		Name:    "Test Camera",
		Type:    "http",
		Enabled: true,
	}

	// Add first time - should succeed
	if _, err := svc.AddCamera(cam); err != nil {
		t.Fatalf("First AddCamera failed: %v", err)
	}

	// Add second time - should fail
	_, err := svc.AddCamera(cam)
	if err == nil {
		t.Error("Expected error when adding duplicate camera")
	}
}

func TestUpdateCamera(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)

	// Add initial camera
	cam := Camera{
		ID:      "test-cam-1",
		Name:    "Original Name",
		Type:    "http",
		Enabled: true,
	}
	if _, err := svc.AddCamera(cam); err != nil {
		t.Fatal(err)
	}

	// Update it
	err := svc.UpdateCamera("test-cam-1", func(c *Camera) error {
		c.Name = "Updated Name"
		c.Enabled = false
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateCamera failed: %v", err)
	}

	// Verify update
	updated, _ := svc.GetCamera("test-cam-1")
	if updated.Name != "Updated Name" {
		t.Errorf("Expected name 'Updated Name', got %s", updated.Name)
	}
	if updated.Enabled {
		t.Error("Expected camera to be disabled")
	}
}

func TestUpdateCamera_IDPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)

	cam := Camera{
		ID:      "test-cam-1",
		Name:    "Test",
		Type:    "http",
		Enabled: true,
	}
	if _, err := svc.AddCamera(cam); err != nil {
		t.Fatal(err)
	}

	// Try to change ID
	err := svc.UpdateCamera("test-cam-1", func(c *Camera) error {
		c.ID = "HACKED-ID"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateCamera failed: %v", err)
	}

	// Verify ID was preserved
	updated, _ := svc.GetCamera("test-cam-1")
	if updated.ID != "test-cam-1" {
		t.Errorf("ID was changed to %s", updated.ID)
	}
}

func TestUpdateCamera_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)

	err := svc.UpdateCamera("nonexistent", func(c *Camera) error {
		return nil
	})
	if err == nil {
		t.Error("Expected error when updating nonexistent camera")
	}
}

func TestDeleteCamera(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)

	// Add camera
	cam := Camera{
		ID:      "test-cam-1",
		Name:    "Test",
		Type:    "http",
		Enabled: true,
	}
	if _, err := svc.AddCamera(cam); err != nil {
		t.Fatal(err)
	}

	// Delete it
	err := svc.DeleteCamera("test-cam-1")
	if err != nil {
		t.Fatalf("DeleteCamera failed: %v", err)
	}

	// Verify file was deleted
	camPath := filepath.Join(tmpDir, "cameras", "test-cam-1.json")
	if _, err := os.Stat(camPath); !os.IsNotExist(err) {
		t.Error("Camera file was not deleted")
	}

	// Verify it's not in list
	cameras := svc.ListCameras()
	if len(cameras) != 0 {
		t.Errorf("Expected 0 cameras, got %d", len(cameras))
	}

	// Verify GetCamera returns error
	_, err = svc.GetCamera("test-cam-1")
	if err == nil {
		t.Error("Expected error when getting deleted camera")
	}
}

func TestListCameras(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)

	// Add multiple cameras
	for i := 1; i <= 3; i++ {
		cam := Camera{
			ID:      string(rune('a' + i - 1)), // "a", "b", "c"
			Name:    "Camera " + string(rune('0'+i)),
			Type:    "http",
			Enabled: true,
		}
		if _, err := svc.AddCamera(cam); err != nil {
			t.Fatal(err)
		}
	}

	// List them
	cameras := svc.ListCameras()
	if len(cameras) != 3 {
		t.Errorf("Expected 3 cameras, got %d", len(cameras))
	}
}

func TestUpdateGlobal(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)

	// Update global config
	err := svc.UpdateGlobal(func(g *GlobalSettings) error {
		g.Timezone = "America/New_York"
		g.WebConsole.Port = 8080
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateGlobal failed: %v", err)
	}

	// Verify update
	global := svc.GetGlobal()
	if global.Timezone != "America/New_York" {
		t.Errorf("Expected timezone America/New_York, got %s", global.Timezone)
	}
	if global.WebConsole.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", global.WebConsole.Port)
	}
}

func TestEffectiveMaxConcurrentUploads(t *testing.T) {
	if n := EffectiveMaxConcurrentUploads(GlobalSettings{MaxConcurrentUploads: 7}); n != 7 {
		t.Errorf("top-level: want 7, got %d", n)
	}
	nested := &Global{MaxConcurrentUploads: 4}
	if n := EffectiveMaxConcurrentUploads(GlobalSettings{Global: nested}); n != 4 {
		t.Errorf("nested only: want 4, got %d", n)
	}
	if n := EffectiveMaxConcurrentUploads(GlobalSettings{MaxConcurrentUploads: 6, Global: nested}); n != 6 {
		t.Errorf("top-level wins over nested: want 6, got %d", n)
	}
	if n := EffectiveMaxConcurrentUploads(GlobalSettings{}); n != 2 {
		t.Errorf("default: want 2, got %d", n)
	}
}

func TestEffectiveMaxConcurrentCaptures(t *testing.T) {
	if n := EffectiveMaxConcurrentCaptures(GlobalSettings{MaxConcurrentCaptures: 5}); n != 5 {
		t.Errorf("top-level: want 5, got %d", n)
	}
	nested := &Global{MaxConcurrentCaptures: 3}
	if n := EffectiveMaxConcurrentCaptures(GlobalSettings{Global: nested}); n != 3 {
		t.Errorf("nested only: want 3, got %d", n)
	}
	if n := EffectiveMaxConcurrentCaptures(GlobalSettings{MaxConcurrentCaptures: 4, Global: nested}); n != 4 {
		t.Errorf("top-level wins: want 4, got %d", n)
	}
	// Unset uses profiled default (depends on RAM/CPU on this machine).
	if n := EffectiveMaxConcurrentCaptures(GlobalSettings{}); n != hardware.DefaultMaxConcurrentCaptures() {
		t.Errorf("default: want profiled default %d, got %d", hardware.DefaultMaxConcurrentCaptures(), n)
	}
	// Any explicit 1–10 must match exactly; profiled default must never be blended in.
	for want := 1; want <= 10; want++ {
		if got := EffectiveMaxConcurrentCaptures(GlobalSettings{MaxConcurrentCaptures: want}); got != want {
			t.Errorf("MaxConcurrentCaptures=%d: want %d, got %d", want, want, got)
		}
	}
}

func TestSubscribe(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)

	events := make(chan ConfigEvent, 10)

	// Subscribe to events
	svc.Subscribe(func(event ConfigEvent) {
		events <- event
	})

	// Add camera
	cam := Camera{
		ID:      "test-cam-1",
		Name:    "Test",
		Type:    "http",
		Enabled: true,
	}
	if _, err := svc.AddCamera(cam); err != nil {
		t.Fatal(err)
	}

	// Wait for event
	select {
	case event := <-events:
		if event.Type != "camera_added" {
			t.Errorf("Expected camera_added, got %s", event.Type)
		}
		if event.CameraID != "test-cam-1" {
			t.Errorf("Expected CameraID test-cam-1, got %s", event.CameraID)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event")
	}

	// Update camera
	svc.UpdateCamera("test-cam-1", func(c *Camera) error {
		c.Name = "Updated"
		return nil
	})

	// Wait for event
	select {
	case event := <-events:
		if event.Type != "camera_updated" {
			t.Errorf("Expected camera_updated, got %s", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for update event")
	}

	// Delete camera
	svc.DeleteCamera("test-cam-1")

	// Wait for event
	select {
	case event := <-events:
		if event.Type != "camera_deleted" {
			t.Errorf("Expected camera_deleted, got %s", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for delete event")
	}
}

func TestReload(t *testing.T) {
	tmpDir := t.TempDir()

	// Create first service and add camera
	svc1, _ := NewService(tmpDir)
	cam := Camera{
		ID:      "test-cam-1",
		Name:    "Test Camera",
		Type:    "http",
		Enabled: true,
	}
	if _, err := svc1.AddCamera(cam); err != nil {
		t.Fatal(err)
	}

	// Create second service (should reload from disk)
	svc2, err := NewService(tmpDir)
	if err != nil {
		t.Fatalf("Second NewService failed: %v", err)
	}

	// Verify camera was loaded
	cameras := svc2.ListCameras()
	if len(cameras) != 1 {
		t.Fatalf("Expected 1 camera, got %d", len(cameras))
	}
	if cameras[0].ID != "test-cam-1" {
		t.Errorf("Expected ID test-cam-1, got %s", cameras[0].ID)
	}
}

func TestGetWebPassword(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)

	// Test default
	password := svc.GetWebPassword()
	if password != "aviationwx" {
		t.Errorf("Expected default password 'aviationwx', got %s", password)
	}

	// Update password
	svc.UpdateGlobal(func(g *GlobalSettings) error {
		g.WebConsole.Password = "newpassword"
		return nil
	})

	// Test updated
	password = svc.GetWebPassword()
	if password != "newpassword" {
		t.Errorf("Expected password 'newpassword', got %s", password)
	}
}

func TestGetWebPort(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)

	// Test default
	port := svc.GetWebPort()
	if port != 1229 {
		t.Errorf("Expected default port 1229, got %d", port)
	}

	// Update port
	svc.UpdateGlobal(func(g *GlobalSettings) error {
		g.WebConsole.Port = 8080
		return nil
	})

	// Test updated
	port = svc.GetWebPort()
	if port != 8080 {
		t.Errorf("Expected port 8080, got %d", port)
	}
}

func TestImmutability(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := NewService(tmpDir)

	// Add camera
	cam := Camera{
		ID:      "test-cam-1",
		Name:    "Original",
		Type:    "http",
		Enabled: true,
	}
	if _, err := svc.AddCamera(cam); err != nil {
		t.Fatal(err)
	}

	// Get camera and try to modify it
	retrieved, _ := svc.GetCamera("test-cam-1")
	retrieved.Name = "HACKED"

	// Get it again and verify it wasn't changed
	retrieved2, _ := svc.GetCamera("test-cam-1")
	if retrieved2.Name != "Original" {
		t.Error("Camera was mutated through returned copy!")
	}
}
