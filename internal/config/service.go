package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/hardware"
)

// Service provides centralized config management with file-per-camera storage
// This eliminates shared mutable state and pointer synchronization issues
//
// Storage layout:
//
//	baseDir/
//	  global.json       - Bridge-wide settings (timezone, web console, etc.)
//	  cameras/
//	    cam1.json       - Individual camera configs
//	    cam2.json
//
// Design principles:
//   - Single source of truth (Service owns all config)
//   - No shared pointers (returns copies)
//   - Atomic operations (all updates transactional)
//   - Event-driven (non-blocking notifications)
type Service struct {
	baseDir string
	mu      sync.RWMutex

	// In-memory cache (immutable snapshots)
	global  *GlobalSettings
	cameras map[string]*Camera // Key: camera ID

	// Event listeners (called asynchronously)
	listeners []func(event ConfigEvent)
}

// GlobalSettings holds bridge-wide configuration
type GlobalSettings struct {
	Version               int          `json:"version"`                           // Config version, current: 2
	Timezone              string       `json:"timezone,omitempty"`                // IANA timezone
	UpdateChannel         string       `json:"update_channel,omitempty"`          // Update channel: "latest" or "edge"
	MaxConcurrentUploads  int          `json:"max_concurrent_uploads,omitempty"`  // Max concurrent uploads (default: 2)
	MaxConcurrentCaptures int          `json:"max_concurrent_captures,omitempty"` // If unset: profiled default (see EffectiveMaxConcurrentCaptures)
	TimeoutConnectSeconds int          `json:"timeout_connect_seconds,omitempty"` // SFTP connect timeout (default: 60)
	TimeoutUploadSeconds  int          `json:"timeout_upload_seconds,omitempty"`  // SFTP upload timeout (default: 300)
	Global                *Global      `json:"global,omitempty"`                  // Global operational settings
	Queue                 *QueueGlobal `json:"queue,omitempty"`                   // Queue settings
	SNTP                  *SNTP        `json:"sntp,omitempty"`                    // Time sync settings
	WebConsole            *WebConsole  `json:"web_console,omitempty"`             // Web console settings
}

// ConfigEvent represents a configuration change
type ConfigEvent struct {
	Type     string // "camera_added", "camera_updated", "camera_deleted", "global_updated"
	CameraID string // Empty for global events
}

// NewService creates a config service
func NewService(baseDir string) (*Service, error) {
	s := &Service{
		baseDir:   baseDir,
		cameras:   make(map[string]*Camera),
		listeners: []func(ConfigEvent){},
	}

	// Ensure directories exist
	if err := os.MkdirAll(filepath.Join(baseDir, "cameras"), 0755); err != nil {
		return nil, fmt.Errorf("create config directories: %w", err)
	}

	// Load existing config
	if err := s.reload(); err != nil {
		// If reload fails, use defaults
		defaultSNTP := DefaultSNTP()
		s.global = &GlobalSettings{
			Version:               2,
			Timezone:              "UTC",
			UpdateChannel:         "latest",
			MaxConcurrentUploads:  2,
			MaxConcurrentCaptures: 0, // omitted on disk (omitempty); profile each boot via EffectiveMaxConcurrentCaptures
			TimeoutConnectSeconds: 60,
			TimeoutUploadSeconds:  300,
			SNTP:                  &defaultSNTP,
			WebConsole: &WebConsole{
				Enabled:  true,
				Port:     1229,
				Password: "aviationwx",
			},
		}
		// Save defaults
		if err := s.saveGlobal(); err != nil {
			return nil, fmt.Errorf("save default global config: %w", err)
		}
	} else if s.global.SNTP == nil {
		// If SNTP wasn't configured, add defaults
		defaultSNTP := DefaultSNTP()
		s.global.SNTP = &defaultSNTP
		if err := s.saveGlobal(); err != nil {
			return nil, fmt.Errorf("save SNTP defaults: %w", err)
		}
	}

	return s, nil
}

// EffectiveMaxConcurrentUploads returns the configured maximum concurrent uploads, or 2 if unset.
// Top-level MaxConcurrentUploads takes precedence over global.global.max_concurrent_uploads.
func EffectiveMaxConcurrentUploads(g GlobalSettings) int {
	n := g.MaxConcurrentUploads
	if n <= 0 && g.Global != nil {
		n = g.Global.MaxConcurrentUploads
	}
	if n > 0 {
		return n
	}
	return 2
}

