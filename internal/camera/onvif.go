package camera

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/korylprince/go-onvif"
	"github.com/korylprince/go-onvif/soap"
)

// ONVIFCamera implements Camera interface for ONVIF-compliant cameras.
// Uses ONVIF SOAP API to discover and fetch snapshot URIs.
type ONVIFCamera struct {
	config      Config
	httpClient  *http.Client
	onvifClient *onvif.Client
	snapshotURI string // Cached snapshot URI
	mediaXAddr  string // Media service XAddr
	mediaNS     string // Cached media namespace (v1 or v2)
}

// NewONVIFCamera creates a new ONVIF camera instance.
// Returns an error if required ONVIF configuration is missing.
func NewONVIFCamera(config Config) (*ONVIFCamera, error) {
	if config.ONVIF == nil {
		return nil, fmt.Errorf("onvif config is required")
	}
	if config.ONVIF.Endpoint == "" {
		return nil, fmt.Errorf("onvif.endpoint is required")
	}
	if config.ONVIF.Username == "" {
		return nil, fmt.Errorf("onvif.username is required")
	}
	if config.ONVIF.Password == "" {
		return nil, fmt.Errorf("onvif.password is required")
	}

	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	if timeout == 0 {
		timeout = 15 * time.Second // Default timeout
	}

	httpClient := &http.Client{
		Timeout: timeout,
	}

	onvifClient := &onvif.Client{
		Username:   config.ONVIF.Username,
		Password:   config.ONVIF.Password,
		HTTPClient: httpClient,
	}

	return &ONVIFCamera{
		config:      config,
		httpClient:  httpClient,
		onvifClient: onvifClient,
	}, nil
}

