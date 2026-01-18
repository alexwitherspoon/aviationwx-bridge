package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
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

	err := svc.AddCamera(cam)
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
	if err := svc.AddCamera(cam); err != nil {
		t.Fatalf("First AddCamera failed: %v", err)
	}

	// Add second time - should fail
	err := svc.AddCamera(cam)
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
	svc.AddCamera(cam)

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
	svc.AddCamera(cam)

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
	svc.AddCamera(cam)

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
		svc.AddCamera(cam)
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
	svc.AddCamera(cam)

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
	svc1.AddCamera(cam)

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
	svc.AddCamera(cam)

	// Get camera and try to modify it
	retrieved, _ := svc.GetCamera("test-cam-1")
	retrieved.Name = "HACKED"

	// Get it again and verify it wasn't changed
	retrieved2, _ := svc.GetCamera("test-cam-1")
	if retrieved2.Name != "Original" {
		t.Error("Camera was mutated through returned copy!")
	}
}
