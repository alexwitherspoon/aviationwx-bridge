package camera

import (
	"testing"
)

func TestNewCamera(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		wantErr  bool
		wantType string
	}{
		{
			name: "HTTP camera",
			config: Config{
				ID:          "http-cam",
				Type:        "http",
				SnapshotURL: "http://192.168.1.100/snapshot.jpg",
			},
			wantErr:  false,
			wantType: "http",
		},
		{
			name: "ONVIF camera",
			config: Config{
				ID:   "onvif-cam",
				Type: "onvif",
				ONVIF: &ONVIFConfig{
					Endpoint: "http://192.168.1.100/onvif/device_service",
					Username: "admin",
					Password: "secret",
				},
			},
			wantErr:  false,
			wantType: "onvif",
		},
		{
			name: "RTSP camera",
			config: Config{
				ID:   "rtsp-cam",
				Type: "rtsp",
				RTSP: &RTSPConfig{
					URL: "rtsp://192.168.1.100:554/stream1",
				},
			},
			wantErr:  false,
			wantType: "rtsp",
		},
		{
			name: "unsupported type",
			config: Config{
				ID:   "unknown-cam",
				Type: "unknown",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cam, err := NewCamera(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewCamera() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if cam == nil {
					t.Error("NewCamera() returned nil camera")
					return
				}
				if cam.Type() != tt.wantType {
					t.Errorf("Type() = %s, want %s", cam.Type(), tt.wantType)
				}
				if cam.ID() != tt.config.ID {
					t.Errorf("ID() = %s, want %s", cam.ID(), tt.config.ID)
				}
			}
		})
	}
}
