package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const trustedHostKeysFile = "upload_ssh_trusted_keys.json"

// trustedHostKeysFileData is persisted under the config directory.
type trustedHostKeysFileData struct {
	SHA256    []string  `json:"sha256"`
	UpdatedAt time.Time `json:"updated_at"`
	Source    string    `json:"source,omitempty"`
}

// SyncTrustedUploadHostKeys fetches the latest GitHub release metadata and persists
// trusted upload host key fingerprints for unsupervised SSH key rotation.
func SyncTrustedUploadHostKeys(configDir string) error {
	if strings.TrimSpace(configDir) == "" {
		return fmt.Errorf("config directory is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	body, err := fetchLatestReleaseBody(ctx)
	if err != nil {
		return err
	}
	fps := ParseUploadSSHHostKeysSHA256(body)
	if len(fps) == 0 {
		return nil
	}
	return writeTrustedHostKeysFile(filepath.Join(configDir, trustedHostKeysFile), fps, "github-release")
}

func fetchLatestReleaseBody(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "aviationwx-org-bridge/trusted-hostkeys")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release struct {
		Body string `json:"body"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", err
	}
	return release.Body, nil
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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0600); err != nil {
		return fmt.Errorf("write trusted host keys: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
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
	return normalizeFingerprintList(data.SHA256), nil
}
