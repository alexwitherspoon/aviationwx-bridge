package update

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const trustedHostKeysFile = "upload_ssh_trusted_keys.json"

var trustedHostKeysSyncMu sync.Mutex

// trustedHostKeysFileData is persisted under the config directory.
type trustedHostKeysFileData struct {
	SHA256    []string  `json:"sha256"`
	UpdatedAt time.Time `json:"updated_at"`
	Source    string    `json:"source,omitempty"`
}

func writeTrustedHostKeysFile(path string, fps []string, source string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create trusted keys directory: %w", err)
	}
	data := trustedHostKeysFileData{
		SHA256:    normalizeFingerprintList(fps),
		UpdatedAt: time.Now().UTC(),
		Source:    source,
	}
	if len(data.SHA256) == 0 {
		return nil
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("write trusted host keys: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write trusted host keys: %w", err)
	}
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write trusted host keys: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write trusted host keys: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("install trusted host keys: %w", err)
	}
	return nil
}

func normalizeFingerprintList(fps []string) []string {
	seen := make(map[string]struct{}, len(fps))
	out := make([]string, 0, len(fps))
	for _, fp := range fps {
		fp = normalizeFingerprint(fp)
		if fp == "" {
			continue
		}
		if _, ok := seen[fp]; ok {
			continue
		}
		seen[fp] = struct{}{}
		out = append(out, fp)
	}
	return out
}

func normalizeFingerprint(fp string) string {
	fp = strings.TrimSpace(fp)
	if fp == "" {
		return ""
	}
	for strings.HasPrefix(strings.ToUpper(fp), "SHA256:") {
		fp = strings.TrimSpace(fp[len("SHA256:"):])
	}
	if fp == "" {
		return ""
	}
	return "SHA256:" + fp
}

// LoadTrustedUploadHostKeys reads persisted trusted fingerprints (empty slice if missing).
func LoadTrustedUploadHostKeys(configDir string) ([]string, error) {
	data, err := loadTrustedHostKeysFileData(configDir)
	if err != nil || data == nil {
		return nil, err
	}
	return data.SHA256, nil
}

// LoadTrustedUploadHostKeysFileData reads persisted trusted key metadata (nil if missing).
func LoadTrustedUploadHostKeysFileData(configDir string) (*trustedHostKeysFileData, error) {
	return loadTrustedHostKeysFileData(configDir)
}

func loadTrustedHostKeysFileData(configDir string) (*trustedHostKeysFileData, error) {
	path := filepath.Join(configDir, trustedHostKeysFile)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()
	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var data trustedHostKeysFileData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	data.SHA256 = normalizeFingerprintList(data.SHA256)
	return &data, nil
}
