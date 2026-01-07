package config

import (
	"fmt"
	"strings"
)

// Validate validates the configuration
func Validate(c *Config) error {
	if c.Version != 1 {
		return fmt.Errorf("unsupported config version: %d", c.Version)
	}

	// Validate upload
	if c.Upload.Host == "" {
		return fmt.Errorf("upload.host is required")
	}
	if c.Upload.Username == "" {
		return fmt.Errorf("upload.username is required")
	}
	if c.Upload.Password == "" {
		return fmt.Errorf("upload.password is required")
	}

	// Validate cameras
	if len(c.Cameras) == 0 {
		return fmt.Errorf("at least one camera is required")
	}

	cameraIDs := make(map[string]bool)
	for i, cam := range c.Cameras {
		if err := validateCamera(&cam, i); err != nil {
			return fmt.Errorf("camera[%d]: %w", i, err)
		}

		// Check for duplicate IDs
		if cameraIDs[cam.ID] {
			return fmt.Errorf("camera[%d]: duplicate camera ID: %s", i, cam.ID)
		}
		cameraIDs[cam.ID] = true
	}

	return nil
}

// validateCamera validates a single camera configuration
func validateCamera(cam *Camera, index int) error {
	if cam.ID == "" {
		return fmt.Errorf("id is required")
	}

	// Validate ID format (alphanumeric + hyphens)
	for _, r := range cam.ID {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-') {
			return fmt.Errorf("id contains invalid characters (alphanumeric and hyphens only)")
		}
	}

	if cam.Name == "" {
		return fmt.Errorf("name is required")
	}

	// Validate type
	validTypes := map[string]bool{"http": true, "onvif": true, "rtsp": true}
	if !validTypes[cam.Type] {
		return fmt.Errorf("type must be 'http', 'onvif', or 'rtsp'")
	}

	// Type-specific validation
	switch cam.Type {
	case "http":
		if cam.SnapshotURL == "" {
			return fmt.Errorf("snapshot_url is required for http type")
		}
	case "onvif":
		if cam.ONVIF == nil {
			return fmt.Errorf("onvif settings are required for onvif type")
		}
		if cam.ONVIF.Endpoint == "" {
			return fmt.Errorf("onvif.endpoint is required")
		}
		if cam.ONVIF.Username == "" {
			return fmt.Errorf("onvif.username is required")
		}
		if cam.ONVIF.Password == "" {
			return fmt.Errorf("onvif.password is required")
		}
	case "rtsp":
		if cam.RTSP == nil {
			return fmt.Errorf("rtsp settings are required for rtsp type")
		}
		if cam.RTSP.URL == "" {
			return fmt.Errorf("rtsp.url is required")
		}
	}

	// Validate interval
	if cam.IntervalSeconds < 30 {
		return fmt.Errorf("interval_seconds must be at least 30")
	}

	// Validate remote path
	if cam.RemotePath == "" {
		return fmt.Errorf("remote_path is required")
	}
	if strings.Contains(cam.RemotePath, "..") {
		return fmt.Errorf("remote_path cannot contain '..'")
	}
	if strings.HasPrefix(cam.RemotePath, "/") {
		return fmt.Errorf("remote_path cannot start with '/'")
	}

	return nil
}
