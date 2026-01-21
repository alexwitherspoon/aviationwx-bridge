package upload

import (
	"fmt"
	"path/filepath"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTPClient implements the Client interface using SFTP protocol
type SFTPClient struct {
	config     Config
	sshClient  *ssh.Client
	sftpClient *sftp.Client
}

// NewSFTPClient creates a new SFTP upload client
func NewSFTPClient(cfg Config) (*SFTPClient, error) {
	if cfg.Host == "" {
		return nil, fmt.Errorf("host is required")
	}
	if cfg.Username == "" {
		return nil, fmt.Errorf("username is required")
	}
	if cfg.Password == "" {
		return nil, fmt.Errorf("password is required")
	}
	if cfg.Port == 0 {
		cfg.Port = 22 // Default SFTP port
	}

	return &SFTPClient{
		config: cfg,
	}, nil
}

// Upload uploads a file via SFTP with atomic write (tmp + rename)
func (c *SFTPClient) Upload(remotePath string, data []byte) error {
	start := time.Now()

	// Connect
	if err := c.connect(); err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer func() { _ = c.Close() }() // Best-effort cleanup

	fileSize := int64(len(data))

	// Normalize remote path and prepend base path
	remotePath = normalizeRemotePath(remotePath)
	if c.config.BasePath != "" {
		remotePath = filepath.Join(c.config.BasePath, remotePath)
	}

	// Create remote directory if needed
	remoteDir := filepath.Dir(remotePath)
	if err := c.sftpClient.MkdirAll(remoteDir); err != nil {
		// Ignore error - directory may already exist
		_ = err
	}

	// Atomic upload: write to .tmp, then rename
	tmpPath := fmt.Sprintf("%s.tmp.%d", remotePath, time.Now().UnixNano())

	remote, err := c.sftpClient.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create remote file: %w", err)
	}

	// Write data
	_, err = remote.Write(data)
	_ = remote.Close() // Close before checking write error
	if err != nil {
		_ = c.sftpClient.Remove(tmpPath) // Cleanup on failure (best-effort)
		return fmt.Errorf("upload failed: %w", err)
	}

	// Atomic rename
	if err := c.sftpClient.Rename(tmpPath, remotePath); err != nil {
		_ = c.sftpClient.Remove(tmpPath) // Cleanup on rename failure (best-effort)
		return fmt.Errorf("rename failed: %w", err)
	}

	duration := time.Since(start)
	speedKBps := float64(fileSize) / duration.Seconds() / 1024

	// Note: Logging is handled by the scheduler layer
	_ = speedKBps // Avoid unused variable warning

	return nil
}

// TestConnection tests the SFTP connection and authentication
func (c *SFTPClient) TestConnection() error {
	if err := c.connect(); err != nil {
		return err
	}
	defer func() { _ = c.Close() }() // Best-effort cleanup

	// Try to stat the base path directory to verify connection works
	testPath := "."
	if c.config.BasePath != "" {
		testPath = c.config.BasePath
	}
	if _, err := c.sftpClient.Stat(testPath); err != nil {
		return fmt.Errorf("connection test failed (path: %s): %w", testPath, err)
	}

	return nil
}

// connect establishes SSH and SFTP connections
func (c *SFTPClient) connect() error {
	// SSH client config
	timeout := time.Duration(c.config.TimeoutConnectSeconds) * time.Second
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	sshConfig := &ssh.ClientConfig{
		User: c.config.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(c.config.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // TODO: Add host key verification in future
		Timeout:         timeout,
	}

	// Connect SSH
	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)
	var err error
	c.sshClient, err = ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return fmt.Errorf("ssh dial: %w", err)
	}

	// Open SFTP session
	c.sftpClient, err = sftp.NewClient(c.sshClient)
	if err != nil {
		_ = c.sshClient.Close() // Best-effort cleanup on SFTP session failure
		return fmt.Errorf("sftp session: %w", err)
	}

	return nil
}

// Close closes SFTP and SSH connections
func (c *SFTPClient) Close() error {
	var errs []error

	if c.sftpClient != nil {
		if err := c.sftpClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("sftp close: %w", err))
		}
	}

	if c.sshClient != nil {
		if err := c.sshClient.Close(); err != nil {
			errs = append(errs, fmt.Errorf("ssh close: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}

	return nil
}
