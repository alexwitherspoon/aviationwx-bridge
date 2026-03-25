package config

import (
	"fmt"
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
