package update

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

func TestUploadEndpointsForRoster_defaultWhenNoCameras(t *testing.T) {
	eps := UploadEndpointsForRoster(nil)
	if len(eps) != 1 {
		t.Fatalf("len = %d, want 1", len(eps))
	}
	if eps[0] != DefaultUploadEndpoint() {
		t.Fatalf("endpoint = %+v, want %+v", eps[0], DefaultUploadEndpoint())
	}
}

func TestUploadEndpointsForRoster_defaultWhenAllDisabled(t *testing.T) {
	cameras := []config.Camera{
		{
			ID:      "cam-a",
			Enabled: false,
			Upload:  &config.Upload{Host: "upload.example.test", Port: 2222},
		},
	}
	eps := UploadEndpointsForRoster(cameras)
	if len(eps) != 1 {
		t.Fatalf("len = %d, want 1", len(eps))
	}
	if eps[0].Host != "upload.aviationwx.org" || eps[0].Port != 2222 {
		t.Fatalf("endpoint = %+v", eps[0])
	}
}

func TestUploadEndpointsForRoster_usesEnabledCameraUpload(t *testing.T) {
	cameras := []config.Camera{
		{
			ID:      "cam-a",
			Enabled: true,
			Upload:  &config.Upload{Host: "upload.e2e.test", Port: 2222},
		},
		{
			ID:      "cam-b",
			Enabled: false,
			Upload:  &config.Upload{Host: "upload.other.test", Port: 2222},
		},
	}
	eps := UploadEndpointsForRoster(cameras)
	if len(eps) != 1 {
		t.Fatalf("len = %d, want 1", len(eps))
	}
	if eps[0].Host != "upload.e2e.test" || eps[0].Port != 2222 {
		t.Fatalf("endpoint = %+v", eps[0])
	}
}

func TestUploadEndpointsForRoster_deduplicatesHosts(t *testing.T) {
	cameras := []config.Camera{
		{ID: "a", Enabled: true, Upload: &config.Upload{Host: "upload.e2e.test", Port: 2222}},
		{ID: "b", Enabled: true, Upload: &config.Upload{Host: "UPLOAD.E2E.TEST", Port: 2222}},
	}
	eps := UploadEndpointsForRoster(cameras)
	if len(eps) != 1 {
		t.Fatalf("len = %d, want 1", len(eps))
	}
}

func TestSyncUploadSSHHostKeysForCameras_syncsDefaultWhenNoCameras(t *testing.T) {
	body := `{"version":1,"hostname":"upload.aviationwx.org","port":2222,"sha256":["SHA256:abc"],"updated_at":"2026-06-26T00:00:00Z"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	old := uploadHostKeysHTTPClient
	uploadHostKeysHTTPClient = &http.Client{
		Transport: &rewriteHostTransport{base: server.URL, inner: http.DefaultTransport},
	}
	defer func() { uploadHostKeysHTTPClient = old }()

	dir := t.TempDir()
	if err := SyncUploadSSHHostKeysForCameras(dir, nil); err != nil {
		t.Fatalf("sync: %v", err)
	}
	trusted, err := LoadTrustedUploadHostKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(trusted) != 1 || trusted[0] != "SHA256:abc" {
		t.Fatalf("trusted = %v", trusted)
	}
}
