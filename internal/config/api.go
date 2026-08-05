package config

import (
	"fmt"
	"net/url"
	"strings"
	"unicode"
)

// DefaultAPIBaseURL is the production AviationWX bridge API origin (paths are /v1/bridge/*).
// Shared contract: aviationwx OpenAPI on PR https://github.com/alexwitherspoon/aviationwx/pull/277
const DefaultAPIBaseURL = "https://api.aviationwx.org"

// API key shape matches core lib/bridge/keys.php (awxb_ + 48 alphanumeric).
const (
	APIKeyPrefix       = "awxb_"
	APIKeySecretLength = 48
)

// APISettings configures the optional HTTPS link to api.aviationwx.org.
// When omitted or Enabled is false, the bridge does not call the API.
type APISettings struct {
	Enabled bool   `json:"enabled"`
	Key     string `json:"key,omitempty"`
	BaseURL string `json:"base_url,omitempty"` // Advanced override; default DefaultAPIBaseURL
}

// EffectiveAPIBaseURL returns the configured base URL or the production default.
func EffectiveAPIBaseURL(api *APISettings) string {
	if api == nil {
		return DefaultAPIBaseURL
	}
	u := strings.TrimSpace(api.BaseURL)
	if u == "" {
		return DefaultAPIBaseURL
	}
	return strings.TrimRight(u, "/")
}

// APIConfigured reports whether the bridge should run the HTTPS client.
func APIConfigured(api *APISettings) bool {
	return api != nil && api.Enabled && strings.TrimSpace(api.Key) != ""
}

// APIKeyHint returns a truncated display form for a stored key (awxb_...xxxx).
func APIKeyHint(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 12 {
		return APIKeyPrefix + "..."
	}
	return key[:8] + "..." + key[len(key)-4:]
}

// ValidAPIKeyShape reports whether key matches core generate-bridge-api-key.php output.
func ValidAPIKeyShape(key string) bool {
	if !strings.HasPrefix(key, APIKeyPrefix) {
		return false
	}
	secret := key[len(APIKeyPrefix):]
	if len(secret) != APIKeySecretLength {
		return false
	}
	for _, r := range secret {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
		if r > unicode.MaxASCII {
			return false
		}
	}
	return true
}

// ValidateAPISettings checks api link settings. nil is valid (link unused).
func ValidateAPISettings(api *APISettings) error {
	if api == nil || !api.Enabled {
		return nil
	}
	key := strings.TrimSpace(api.Key)
	if key == "" {
		return fmt.Errorf("api.key is required when api.enabled is true")
	}
	if !ValidAPIKeyShape(key) {
		return fmt.Errorf("api.key must be awxb_ plus %d alphanumeric characters", APIKeySecretLength)
	}
	base := strings.TrimSpace(api.BaseURL)
	if base == "" {
		return nil
	}
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("api.base_url: %w", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("api.base_url must use https")
	}
	if u.Host == "" {
		return fmt.Errorf("api.base_url host is required")
	}
	return nil
}
