package upload

import (
	"sync"
	"testing"
)

func TestNewSFTPClient(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: Config{
				Host:     "sftp.example.com",
				Port:     22,
				Username: "testuser",
				Password: "testpass",
			},
			wantErr: false,
		},
		{
			name: "default port",
			config: Config{
				Host:     "sftp.example.com",
				Username: "testuser",
				Password: "testpass",
			},
			wantErr: false,
		},
		{
			name: "missing host",
			config: Config{
				Port:     22,
				Username: "testuser",
				Password: "testpass",
			},
			wantErr: true,
		},
		{
			name: "missing username",
			config: Config{
				Host:     "sftp.example.com",
				Port:     22,
				Password: "testpass",
			},
			wantErr: true,
		},
		{
			name: "missing password",
			config: Config{
				Host:     "sftp.example.com",
				Port:     22,
				Username: "testuser",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewSFTPClient(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewSFTPClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && client == nil {
				t.Error("NewSFTPClient() returned nil client without error")
			}
			if !tt.wantErr && client.config.Port != 2222 && tt.config.Port == 0 {
				t.Errorf("NewSFTPClient() port = %d, want 2222 (default)", client.config.Port)
			}
		})
	}
}

func TestNewSFTPClient_WithBasePath(t *testing.T) {
	tests := []struct {
		name         string
		config       Config
		wantBasePath string
	}{
		{
			name: "with base path",
			config: Config{
				Host:     "sftp.example.com",
				Port:     22,
				Username: "testuser",
				Password: "testpass",
				BasePath: "/files",
			},
			wantBasePath: "/files",
		},
		{
			name: "with custom base path",
			config: Config{
				Host:     "sftp.example.com",
				Port:     22,
				Username: "testuser",
				Password: "testpass",
				BasePath: "/custom/upload/dir",
			},
			wantBasePath: "/custom/upload/dir",
		},
		{
			name: "without base path",
			config: Config{
				Host:     "sftp.example.com",
				Port:     22,
				Username: "testuser",
				Password: "testpass",
			},
			wantBasePath: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewSFTPClient(tt.config)
			if err != nil {
				t.Errorf("NewSFTPClient() error = %v", err)
				return
			}
			if client.config.BasePath != tt.wantBasePath {
				t.Errorf("NewSFTPClient() BasePath = %v, want %v", client.config.BasePath, tt.wantBasePath)
			}
		})
	}
}

func TestSFTPClient_ConcurrentUploads(t *testing.T) {
	// Concurrent Upload calls each dial independently; must not race on client state.
	// This test verifies that many goroutines calling Upload (which will fail to connect)
	// complete without panic or data race. Run with: go test -race
	// Use .invalid TLD (RFC 6761) - reserved for invalid names, never resolves.
	// Short timeout so connection attempts fail quickly.
	client, err := NewSFTPClient(Config{
		Host:                  "test.invalid",
		Port:                  22,
		Username:              "test",
		Password:              "test",
		TimeoutConnectSeconds: 1,
	})
	if err != nil {
		t.Fatalf("NewSFTPClient: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.Upload("test/path.jpg", []byte("data"))
		}()
	}
	wg.Wait()
}

func TestSFTPClient_Close(t *testing.T) {
	client, err := NewSFTPClient(Config{
		Host:     "sftp.example.com",
		Port:     22,
		Username: "testuser",
		Password: "testpass",
	})
	if err != nil {
		t.Fatalf("NewSFTPClient: %v", err)
	}

	// Close without connection should not panic
	err = client.Close()
	if err != nil {
		t.Errorf("Close() without connection returned error: %v", err)
	}
}
