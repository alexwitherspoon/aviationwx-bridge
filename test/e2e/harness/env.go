//go:build e2e

package harness

import (
	"os"
	"time"
)

const (
	UploadHost     = "upload.e2e.test"
	UploadSFTPPort = "2222"
	UploadHTTPS    = "https://upload.e2e.test"
	RosterPath     = "/.well-known/aviationwx-upload-ssh-host-keys.json"

	SimulatorHost = "camera-simulator"
	SimulatorHTTP = "http://camera-simulator"

	BridgeUser = "admin"
	BridgePass = "aviationwx"

	DefaultWaitTimeout = 3 * time.Minute
	DefaultPoll        = 2 * time.Second
)

// BridgeWebURL returns the bridge web console base URL for E2E tests.
func BridgeWebURL() string {
	if v := os.Getenv("E2E_BRIDGE_URL"); v != "" {
		return v
	}
	return "http://127.0.0.1:1231"
}

// UploadRosterURL returns the HTTPS roster URL for the E2E upload host.
func UploadRosterURL() string {
	return UploadHTTPS + RosterPath
}

// UploadRosterCAFile returns the path to the harness CA PEM for bridge roster TLS.
func UploadRosterCAFile() string {
	if v := os.Getenv("AVIATIONWX_UPLOAD_ROSTER_CA_FILE"); v != "" {
		return v
	}
	return E2EPath("testdata", "e2e", "tls", "ca.pem")
}

// E2EUploadSFTPUser returns fixture SFTP username.
func E2EUploadSFTPUser() string { return "e2eCam01Push01" }

// E2EUploadSFTPPassword returns fixture SFTP password (14 alphanumeric).
func E2EUploadSFTPPassword() string { return "E2ePass1234567" }
