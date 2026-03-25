package config

import (
	"testing"
)

func TestNormalizeUploadPort(t *testing.T) {
	if got := NormalizeUploadPort(0); got != 2222 {
		t.Errorf("NormalizeUploadPort(0) = %d, want 2222", got)
	}
	if got := NormalizeUploadPort(2222); got != 2222 {
		t.Errorf("NormalizeUploadPort(2222) = %d, want 2222", got)
	}
}

func TestUploadIdentityKey(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     int
		user     string
		expected string
	}{
		{
			name:     "typical",
			host:     "upload.aviationwx.org",
			port:     2222,
			user:     "cam-a",
			expected: "upload.aviationwx.org:2222:cam-a",
		},
		{
			name:     "host_trimmed_casefolded",
			host:     "  Upload.AVIATIONWX.org ",
			port:     2222,
			user:     "cam-a",
			expected: "upload.aviationwx.org:2222:cam-a",
		},
		{
			name:     "zero_port_defaults",
			host:     "h.example.com",
			port:     0,
			user:     "u",
			expected: "h.example.com:2222:u",
		},
		{
			name:     "empty_username",
			host:     "h.example.com",
			port:     2222,
			user:     "  ",
			expected: "",
		},
		{
			name:     "empty_host",
			host:     "",
			port:     2222,
			user:     "u",
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := UploadIdentityKey(tt.host, tt.port, tt.user)
			if got != tt.expected {
				t.Fatalf("UploadIdentityKey(%q, %d, %q) = %q, want %q", tt.host, tt.port, tt.user, got, tt.expected)
			}
		})
	}
}
