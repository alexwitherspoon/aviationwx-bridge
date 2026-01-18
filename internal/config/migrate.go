package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MigrateFromLegacy migrates from old config.json format to new ConfigService format
// This is a one-time migration for existing installations
func MigrateFromLegacy(legacyPath string, newBaseDir string) error {
	// Read legacy config
	data, err := os.ReadFile(legacyPath)
	if err != nil {
		return fmt.Errorf("read legacy config: %w", err)
	}

	var legacy Config
	if err := json.Unmarshal(data, &legacy); err != nil {
		return fmt.Errorf("parse legacy config: %w", err)
	}

	// Create new service
	svc, err := NewService(newBaseDir)
	if err != nil {
		return fmt.Errorf("create config service: %w", err)
	}

	// Migrate global settings
	err = svc.UpdateGlobal(func(g *GlobalSettings) error {
		g.Version = legacy.Version
		g.Timezone = legacy.Timezone
		g.Global = legacy.Global
		g.Queue = legacy.Queue
		g.SNTP = legacy.SNTP
		g.WebConsole = legacy.WebConsole
		return nil
	})
	if err != nil {
		return fmt.Errorf("migrate global settings: %w", err)
	}

	// Migrate cameras
	for _, cam := range legacy.Cameras {
		if err := svc.AddCamera(cam); err != nil {
			return fmt.Errorf("migrate camera %s: %w", cam.ID, err)
		}
	}

	// Backup old config
	backupPath := legacyPath + ".migrated"
	if err := os.Rename(legacyPath, backupPath); err != nil {
		// Don't fail if backup fails
		fmt.Fprintf(os.Stderr, "Warning: could not backup old config: %v\n", err)
	}

	return nil
}

// InitOrMigrate initializes ConfigService, migrating from legacy format if needed
func InitOrMigrate(baseDir string, legacyPath string) (*Service, error) {
	// Check if new format already exists
	globalPath := filepath.Join(baseDir, "global.json")
	if _, err := os.Stat(globalPath); err == nil {
		// New format exists, just load it
		return NewService(baseDir)
	}

	// Check if legacy config exists
	if _, err := os.Stat(legacyPath); err == nil {
		// Legacy config exists, migrate it
		if err := MigrateFromLegacy(legacyPath, baseDir); err != nil {
			return nil, fmt.Errorf("migrate legacy config: %w", err)
		}
		return NewService(baseDir)
	}

	// Neither exists, create fresh
	return NewService(baseDir)
}
