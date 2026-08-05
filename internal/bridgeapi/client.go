package bridgeapi

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	defaultRequestTimeout = 15 * time.Second
	maxResponseBytes      = 1 << 20
	userAgentPrefix       = "aviationwx-org-bridge/"
	apiKeyHeader          = "X-Api-Key"
)

// Client talks to https://api.aviationwx.org/v1/bridge/* over TLS.
// Contract: aviationwx OpenAPI (PR https://github.com/alexwitherspoon/aviationwx/pull/277).
type Client struct {
	baseURL    string
	apiKey     string
	userAgent  string
	httpClient *http.Client
}

// ClientConfig configures a bridge API client.
type ClientConfig struct {
	BaseURL    string
	APIKey     string
	Version    string
	HTTPClient *http.Client // optional; tests inject httptest client
	// InsecureSkipVerify skips TLS certificate verification. For local mock only.
	InsecureSkipVerify bool
}

// NewClient builds a TLS client. BaseURL should be https://host without a trailing slash.
func NewClient(cfg ClientConfig) (*Client, error) {
	key := strings.TrimSpace(cfg.APIKey)
	if key == "" {
		return nil, fmt.Errorf("api key is required")
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("base URL is required")
	}
	if !strings.HasPrefix(base, "https://") {
		return nil, fmt.Errorf("base URL must use https")
	}
	hc := cfg.HTTPClient
	if hc == nil {
		insecure := cfg.InsecureSkipVerify || apiTLSInsecureFromEnv()
		hc = newTLSHTTPClient(defaultRequestTimeout, insecure)
	}
	version := strings.TrimSpace(cfg.Version)
	if version == "" {
		version = "dev"
	}
	return &Client{
		baseURL:    base,
		apiKey:     key,
		userAgent:  userAgentPrefix + version,
		httpClient: hc,
	}, nil
}

func apiTLSInsecureFromEnv() bool {
	v := strings.TrimSpace(os.Getenv("AVIATIONWX_API_TLS_INSECURE"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func newTLSHTTPClient(timeout time.Duration, insecureSkipVerify bool) *http.Client {
	if insecureSkipVerify {
		warnAPIInsecureTLS()
	}
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout: 10 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: insecureSkipVerify, //nolint:gosec // gated by AVIATIONWX_API_TLS_INSECURE for local mock
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("redirect not allowed")
		},
	}
}

var apiInsecureTLSWarn sync.Once

func warnAPIInsecureTLS() {
	apiInsecureTLSWarn.Do(func() {
		fmt.Fprintf(os.Stderr, "WARNING: AVIATIONWX_API_TLS_INSECURE is set - TLS certificate verification is disabled (local mock only)\n")
	})
}

// Bootstrap calls GET /v1/bridge/bootstrap.
// Success body is { "success": true, "data": { ... } } per OpenAPI.
func (c *Client) Bootstrap(ctx context.Context) (*BootstrapResponse, error) {
	var env bootstrapEnvelope
	if err := c.doJSON(ctx, http.MethodGet, "/v1/bridge/bootstrap", nil, &env); err != nil {
		return nil, err
	}
	if !env.Success {
		return nil, fmt.Errorf("bridge api bootstrap success=false")
	}
	if env.Data == nil {
		return nil, fmt.Errorf("bridge api bootstrap missing data")
	}
	if strings.TrimSpace(env.Data.BridgeID) == "" {
		return nil, fmt.Errorf("bridge api bootstrap missing bridge_id")
	}
	return env.Data, nil
}

// PostHealth calls POST /v1/bridge/health.
func (c *Client) PostHealth(ctx context.Context, req HealthRequest) error {
	return c.doJSON(ctx, http.MethodPost, "/v1/bridge/health", req, nil)
}

// PostWeather calls POST /v1/bridge/weather.
func (c *Client) PostWeather(ctx context.Context, req WeatherRequest) error {
	if strings.TrimSpace(req.SourceID) == "" {
		return fmt.Errorf("source_id is required")
	}
	return c.doJSON(ctx, http.MethodPost, "/v1/bridge/weather", req, nil)
}

type apiError struct {
	StatusCode int
	Body       string
}

func (e *apiError) Error() string {
	if e.Body != "" {
		return fmt.Sprintf("bridge api status %d: %s", e.StatusCode, e.Body)
	}
	return fmt.Sprintf("bridge api status %d", e.StatusCode)
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var bodyReader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set(apiKeyHeader, c.apiKey)
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bridge api request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 256 {
			msg = msg[:256]
		}
		return &apiError{StatusCode: resp.StatusCode, Body: msg}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parse response: %w", err)
	}
	return nil
}

// IsUnauthorized reports whether err is an HTTP 401 from the API.
func IsUnauthorized(err error) bool {
	return StatusCode(err) == http.StatusUnauthorized
}

// StatusCode returns the HTTP status from an API error, or 0.
func StatusCode(err error) int {
	var e *apiError
	if errors.As(err, &e) {
		return e.StatusCode
	}
	return 0
}
