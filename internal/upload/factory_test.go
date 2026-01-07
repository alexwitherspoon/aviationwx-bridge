package upload

import (
	"testing"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

func TestNewClientFromConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     config.Upload
		wantErr bool
	}{
		{
			name: "valid config",
			cfg: config.Upload{
				Host:     "upload.aviationwx.org",
				Port:     21,
				Username: "testuser",
				Password: "testpass",
			},
			wantErr: false,
		},
		{
			name: "config with defaults",
			cfg: config.Upload{
				Host:     "upload.aviationwx.org",
				Username: "testuser",
				Password: "testpass",
			},
			wantErr: false,
		},
		{
			name: "missing host",
			cfg: config.Upload{
				Username: "testuser",
				Password: "testpass",
			},
			wantErr: true,
		},
		{
			name: "missing username",
			cfg: config.Upload{
				Host:     "upload.aviationwx.org",
				Password: "testpass",
			},
			wantErr: true,
		},
		{
			name: "missing password",
			cfg: config.Upload{
				Host:     "upload.aviationwx.org",
				Username: "testuser",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClientFromConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewClientFromConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && client == nil {
				t.Error("NewClientFromConfig() returned nil client")
			}
		})
	}
}
