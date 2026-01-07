package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

func newTestConfig() *config.Config {
	return &config.Config{
		Version:  2,
		Timezone: "America/New_York",
		Cameras: []config.Camera{
			{
				ID:                     "test-cam",
				Name:                   "Test Camera",
				Type:                   "http",
				Enabled:                true,
				SnapshotURL:            "http://example.com/snapshot.jpg",
				CaptureIntervalSeconds: 60,
				Upload: &config.Upload{
					Host:     "upload.aviationwx.org",
					Port:     21,
					Username: "testuser",
					Password: "testpass",
					TLS:      true,
				},
			},
		},
		WebConsole: &config.WebConsole{
			Enabled:  true,
			Port:     1229,
			Password: "testpassword",
		},
	}
}

func newTestServer(cfg *config.Config) *Server {
	if cfg == nil {
		cfg = newTestConfig()
	}
	return NewServer(ServerConfig{
		Config:     cfg,
		ConfigPath: "/tmp/test-config.json",
	})
}

func setBasicAuth(req *http.Request, password string) {
	req.SetBasicAuth("admin", password)
}

func TestNewServer(t *testing.T) {
	cfg := newTestConfig()
	server := NewServer(ServerConfig{
		Config:     cfg,
		ConfigPath: "/tmp/test.json",
	})

	if server == nil {
		t.Fatal("NewServer returned nil")
	}
	if server.config != cfg {
		t.Error("config not set")
	}
	if server.mux == nil {
		t.Error("mux not initialized")
	}
}

func TestHealthz(t *testing.T) {
	server := newTestServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	server.handleHealthz(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("expected status 'ok', got %v", response["status"])
	}

	if _, ok := response["time"]; !ok {
		t.Error("expected 'time' in response")
	}
}

func TestAuthMiddleware_NoAuth(t *testing.T) {
	server := newTestServer(nil)

	handler := server.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("success"))
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}

	if !strings.Contains(rr.Header().Get("WWW-Authenticate"), "Basic") {
		t.Error("expected WWW-Authenticate header")
	}
}

func TestAuthMiddleware_WrongPassword(t *testing.T) {
	server := newTestServer(nil)

	handler := server.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("success"))
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	setBasicAuth(req, "wrongpassword")
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_CorrectPassword(t *testing.T) {
	server := newTestServer(nil)

	handler := server.authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	setBasicAuth(req, "testpassword")
	rr := httptest.NewRecorder()

	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}
}

func TestHandleStatus(t *testing.T) {
	server := newTestServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()

	server.handleStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["timezone"] != "America/New_York" {
		t.Errorf("expected timezone 'America/New_York', got %v", response["timezone"])
	}

	if response["camera_count"].(float64) != 1 {
		t.Errorf("expected camera_count 1, got %v", response["camera_count"])
	}
}

func TestHandleStatus_MethodNotAllowed(t *testing.T) {
	server := newTestServer(nil)

	req := httptest.NewRequest(http.MethodPost, "/api/status", nil)
	rr := httptest.NewRecorder()

	server.handleStatus(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rr.Code)
	}
}

