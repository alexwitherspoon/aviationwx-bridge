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

const (
	trustedHostKeysFile        = "upload_ssh_trusted_keys.json"
	trustedHostKeysFileVersion = 2
)

var trustedHostKeysSyncMu sync.Mutex

// TrustedUploadHostKeysFileData is trusted roster metadata for one upload target.
type TrustedUploadHostKeysFileData struct {
	Host      string    `json:"host,omitempty"`
	Port      int       `json:"port,omitempty"`
	SHA256    []string  `json:"sha256"`
	UpdatedAt time.Time `json:"updated_at"`
	Source    string    `json:"source,omitempty"`
}

type trustedHostKeysStore struct {
	Version   int                                      `json:"version"`
	Endpoints map[string]TrustedUploadHostKeysFileData `json:"endpoints"`
}

func writeTrustedHostKeysForEndpoint(configDir, host string, port int, fps []string, source string) error {
	trustedHostKeysSyncMu.Lock()
	defer trustedHostKeysSyncMu.Unlock()
	return writeTrustedHostKeysForEndpointLocked(configDir, host, port, fps, source)
}

func writeTrustedHostKeysForEndpointLocked(configDir, host string, port int, fps []string, source string) error {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return fmt.Errorf("upload host is required")
	}
	if port <= 0 {
		port = 2222
	}
	fps = normalizeFingerprintList(fps)
	if len(fps) == 0 {
		return nil
	}

	store, err := loadTrustedHostKeysStoreUnlocked(configDir)
	if err != nil {
		return err
	}
	key := RosterSyncEndpointKey(host, port)
	if store.Endpoints == nil {
		store.Endpoints = make(map[string]TrustedUploadHostKeysFileData)
	}
	store.Endpoints[key] = TrustedUploadHostKeysFileData{
		Host:      host,
		Port:      port,
		SHA256:    fps,
		UpdatedAt: time.Now().UTC(),
		Source:    source,
	}
	store.Version = trustedHostKeysFileVersion
	return writeTrustedHostKeysStoreUnlocked(configDir, store)
}

// writeTrustedHostKeysFile writes a v1-shaped roster to the default upload endpoint (tests).
func writeTrustedHostKeysFile(path string, fps []string, source string) error {
	configDir := filepath.Dir(path)
	def := DefaultUploadEndpoint()
	return writeTrustedHostKeysForEndpoint(configDir, def.Host, def.Port, fps, source)
}

