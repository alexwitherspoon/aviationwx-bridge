package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const recoveryExhaustedFilename = "recovery-exhausted.json"

// recoveryExhaustedPath is where the host capture-restart script writes cap-exhaustion state.
func recoveryExhaustedPath() string {
	return filepath.Join(hostDataDir(), recoveryExhaustedFilename)
}

func hostDataDir() string {
	if d := os.Getenv("AVIATIONWX_DATA_DIR"); d != "" {
		return d
	}
	// Host scripts write recovery/upgrade files at the mounted volume root (/data in the container).
	return "/data"
}

// readHostDataLabel returns trimmed contents of a host-written file under the data dir (empty if missing).
func readHostDataLabel(name string) string {
	data, err := os.ReadFile(filepath.Join(hostDataDir(), name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// readRecoveryExhausted loads host recovery-exhausted.json when present (nil if absent or invalid).
func readRecoveryExhausted() map[string]interface{} {
	data, err := os.ReadFile(recoveryExhaustedPath())
	if err != nil {
		return nil
	}
	var out map[string]interface{}
	if json.Unmarshal(data, &out) != nil {
		return nil
	}
	return out
}
