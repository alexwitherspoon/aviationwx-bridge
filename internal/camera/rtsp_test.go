package camera

import (
	"context"
	"os/exec"
	"testing"
)

func TestNewRTSPCamera(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				ID:   "test-camera",
				Type: "rtsp",
				RTSP: &RTSPConfig{
					URL: "rtsp://192.168.1.100:554/stream1",
				},
			},
			wantErr: false,
		},
		{
			name: "missing rtsp config",
			config: Config{
				ID:   "test-camera",
				Type: "rtsp",
			},
			wantErr: true,
		},
		{
			name: "missing URL",
			config: Config{
				ID:   "test-camera",
				Type: "rtsp",
				RTSP: &RTSPConfig{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cam, err := NewRTSPCamera(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewRTSPCamera() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cam == nil {
				t.Error("NewRTSPCamera() returned nil camera")
			}
		})
	}
}

func TestRTSPCamera_ID(t *testing.T) {
	config := Config{
		ID:   "test-camera-123",
		Type: "rtsp",
		RTSP: &RTSPConfig{
			URL: "rtsp://192.168.1.100:554/stream1",
		},
	}

	cam, err := NewRTSPCamera(config)
	if err != nil {
		t.Fatalf("NewRTSPCamera() error = %v", err)
	}

	if cam.ID() != "test-camera-123" {
		t.Errorf("ID() = %s, want 'test-camera-123'", cam.ID())
	}
}

func TestRTSPCamera_Type(t *testing.T) {
	config := Config{
		ID:   "test-camera",
		Type: "rtsp",
		RTSP: &RTSPConfig{
			URL: "rtsp://192.168.1.100:554/stream1",
		},
	}

	cam, err := NewRTSPCamera(config)
	if err != nil {
		t.Fatalf("NewRTSPCamera() error = %v", err)
	}

	if cam.Type() != "rtsp" {
		t.Errorf("Type() = %s, want 'rtsp'", cam.Type())
	}
}

func TestRTSPCamera_modifyURLForSubstream(t *testing.T) {
	config := Config{
		ID:   "test-camera",
		Type: "rtsp",
		RTSP: &RTSPConfig{
			URL:       "rtsp://192.168.1.100:554/stream1",
			Substream: true,
		},
	}

	cam, err := NewRTSPCamera(config)
	if err != nil {
		t.Fatalf("NewRTSPCamera() error = %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "stream1 to stream2",
			input:    "rtsp://192.168.1.100/stream1",
			expected: "rtsp://192.168.1.100/stream2",
		},
		{
			name:     "main to sub",
			input:    "rtsp://192.168.1.100/main",
			expected: "rtsp://192.168.1.100/sub",
		},
		{
			name:     "no pattern match",
			input:    "rtsp://192.168.1.100/custom",
			expected: "rtsp://192.168.1.100/custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cam.modifyURLForSubstream(tt.input)
			if result != tt.expected {
				t.Errorf("modifyURLForSubstream() = %s, want %s", result, tt.expected)
			}
		})
	}
}

// Note: Integration tests for RTSP Capture would require:
// 1. ffmpeg installed in test environment
// 2. Real RTSP camera or RTSP test server
// These are left for integration testing phase.
// Unit test for Capture would need to mock exec.CommandContext which is complex.
// For now, we test the structure and helper functions.

func TestRTSPCamera_Capture_FFmpegNotAvailable(t *testing.T) {
	// Test that we get a reasonable error when ffmpeg is not available
	// This is a best-effort test - actual behavior depends on system

	// Check if ffmpeg is available
	_, err := exec.LookPath("ffmpeg")
	if err != nil {
		// ffmpeg not available, test that we handle it gracefully
		config := Config{
			ID:   "test-camera",
			Type: "rtsp",
			RTSP: &RTSPConfig{
				URL: "rtsp://192.168.1.100:554/stream1",
			},
		}

		cam, err := NewRTSPCamera(config)
		if err != nil {
			t.Fatalf("NewRTSPCamera() error = %v", err)
		}

		ctx := context.Background()
		_, err = cam.Capture(ctx)

		// Should get an error (either from ffmpeg not found or connection failure)
		if err == nil {
			t.Error("Capture() expected error when ffmpeg not available")
		}
	} else {
		t.Skip("ffmpeg is available - skipping ffmpeg not available test")
	}
}