// WriteTrustedHostKeysForEndpointForTest seeds trusted roster data (tests only).
func WriteTrustedHostKeysForEndpointForTest(configDir, host string, port int, fps []string, source string) error {
	return writeTrustedHostKeysForEndpoint(configDir, host, port, fps, source)
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

// LoadTrustedUploadHostKeysForEndpoint reads trusted fingerprints for one upload target.
func LoadTrustedUploadHostKeysForEndpoint(configDir, host string, port int) ([]string, error) {
	data, err := LoadTrustedUploadHostKeysFileDataForEndpoint(configDir, host, port)
	if err != nil {
		return nil, err
	}
	if data == nil {
		return []string{}, nil
	}
	return data.SHA256, nil
}

// LoadTrustedUploadHostKeys reads trusted fingerprints for the default upload endpoint.
func LoadTrustedUploadHostKeys(configDir string) ([]string, error) {
	def := DefaultUploadEndpoint()
	return LoadTrustedUploadHostKeysForEndpoint(configDir, def.Host, def.Port)
}

// LoadTrustedUploadHostKeysFileDataForEndpoint reads trusted roster metadata for one upload target.
func LoadTrustedUploadHostKeysFileDataForEndpoint(configDir, host string, port int) (*TrustedUploadHostKeysFileData, error) {
	trustedHostKeysSyncMu.Lock()
	defer trustedHostKeysSyncMu.Unlock()

	store, err := loadTrustedHostKeysStoreUnlocked(configDir)
	if err != nil {
		return nil, err
	}
	return trustedFileDataFromStore(store, host, port), nil
}

// LoadTrustedUploadHostKeysFileDataMap reads trusted roster metadata for all upload targets.
func LoadTrustedUploadHostKeysFileDataMap(configDir string) (map[string]TrustedUploadHostKeysFileData, error) {
	trustedHostKeysSyncMu.Lock()
	defer trustedHostKeysSyncMu.Unlock()

	store, err := loadTrustedHostKeysStoreUnlocked(configDir)
	if err != nil {
		return nil, err
	}
	out := make(map[string]TrustedUploadHostKeysFileData, len(store.Endpoints))
	for k, v := range store.Endpoints {
		v.SHA256 = normalizeFingerprintList(v.SHA256)
		out[k] = v
	}
	return out, nil
}

// LoadTrustedUploadHostKeysFileData reads trusted roster metadata for the default upload endpoint.
func LoadTrustedUploadHostKeysFileData(configDir string) (*TrustedUploadHostKeysFileData, error) {
	def := DefaultUploadEndpoint()
	return LoadTrustedUploadHostKeysFileDataForEndpoint(configDir, def.Host, def.Port)
}

func trustedFileDataFromStore(store *trustedHostKeysStore, host string, port int) *TrustedUploadHostKeysFileData {
	if store == nil || store.Endpoints == nil {
		return nil
	}
	key := RosterSyncEndpointKey(host, port)
	ep, ok := store.Endpoints[key]
	if !ok {
		return nil
	}
	ep.SHA256 = normalizeFingerprintList(ep.SHA256)
	return &ep
}

func loadTrustedHostKeysStoreUnlocked(configDir string) (*trustedHostKeysStore, error) {
	path := filepath.Join(configDir, trustedHostKeysFile)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &trustedHostKeysStore{
				Version:   trustedHostKeysFileVersion,
				Endpoints: map[string]TrustedUploadHostKeysFileData{},
			}, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	store, migrated, err := parseTrustedHostKeysFile(raw)
	if err != nil {
		return nil, err
	}
	if migrated {
		if err := writeTrustedHostKeysStoreUnlocked(configDir, store); err != nil {
			return nil, err
		}
	}
	return store, nil
}

func parseTrustedHostKeysFile(raw []byte) (*trustedHostKeysStore, bool, error) {
	var probe struct {
		Version   int                                      `json:"version"`
		Endpoints map[string]TrustedUploadHostKeysFileData `json:"endpoints"`
		SHA256    []string                                 `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return nil, false, err
	}
	if probe.Version >= trustedHostKeysFileVersion && probe.Endpoints != nil {
		store := &trustedHostKeysStore{
			Version:   trustedHostKeysFileVersion,
			Endpoints: make(map[string]TrustedUploadHostKeysFileData, len(probe.Endpoints)),
		}
		for k, v := range probe.Endpoints {
			v.SHA256 = normalizeFingerprintList(v.SHA256)
			store.Endpoints[k] = v
		}
		return store, false, nil
	}
	if len(probe.SHA256) == 0 {
		return &trustedHostKeysStore{
			Version:   trustedHostKeysFileVersion,
			Endpoints: map[string]TrustedUploadHostKeysFileData{},
		}, false, nil
	}

	var v1 TrustedUploadHostKeysFileData
	if err := json.Unmarshal(raw, &v1); err != nil {
		return nil, false, err
	}
	v1.SHA256 = normalizeFingerprintList(v1.SHA256)
	if len(v1.SHA256) == 0 {
		return &trustedHostKeysStore{
			Version:   trustedHostKeysFileVersion,
			Endpoints: map[string]TrustedUploadHostKeysFileData{},
		}, false, nil
	}

	def := DefaultUploadEndpoint()
	key := RosterSyncEndpointKey(def.Host, def.Port)
	return &trustedHostKeysStore{
		Version: trustedHostKeysFileVersion,
		Endpoints: map[string]TrustedUploadHostKeysFileData{
			key: {
				Host:      def.Host,
				Port:      def.Port,
				SHA256:    v1.SHA256,
				UpdatedAt: v1.UpdatedAt,
				Source:    v1.Source,
			},
		},
	}, true, nil
}

func writeTrustedHostKeysStoreUnlocked(configDir string, store *trustedHostKeysStore) error {
	if store == nil {
		store = &trustedHostKeysStore{
			Version:   trustedHostKeysFileVersion,
			Endpoints: map[string]TrustedUploadHostKeysFileData{},
		}
	}
	if store.Endpoints == nil {
		store.Endpoints = map[string]TrustedUploadHostKeysFileData{}
	}
	store.Version = trustedHostKeysFileVersion

	path := filepath.Join(configDir, trustedHostKeysFile)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create trusted keys directory: %w", err)
	}
	encoded, err := json.MarshalIndent(store, "", "  ")
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
