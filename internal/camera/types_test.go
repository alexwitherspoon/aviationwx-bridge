package camera

import (
	"errors"
	"testing"
	"time"
)

func TestTimeoutError(t *testing.T) {
	err := &TimeoutError{
		CameraID: "test-camera",
		Timeout:  5 * time.Second,
	}

	if err.Error() != "capture timeout: test-camera" {
		t.Errorf("expected 'capture timeout: test-camera', got '%s'", err.Error())
	}
}

func TestAuthError(t *testing.T) {
	err := &AuthError{
		CameraID: "test-camera",
		Message:  "invalid credentials",
	}

	expected := "authentication failed: test-camera: invalid credentials"
	if err.Error() != expected {
		t.Errorf("expected '%s', got '%s'", expected, err.Error())
	}
}

func TestCaptureError(t *testing.T) {
	tests := []struct {
		name    string
		err     *CaptureError
		wantMsg string
	}{
		{
			name: "without wrapped error",
			err: &CaptureError{
				CameraID: "test-camera",
				Message:  "connection failed",
			},
			wantMsg: "capture failed: test-camera: connection failed",
		},
		{
			name: "with wrapped error",
			err: &CaptureError{
				CameraID: "test-camera",
				Message:  "connection failed",
				Err:      errors.New("network unreachable"),
			},
			wantMsg: "capture failed: test-camera: connection failed: network unreachable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.wantMsg {
				t.Errorf("expected '%s', got '%s'", tt.wantMsg, tt.err.Error())
			}

			if tt.err.Err != nil {
				if !errors.Is(tt.err, tt.err.Err) {
					t.Error("Unwrap() should return wrapped error")
				}
			}
		})
	}
}

func TestCaptureError_Unwrap(t *testing.T) {
	originalErr := errors.New("original error")
	err := &CaptureError{
		CameraID: "test-camera",
		Message:  "test",
		Err:      originalErr,
	}

	unwrapped := err.Unwrap()
	if unwrapped != originalErr {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, originalErr)
	}
}
