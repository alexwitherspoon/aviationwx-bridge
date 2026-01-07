package upload

import (
	"errors"
	"testing"
	"time"
)

func TestConnectionError(t *testing.T) {
	err := &ConnectionError{
		Message: "test connection error",
		Err:     errors.New("underlying error"),
	}

	if err.Error() == "" {
		t.Error("ConnectionError.Error() returned empty string")
	}

	if !errors.Is(err, err.Err) {
		t.Error("ConnectionError.Unwrap() should return underlying error")
	}

	unwrapped := err.Unwrap()
	if unwrapped == nil {
		t.Error("ConnectionError.Unwrap() returned nil")
	}
}

func TestAuthError(t *testing.T) {
	err := &AuthError{
		Message: "test auth error",
		Err:     errors.New("underlying error"),
	}

	if err.Error() == "" {
		t.Error("AuthError.Error() returned empty string")
	}

	unwrapped := err.Unwrap()
	if unwrapped == nil {
		t.Error("AuthError.Unwrap() returned nil")
	}
}

func TestUploadError(t *testing.T) {
	err := &UploadError{
		RemotePath: "test/path.jpg",
		Message:    "test upload error",
		Err:        errors.New("underlying error"),
	}

	if err.Error() == "" {
		t.Error("UploadError.Error() returned empty string")
	}

	if err.RemotePath != "test/path.jpg" {
		t.Errorf("UploadError.RemotePath = %s, want 'test/path.jpg'", err.RemotePath)
	}

	unwrapped := err.Unwrap()
	if unwrapped == nil {
		t.Error("UploadError.Unwrap() returned nil")
	}
}

func TestTimeoutError(t *testing.T) {
	err := &TimeoutError{
		Operation: "test operation",
		Timeout:   10 * time.Second,
		Err:       errors.New("underlying error"),
	}

	if err.Error() == "" {
		t.Error("TimeoutError.Error() returned empty string")
	}

	if err.Operation != "test operation" {
		t.Errorf("TimeoutError.Operation = %s, want 'test operation'", err.Operation)
	}

	if err.Timeout != 10*time.Second {
		t.Errorf("TimeoutError.Timeout = %v, want 10s", err.Timeout)
	}

	unwrapped := err.Unwrap()
	if unwrapped == nil {
		t.Error("TimeoutError.Unwrap() returned nil")
	}
}

func TestConnectionError_WithoutUnderlying(t *testing.T) {
	err := &ConnectionError{
		Message: "test connection error",
	}

	if err.Error() == "" {
		t.Error("ConnectionError.Error() returned empty string")
	}

	unwrapped := err.Unwrap()
	if unwrapped != nil {
		t.Error("ConnectionError.Unwrap() should return nil when no underlying error")
	}
}

func TestAuthError_WithoutUnderlying(t *testing.T) {
	err := &AuthError{
		Message: "test auth error",
	}

	if err.Error() == "" {
		t.Error("AuthError.Error() returned empty string")
	}
}

func TestUploadError_WithoutUnderlying(t *testing.T) {
	err := &UploadError{
		RemotePath: "test/path.jpg",
		Message:    "test upload error",
	}

	if err.Error() == "" {
		t.Error("UploadError.Error() returned empty string")
	}
}

func TestTimeoutError_WithoutUnderlying(t *testing.T) {
	err := &TimeoutError{
		Operation: "test operation",
		Timeout:   10 * time.Second,
	}

	if err.Error() == "" {
		t.Error("TimeoutError.Error() returned empty string")
	}
}
