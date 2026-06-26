package upload

import (
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
		name     string
		server   string
		pinned   string
		pinnedOK bool
		trusted  []string
		want     string
	}{
		{name: "probe failed", want: "probe_failed"},
		{name: "ok pinned match", server: "SHA256:abc", pinned: "SHA256:abc", pinnedOK: true, want: "ok"},
		{name: "pending heal", server: "SHA256:abc", pinned: "SHA256:old", pinnedOK: true, trusted: trusted, want: "mismatch_pending_heal"},
		{name: "mismatch", server: "SHA256:zzz", pinned: "SHA256:old", pinnedOK: true, trusted: trusted, want: "mismatch"},
		{name: "no pin trusted", server: "SHA256:abc", trusted: trusted, want: "ok"},
		{name: "no pin no roster", server: "SHA256:abc", want: "roster_unavailable"},
		{name: "no pin untrusted", server: "SHA256:zzz", trusted: trusted, want: "mismatch"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeSSHHostKeysStatus(tt.server, tt.pinned, tt.pinnedOK, tt.trusted)
			if got != tt.want {
				t.Fatalf("got %q want %q", got, tt.want)
			}
		})
	}
}
