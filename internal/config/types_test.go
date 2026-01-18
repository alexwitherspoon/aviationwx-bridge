package config

import (
	"testing"
)

func TestConfig_IsFirstRun(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected bool
	}{
		{
			name:     "empty cameras is first run",
			config:   Config{Cameras: []Camera{}},
			expected: true,
		},
		{
			name: "with cameras is not first run",
			config: Config{
				Cameras: []Camera{{ID: "test", Type: "http"}},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.IsFirstRun(); got != tt.expected {
				t.Errorf("IsFirstRun() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConfig_GetWebPassword(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{
			name:     "nil web console returns default",
			config:   Config{},
			expected: "aviationwx",
		},
		{
			name: "empty password returns default",
			config: Config{
				WebConsole: &WebConsole{},
			},
			expected: "aviationwx",
		},
		{
			name: "custom password is returned",
			config: Config{
				WebConsole: &WebConsole{Password: "custom123"},
			},
			expected: "custom123",
		},
		{
			name: "deprecated basic auth fallback",
			config: Config{
				WebConsole: &WebConsole{
					BasicAuth: &BasicAuth{Password: "legacy"},
				},
			},
			expected: "legacy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetWebPassword(); got != tt.expected {
				t.Errorf("GetWebPassword() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConfig_GetWebPort(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected int
	}{
		{
			name:     "nil web console returns default",
			config:   Config{},
			expected: 1229,
		},
		{
			name: "zero port returns default",
			config: Config{
				WebConsole: &WebConsole{Port: 0},
			},
			expected: 1229,
		},
		{
			name: "custom port is returned",
			config: Config{
				WebConsole: &WebConsole{Port: 8080},
			},
			expected: 8080,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetWebPort(); got != tt.expected {
				t.Errorf("GetWebPort() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestImageProcessing_NeedsProcessing(t *testing.T) {
	tests := []struct {
		name     string
		config   *ImageProcessing
		expected bool
	}{
		{
			name:     "nil returns false",
			config:   nil,
			expected: false,
		},
		{
			name:     "zero values returns false",
			config:   &ImageProcessing{},
			expected: false,
		},
		{
			name:     "max width set returns true",
			config:   &ImageProcessing{MaxWidth: 1920},
			expected: true,
		},
		{
			name:     "max height set returns true",
			config:   &ImageProcessing{MaxHeight: 1080},
			expected: true,
		},
		{
			name:     "quality set returns true",
			config:   &ImageProcessing{Quality: 85},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.NeedsProcessing(); got != tt.expected {
				t.Errorf("NeedsProcessing() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestImageProcessing_GetQuality(t *testing.T) {
	tests := []struct {
		name     string
		config   *ImageProcessing
		expected int
	}{
		{
			name:     "nil returns 0",
			config:   nil,
			expected: 0,
		},
		{
			name:     "negative returns 0",
			config:   &ImageProcessing{Quality: -10},
			expected: 0,
		},
		{
			name:     "over 100 returns 100",
			config:   &ImageProcessing{Quality: 150},
			expected: 100,
		},
		{
			name:     "valid quality returned",
			config:   &ImageProcessing{Quality: 85},
			expected: 85,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.GetQuality(); got != tt.expected {
				t.Errorf("GetQuality() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultUpload(t *testing.T) {
	upload := DefaultUpload()

	if upload.Host != "upload.aviationwx.org" {
		t.Errorf("Host = %v, want upload.aviationwx.org", upload.Host)
	}
	if upload.Port != 2121 {
		t.Errorf("Port = %v, want 2121", upload.Port)
	}
	if !upload.TLS {
		t.Error("TLS should be true")
	}
	if !upload.TLSVerify {
		t.Error("TLSVerify should be true")
	}
}

func TestDefaultWebConsole(t *testing.T) {
	wc := DefaultWebConsole()

	if !wc.Enabled {
		t.Error("Enabled should be true")
	}
	if wc.Port != 1229 {
		t.Errorf("Port = %v, want 1229", wc.Port)
	}
	if wc.Password != "aviationwx" {
		t.Errorf("Password = %v, want aviationwx", wc.Password)
	}
}
