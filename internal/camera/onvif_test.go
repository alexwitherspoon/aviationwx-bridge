package camera

import (
	"testing"
)

func TestNewONVIFCamera(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				ID:   "test-camera",
				Type: "onvif",
				ONVIF: &ONVIFConfig{
					Endpoint: "http://192.168.1.100/onvif/device_service",
					Username: "admin",
					Password: "secret",
				},
			},
			wantErr: false,
		},
		{
			name: "missing onvif config",
			config: Config{
				ID:   "test-camera",
				Type: "onvif",
			},
			wantErr: true,
		},
		{
			name: "missing endpoint",
			config: Config{
				ID:   "test-camera",
				Type: "onvif",
				ONVIF: &ONVIFConfig{
					Username: "admin",
					Password: "secret",
				},
			},
			wantErr: true,
		},
		{
			name: "missing username",
			config: Config{
				ID:   "test-camera",
				Type: "onvif",
				ONVIF: &ONVIFConfig{
					Endpoint: "http://192.168.1.100/onvif/device_service",
					Password: "secret",
				},
			},
			wantErr: true,
		},
		{
			name: "missing password",
			config: Config{
				ID:   "test-camera",
				Type: "onvif",
				ONVIF: &ONVIFConfig{
					Endpoint: "http://192.168.1.100/onvif/device_service",
					Username: "admin",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cam, err := NewONVIFCamera(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewONVIFCamera() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cam == nil {
				t.Error("NewONVIFCamera() returned nil camera")
			}
		})
	}
}

func TestONVIFCamera_ID(t *testing.T) {
	config := Config{
		ID:   "test-camera-123",
		Type: "onvif",
		ONVIF: &ONVIFConfig{
			Endpoint: "http://192.168.1.100/onvif/device_service",
			Username: "admin",
			Password: "secret",
		},
	}

	cam, err := NewONVIFCamera(config)
	if err != nil {
		t.Fatalf("NewONVIFCamera() error = %v", err)
	}

	if cam.ID() != "test-camera-123" {
		t.Errorf("ID() = %s, want 'test-camera-123'", cam.ID())
	}
}

func TestONVIFCamera_Type(t *testing.T) {
	config := Config{
		ID:   "test-camera",
		Type: "onvif",
		ONVIF: &ONVIFConfig{
			Endpoint: "http://192.168.1.100/onvif/device_service",
			Username: "admin",
			Password: "secret",
		},
	}

	cam, err := NewONVIFCamera(config)
	if err != nil {
		t.Fatalf("NewONVIFCamera() error = %v", err)
	}

	if cam.Type() != "onvif" {
		t.Errorf("Type() = %s, want 'onvif'", cam.Type())
	}
}

// TestNewONVIFCamera_NormalizesDuplicateSchemeEndpoint verifies duplicate http(s):// prefixes
// are collapsed on the camera's stored config (regression for pasted URLs).
func TestNewONVIFCamera_NormalizesDuplicateSchemeEndpoint(t *testing.T) {
	raw := "http://http://192.168.1.100/onvif/device_service"
	want := "http://192.168.1.100/onvif/device_service"
	cfg := Config{
		ID:   "test-camera",
		Type: "onvif",
		ONVIF: &ONVIFConfig{
			Endpoint: raw,
			Username: "admin",
			Password: "secret",
		},
	}
	cam, err := NewONVIFCamera(cfg)
	if err != nil {
		t.Fatalf("NewONVIFCamera: %v", err)
	}
	if cam.config.ONVIF.Endpoint != want {
		t.Errorf("stored endpoint = %q, want %q", cam.config.ONVIF.Endpoint, want)
	}
	if cfg.ONVIF.Endpoint != raw {
		t.Errorf("caller ONVIFConfig should not be mutated: got %q", cfg.ONVIF.Endpoint)
	}
}

// Note: Integration tests for ONVIF Capture would require a real ONVIF camera
// or a mock ONVIF server. These are left for integration testing phase.