// EffectiveMaxConcurrentCaptures returns the configured maximum concurrent capture jobs,
// or a profiled default when unset (RAM-based slots ~500 MB each, max 10; if max CPU
// frequency is below 2 GHz, also capped to about one concurrent capture per two logical CPUs).
//
// Hardware profiling is used only when both top-level and global.global max_concurrent_captures
// are unset (<= 0). Any positive configured value is returned unchanged.
// Top-level max_concurrent_captures takes precedence over global.global.
func EffectiveMaxConcurrentCaptures(g GlobalSettings) int {
	n := g.MaxConcurrentCaptures
	if n <= 0 && g.Global != nil {
		n = g.Global.MaxConcurrentCaptures
	}
	if n > 0 {
		return n
	}
	return hardware.DefaultMaxConcurrentCaptures()
}

// GetGlobal returns a copy of global config (thread-safe)
func (s *Service) GetGlobal() GlobalSettings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s.global
}

// UpdateGlobal updates global config atomically
func (s *Service) UpdateGlobal(fn func(*GlobalSettings) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Make a copy
	updated := *s.global

	// Let caller modify the copy
	if err := fn(&updated); err != nil {
		return err
	}

	// Save to disk
	data, err := json.MarshalIndent(updated, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(s.baseDir, "global.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write global config: %w", err)
	}

	// Update in-memory
	s.global = &updated

	// Notify listeners (async)
	s.notifyListeners(ConfigEvent{Type: "global_updated"})

	return nil
}

// GetCamera returns a copy of camera config (thread-safe)
func (s *Service) GetCamera(id string) (*Camera, error) {
	if err := ValidateCameraID(id); err != nil {
		return nil, err
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	cam, exists := s.cameras[id]
	if !exists {
		return nil, fmt.Errorf("camera not found: %s", id)
	}

	// Return a deep copy
	copy := *cam
	return &copy, nil
}

// ListCameras returns copies of all cameras (thread-safe)
func (s *Service) ListCameras() []Camera {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cameras := make([]Camera, 0, len(s.cameras))
	for _, cam := range s.cameras {
		cameras = append(cameras, *cam)
	}

	// Sort cameras by ID for consistent ordering
	sort.Slice(cameras, func(i, j int) bool {
		return cameras[i].ID < cameras[j].ID
	})

	return cameras
}

// AddCamera adds a new camera atomically and returns the persisted camera (including the assigned ID).
func (s *Service) AddCamera(cam Camera) (Camera, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if strings.TrimSpace(cam.Name) == "" {
		return Camera{}, fmt.Errorf("camera name is required")
	}
	if cam.ID == "" {
		cam.ID = s.allocateUniqueCameraIDLocked(cam.Name)
	}
	if err := ValidateCameraID(cam.ID); err != nil {
		return Camera{}, err
	}
	if _, exists := s.cameras[cam.ID]; exists {
		return Camera{}, fmt.Errorf("camera already exists: %s", cam.ID)
	}

	if cam.Upload != nil {
		if err := s.checkDuplicateUpload("", cam.Upload); err != nil {
			return Camera{}, err
		}
	}

	// Save to disk first (fail-safe)
	if err := s.saveCameraFile(cam); err != nil {
		return Camera{}, err
	}

	// Update in-memory
	copy := cam
	s.cameras[cam.ID] = &copy

	// Notify listeners (async)
	s.notifyListeners(ConfigEvent{Type: "camera_added", CameraID: cam.ID})

	return cam, nil
}

// UpdateCamera updates an existing camera atomically
func (s *Service) UpdateCamera(id string, fn func(*Camera) error) error {
	if err := ValidateCameraID(id); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	cam, exists := s.cameras[id]
	if !exists {
		return fmt.Errorf("camera not found: %s", id)
	}

	// Make a copy
	updated := *cam

	// Let caller modify the copy
	if err := fn(&updated); err != nil {
		return err
	}

	// Ensure ID doesn't change
	updated.ID = id

	if updated.Upload != nil {
		if err := s.checkDuplicateUpload(id, updated.Upload); err != nil {
			return err
		}
	}

	// Save to disk first (fail-safe)
	if err := s.saveCameraFile(updated); err != nil {
		return err
	}

	// Update in-memory
	s.cameras[id] = &updated

	// Notify listeners (async)
	s.notifyListeners(ConfigEvent{Type: "camera_updated", CameraID: id})

	return nil
}

// DeleteCamera removes a camera atomically
func (s *Service) DeleteCamera(id string) error {
	if err := ValidateCameraID(id); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.cameras[id]; !exists {
		return fmt.Errorf("camera not found: %s", id)
	}

	path, err := CameraConfigPath(s.baseDir, id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("delete camera file: %w", err)
	}

	// Update in-memory
	delete(s.cameras, id)

	// Notify listeners (async)
	s.notifyListeners(ConfigEvent{Type: "camera_deleted", CameraID: id})

	return nil
}

// Subscribe registers a listener for config changes
// Listeners are called asynchronously (non-blocking)
func (s *Service) Subscribe(fn func(ConfigEvent)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, fn)
}

