package main

import (
	"testing"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

func TestBuildStatus_includesSelfUpdateEnabled(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	t.Run("disabled by default", func(t *testing.T) {
		t.Setenv("AVIATIONWX_SELF_UPDATE", "")
		b := &Bridge{configService: svc}
		raw, ok := b.buildStatus().(map[string]interface{})
		if !ok {
			t.Fatal("buildStatus() did not return map")
		}
		enabled, ok := raw["self_update_enabled"].(bool)
		if !ok {
			t.Fatalf("self_update_enabled missing or wrong type: %#v", raw["self_update_enabled"])
		}
		if enabled {
			t.Fatal("self_update_enabled should be false when env unset")
		}
	})

	t.Run("enabled when env set", func(t *testing.T) {
		t.Setenv("AVIATIONWX_SELF_UPDATE", "true")
		b := &Bridge{configService: svc}
		raw := b.buildStatus().(map[string]interface{})
		if raw["self_update_enabled"] != true {
			t.Fatalf("self_update_enabled = %#v, want true", raw["self_update_enabled"])
		}
	})
}
