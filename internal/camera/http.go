package camera

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPCamera implements Camera interface for HTTP snapshot URLs
type HTTPCamera struct {
	config Config
	client *http.Client
}

// NewHTTPCamera creates a new HTTP camera instance.
// Returns an error if the snapshot URL is missing.
func NewHTTPCamera(config Config) (*HTTPCamera, error) {
	if config.SnapshotURL == "" {
		return nil, fmt.Errorf("snapshot_url is required for HTTP camera")
	}

	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 15 * time.Second // Default timeout
	}

	return &HTTPCamera{
		config: config,
		client: &http.Client{
			Timeout: timeout,
		},
	}, nil
}

// Capture fetches a fresh snapshot from the HTTP URL.
// Uses cache-busting headers and query parameter to ensure fresh image.
// Always returns fresh data - never cached or stale images.
func (c *HTTPCamera) Capture(ctx context.Context) ([]byte, error) {
	// Add cache-busting query parameter
	url := c.config.SnapshotURL
	separator := "?"
	if strings.Contains(url, "?") {
		separator = "&"
	}
	url = fmt.Sprintf("%s%st=%d", url, separator, time.Now().UnixMilli())

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, &CaptureError{
			CameraID: c.config.ID,
			Message:  "create request",
			Err:      err,
		}
	}

	// Add cache-busting headers
	req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	req.Header.Set("Pragma", "no-cache")
	req.Header.Set("Expires", "0")

	// Add authentication if configured
	if c.config.Auth != nil {
		if err := c.addAuth(req); err != nil {
			return nil, err
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		// Check if error is due to timeout
		if ctx.Err() == context.DeadlineExceeded || isTimeoutError(err) {
			return nil, &TimeoutError{
				CameraID: c.config.ID,
				Timeout:  c.client.Timeout,
			}
		}
		return nil, &CaptureError{
			CameraID: c.config.ID,
			Message:  "HTTP request failed",
			Err:      err,
		}
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			return
		}
	}()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, &AuthError{
			CameraID: c.config.ID,
			Message:  "authentication failed",
		}
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &CaptureError{
			CameraID: c.config.ID,
			Message:  fmt.Sprintf("HTTP status %d", resp.StatusCode),
		}
	}

	// Read image data
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, &CaptureError{
			CameraID: c.config.ID,
			Message:  "read response body",
			Err:      err,
		}
	}

	if len(data) == 0 {
		return nil, &CaptureError{
			CameraID: c.config.ID,
			Message:  "empty response body",
		}
	}

	return data, nil
}

// ID returns the camera identifier
func (c *HTTPCamera) ID() string {
	return c.config.ID
}

// Type returns the camera type
func (c *HTTPCamera) Type() string {
	return "http"
}

// addAuth adds authentication headers to the request
func (c *HTTPCamera) addAuth(req *http.Request) error {
	auth := c.config.Auth

	switch auth.Type {
	case "basic":
		if auth.Username == "" || auth.Password == "" {
			return &AuthError{
				CameraID: c.config.ID,
				Message:  "username and password required for basic auth",
			}
		}
		req.SetBasicAuth(auth.Username, auth.Password)

	case "digest":
		// Digest auth not implemented - falls back to basic auth
		if auth.Username == "" || auth.Password == "" {
			return &AuthError{
				CameraID: c.config.ID,
				Message:  "username and password required for digest auth",
			}
		}
		req.SetBasicAuth(auth.Username, auth.Password)

	case "bearer":
		if auth.Token == "" {
			return &AuthError{
				CameraID: c.config.ID,
				Message:  "token required for bearer auth",
			}
		}
		req.Header.Set("Authorization", "Bearer "+auth.Token)

	default:
		return &AuthError{
			CameraID: c.config.ID,
			Message:  fmt.Sprintf("unsupported auth type: %s", auth.Type),
		}
	}

	return nil
}

// Helper functions

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	// Check for common timeout error messages
	errStr := err.Error()
	return strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded")
}
