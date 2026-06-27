package upload

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/update"
	"golang.org/x/crypto/ssh"
)

// SSHHostKeysEndpointStatus summarizes SFTP host key state for one upload target.
type SSHHostKeysEndpointStatus struct {
	Host                string     `json:"host"`
	Port                int        `json:"port"`
	ServerKeySHA256     string     `json:"server_key_sha256,omitempty"`
	ServerKeyError      string     `json:"server_key_error,omitempty"`
	PinnedKeySHA256     string     `json:"pinned_key_sha256,omitempty"`
	PinnedKeyError      string     `json:"pinned_key_error,omitempty"`
	TrustedRosterSHA256 []string   `json:"trusted_roster_sha256,omitempty"`
	TrustedSource       string     `json:"trusted_source,omitempty"`
	TrustedUpdatedAt    *time.Time `json:"trusted_updated_at,omitempty"`
	HttpsRosterURL      string     `json:"https_roster_url,omitempty"`
	RosterSyncError     string     `json:"roster_sync_error,omitempty"`
	Status              string     `json:"status"`
}

// SSHHostKeysProbeMaxTimeout caps live SSH probes from the web console (independent of upload connect timeout).
const SSHHostKeysProbeMaxTimeout = 15 * time.Second

var errHostKeyProbeAbort = errors.New("host key captured; abort probe")

// SSHHostKeysProbeTimeout returns the probe timeout for the settings API, capped at SSHHostKeysProbeMaxTimeout.
func SSHHostKeysProbeTimeout(connectSeconds int) time.Duration {
	if connectSeconds <= 0 {
		return SSHHostKeysProbeMaxTimeout
	}
	t := time.Duration(connectSeconds) * time.Second
	if t > SSHHostKeysProbeMaxTimeout {
		return SSHHostKeysProbeMaxTimeout
	}
	return t
}

// probeSSHHostKeyFingerprintHook overrides ProbeSSHHostKeyFingerprint in tests (nil uses the default).
var (
	probeHookMu                    sync.RWMutex
	probeSSHHostKeyFingerprintHook func(host string, port int, timeout time.Duration) (string, error)
)

// SetProbeSSHHostKeyFingerprintForTest overrides the SSH host key probe (tests only).
func SetProbeSSHHostKeyFingerprintForTest(fn func(host string, port int, timeout time.Duration) (string, error)) {
	probeHookMu.Lock()
	probeSSHHostKeyFingerprintHook = fn
	probeHookMu.Unlock()
}

func probeSSHHostKeyFingerprint(host string, port int, timeout time.Duration) (string, error) {
	probeHookMu.RLock()
	hook := probeSSHHostKeyFingerprintHook
	probeHookMu.RUnlock()
	if hook != nil {
		return hook(host, port, timeout)
	}
	return ProbeSSHHostKeyFingerprint(host, port, timeout)
}

// ProbeSSHHostKeyFingerprint dials the SFTP host and returns the server SSH host key fingerprint.
// The probe accepts any presented host key to read the fingerprint for the Settings UI; it does not authenticate or transfer files.
func ProbeSSHHostKeyFingerprint(host string, port int, timeout time.Duration) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("host is required")
	}
	if port <= 0 {
		port = 2222
	}
	if timeout <= 0 {
		timeout = SSHHostKeysProbeMaxTimeout
	}

	var fingerprint string
	sshConfig := &ssh.ClientConfig{
		User: "probe",
		Auth: []ssh.AuthMethod{
			ssh.Password(""),
		},
		// codeql[go/insecure-hostkeycallback]: read-only fingerprint probe for Settings UI (same role as ssh-keyscan); uploads use hostKeyStore verification.
		HostKeyCallback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			fingerprint = ssh.FingerprintSHA256(key)
			return errHostKeyProbeAbort
		},
		Timeout: timeout,
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if client != nil {
		_ = client.Close()
	}
	if fingerprint != "" {
		return fingerprint, nil
	}
	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("ssh host key probe failed")
}

// CollectSSHHostKeysStatus reports host key state for each roster upload endpoint.
func CollectSSHHostKeysStatus(configDir, knownHostsPath string, cameras []config.Camera, probeTimeout time.Duration) []SSHHostKeysEndpointStatus {
	endpoints := update.UploadEndpointsForRoster(cameras)
	return collectSSHHostKeysStatus(configDir, knownHostsPath, endpoints, probeTimeout, nil)
}

