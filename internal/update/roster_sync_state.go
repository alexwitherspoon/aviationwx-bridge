package update

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const rosterSyncStateFile = "upload_ssh_roster_sync.json"

var rosterSyncStateMu sync.Mutex

// RosterSyncEndpointState records the last HTTPS roster sync attempt for one upload target.
type RosterSyncEndpointState struct {
	Host          string     `json:"host"`
	Port          int        `json:"port"`
	LastError     string     `json:"last_error,omitempty"`
	LastAttemptAt time.Time  `json:"last_attempt_at"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
}

type rosterSyncStateFileData struct {
	Endpoints map[string]RosterSyncEndpointState `json:"endpoints"`
}

// RosterSyncEndpointKey returns the map key for an upload target.
func RosterSyncEndpointKey(host string, port int) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if port <= 0 {
		port = 2222
	}
	return host + ":" + strconv.Itoa(port)
}

// RecordRosterSyncEndpoint persists the result of an HTTPS roster sync attempt.
func RecordRosterSyncEndpoint(configDir, host string, port int, syncErr error) error {
	if strings.TrimSpace(configDir) == "" {
		return fmt.Errorf("config directory is required")
	}
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		return fmt.Errorf("upload host is required")
	}
	if port <= 0 {
		port = 2222
	}

	rosterSyncStateMu.Lock()
	defer rosterSyncStateMu.Unlock()

	data, err := loadRosterSyncStateFile(configDir)
	if err != nil {
		return err
	}
	if data.Endpoints == nil {
		data.Endpoints = make(map[string]RosterSyncEndpointState)
	}

	key := RosterSyncEndpointKey(host, port)
	now := time.Now().UTC()
	state := RosterSyncEndpointState{
		Host:          host,
		Port:          port,
		LastAttemptAt: now,
	}
	if prior, ok := data.Endpoints[key]; ok && prior.LastSuccessAt != nil {
		t := *prior.LastSuccessAt
		state.LastSuccessAt = &t
	}
	if syncErr != nil {
		state.LastError = syncErr.Error()
	} else {
		state.LastSuccessAt = &now
	}
	data.Endpoints[key] = state
	return writeRosterSyncStateFile(configDir, data)
}

// LoadRosterSyncState reads persisted roster sync attempts (nil map when missing).
func LoadRosterSyncState(configDir string) (map[string]RosterSyncEndpointState, error) {
	rosterSyncStateMu.Lock()
	defer rosterSyncStateMu.Unlock()

	data, err := loadRosterSyncStateFile(configDir)
	if err != nil {
		return nil, err
	}
	if data.Endpoints == nil {
		return map[string]RosterSyncEndpointState{}, nil
	}
	out := make(map[string]RosterSyncEndpointState, len(data.Endpoints))
	for k, v := range data.Endpoints {
		out[k] = v
	}
	return out, nil
}

func loadRosterSyncStateFile(configDir string) (*rosterSyncStateFileData, error) {
	path := filepath.Join(configDir, rosterSyncStateFile)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &rosterSyncStateFileData{Endpoints: map[string]RosterSyncEndpointState{}}, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	raw, err := io.ReadAll(f)
	if err != nil {
		return nil, err
	}
	var data rosterSyncStateFileData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, err
	}
	if data.Endpoints == nil {
		data.Endpoints = map[string]RosterSyncEndpointState{}
	}
	return &data, nil
}

func writeRosterSyncStateFile(configDir string, data *rosterSyncStateFileData) error {
	if data == nil {
		data = &rosterSyncStateFileData{Endpoints: map[string]RosterSyncEndpointState{}}
	}
	if data.Endpoints == nil {
		data.Endpoints = map[string]RosterSyncEndpointState{}
	}
	path := filepath.Join(configDir, rosterSyncStateFile)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create roster sync state directory: %w", err)
	}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("write roster sync state: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(0600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write roster sync state: %w", err)
	}
	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write roster sync state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write roster sync state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("install roster sync state: %w", err)
	}
	return nil
}
