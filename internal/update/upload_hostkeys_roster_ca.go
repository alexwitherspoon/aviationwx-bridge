package update

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"strings"
	"sync"
)

const uploadRosterCAFileEnv = "AVIATIONWX_UPLOAD_ROSTER_CA_FILE"

var (
	rosterCAMu     sync.Mutex
	rosterCACached *x509.CertPool
	rosterCAPath   string
)

// loadUploadRosterRootCAs returns extra root CAs from AVIATIONWX_UPLOAD_ROSTER_CA_FILE.
// When unset, returns (nil, nil) and the TLS client uses the system pool only.
func loadUploadRosterRootCAs() (*x509.CertPool, error) {
	path := strings.TrimSpace(os.Getenv(uploadRosterCAFileEnv))
	if path == "" {
		return nil, nil
	}

	rosterCAMu.Lock()
	defer rosterCAMu.Unlock()
	if rosterCAPath == path && rosterCACached != nil {
		return rosterCACached, nil
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read upload roster CA file: %w", err)
	}

	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(raw) {
		return nil, fmt.Errorf("upload roster CA file contains no valid PEM certificates")
	}

	rosterCAPath = path
	rosterCACached = pool
	return rosterCACached, nil
}

// resetUploadRosterCAForTest clears the roster CA cache (tests only).
func resetUploadRosterCAForTest() {
	rosterCAMu.Lock()
	defer rosterCAMu.Unlock()
	rosterCACached = nil
	rosterCAPath = ""
}

func rosterTLSConfig(serverName string) (*tls.Config, error) {
	serverName = strings.TrimSpace(serverName)
	pool, err := loadUploadRosterRootCAs()
	if err != nil {
		return nil, err
	}
	cfg := &tls.Config{
		ServerName: serverName,
		MinVersion: tls.VersionTLS12,
	}
	if pool != nil {
		cfg.RootCAs = pool
	}
	return cfg, nil
}
