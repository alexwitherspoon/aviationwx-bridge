package upload

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/update"
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
		lastSyncError  string
		want           string
	}{
		{name: "probe failed", want: "probe_failed"},
		{name: "pinned read error", server: "SHA256:abc", pinnedKeyError: "read known hosts: permission denied", want: "pinned_key_error"},
		{name: "ok pinned match", server: "SHA256:abc", pinned: "SHA256:abc", pinnedOK: true, want: "ok"},
		{name: "pending heal", server: "SHA256:abc", pinned: "SHA256:old", pinnedOK: true, trusted: trusted, want: "mismatch_pending_heal"},
		{name: "mismatch", server: "SHA256:zzz", pinned: "SHA256:old", pinnedOK: true, trusted: trusted, want: "mismatch"},
		{name: "no pin trusted", server: "SHA256:abc", trusted: trusted, want: "ok"},
		{name: "no pin no roster", server: "SHA256:abc", want: "roster_not_synced"},
		{name: "no pin roster sync failed", server: "SHA256:abc", lastSyncError: "fetch upload ssh host keys: connection refused", want: "roster_sync_failed"},
		{name: "no pin untrusted", server: "SHA256:zzz", trusted: trusted, want: "mismatch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeSSHHostKeysStatus(tt.server, tt.pinned, tt.pinnedOK, tt.pinnedKeyError, tt.trusted, tt.lastSyncError)
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
	if status[0].Status != "roster_sync_failed" {
		t.Fatalf("status = %q, want roster_sync_failed", status[0].Status)
	}
}

func TestCollectSSHHostKeysStatus_surfacesPersistedSyncError(t *testing.T) {
	SetProbeSSHHostKeyFingerprintForTest(func(string, int, time.Duration) (string, error) {
		return "SHA256:probe-test-key", nil
	})
	t.Cleanup(func() { SetProbeSSHHostKeyFingerprintForTest(nil) })

	dir := t.TempDir()
	if err := update.RecordRosterSyncEndpoint(dir, "upload.aviationwx.org", 2222, errors.New("fetch upload ssh host keys: tls: handshake failure")); err != nil {
		t.Fatalf("record sync state: %v", err)
	}

	status := CollectSSHHostKeysStatus(dir, filepath.Join(dir, "ssh_known_hosts"), nil, SSHHostKeysProbeMaxTimeout)
	if len(status) != 1 {
		t.Fatalf("len = %d, want 1", len(status))
	}
	if status[0].Status != "roster_sync_failed" {
		t.Fatalf("status = %q, want roster_sync_failed", status[0].Status)
	}
	if status[0].RosterSyncError == "" {
		t.Fatal("expected persisted roster_sync_error")
	}
}

func TestCollectSSHHostKeysStatus_perEndpointTrustedRoster(t *testing.T) {
	SetProbeSSHHostKeyFingerprintForTest(func(host string, port int, _ time.Duration) (string, error) {
		switch host {
		case "upload.a.test":
			return "SHA256:key-a", nil
		case "upload.b.test":
			return "SHA256:key-b", nil
		default:
			t.Fatalf("unexpected host %s:%d", host, port)
			return "", nil
		}
	})
	t.Cleanup(func() { SetProbeSSHHostKeyFingerprintForTest(nil) })

	dir := t.TempDir()
	if err := update.WriteTrustedHostKeysForEndpointForTest(dir, "upload.a.test", 2222, []string{"SHA256:key-a"}, "https-roster"); err != nil {
		t.Fatal(err)
	}
	if err := update.WriteTrustedHostKeysForEndpointForTest(dir, "upload.b.test", 2222, []string{"SHA256:key-b"}, "https-roster"); err != nil {
		t.Fatal(err)
	}

	cameras := []config.Camera{
		{ID: "a", Enabled: true, Upload: &config.Upload{Host: "upload.a.test", Port: 2222}},
		{ID: "b", Enabled: true, Upload: &config.Upload{Host: "upload.b.test", Port: 2222}},
	}
	status := CollectSSHHostKeysStatus(dir, filepath.Join(dir, "ssh_known_hosts"), cameras, SSHHostKeysProbeMaxTimeout)
	if len(status) != 2 {
		t.Fatalf("len = %d, want 2", len(status))
	}
	if status[0].Status != "ok" || status[0].TrustedRosterSHA256[0] != "SHA256:key-a" {
		t.Fatalf("endpoint A = %+v", status[0])
	}
	if status[1].Status != "ok" || status[1].TrustedRosterSHA256[0] != "SHA256:key-b" {
		t.Fatalf("endpoint B = %+v", status[1])
	}
}

func TestCollectSSHHostKeysStatus_surfacesSyncStateLoadError(t *testing.T) {
	SetProbeSSHHostKeyFingerprintForTest(func(string, int, time.Duration) (string, error) {
		return "SHA256:probe-test-key", nil
	})
	t.Cleanup(func() { SetProbeSSHHostKeyFingerprintForTest(nil) })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "upload_ssh_roster_sync.json"), []byte("{not-json"), 0600); err != nil {
		t.Fatal(err)
	}

	status := CollectSSHHostKeysStatus(dir, filepath.Join(dir, "ssh_known_hosts"), nil, SSHHostKeysProbeMaxTimeout)
	if len(status) != 1 {
		t.Fatalf("len = %d, want 1", len(status))
	}
	if status[0].Status != "roster_sync_failed" {
		t.Fatalf("status = %q, want roster_sync_failed", status[0].Status)
	}
	if !strings.Contains(status[0].RosterSyncError, "read roster sync state:") {
		t.Fatalf("roster_sync_error = %q", status[0].RosterSyncError)
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
		return update.WriteTrustedHostKeysForEndpointForTest(configDir, host, port, []string{probeFP}, "https-roster")
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
