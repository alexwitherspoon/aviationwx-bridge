package camera

import "testing"

func TestNormalizeONVIFEndpoint(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"spaces", "   ", ""},
		{"single_http", "http://192.168.1.10/onvif/device_service", "http://192.168.1.10/onvif/device_service"},
		{"single_https", "https://192.168.1.10/onvif/device_service", "https://192.168.1.10/onvif/device_service"},
		{"double_http", "http://http://192.168.1.10/onvif/device_service", "http://192.168.1.10/onvif/device_service"},
		{"double_https", "https://https://192.168.1.10/onvif/device_service", "https://192.168.1.10/onvif/device_service"},
		{"mixed_http_then_https", "http://https://192.168.1.10/path", "http://192.168.1.10/path"},
		{"mixed_https_then_http", "https://http://192.168.1.10/path", "https://192.168.1.10/path"},
		{"triple_http", "http://http://http://192.168.1.1/x", "http://192.168.1.1/x"},
		{"case_insensitive_HTTP", "HTTP://HTTP://192.168.1.1/x", "http://192.168.1.1/x"},
		{"leading_slash_after_strip", "http:////192.168.1.1/x", "http://192.168.1.1/x"},
		{"host_only_adds_http", "192.168.1.10/onvif/device_service", "http://192.168.1.10/onvif/device_service"},
		{"https_host_only", "https://cam.local/path", "https://cam.local/path"},
		{"scheme_http_only_empty", "http://", ""},
		{"scheme_https_only_empty", "https://", ""},
		{"trim_inner_spaces", "  http://192.168.1.1/x  ", "http://192.168.1.1/x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeONVIFEndpoint(tt.in)
			if got != tt.want {
				t.Fatalf("NormalizeONVIFEndpoint(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
