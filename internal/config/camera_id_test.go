package config

import (
	"path/filepath"
	"testing"
)

func TestSlugCameraIDFromName(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"spaces", "   ", ""},
		{"kord", "KORD West Camera", "kord-west-camera"},
		{"test_camera", "Test_Camera", "test-camera"},
		{"digits", "Cam 123 North", "cam-123-north"},
		{"collapse_hyphens", "a  -  b", "a-b"},
		{"trim_hyphens", "  -hello- ", "hello"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SlugCameraIDFromName(tt.in)
			if got != tt.want {
				t.Fatalf("SlugCameraIDFromName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestValidateCameraID(t *testing.T) {
	if err := ValidateCameraID("tower-north"); err != nil {
		t.Fatalf("valid id: %v", err)
	}
	for _, id := range []string{"", "../x", "bad/id", `bad\id`, "dots.not", ".."} {
		if err := ValidateCameraID(id); err == nil {
			t.Fatalf("ValidateCameraID(%q) expected error", id)
		}
	}
}

func TestCameraConfigPath_staysUnderCamerasDir(t *testing.T) {
	base := t.TempDir()
	path, err := CameraConfigPath(base, "cam-a")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(path) != "cam-a.json" {
		t.Fatalf("path = %q", path)
	}
	if _, err := CameraConfigPath(base, "../etc/passwd"); err == nil {
		t.Fatal("expected traversal id to fail")
	}
}

func TestDeleteCamera_rejectsInvalidID(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := NewService(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteCamera("../escape"); err == nil {
		t.Fatal("expected invalid id error")
	}
}

func TestAddCamera_AutoIDFromNameAndUniqueness(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := NewService(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	u := func(user string) *Upload {
		return &Upload{
			Protocol: "sftp",
			Host:     "h.example.com",
			Port:     2222,
			Username: user,
			Password: "p",
		}
	}
	if _, err := svc.AddCamera(Camera{
		Name:   "Tower North",
		Type:   "http",
		Upload: u("a"),
	}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	c1, _ := svc.GetCamera("tower-north")
	if c1 == nil || c1.ID != "tower-north" {
		t.Fatalf("expected id tower-north, got %+v", c1)
	}
	if _, err := svc.AddCamera(Camera{
		Name:   "Tower North",
		Type:   "http",
		Upload: u("b"),
	}); err != nil {
		t.Fatalf("second add same display name: %v", err)
	}
	c2, _ := svc.GetCamera("tower-north-2")
	if c2 == nil || c2.Name != "Tower North" {
		t.Fatalf("expected tower-north-2, got %+v", c2)
	}
}
