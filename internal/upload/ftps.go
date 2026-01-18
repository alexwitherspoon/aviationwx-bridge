package upload

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/secsy/goftp"
)

// FTPSClient implements Client interface for FTPS uploads
type FTPSClient struct {
	config Config
}

// NewFTPSClient creates a new FTPS client instance
func NewFTPSClient(config Config) (*FTPSClient, error) {
	if config.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if config.Username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if config.Password == "" {
		return nil, fmt.Errorf("password is required")
	}

	port := config.Port
	if port == 0 {
		port = 21          // Default FTP port
		config.Port = port // Update config for consistency
	}

	// Apply defaults
	if !config.TLS {
		config.TLS = true // Default to TLS
	}
	if !config.TLSVerify {
		config.TLSVerify = true // Default to verify
	}
	if config.TimeoutConnectSeconds == 0 {
		config.TimeoutConnectSeconds = 10
	}
	if config.TimeoutUploadSeconds == 0 {
		config.TimeoutUploadSeconds = 30
	}

	return &FTPSClient{
		config: config,
	}, nil
}

// Upload uploads image data to the remote path using atomic operations.
// Uploads to .tmp file first, then renames to final filename.
// Ensures .tmp files are never treated as displayable.
func (c *FTPSClient) Upload(remotePath string, data []byte) error {
	// Normalize remote path
	remotePath = normalizeRemotePath(remotePath)

	// Create .tmp filename
	tmpPath := remotePath + ".tmp"

	// Connect to FTPS server first (needed for directory creation)
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			return
		}
	}()

	// Ensure directory exists
	dir := filepath.Dir(remotePath)
	if dir != "." && dir != "" {
		if err := c.ensureDirectoryWithConn(conn, dir); err != nil {
			return &UploadError{
				RemotePath: remotePath,
				Message:    "ensure directory",
				Err:        err,
			}
		}
	}

	// Upload to .tmp file
	if err := c.uploadFile(conn, tmpPath, data); err != nil {
		// Try to cleanup .tmp file on failure
		_ = conn.Delete(tmpPath)
		return &UploadError{
			RemotePath: remotePath,
			Message:    "upload to .tmp",
			Err:        err,
		}
	}

	// Rename .tmp to final filename (atomic operation)
	if err := c.renameFile(conn, tmpPath, remotePath); err != nil {
		// Try to cleanup .tmp file on failure
		_ = conn.Delete(tmpPath)
		return &UploadError{
			RemotePath: remotePath,
			Message:    "rename .tmp to final",
			Err:        err,
		}
	}

	return nil
}

// TestConnection tests the FTPS connection and authentication.
// Returns error if connection or authentication fails.
func (c *FTPSClient) TestConnection() error {
	conn, err := c.connect()
	if err != nil {
		return err
	}
	defer func() {
		if err := conn.Close(); err != nil {
			return
		}
	}()

	// Test by reading current directory
	_, err = conn.ReadDir(".")
	if err != nil {
		return &ConnectionError{
			Message: "test connection failed",
			Err:     err,
		}
	}

	return nil
}

// connect establishes FTPS connection with proper TLS configuration
func (c *FTPSClient) connect() (*goftp.Client, error) {
	// Build FTP address
	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)

	// Configure TLS
	tlsConfig := &tls.Config{
		ServerName:         c.config.Host,
		InsecureSkipVerify: !c.config.TLSVerify,
	}

	// Load custom CA bundle if provided
	if c.config.CABundlePath != "" {
		caCert, err := os.ReadFile(c.config.CABundlePath)
		if err != nil {
			return nil, fmt.Errorf("read CA bundle: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA bundle")
		}

		tlsConfig.RootCAs = caCertPool
	}

	// Configure FTP client
	ftpConfig := goftp.Config{
		User:            c.config.Username,
		Password:        c.config.Password,
		Timeout:         time.Duration(c.config.TimeoutConnectSeconds) * time.Second,
		TLSConfig:       tlsConfig,
		TLSMode:         goftp.TLSExplicit, // Use explicit TLS (FTPS)
		DisableEPSV:     false,
		ActiveTransfers: false, // Use passive mode
	}

	// Connect with timeout
	conn, err := goftp.DialConfig(ftpConfig, addr)
	if err != nil {
		// Check for timeout
		if isTimeoutError(err) {
			return nil, &TimeoutError{
				Operation: "connect",
				Timeout:   time.Duration(c.config.TimeoutConnectSeconds) * time.Second,
				Err:       err,
			}
		}

		// Check for auth errors
		if isAuthError(err) {
			return nil, &AuthError{
				Message: "authentication failed",
				Err:     err,
			}
		}

		return nil, &ConnectionError{
			Message: "dial failed",
			Err:     err,
		}
	}

	return conn, nil
}

// uploadFile uploads data to the specified remote path
func (c *FTPSClient) uploadFile(conn *goftp.Client, remotePath string, data []byte) error {
	// Create reader from data
	reader := bytes.NewReader(data)

	// Upload with timeout
	// Note: goftp doesn't have built-in upload timeout, so we rely on connection timeout
	// For upload timeout, we'd need to implement a timeout wrapper around the reader
	err := conn.Store(remotePath, reader)
	if err != nil {
		// Check for timeout
		if isTimeoutError(err) {
			return &TimeoutError{
				Operation: "upload",
				Timeout:   time.Duration(c.config.TimeoutUploadSeconds) * time.Second,
				Err:       err,
			}
		}
		return fmt.Errorf("store file: %w", err)
	}

	return nil
}

// renameFile renames a file using FTP RNFR/RNTO commands (atomic operation)
func (c *FTPSClient) renameFile(conn *goftp.Client, oldPath, newPath string) error {
	err := conn.Rename(oldPath, newPath)
	if err != nil {
		return fmt.Errorf("rename file: %w", err)
	}
	return nil
}

// ensureDirectoryWithConn ensures the remote directory exists, creating it if needed
func (c *FTPSClient) ensureDirectoryWithConn(conn *goftp.Client, remoteDir string) error {
	// Split path into components
	parts := strings.Split(strings.Trim(remoteDir, "/"), "/")

	currentPath := ""
	for _, part := range parts {
		if part == "" {
			continue
		}

		if currentPath == "" {
			currentPath = part
		} else {
			currentPath = currentPath + "/" + part
		}

		// Try to create directory (may already exist, ignore error)
		// goftp.Mkdir returns the path if successful, or error if it already exists
		_, err := conn.Mkdir(currentPath)
		if err != nil {
			// If error is not "already exists", we'll still try to continue
			// Many FTP servers return error even if directory exists
			// We'll verify by trying to read it
			_, readErr := conn.ReadDir(currentPath)
			if readErr != nil {
				// Directory doesn't exist and we couldn't create it
				return fmt.Errorf("create directory %s: %w", currentPath, err)
			}
			// Directory exists, continue
		}
	}

	return nil
}

// normalizeRemotePath normalizes the remote path by removing leading slashes
func normalizeRemotePath(remotePath string) string {
	return strings.TrimPrefix(remotePath, "/")
}

// Helper functions

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "deadline exceeded") ||
		strings.Contains(errStr, "i/o timeout")
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "530") || // FTP 530 = Not logged in
		strings.Contains(errStr, "authentication") ||
		strings.Contains(errStr, "login") ||
		strings.Contains(errStr, "password")
}
