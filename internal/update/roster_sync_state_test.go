package update

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordRosterSyncEndpoint_failureThenSuccess(t *testing.T) {
	dir := t.TempDir()
	const host = "upload.aviationwx.org"
	const port = 2222

	if err := RecordRosterSyncEndpoint(dir, host, port, errors.New("fetch upload ssh host keys: connection refused")); err != nil {
		t.Fatalf("record failure: %v", err)
	}

	state, err := LoadRosterSyncState(dir)
	if err != nil {
		t.Fatal(err)
	}
	key := RosterSyncEndpointKey(host, port)
	got, ok := state[key]
	if !ok {
		t.Fatalf("missing state for %s", key)
	}
	if got.LastError == "" {
		t.Fatal("expected last_error")
	}
	if got.LastSuccessAt != nil {
		t.Fatal("expected no last_success_at after failure")
	}

	if err := RecordRosterSyncEndpoint(dir, host, port, nil); err != nil {
		t.Fatalf("record success: %v", err)
	}
	state, err = LoadRosterSyncState(dir)
	if err != nil {
		t.Fatal(err)
	}
	got = state[key]
	if got.LastError != "" {
		t.Fatalf("last_error = %q, want cleared", got.LastError)
	}
	if got.LastSuccessAt == nil {
		t.Fatal("expected last_success_at after success")
	}

	if err := RecordRosterSyncEndpoint(dir, host, port, errors.New("fetch upload ssh host keys: connection refused")); err != nil {
		t.Fatalf("record failure after success: %v", err)
	}
	state, err = LoadRosterSyncState(dir)
	if err != nil {
		t.Fatal(err)
	}
	got = state[key]
	if got.LastError == "" {
		t.Fatal("expected last_error after failure")
	}
	if got.LastSuccessAt == nil {
		t.Fatal("expected last_success_at preserved after failure")
	}
}

func TestLoadRosterSyncState_missingFile(t *testing.T) {
	dir := t.TempDir()
	state, err := LoadRosterSyncState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(state) != 0 {
		t.Fatalf("state = %#v, want empty", state)
	}
}

func TestRosterSyncStateFile_permissions(t *testing.T) {
	dir := t.TempDir()
	if err := RecordRosterSyncEndpoint(dir, "upload.test", 2222, errors.New("boom")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, rosterSyncStateFile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %#o, want 0600", info.Mode().Perm())
	}
}
