package config

import (
	"fmt"
)

const maxCameraIDSlugLen = 64

// SlugCameraIDFromName derives a filesystem-safe camera id from a display name (lowercase,
// letters, digits, hyphens only). Empty or invalid input yields "".
func SlugCameraIDFromName(name string) string {
	return slugIDFromName(name, maxCameraIDSlugLen)
}

// ValidateCameraID reports whether id is safe for use in camera config filenames.
// Allowed: ASCII letters, digits, and hyphens (same rules as legacy config validation).
func ValidateCameraID(id string) error {
	return validateConfigFileID(id, "camera")
}

// CameraConfigPath returns the absolute path for a camera JSON file under baseDir/cameras.
func CameraConfigPath(baseDir, id string) (string, error) {
	return containedConfigPath(baseDir, "cameras", id, "camera")
}

// allocateUniqueCameraIDLocked returns a unique camera id from the display name.
// Caller must hold s.mu (write lock). If the slug is empty, "camera" is used as the base.
func (s *Service) allocateUniqueCameraIDLocked(displayName string) string {
	base := SlugCameraIDFromName(displayName)
	if base == "" {
		base = "camera"
	}
	for i := 0; i < 1000; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", base, i+1)
		}
		if _, exists := s.cameras[candidate]; !exists {
			return candidate
		}
	}
	return base + "-x"
}
