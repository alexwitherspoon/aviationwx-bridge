package update

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

const (
	uploadSSHHostKeysWellKnownPath  = "/.well-known/aviationwx-upload-ssh-host-keys.json"
	uploadSSHHostKeysHTTPSUserAgent = "aviationwx-org-bridge/upload-hostkeys-https"
)

// uploadSSHHostKeysDocument is the v1 well-known roster JSON from the upload host.
type uploadSSHHostKeysDocument struct {
	Version   int      `json:"version"`
	Hostname  string   `json:"hostname"`
	Port      int      `json:"port"`
	SHA256    []string `json:"sha256"`
	UpdatedAt string   `json:"updated_at"`
}

// uploadHostKeysHTTPClient is injectable for tests (nil uses NewUploadHostKeysTLSHTTPClient).
var uploadHostKeysHTTPClient *http.Client

// SyncUploadSSHHostKeysHTTPS fetches the live roster from the upload host and replaces
// the on-disk trusted fingerprint list when the response is valid. On fetch or validation
// failure the prior trusted file is left unchanged.
func SyncUploadSSHHostKeysHTTPS(configDir, uploadHost string, uploadPort int) (err error) {
	if strings.TrimSpace(configDir) == "" {
		return fmt.Errorf("config directory is required")
	}
	uploadHost = strings.TrimSpace(strings.ToLower(uploadHost))
	if uploadHost == "" {
		return fmt.Errorf("upload host is required")
	}
	if uploadPort <= 0 {
		uploadPort = 2222
	}
	defer func() {
		_ = RecordRosterSyncEndpoint(configDir, uploadHost, uploadPort, err)
	}()

	if _, err = loadUploadRosterRootCAs(); err != nil {
		return err
	}

	trustedHostKeysSyncMu.Lock()
	defer trustedHostKeysSyncMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	doc, err := fetchUploadSSHHostKeysDocument(ctx, uploadHost, uploadPort)
	if err != nil {
		return err
	}
	if err := validateUploadSSHHostKeysDocument(doc, uploadHost, uploadPort); err != nil {
		return err
	}
	if len(doc.SHA256) == 0 {
		return fmt.Errorf("upload ssh host keys roster is empty")
	}

	return writeTrustedHostKeysFile(filepath.Join(configDir, trustedHostKeysFile), doc.SHA256, "https-roster")
}

func fetchUploadSSHHostKeysDocument(ctx context.Context, uploadHost string, uploadPort int) (*uploadSSHHostKeysDocument, error) {
	url := fmt.Sprintf("https://%s%s", uploadHost, uploadSSHHostKeysWellKnownPath)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", uploadSSHHostKeysHTTPSUserAgent)

	client := rosterHTTPClient(uploadHost, requestTimeout)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch upload ssh host keys: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upload ssh host keys roster returned status %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read upload ssh host keys roster: %w", err)
	}

	var doc uploadSSHHostKeysDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse upload ssh host keys roster: %w", err)
	}
	return &doc, nil
}

func validateUploadSSHHostKeysDocument(doc *uploadSSHHostKeysDocument, expectHost string, expectPort int) error {
	if doc == nil {
		return fmt.Errorf("upload ssh host keys roster is missing")
	}
	if doc.Version != 1 {
		return fmt.Errorf("unsupported upload ssh host keys roster version %d", doc.Version)
	}
	gotHost := strings.TrimSpace(strings.ToLower(doc.Hostname))
	expectHost = strings.TrimSpace(strings.ToLower(expectHost))
	if gotHost != expectHost {
		return fmt.Errorf("roster hostname %q does not match upload host %q", doc.Hostname, expectHost)
	}
	if doc.Port != expectPort {
		return fmt.Errorf("roster port %d does not match upload port %d", doc.Port, expectPort)
	}
	return nil
}

func rosterHTTPClient(uploadHost string, timeout time.Duration) *http.Client {
	if uploadHostKeysHTTPClient == nil {
		return NewUploadHostKeysTLSHTTPClient(uploadHost, timeout)
	}
	if uploadHostKeysHTTPClient.CheckRedirect != nil {
		return uploadHostKeysHTTPClient
	}
	copy := *uploadHostKeysHTTPClient
	copy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("upload ssh host keys roster: redirects not allowed")
	}
	return &copy
}

// SetUploadHostKeysHTTPClientForTest overrides the HTTP client used for HTTPS roster fetch.
// Pass nil to restore the production TLS client.
func SetUploadHostKeysHTTPClientForTest(client *http.Client) {
	uploadHostKeysHTTPClient = client
}

// UploadSSHHostKeysWellKnownURL builds the roster URL for an upload host.
func UploadSSHHostKeysWellKnownURL(uploadHost string) string {
	uploadHost = strings.TrimSpace(strings.ToLower(uploadHost))
	return fmt.Sprintf("https://%s%s", uploadHost, uploadSSHHostKeysWellKnownPath)
}

// NewUploadHostKeysTLSHTTPClient returns an HTTP client that verifies TLS for uploadHost.
func NewUploadHostKeysTLSHTTPClient(uploadHost string, timeout time.Duration) *http.Client {
	uploadHost = strings.TrimSpace(uploadHost)
	tlsConfig, err := rosterTLSConfig(uploadHost)
	if err != nil {
		tlsConfig = &tls.Config{
			ServerName: uploadHost,
			MinVersion: tls.VersionTLS12,
		}
	}
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("upload ssh host keys roster: redirects not allowed")
		},
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
			DialContext:     (&net.Dialer{Timeout: timeout}).DialContext,
		},
	}
}
