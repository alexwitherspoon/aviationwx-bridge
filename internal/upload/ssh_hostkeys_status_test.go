package upload

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSSHHostKeysProbeTimeout(t *testing.T) {
	tests := []struct {
		name           string
		connectSeconds int
		want           time.Duration
	}{
		{name: "default when unset", connectSeconds: 0, want: SSHHostKeysProbeMaxTimeout},
		{name: "uses lower connect timeout", connectSeconds: 10, want: 10 * time.Second},
		{name: "caps high connect timeout", connectSeconds: 300, want: SSHHostKeysProbeMaxTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SSHHostKeysProbeTimeout(tt.connectSeconds); got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestComputeSSHHostKeysStatus(t *testing.T) {
	trusted := []string{"SHA256:abc", "SHA256:def"}

	tests := []struct {
		name           string
		server         string
		pinned         string
		pinnedOK       bool
		pinnedKeyError string
		trusted        []string
		want           string
	}{
		{name: "probe failed", want: "probe_failed"},
		{name: "pinned read error", server: "SHA256:abc", pinnedKeyError: "read known hosts: permission denied", want: "pinned_key_error"},
		{name: "ok pinned match", server: "SHA256:abc", pinned: "SHA256:abc", pinnedOK: true, want: "ok"},
		{name: "pending heal", server: "SHA256:abc", pinned: "SHA256:old", pinnedOK: true, trusted: trusted, want: "mismatch_pending_heal"},
		{name: "mismatch", server: "SHA256:zzz", pinned: "SHA256:old", pinnedOK: true, trusted: trusted, want: "mismatch"},
		{name: "no pin trusted", server: "SHA256:abc", trusted: trusted, want: "ok"},
		{name: "no pin no roster", server: "SHA256:abc", want: "roster_unavailable"},
		{name: "no pin untrusted", server: "SHA256:zzz", trusted: trusted, want: "mismatch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeSSHHostKeysStatus(tt.server, tt.pinned, tt.pinnedOK, tt.pinnedKeyError, tt.trusted)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}

func TestRefreshSSHHostKeysStatus_recordsSyncError(t *testing.T) {
	SetProbeSSHHostKeyFingerprintForTest(func(string, int, time.Duration) (string, error) {
		return "SHA256:probe-test-key", nil
	})
	t.Cleanup(func() { SetProbeSSHHostKeyFingerprintForTest(nil) })

	SetRosterSyncHTTPSForTest(func(string, string, int) error {
		return errors.New("fetch upload ssh host keys: connection refused")
	})
	t.Cleanup(func() { SetRosterSyncHTTPSForTest(nil) })

	dir := t.TempDir()
	status := RefreshSSHHostKeysStatus(dir, filepath.Join(dir, "ssh_known_hosts"), nil, SSHHostKeysProbeMaxTimeout)
	if len(status) != 1 {
		t.Fatalf("len = %d, want 1", len(status))
	}
	if status[0].RosterSyncError == "" {
		t.Fatal("expected roster_sync_error")
	}
	if status[0].Status != "roster_unavailable" {
		t.Fatalf("status = %q, want roster_unavailable", status[0].Status)
	}
}

func TestRefreshSSHHostKeysStatus_updatesTrustedRoster(t *testing.T) {
	const probeFP = "SHA256:probe-test-key"
	SetProbeSSHHostKeyFingerprintForTest(func(string, int, time.Duration) (string, error) {
		return probeFP, nil
	})
	t.Cleanup(func() { SetProbeSSHHostKeyFingerprintForTest(nil) })

	dir := t.TempDir()
	SetRosterSyncHTTPSForTest(func(configDir, host string, port int) error {
		path := filepath.Join(configDir, "upload_ssh_trusted_keys.json")
		return os.WriteFile(path, []byte(`{
  "sha256": ["`+probeFP+`"],
  "updated_at": "2026-06-26T00:00:00Z",
  "source": "https-roster"
}`), 0600)
	})
	t.Cleanup(func() { SetRosterSyncHTTPSForTest(nil) })

	status := RefreshSSHHostKeysStatus(dir, filepath.Join(dir, "ssh_known_hosts"), nil, SSHHostKeysProbeMaxTimeout)
	if len(status) != 1 {
		t.Fatalf("len = %d, want 1", len(status))
	}
	if status[0].RosterSyncError != "" {
		t.Fatalf("roster_sync_error = %q", status[0].RosterSyncError)
	}
	if status[0].Status != "ok" {
		t.Fatalf("status = %q, want ok", status[0].Status)
	}
}