// SSHKnownHostsPath returns the path to the SSH known_hosts file for upload connections.
func (s *Service) SSHKnownHostsPath() string {
	return filepath.Join(s.baseDir, "ssh_known_hosts")
}
func (s *Service) GetWebPassword() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.global.WebConsole == nil {
		return "aviationwx"
	}
	if s.global.WebConsole.Password != "" {
		return s.global.WebConsole.Password
	}
	return "aviationwx"
}

// GetWebPort returns the web console port
func (s *Service) GetWebPort() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.global.WebConsole == nil || s.global.WebConsole.Port == 0 {
		return 1229
	}
	return s.global.WebConsole.Port
}

// reload loads all config from disk
func (s *Service) reload() error {
	// Load global config
	globalPath := filepath.Join(s.baseDir, "global.json")
	data, err := os.ReadFile(globalPath)
	if err != nil {
		return fmt.Errorf("read global config: %w", err)
	}

	var global GlobalSettings
	if err := json.Unmarshal(data, &global); err != nil {
		return fmt.Errorf("parse global config: %w", err)
	}
	s.global = &global

	// Load all camera configs
	camerasDir := filepath.Join(s.baseDir, "cameras")
	entries, err := os.ReadDir(camerasDir)
	if err != nil {
		// Camera directory might not exist yet
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read cameras directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		camPath := filepath.Join(camerasDir, entry.Name())
		data, err := os.ReadFile(camPath)
		if err != nil {
			return fmt.Errorf("read camera file %s: %w", entry.Name(), err)
		}

		var cam Camera
		if err := json.Unmarshal(data, &cam); err != nil {
			return fmt.Errorf("parse camera file %s: %w", entry.Name(), err)
		}

		// Normalize upload config for backward compatibility
		NormalizeUploadConfig(cam.Upload)

		if err := ValidateCameraID(cam.ID); err != nil {
			return fmt.Errorf("camera file %s: %w", entry.Name(), err)
		}
		if _, err := CameraConfigPath(s.baseDir, cam.ID); err != nil {
			return fmt.Errorf("camera file %s: %w", entry.Name(), err)
		}

		s.cameras[cam.ID] = &cam
	}

	return nil
}

// saveGlobal saves global config to disk (caller must hold lock)
func (s *Service) saveGlobal() error {
	path := filepath.Join(s.baseDir, "global.json")

	// Backup existing file before overwriting
	if _, err := os.Stat(path); err == nil {
		backupPath := path + ".bak"
		if err := copyFile(path, backupPath); err != nil {
			// Log warning but don't fail - backup is best-effort
			fmt.Fprintf(os.Stderr, "Warning: could not backup global config: %v\n", err)
		}
	}

	data, err := json.MarshalIndent(s.global, "", "  ")
	if err != nil {
		return err
	}

	// Write with proper permissions (0644 = rw-r--r--)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write global config: %w", err)
	}

	// Ensure parent directory has correct permissions
	if err := os.Chmod(s.baseDir, 0755); err != nil {
		// Don't fail on chmod error, just log
		fmt.Fprintf(os.Stderr, "Warning: could not set directory permissions: %v\n", err)
	}

	return nil
}

// saveCameraFile saves a camera config to disk (caller must hold lock)
func (s *Service) saveCameraFile(cam Camera) error {
	path, err := CameraConfigPath(s.baseDir, cam.ID)
	if err != nil {
		return err
	}

	// Backup existing file before overwriting
	if _, err := os.Stat(path); err == nil {
		backupPath := path + ".bak"
		if err := copyFile(path, backupPath); err != nil {
			// Log warning but don't fail - backup is best-effort
			fmt.Fprintf(os.Stderr, "Warning: could not backup camera config: %v\n", err)
		}
	}

	data, err := json.MarshalIndent(cam, "", "  ")
	if err != nil {
		return err
	}

	// Write with proper permissions (0644 = rw-r--r--)
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write camera config: %w", err)
	}

	// Ensure cameras directory has correct permissions
	camerasDir := filepath.Join(s.baseDir, "cameras")
	if err := os.Chmod(camerasDir, 0755); err != nil {
		// Don't fail on chmod error
		fmt.Fprintf(os.Stderr, "Warning: could not set cameras directory permissions: %v\n", err)
	}

	return nil
}

// notifyListeners calls all registered listeners (caller must hold lock)
func (s *Service) notifyListeners(event ConfigEvent) {
	for _, fn := range s.listeners {
		// Call async to avoid blocking the caller
		go fn(event)
	}
}

// copyFile creates a backup copy of a file
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer func() {
		if err := sourceFile.Close(); err != nil {
			return
		}
	}()

	destFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer func() {
		if err := destFile.Close(); err != nil {
			return
		}
	}()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}

	// Sync to ensure data is written to disk
	if err := destFile.Sync(); err != nil {
		return fmt.Errorf("sync destination: %w", err)
	}

	return nil
}
