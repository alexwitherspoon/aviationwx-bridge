package paths

import (
	"path/filepath"
	"testing"
)

func TestHostDataDir(t *testing.T) {
	t.Setenv("AVIATIONWX_DATA_DIR", "")
	t.Setenv("AVIATIONWX_CONFIG_DIR", "")
	if got := HostDataDir(); got != "/data" {
		t.Fatalf("HostDataDir() = %q, want /data", got)
	}
	t.Setenv("AVIATIONWX_DATA_DIR", "/custom/data")
	if got := HostDataDir(); got != "/custom/data" {
		t.Fatalf("HostDataDir() = %q, want /custom/data", got)
	}
}

func TestHostDataDir_ignoresConfigDir(t *testing.T) {
	t.Setenv("AVIATIONWX_DATA_DIR", "")
	t.Setenv("AVIATIONWX_CONFIG_DIR", "/data/config")
	if got := HostDataDir(); got != "/data" {
		t.Fatalf("HostDataDir() = %q, want /data (config dir must not change data root)", got)
	}
}

func TestUpdateTriggerPath_joinsAtDataRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AVIATIONWX_DATA_DIR", dir)
	got := filepath.Join(HostDataDir(), "trigger-update")
	want := filepath.Join(dir, "trigger-update")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// Legacy bug path must not be implied by HostDataDir.
	legacy := filepath.Join(HostDataDir(), "aviationwx", "trigger-update")
	if got == legacy {
		t.Fatal("trigger path must not nest aviationwx under data root")
	}
}