// RefreshSSHHostKeysStatus fetches the HTTPS roster for each endpoint, then probes and reports status.
// RosterSyncError is set per endpoint when the HTTPS fetch fails.
func RefreshSSHHostKeysStatus(configDir, knownHostsPath string, cameras []config.Camera, probeTimeout time.Duration) []SSHHostKeysEndpointStatus {
	endpoints := update.UploadEndpointsForRoster(cameras)
	syncErrors := make(map[string]string, len(endpoints))
	for _, ep := range endpoints {
		if err := syncRosterHTTPS(configDir, ep.Host, ep.Port); err != nil {
			syncErrors[sshHostKeysEndpointKey(ep.Host, ep.Port)] = err.Error()
		}
	}
	return collectSSHHostKeysStatus(configDir, knownHostsPath, endpoints, probeTimeout, syncErrors)
}

// rosterSyncHTTPSHook overrides HTTPS roster sync during RefreshSSHHostKeysStatus (tests only).
var rosterSyncHTTPSHook func(configDir, host string, port int) error

// SetRosterSyncHTTPSForTest overrides HTTPS roster sync during RefreshSSHHostKeysStatus (tests only).
func SetRosterSyncHTTPSForTest(fn func(configDir, host string, port int) error) {
	rosterSyncHTTPSHook = fn
}

func syncRosterHTTPS(configDir, host string, port int) error {
	if rosterSyncHTTPSHook != nil {
		return rosterSyncHTTPSHook(configDir, host, port)
	}
	return update.SyncUploadSSHHostKeysHTTPS(configDir, host, port)
}

func sshHostKeysEndpointKey(host string, port int) string {
	return strings.TrimSpace(strings.ToLower(host)) + ":" + strconv.Itoa(port)
}

func collectSSHHostKeysStatus(configDir, knownHostsPath string, endpoints []update.UploadEndpoint, probeTimeout time.Duration, rosterSyncErrors map[string]string) []SSHHostKeysEndpointStatus {
	trustedMeta, trustedLoadErr := update.LoadTrustedUploadHostKeysFileData(configDir)
	if trustedLoadErr != nil {
		trustedMeta = nil
	}
	trusted := []string(nil)
	var trustedSource string
	var trustedUpdatedAt *time.Time
	if trustedMeta != nil {
		trusted = trustedMeta.SHA256
		trustedSource = trustedMeta.Source
		if !trustedMeta.UpdatedAt.IsZero() {
			t := trustedMeta.UpdatedAt.UTC()
			trustedUpdatedAt = &t
		}
	}

	out := make([]SSHHostKeysEndpointStatus, 0, len(endpoints))
	for _, ep := range endpoints {
		status := SSHHostKeysEndpointStatus{
			Host:                ep.Host,
			Port:                ep.Port,
			TrustedRosterSHA256: trusted,
			TrustedSource:       trustedSource,
			TrustedUpdatedAt:    trustedUpdatedAt,
			HttpsRosterURL:      update.UploadSSHHostKeysWellKnownURL(ep.Host),
		}

		serverFP, probeErr := probeSSHHostKeyFingerprint(ep.Host, ep.Port, probeTimeout)
		if probeErr != nil {
			status.ServerKeyError = probeErr.Error()
		} else {
			status.ServerKeySHA256 = serverFP
		}

		pinnedFP, pinnedOK, pinErr := PinnedHostKeyFingerprint(knownHostsPath, ep.Host, ep.Port)
		if pinErr != nil {
			status.PinnedKeyError = pinErr.Error()
		} else if pinnedOK {
			status.PinnedKeySHA256 = pinnedFP
		}

		status.Status = computeSSHHostKeysStatus(status.ServerKeySHA256, status.PinnedKeySHA256, pinnedOK, status.PinnedKeyError, trusted)
		if rosterSyncErrors != nil {
			if errMsg, ok := rosterSyncErrors[sshHostKeysEndpointKey(ep.Host, ep.Port)]; ok {
				status.RosterSyncError = errMsg
			}
		}
		out = append(out, status)
	}
	return out
}

func computeSSHHostKeysStatus(serverFP, pinnedFP string, pinnedOK bool, pinnedKeyError string, trusted []string) string {
	if serverFP == "" {
		return "probe_failed"
	}
	if strings.TrimSpace(pinnedKeyError) != "" {
		return "pinned_key_error"
	}
	inTrusted := fingerprintInList(serverFP, trusted)
	if pinnedOK {
		if pinnedFP == serverFP {
			return "ok"
		}
		if inTrusted {
			return "mismatch_pending_heal"
		}
		return "mismatch"
	}
	if inTrusted {
		return "ok"
	}
	if len(trusted) == 0 {
		return "roster_unavailable"
	}
	return "mismatch"
}

func fingerprintInList(fp string, list []string) bool {
	fp = strings.TrimSpace(fp)
	for _, item := range list {
		if strings.TrimSpace(item) == fp {
			return true
		}
	}
	return false
}