// Capture fetches a fresh snapshot from the ONVIF camera.
// First obtains the snapshot URI if not cached, then fetches the image.
// Always returns fresh data - never cached or stale images.
func (c *ONVIFCamera) Capture(ctx context.Context) ([]byte, error) {
	// Get snapshot URI if not cached
	if c.snapshotURI == "" {
		uri, err := c.getSnapshotURI(ctx)
		if err != nil {
			return nil, &CaptureError{
				CameraID: c.config.ID,
				Message:  "get snapshot URI",
				Err:      err,
			}
		}
		c.snapshotURI = uri
	}

	// Fetch snapshot from URI
	req, err := http.NewRequestWithContext(ctx, "GET", c.snapshotURI, nil)
	if err != nil {
		return nil, &CaptureError{
			CameraID: c.config.ID,
			Message:  "create snapshot request",
			Err:      err,
		}
	}

	// Add authentication
	req.SetBasicAuth(c.config.ONVIF.Username, c.config.ONVIF.Password)

	// Add cache-busting headers
	req.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
	req.Header.Set("Pragma", "no-cache")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Check if error is due to timeout
		if ctx.Err() == context.DeadlineExceeded || isTimeoutError(err) {
			return nil, &TimeoutError{
				CameraID: c.config.ID,
				Timeout:  c.httpClient.Timeout,
			}
		}

		// Snapshot URI might be stale, clear cache and retry once
		// Limit retry to prevent infinite recursion
		if c.snapshotURI != "" {
			c.snapshotURI = ""
			// Use iterative retry instead of recursion to prevent stack overflow
			uri, retryErr := c.getSnapshotURI(ctx)
			if retryErr != nil {
				return nil, &CaptureError{
					CameraID: c.config.ID,
					Message:  "retry get snapshot URI",
					Err:      retryErr,
				}
			}
			c.snapshotURI = uri
			// Retry the HTTP request with new URI
			retryReq, retryErr := http.NewRequestWithContext(ctx, "GET", c.snapshotURI, nil)
			if retryErr != nil {
				return nil, &CaptureError{
					CameraID: c.config.ID,
					Message:  "create retry snapshot request",
					Err:      retryErr,
				}
			}
			retryReq.SetBasicAuth(c.config.ONVIF.Username, c.config.ONVIF.Password)
			retryReq.Header.Set("Cache-Control", "no-cache, no-store, must-revalidate")
			retryReq.Header.Set("Pragma", "no-cache")

			retryResp, retryErr := c.httpClient.Do(retryReq)
			if retryErr != nil {
				return nil, &CaptureError{
					CameraID: c.config.ID,
					Message:  "HTTP retry request failed",
					Err:      retryErr,
				}
			}
			defer func() {
				if err := retryResp.Body.Close(); err != nil {
					return
				}
			}()

			if retryResp.StatusCode == http.StatusUnauthorized {
				c.snapshotURI = ""
				return nil, &AuthError{
					CameraID: c.config.ID,
					Message:  "authentication failed",
				}
			}

			if retryResp.StatusCode != http.StatusOK {
				return nil, &CaptureError{
					CameraID: c.config.ID,
					Message:  fmt.Sprintf("HTTP status %d", retryResp.StatusCode),
				}
			}

			// Read image data from retry response
			retryData, retryErr := io.ReadAll(retryResp.Body)
			if retryErr != nil {
				return nil, &CaptureError{
					CameraID: c.config.ID,
					Message:  "read retry response body",
					Err:      retryErr,
				}
			}

			if len(retryData) == 0 {
				return nil, &CaptureError{
					CameraID: c.config.ID,
					Message:  "empty retry response body",
				}
			}

			return retryData, nil
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
		// Auth failed, clear snapshot URI cache
		c.snapshotURI = ""
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

// getSnapshotURI obtains the snapshot URI from the ONVIF device
// Uses ONVIF SOAP API to get the snapshot URI from the media service
func (c *ONVIFCamera) getSnapshotURI(ctx context.Context) (string, error) {
	// First, get device services to find media service
	if c.mediaXAddr == "" {
		services, err := c.onvifClient.GetServices(c.config.ONVIF.Endpoint)
		if err != nil {
			return "", fmt.Errorf("get services: %w", err)
		}

		// Find media service (try v2 first, then v1)
		c.mediaXAddr = services.URL(onvif.NamespaceMedia2)
		if c.mediaXAddr != "" {
			c.mediaNS = onvif.NamespaceMedia2
		} else {
			c.mediaXAddr = services.URL(onvif.NamespaceMedia)
			if c.mediaXAddr != "" {
				c.mediaNS = onvif.NamespaceMedia
			}
		}

		if c.mediaXAddr == "" {
			return "", fmt.Errorf("media service not found")
		}
	}

	// Get profiles to find profile token
	profileToken := c.config.ONVIF.ProfileToken
	if profileToken == "" {
		// Auto-discover first profile if not specified
		token, err := c.getFirstProfileToken(ctx)
		if err != nil {
			return "", fmt.Errorf("get profile token: %w", err)
		}
		profileToken = token
	}

	// Use cached media namespace
	mediaNS := c.mediaNS
	if mediaNS == "" {
		// Fallback if not cached (shouldn't happen)
		mediaNS = onvif.NamespaceMedia
	}

	// Construct GetSnapshotUri SOAP request
	type GetSnapshotURI struct {
		XMLName      xml.Name `xml:"trt:GetSnapshotUri"`
		ProfileToken string   `xml:"trt:ProfileToken"`
	}

	req := &onvif.Request{
		URL:        c.mediaXAddr,
		Namespaces: soap.Namespaces{"trt": mediaNS},
		Body:       &GetSnapshotURI{ProfileToken: profileToken},
	}

	// Make SOAP request
	envelope, err := c.onvifClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("SOAP request failed: %w", err)
	}

	// Parse response to extract snapshot URI
	uri, err := c.parseSnapshotURIResponse(envelope)
	if err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	return uri, nil
}

// getFirstProfileToken gets the first available profile token
func (c *ONVIFCamera) getFirstProfileToken(ctx context.Context) (string, error) {
	// Use cached media namespace
	mediaNS := c.mediaNS
	if mediaNS == "" {
		// Fallback if not cached (shouldn't happen)
		mediaNS = onvif.NamespaceMedia
	}

	// Define GetProfiles request struct
	type GetProfiles struct {
		XMLName xml.Name `xml:"trt:GetProfiles"`
	}

	req := &onvif.Request{
		URL:        c.mediaXAddr,
		Namespaces: soap.Namespaces{"trt": mediaNS},
		Body:       &GetProfiles{},
	}

	envelope, err := c.onvifClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("get profiles: %w", err)
	}

	// Define response struct
	type Profile struct {
		Token string `xml:"token,attr"`
	}

	type GetProfilesResponse struct {
		XMLName  xml.Name  `xml:"GetProfilesResponse"`
		Profiles []Profile `xml:"Profiles>Profile"`
	}

	var resp GetProfilesResponse
	if err := envelope.Body.Unmarshal(&resp); err != nil {
		return "", fmt.Errorf("parse profiles response: %w", err)
	}

	if len(resp.Profiles) == 0 {
		return "", fmt.Errorf("no profiles found")
	}

	return resp.Profiles[0].Token, nil
}

// parseSnapshotURIResponse extracts the snapshot URI from SOAP response
func (c *ONVIFCamera) parseSnapshotURIResponse(envelope *soap.Envelope) (string, error) {
	type MediaURI struct {
		URI string `xml:"Uri"`
	}

	type GetSnapshotURIResponse struct {
		XMLName  xml.Name `xml:"GetSnapshotUriResponse"`
		MediaURI MediaURI `xml:"MediaUri"`
	}

	var resp GetSnapshotURIResponse
	if err := envelope.Body.Unmarshal(&resp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}

	if resp.MediaURI.URI == "" {
		return "", fmt.Errorf("snapshot URI not found in response")
	}

	return resp.MediaURI.URI, nil
}

// ID returns the camera identifier
func (c *ONVIFCamera) ID() string {
	return c.config.ID
}

// Type returns the camera type
func (c *ONVIFCamera) Type() string {
	return "onvif"
}
