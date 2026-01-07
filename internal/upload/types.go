package upload

import (
	"time"
)

// Client defines the interface for upload clients
type Client interface {
	// Upload uploads image data to the remote path using atomic operations
	// Uploads to .tmp file first, then renames to final filename
	// Returns error if upload or rename fails
	Upload(remotePath string, data []byte) error

	// TestConnection tests the FTPS connection and authentication
	// Returns error if connection fails
	TestConnection() error
}

// Config represents upload configuration
type Config struct {
	Host                  string
	Port                  int
	Username              string
	Password              string
	TLS                   bool
	TLSVerify             bool
	CABundlePath          string
	TimeoutConnectSeconds int
	TimeoutUploadSeconds  int
}

// Error types for upload operations
type (
	// ConnectionError indicates a connection failure
	ConnectionError struct {
		Message string
		Err     error
	}

	// AuthError indicates authentication failed
	AuthError struct {
		Message string
		Err     error
	}

	// UploadError indicates an upload failure
	UploadError struct {
		RemotePath string
		Message    string
		Err        error
	}

	// TimeoutError indicates an operation timed out
	TimeoutError struct {
		Operation string
		Timeout   time.Duration
		Err       error
	}
)

func (e *ConnectionError) Error() string {
	if e.Err != nil {
		return "connection failed: " + e.Message + ": " + e.Err.Error()
	}
	return "connection failed: " + e.Message
}

func (e *ConnectionError) Unwrap() error {
	return e.Err
}

func (e *AuthError) Error() string {
	if e.Err != nil {
		return "authentication failed: " + e.Message + ": " + e.Err.Error()
	}
	return "authentication failed: " + e.Message
}

func (e *AuthError) Unwrap() error {
	return e.Err
}

func (e *UploadError) Error() string {
	if e.Err != nil {
		return "upload failed: " + e.RemotePath + ": " + e.Message + ": " + e.Err.Error()
	}
	return "upload failed: " + e.RemotePath + ": " + e.Message
}

func (e *UploadError) Unwrap() error {
	return e.Err
}

func (e *TimeoutError) Error() string {
	if e.Err != nil {
		return "timeout: " + e.Operation + " (timeout: " + e.Timeout.String() + "): " + e.Err.Error()
	}
	return "timeout: " + e.Operation + " (timeout: " + e.Timeout.String() + ")"
}

func (e *TimeoutError) Unwrap() error {
	return e.Err
}
