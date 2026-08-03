package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// configFileIDPattern is the allowlist for cameras/<id>.json and stations/<id>.json
// basenames. Kept as regexp.MustCompile so CodeQL go/path-injection treats a
// successful MatchString as a path sanitizer guard.
var configFileIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// validateConfigFileID rejects empty or unsafe ids used as a single path
// component under the config tree. Multiple overlapping checks are intentional:
// CodeQL recognizes Contains / IsLocal / regexp guards more reliably than a
// custom rune allowlist alone (alerts #8-#9, #13-#15).
func validateConfigFileID(id, kind string) error {
	if id == "" {
		return fmt.Errorf("%s id is required", kind)
	}
	// Single path-component guards (CodeQL go/path-injection docs).
	if strings.Contains(id, "/") || strings.Contains(id, `\`) || strings.Contains(id, "..") {
		return fmt.Errorf("%s id contains invalid path characters", kind)
	}
	if !filepath.IsLocal(id) {
		return fmt.Errorf("%s id is not a local path component", kind)
	}
	if !configFileIDPattern.MatchString(id) {
		return fmt.Errorf("%s id contains invalid characters (alphanumeric and hyphens only)", kind)
	}
	return nil
}

// containedConfigPath joins baseDir/subdir/id.json, resolves to absolute paths,
// and rejects anything that escapes subdir. Callers must pass a validated id;
// guards are repeated so taint tracking sees them on the same CFG as Join.
func containedConfigPath(baseDir, subdir, id, kind string) (string, error) {
	if err := validateConfigFileID(id, kind); err != nil {
		return "", err
	}
	// Repeat CodeQL-visible guards on id before it enters filepath.Join.
	if strings.Contains(id, "/") || strings.Contains(id, `\`) || strings.Contains(id, "..") {
		return "", fmt.Errorf("%s id contains invalid path characters", kind)
	}
	if !filepath.IsLocal(id) {
		return "", fmt.Errorf("%s id is not a local path component", kind)
	}
	if !configFileIDPattern.MatchString(id) {
		return "", fmt.Errorf("%s id contains invalid characters (alphanumeric and hyphens only)", kind)
	}

	parent := filepath.Join(baseDir, subdir)
	path := filepath.Join(parent, id+".json")
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s config path: %w", kind, err)
	}
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return "", fmt.Errorf("resolve %s directory: %w", subdir, err)
	}
	rel, err := filepath.Rel(absParent, absPath)
	if err != nil || strings.HasPrefix(rel, "..") || strings.Contains(rel, string(filepath.Separator)+"..") {
		return "", fmt.Errorf("%s id escapes %s directory", kind, subdir)
	}
	// HasPrefix is another CodeQL-recognized containment sanitizer for absPath.
	prefix := absParent + string(filepath.Separator)
	if !strings.HasPrefix(absPath, prefix) {
		return "", fmt.Errorf("%s id escapes %s directory", kind, subdir)
	}
	return absPath, nil
}
