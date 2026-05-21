package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

const recoveryExhaustedFilename = "recovery-exhausted.json"

// recoveryExhaustedPath is where the host capture-restart script writes cap-exhaustion state.
func recoveryExhaustedPath() string {
	if d := os.Getenv("AVIATIONWX_DATA_DIR"); d != "" {
		return filepath.Join(d, recoveryExhaustedFilename)
	}
	configDir := os.Getenv("AVIATIONWX_CONFIG_DIR")
	if configDir == "" {
		configDir = "/data"
	}
	return filepath.Join(configDir, recoveryExhaustedFilename)
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
