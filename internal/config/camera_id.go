package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

const maxCameraIDSlugLen = 64

// SlugCameraIDFromName derives a filesystem-safe camera id from a display name (lowercase,
// letters, digits, hyphens only). Empty or invalid input yields "".
func SlugCameraIDFromName(name string) string {
	s := strings.TrimSpace(strings.ToLower(name))
	if s == "" {
		return ""
	}
	var parts []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			parts = append(parts, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	out := strings.Join(parts, "-")
	if len(out) > maxCameraIDSlugLen {
		out = strings.TrimRight(out[:maxCameraIDSlugLen], "-")
	}
	return out
}

// ValidateCameraID reports whether id is safe for use in camera config filenames.
// Allowed: ASCII letters, digits, and hyphens (same rules as legacy config validation).
func ValidateCameraID(id string) error {
	if id == "" {
		return fmt.Errorf("camera id is required")
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("camera id contains invalid characters (alphanumeric and hyphens only)")
	}
	return nil
}

// CameraConfigPath returns the absolute path for a camera JSON file under baseDir/cameras.
func CameraConfigPath(baseDir, id string) (string, error) {
	if err := ValidateCameraID(id); err != nil {
		return "", err
	}
	camerasDir := filepath.Join(baseDir, "cameras")
	path := filepath.Join(camerasDir, id+".json")
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve camera config path: %w", err)
	}
	absCameras, err := filepath.Abs(camerasDir)
	if err != nil {
		return "", fmt.Errorf("resolve cameras directory: %w", err)
	}
	rel, err := filepath.Rel(absCameras, absPath)
	if err != nil || strings.HasPrefix(rel, "..") || strings.Contains(rel, string(filepath.Separator)+"..") {
		return "", fmt.Errorf("camera id escapes cameras directory")
	}
	return absPath, nil
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
