package config

import (
	"fmt"
	"path/filepath"
	"strings"
)

const maxStationIDSlugLen = 64

// SlugStationIDFromName derives a filesystem-safe station id from a display name.
func SlugStationIDFromName(name string) string {
	return slugIDFromName(name, maxStationIDSlugLen)
}

func slugIDFromName(name string, maxLen int) string {
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
	if len(out) > maxLen {
		out = strings.TrimRight(out[:maxLen], "-")
	}
	return out
}

// ValidateStationID reports whether id is safe for stations/<id>.json filenames.
func ValidateStationID(id string) error {
	if id == "" {
		return fmt.Errorf("station id is required")
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return fmt.Errorf("station id contains invalid characters (alphanumeric and hyphens only)")
	}
	return nil
}

// StationConfigPath returns the absolute path for a station JSON file under baseDir/stations.
func StationConfigPath(baseDir, id string) (string, error) {
	if err := ValidateStationID(id); err != nil {
		return "", err
	}
	stationsDir := filepath.Join(baseDir, "stations")
	path := filepath.Join(stationsDir, id+".json")
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve station config path: %w", err)
	}
	absStations, err := filepath.Abs(stationsDir)
	if err != nil {
		return "", fmt.Errorf("resolve stations directory: %w", err)
	}
	rel, err := filepath.Rel(absStations, absPath)
	if err != nil || strings.HasPrefix(rel, "..") || strings.Contains(rel, string(filepath.Separator)+"..") {
		return "", fmt.Errorf("station id escapes stations directory")
	}
	return absPath, nil
}

func (s *Service) allocateUniqueStationIDLocked(displayName string) string {
	base := SlugStationIDFromName(displayName)
	if base == "" {
		base = "station"
	}
	for i := 0; i < 1000; i++ {
		candidate := base
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d", base, i+1)
		}
		if _, exists := s.stations[candidate]; !exists {
			return candidate
		}
	}
	return base + "-x"
}