func TestHandleStatus_WithOrchestratorCallback(t *testing.T) {
	cfg := newTestConfig()
	server := NewServer(ServerConfig{
		Config:     cfg,
		ConfigPath: "/tmp/test.json",
		GetStatus: func() interface{} {
			return map[string]string{"state": "running"}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	rr := httptest.NewRecorder()

	server.handleStatus(rr, req)

	var response map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&response)

	if _, ok := response["orchestrator"]; !ok {
		t.Error("expected 'orchestrator' in response")
	}
}

func TestGetConfig(t *testing.T) {
	server := newTestServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rr := httptest.NewRecorder()

	server.handleConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Passwords should be masked
	if wc, ok := response["web_console"].(map[string]interface{}); ok {
		if wc["password"] != "********" {
			t.Error("web console password should be masked")
		}
	}
}

func TestPutConfig(t *testing.T) {
	configChanged := false
	cfg := newTestConfig()
	server := NewServer(ServerConfig{
		Config:     cfg,
		ConfigPath: "/tmp/test.json",
		OnConfigChange: func(c *config.Config) error {
			configChanged = true
			return nil
		},
	})

	newConfig := &config.Config{
		Version:  2,
		Timezone: "America/Los_Angeles",
		Cameras: []config.Camera{
			{
				ID:   "new-cam",
				Name: "New Camera",
				Type: "http",
				Upload: &config.Upload{
					Username: "user",
					Password: "pass",
				},
			},
		},
	}

	body, _ := json.Marshal(newConfig)
	req := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleConfig(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if !configChanged {
		t.Error("config change callback not called")
	}

	if server.config.Timezone != "America/Los_Angeles" {
		t.Errorf("timezone not updated, got %s", server.config.Timezone)
	}
}

func TestPutConfig_InvalidJSON(t *testing.T) {
	server := newTestServer(nil)

	req := httptest.NewRequest(http.MethodPut, "/api/config", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleConfig(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}
}

func TestListCameras(t *testing.T) {
	server := newTestServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/cameras", nil)
	rr := httptest.NewRecorder()

	server.handleCameras(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var cameras []map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&cameras); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(cameras) != 1 {
		t.Errorf("expected 1 camera, got %d", len(cameras))
	}

	if cameras[0]["id"] != "test-cam" {
		t.Errorf("expected camera id 'test-cam', got %v", cameras[0]["id"])
	}

	// Check password is masked
	if upload, ok := cameras[0]["upload"].(map[string]interface{}); ok {
		if upload["password"] != "********" {
			t.Error("upload password should be masked")
		}
	}
}

func TestAddCamera(t *testing.T) {
	configChanged := false
	cfg := newTestConfig()
	server := NewServer(ServerConfig{
		Config:     cfg,
		ConfigPath: "/tmp/test.json",
		OnConfigChange: func(c *config.Config) error {
			configChanged = true
			return nil
		},
	})

	newCam := config.Camera{
		ID:          "new-camera",
		Name:        "New Camera",
		Type:        "http",
		SnapshotURL: "http://example.com/new.jpg",
		Upload: &config.Upload{
			Username: "user",
			Password: "pass",
		},
	}

	body, _ := json.Marshal(newCam)
	req := httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCameras(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", rr.Code, rr.Body.String())
	}

	if !configChanged {
		t.Error("config change callback not called")
	}

	if len(server.config.Cameras) != 2 {
		t.Errorf("expected 2 cameras, got %d", len(server.config.Cameras))
	}
}

func TestAddCamera_DuplicateID(t *testing.T) {
	server := newTestServer(nil)

	// Try to add camera with same ID as existing
	newCam := config.Camera{
		ID:   "test-cam", // Same as existing
		Name: "Duplicate Camera",
		Type: "http",
		Upload: &config.Upload{
			Username: "user",
			Password: "pass",
		},
	}

	body, _ := json.Marshal(newCam)
	req := httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCameras(rr, req)

	if rr.Code != http.StatusConflict {
		t.Errorf("expected status 409, got %d", rr.Code)
	}
}

func TestAddCamera_MissingID(t *testing.T) {
	server := newTestServer(nil)

	newCam := config.Camera{
		Name: "No ID Camera",
		Type: "http",
	}

	body, _ := json.Marshal(newCam)
	req := httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCameras(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}
}

func TestAddCamera_MissingUpload(t *testing.T) {
	server := newTestServer(nil)

	newCam := config.Camera{
		ID:   "no-upload-cam",
		Name: "No Upload Camera",
		Type: "http",
	}

	body, _ := json.Marshal(newCam)
	req := httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCameras(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}
}

func TestGetCamera(t *testing.T) {
	server := newTestServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/cameras/test-cam", nil)
	rr := httptest.NewRecorder()

	server.handleCamera(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var camera map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&camera); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if camera["id"] != "test-cam" {
		t.Errorf("expected camera id 'test-cam', got %v", camera["id"])
	}
}

func TestGetCamera_NotFound(t *testing.T) {
	server := newTestServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/cameras/nonexistent", nil)
	rr := httptest.NewRecorder()

	server.handleCamera(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rr.Code)
	}
}

func TestUpdateCamera(t *testing.T) {
	configChanged := false
	cfg := newTestConfig()
	server := NewServer(ServerConfig{
		Config:     cfg,
		ConfigPath: "/tmp/test.json",
		OnConfigChange: func(c *config.Config) error {
			configChanged = true
			return nil
		},
	})

	update := config.Camera{
		Name:                   "Updated Camera",
		Type:                   "http",
		CaptureIntervalSeconds: 120,
		Upload: &config.Upload{
			Username: "newuser",
			Password: "newpass",
		},
	}

	body, _ := json.Marshal(update)
	req := httptest.NewRequest(http.MethodPut, "/api/cameras/test-cam", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleCamera(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if !configChanged {
		t.Error("config change callback not called")
	}

	// Verify update
	if server.config.Cameras[0].Name != "Updated Camera" {
		t.Errorf("camera name not updated, got %s", server.config.Cameras[0].Name)
	}
	if server.config.Cameras[0].CaptureIntervalSeconds != 120 {
		t.Errorf("capture interval not updated, got %d", server.config.Cameras[0].CaptureIntervalSeconds)
	}
}

func TestDeleteCamera(t *testing.T) {
	configChanged := false
	cfg := newTestConfig()
	server := NewServer(ServerConfig{
		Config:     cfg,
		ConfigPath: "/tmp/test.json",
		OnConfigChange: func(c *config.Config) error {
			configChanged = true
			return nil
		},
	})

	req := httptest.NewRequest(http.MethodDelete, "/api/cameras/test-cam", nil)
	rr := httptest.NewRecorder()

	server.handleCamera(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected status 204, got %d", rr.Code)
	}

	if !configChanged {
		t.Error("config change callback not called")
	}

	if len(server.config.Cameras) != 0 {
		t.Errorf("expected 0 cameras after delete, got %d", len(server.config.Cameras))
	}
}

func TestDeleteCamera_NotFound(t *testing.T) {
	server := newTestServer(nil)

	req := httptest.NewRequest(http.MethodDelete, "/api/cameras/nonexistent", nil)
	rr := httptest.NewRecorder()

	server.handleCamera(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rr.Code)
	}
}

func TestGetTime(t *testing.T) {
	server := newTestServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/time", nil)
	rr := httptest.NewRecorder()

	server.handleTime(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var response map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if _, ok := response["utc"]; !ok {
		t.Error("expected 'utc' in response")
	}
	if _, ok := response["local"]; !ok {
		t.Error("expected 'local' in response")
	}
	if _, ok := response["timezone"]; !ok {
		t.Error("expected 'timezone' in response")
	}
	if _, ok := response["utc_offset"]; !ok {
		t.Error("expected 'utc_offset' in response")
	}
}

func TestSetTimezone(t *testing.T) {
	configChanged := false
	cfg := newTestConfig()
	server := NewServer(ServerConfig{
		Config:     cfg,
		ConfigPath: "/tmp/test.json",
		OnConfigChange: func(c *config.Config) error {
			configChanged = true
			return nil
		},
	})

	body := `{"timezone": "Europe/London"}`
	req := httptest.NewRequest(http.MethodPut, "/api/time", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleTime(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if !configChanged {
		t.Error("config change callback not called")
	}

	if server.config.Timezone != "Europe/London" {
		t.Errorf("timezone not updated, got %s", server.config.Timezone)
	}
}

func TestSetTimezone_Invalid(t *testing.T) {
	server := newTestServer(nil)

	body := `{"timezone": "Invalid/Timezone"}`
	req := httptest.NewRequest(http.MethodPut, "/api/time", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleTime(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", rr.Code)
	}
}

func TestTestCamera(t *testing.T) {
	server := NewServer(ServerConfig{
		Config:     newTestConfig(),
		ConfigPath: "/tmp/test.json",
		TestCamera: func(cam config.Camera) ([]byte, error) {
			return []byte("fake image data"), nil
		},
	})

	cam := config.Camera{
		ID:          "test",
		Type:        "http",
		SnapshotURL: "http://example.com/test.jpg",
	}

	body, _ := json.Marshal(cam)
	req := httptest.NewRequest(http.MethodPost, "/api/test/camera", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleTestCamera(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var response map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&response)

	if response["success"] != true {
		t.Error("expected success true")
	}
}

func TestTestCamera_NotAvailable(t *testing.T) {
	server := newTestServer(nil) // No TestCamera callback

	cam := config.Camera{ID: "test"}
	body, _ := json.Marshal(cam)
	req := httptest.NewRequest(http.MethodPost, "/api/test/camera", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleTestCamera(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rr.Code)
	}
}

func TestTestCamera_MethodNotAllowed(t *testing.T) {
	server := newTestServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/test/camera", nil)
	rr := httptest.NewRecorder()

	server.handleTestCamera(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", rr.Code)
	}
}

func TestTestUpload(t *testing.T) {
	server := NewServer(ServerConfig{
		Config:     newTestConfig(),
		ConfigPath: "/tmp/test.json",
		TestUpload: func(upload config.Upload) error {
			return nil
		},
	})

	upload := config.Upload{
		Host:     "ftp.example.com",
		Port:     21,
		Username: "user",
		Password: "pass",
	}

	body, _ := json.Marshal(upload)
	req := httptest.NewRequest(http.MethodPost, "/api/test/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleTestUpload(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	var response map[string]interface{}
	json.NewDecoder(rr.Body).Decode(&response)

	if response["success"] != true {
		t.Error("expected success true")
	}
}

func TestTestUpload_NotAvailable(t *testing.T) {
	server := newTestServer(nil) // No TestUpload callback

	upload := config.Upload{}
	body, _ := json.Marshal(upload)
	req := httptest.NewRequest(http.MethodPost, "/api/test/upload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	server.handleTestUpload(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rr.Code)
	}
}

func TestCameraPreview(t *testing.T) {
	cfg := newTestConfig()
	server := NewServer(ServerConfig{
		Config:     cfg,
		ConfigPath: "/tmp/test.json",
		GetCameraImage: func(cameraID string) ([]byte, error) {
			if cameraID == "test-cam" {
				return []byte{0xFF, 0xD8, 0xFF, 0xE0}, nil // Fake JPEG header
			}
			return nil, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/cameras/test-cam/preview", nil)
	rr := httptest.NewRecorder()

	server.handleCamera(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", rr.Code, rr.Body.String())
	}

	if rr.Header().Get("Content-Type") != "image/jpeg" {
		t.Errorf("expected Content-Type image/jpeg, got %s", rr.Header().Get("Content-Type"))
	}
}

func TestCameraPreview_NotFound(t *testing.T) {
	server := newTestServer(nil)

	req := httptest.NewRequest(http.MethodGet, "/api/cameras/nonexistent/preview", nil)
	rr := httptest.NewRecorder()

	server.handleCamera(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", rr.Code)
	}
}

func TestCameraPreview_NoCallback(t *testing.T) {
	server := newTestServer(nil) // No GetCameraImage callback

	req := httptest.NewRequest(http.MethodGet, "/api/cameras/test-cam/preview", nil)
	rr := httptest.NewRecorder()

	server.handleCamera(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", rr.Code)
	}
}

func TestValidateConfig(t *testing.T) {
	server := newTestServer(nil)

	tests := []struct {
		name        string
		config      *config.Config
		expectError bool
	}{
		{
			name: "valid config",
			config: &config.Config{
				Cameras: []config.Camera{
					{
						ID:   "cam1",
						Type: "http",
						Upload: &config.Upload{
							Username: "user",
							Password: "pass",
						},
					},
				},
			},
			expectError: false,
		},
		{
			name: "missing camera ID",
			config: &config.Config{
				Cameras: []config.Camera{
					{
						Type: "http",
						Upload: &config.Upload{
							Username: "user",
							Password: "pass",
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "missing camera type",
			config: &config.Config{
				Cameras: []config.Camera{
					{
						ID: "cam1",
						Upload: &config.Upload{
							Username: "user",
							Password: "pass",
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "invalid camera type",
			config: &config.Config{
				Cameras: []config.Camera{
					{
						ID:   "cam1",
						Type: "invalid",
						Upload: &config.Upload{
							Username: "user",
							Password: "pass",
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "missing upload",
			config: &config.Config{
				Cameras: []config.Camera{
					{
						ID:   "cam1",
						Type: "http",
					},
				},
			},
			expectError: true,
		},
		{
			name: "missing upload username",
			config: &config.Config{
				Cameras: []config.Camera{
					{
						ID:   "cam1",
						Type: "http",
						Upload: &config.Upload{
							Password: "pass",
						},
					},
				},
			},
			expectError: true,
		},
		{
			name: "missing upload password",
			config: &config.Config{
				Cameras: []config.Camera{
					{
						ID:   "cam1",
						Type: "http",
						Upload: &config.Upload{
							Username: "user",
						},
					},
				},
			},
			expectError: true,
		},
		{
			name:        "empty cameras",
			config:      &config.Config{},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := server.validateConfig(tt.config)
			if tt.expectError && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectError && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestUpdateConfig(t *testing.T) {
	server := newTestServer(nil)
	original := server.config

	newCfg := &config.Config{
		Version:  2,
		Timezone: "UTC",
	}

	server.UpdateConfig(newCfg)

	if server.config == original {
		t.Error("config should have been updated")
	}
	if server.config.Timezone != "UTC" {
		t.Errorf("timezone not updated, got %s", server.config.Timezone)
	}
}

func TestMaskPasswords(t *testing.T) {
	server := newTestServer(nil)

	masked := server.maskPasswords(server.config)

	// Check cameras have masked passwords
	cameras, ok := masked["cameras"].([]map[string]interface{})
	if !ok || len(cameras) == 0 {
		t.Fatal("expected cameras in masked config")
	}

	if upload, ok := cameras[0]["upload"].(map[string]interface{}); ok {
		if upload["password"] != "********" {
			t.Error("upload password not masked")
		}
	}

	// Check web console has masked password
	if wc, ok := masked["web_console"].(map[string]interface{}); ok {
		if wc["password"] != "********" {
			t.Error("web console password not masked")
		}
	}
}

func TestCameraToMap_WithAuth(t *testing.T) {
	server := newTestServer(nil)

	cam := config.Camera{
		ID:   "cam1",
		Name: "Test",
		Type: "http",
		Auth: &config.Auth{
			Type:     "basic",
			Username: "user",
			Password: "secret",
		},
	}

	m := server.cameraToMap(cam)

	auth, ok := m["auth"].(map[string]interface{})
	if !ok {
		t.Fatal("expected auth in map")
	}

	if auth["password"] != "********" {
		t.Error("auth password not masked")
	}
}

func TestCameraToMap_WithRTSP(t *testing.T) {
	server := newTestServer(nil)

	cam := config.Camera{
		ID:   "cam1",
		Name: "Test",
		Type: "rtsp",
		RTSP: &config.RTSP{
			URL:      "rtsp://example.com/stream",
			Username: "user",
			Password: "secret",
		},
	}

	m := server.cameraToMap(cam)

	rtsp, ok := m["rtsp"].(map[string]interface{})
	if !ok {
		t.Fatal("expected rtsp in map")
	}

	if rtsp["password"] != "********" {
		t.Error("rtsp password not masked")
	}
}
