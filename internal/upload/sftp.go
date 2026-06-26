package upload

import (
	"fmt"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SFTPClient implements the Client interface using SFTP protocol.
// Each Upload uses its own SSH/SFTP session; Interrupt closes the active session on timeout.
// uploadMu serializes Upload (one in-flight per client) so activeSSH is not shared across goroutines.
type SFTPClient struct {
	mu           sync.Mutex // serializes TestConnection
	uploadMu     sync.Mutex // serializes Upload
	config       Config
	hostKeyStore *hostKeyStore
	activeSSH    atomic.Pointer[ssh.Client]
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
		cfg.Port = 2222 // aviationwx.org SFTP port
	}

	store, err := getSharedHostKeyStore(cfg.KnownHostsPath, "")
	if err != nil {
		return nil, err
	}

	return &SFTPClient{
		config:       cfg,
		hostKeyStore: store,
	}, nil
}

// Upload uploads a file via SFTP with atomic write (tmp + rename).
func (c *SFTPClient) Upload(remotePath string, data []byte) error {
	c.uploadMu.Lock()
	defer c.uploadMu.Unlock()

	sshClient, sftpClient, err := c.dial()
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	c.activeSSH.Store(sshClient)
	defer func() {
		c.activeSSH.Store(nil)
		_ = sftpClient.Close()
		_ = sshClient.Close()
	}()

	remotePath = normalizeRemotePath(remotePath)
	if c.config.BasePath != "" {
		remotePath = path.Join(c.config.BasePath, remotePath)
	}

	remoteDir := path.Dir(remotePath)
	if err := sftpClient.MkdirAll(remoteDir); err != nil {
		_ = err
	}

	tmpPath := fmt.Sprintf("%s.tmp.%d", remotePath, time.Now().UnixNano())

	remote, err := sftpClient.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create remote file: %w", err)
	}

	_, err = remote.Write(data)
	_ = remote.Close()
	if err != nil {
		_ = sftpClient.Remove(tmpPath)
		return fmt.Errorf("upload failed: %w", err)
	}

	if err := sftpClient.Rename(tmpPath, remotePath); err != nil {
		_ = sftpClient.Remove(tmpPath)
		return fmt.Errorf("rename failed: %w", err)
	}

	return nil
}

// Interrupt closes the active SSH connection to unblock a timed-out Upload.
func (c *SFTPClient) Interrupt() {
	if sshClient := c.activeSSH.Load(); sshClient != nil {
		_ = sshClient.Close()
	}
}

// TestConnection tests the SFTP connection and authentication
func (c *SFTPClient) TestConnection() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	sshClient, sftpClient, err := c.dial()
	if err != nil {
		return err
	}
	defer func() {
		_ = sftpClient.Close()
		_ = sshClient.Close()
	}()

	testPath := "."
	if c.config.BasePath != "" {
		testPath = c.config.BasePath
	}
	if _, err := sftpClient.Stat(testPath); err != nil {
		return fmt.Errorf("connection test failed (path: %s): %w", testPath, err)
	}

	return nil
}

// dial establishes a new SSH and SFTP session (not shared across concurrent Upload calls).
func (c *SFTPClient) dial() (*ssh.Client, *sftp.Client, error) {
	timeout := time.Duration(c.config.TimeoutConnectSeconds) * time.Second
	if timeout == 0 {
		timeout = 60 * time.Second
	}

	sshConfig := &ssh.ClientConfig{
		User: c.config.Username,
		Auth: []ssh.AuthMethod{
			ssh.Password(c.config.Password),
		},
		HostKeyCallback: c.hostKeyStore.callback(c.config.Host, c.config.Port),
		Timeout:         timeout,
	}

	addr := fmt.Sprintf("%s:%d", c.config.Host, c.config.Port)
	sshClient, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, nil, fmt.Errorf("ssh dial: %w", err)
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		_ = sshClient.Close()
		return nil, nil, fmt.Errorf("sftp session: %w", err)
	}

	return sshClient, sftpClient, nil
}

// normalizeRemotePath normalizes the remote path by removing leading slashes
func normalizeRemotePath(remotePath string) string {
	return strings.TrimPrefix(remotePath, "/")
}

// Close aborts any in-flight Upload by closing the active SSH session (same as Interrupt).
// Each Upload and TestConnection still closes its own session when the call returns.
func (c *SFTPClient) Close() error {
	c.Interrupt()
	return nil
}
