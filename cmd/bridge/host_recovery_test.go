package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestReadRecoveryExhausted(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AVIATIONWX_DATA_DIR", dir)

	if got := readRecoveryExhausted(); got != nil {
		t.Fatal("expected nil when file missing")
	}

	payload := map[string]interface{}{
		"exhausted":    true,
		"reason":       "capture readiness stuck",
		"restarts_24h": 6,
		"max_per_24h":  6,
		"since":        "2026-05-21T00:00:00Z",
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, recoveryExhaustedFilename), data, 0o644); err != nil {
		t.Fatal(err)
	}

	got := readRecoveryExhausted()
	if got == nil {
		t.Fatal("expected recovery payload")
	}
	if exhausted, _ := got["exhausted"].(bool); !exhausted {
		t.Fatalf("exhausted: got %v", got["exhausted"])
	}
}
