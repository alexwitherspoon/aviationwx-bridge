package web

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

// TestTimezoneUpdate tests the PUT /api/time endpoint
func TestTimezoneUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create config service: %v", err)
	}

	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
	})

	// Test PUT /api/time
	reqBody := map[string]string{"timezone": "America/Los_Angeles"}
	body, _ := json.Marshal(reqBody)
	req := httptest.NewRequest("PUT", "/api/time", bytes.NewBuffer(body))
	req.SetBasicAuth("admin", svc.GetWebPassword())
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify timezone was updated in ConfigService
	global := svc.GetGlobal()
	if global.Timezone != "America/Los_Angeles" {
		t.Errorf("Expected timezone America/Los_Angeles, got %s", global.Timezone)
	}

	// Verify it persisted to disk
	svc2, _ := config.NewService(tmpDir)
	global2 := svc2.GetGlobal()
	if global2.Timezone != "America/Los_Angeles" {
		t.Error("Timezone did not persist to disk")
	}
}

// TestConfigPutTopLevelFields verifies PUT /api/config persists top-level global fields used by the settings UI.
func TestConfigPutTopLevelFields(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
	})

	base := svc.GetGlobal()
	body, err := json.Marshal(map[string]interface{}{
		"version":                 base.Version,
		"timezone":                base.Timezone,
		"update_channel":          "edge",
		"max_concurrent_uploads":  5,
		"max_concurrent_captures": 3,
		"timeout_connect_seconds": 45,
		"timeout_upload_seconds":  400,
		"web_console":             base.WebConsole,
		"global":                  base.Global,
		"queue":                   base.Queue,
		"sntp":                    base.SNTP,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	req := httptest.NewRequest("PUT", "/api/config", bytes.NewBuffer(body))
	req.SetBasicAuth("admin", svc.GetWebPassword())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	g := svc.GetGlobal()
	if g.UpdateChannel != "edge" {
		t.Errorf("update_channel: want edge, got %q", g.UpdateChannel)
	}
	if g.MaxConcurrentUploads != 5 {
		t.Errorf("max_concurrent_uploads: want 5, got %d", g.MaxConcurrentUploads)
	}
	if g.Global == nil || g.Global.MaxConcurrentUploads != 5 {
		t.Errorf("global.max_concurrent_uploads should mirror top-level, got %+v", g.Global)
	}
	if g.MaxConcurrentCaptures != 3 {
		t.Errorf("max_concurrent_captures: want 3, got %d", g.MaxConcurrentCaptures)
	}
	if g.Global == nil || g.Global.MaxConcurrentCaptures != 3 {
		t.Errorf("global.max_concurrent_captures should mirror top-level, got %+v", g.Global)
	}
	if g.TimeoutConnectSeconds != 45 {
		t.Errorf("timeout_connect_seconds: want 45, got %d", g.TimeoutConnectSeconds)
	}
	if g.TimeoutUploadSeconds != 400 {
		t.Errorf("timeout_upload_seconds: want 400, got %d", g.TimeoutUploadSeconds)
	}
}

func TestConfigGetPreservesUserMaxConcurrentCaptures(t *testing.T) {
	tmpDir := t.TempDir()
	globalPath := filepath.Join(tmpDir, "global.json")
	if err := os.WriteFile(globalPath, []byte(`{"version":2,"timezone":"UTC","max_concurrent_captures":7}`), 0644); err != nil {
		t.Fatalf("write global.json: %v", err)
	}
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.SetBasicAuth("admin", svc.GetWebPassword())
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var got config.GlobalSettings
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if got.MaxConcurrentCaptures != 7 {
		t.Errorf("max_concurrent_captures: want 7 (stored), got %d", got.MaxConcurrentCaptures)
	}
}

func TestConfigGetFillsProfiledWhenMaxConcurrentCapturesUnset(t *testing.T) {
	tmpDir := t.TempDir()
	globalPath := filepath.Join(tmpDir, "global.json")
	if err := os.WriteFile(globalPath, []byte(`{"version":2,"timezone":"UTC"}`), 0644); err != nil {
		t.Fatalf("write global.json: %v", err)
	}
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	pre := svc.GetGlobal()
	want := config.EffectiveMaxConcurrentCaptures(pre)
	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.SetBasicAuth("admin", svc.GetWebPassword())
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var got config.GlobalSettings
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if got.MaxConcurrentCaptures != want {
		t.Errorf("max_concurrent_captures: want %d (effective when unset), got %d", want, got.MaxConcurrentCaptures)
	}
}

func TestConfigGetFillsTopLevelWhenOnlyNestedMaxConcurrentCapturesSet(t *testing.T) {
	tmpDir := t.TempDir()
	globalPath := filepath.Join(tmpDir, "global.json")
	// Only nested global.global.max_concurrent_captures — UI reads top-level field from GET.
	body := `{"version":2,"timezone":"UTC","global":{"max_concurrent_captures":4}}`
	if err := os.WriteFile(globalPath, []byte(body), 0644); err != nil {
		t.Fatalf("write global.json: %v", err)
	}
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	pre := svc.GetGlobal()
	if pre.MaxConcurrentCaptures != 0 {
		t.Fatalf("pre: want top-level 0 on disk, got %d", pre.MaxConcurrentCaptures)
	}
	want := config.EffectiveMaxConcurrentCaptures(pre)
	if want != 4 {
		t.Fatalf("EffectiveMaxConcurrentCaptures: want 4 from nested, got %d", want)
	}
	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.SetBasicAuth("admin", svc.GetWebPassword())
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/config: want 200, got %d: %s", w.Code, w.Body.String())
	}
	var got config.GlobalSettings
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if got.MaxConcurrentCaptures != 4 {
		t.Errorf("max_concurrent_captures: want 4 (effective from nested), got %d", got.MaxConcurrentCaptures)
	}
}

func TestReadyz(t *testing.T) {
	t.Run("notConfiguredReturns200", func(t *testing.T) {
		s := NewServer(ServerConfig{
			ConfigService: testServerConfigService(t),
			GetStatus: func() interface{} {
				return map[string]interface{}{"status": "ok"}
			},
		})
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		w := httptest.NewRecorder()
		s.GetMux().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})

	t.Run("callbackNotReadyReturns503", func(t *testing.T) {
		s := NewServer(ServerConfig{
			ConfigService: testServerConfigService(t),
			GetStatus: func() interface{} {
				return map[string]interface{}{"status": "ok"}
			},
			GetCaptureReadiness: func() (bool, string) {
				return false, "test reason"
			},
		})
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		w := httptest.NewRecorder()
		s.GetMux().ServeHTTP(w, req)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", w.Code)
		}
	})

	t.Run("callbackReadyReturns200", func(t *testing.T) {
		s := NewServer(ServerConfig{
			ConfigService: testServerConfigService(t),
			GetStatus: func() interface{} {
				return map[string]interface{}{"status": "ok"}
			},
			GetCaptureReadiness: func() (bool, string) {
				return true, ""
			},
		})
		req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
		w := httptest.NewRecorder()
		s.GetMux().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", w.Code)
		}
	})
}

func testServerConfigService(t *testing.T) *config.Service {
	t.Helper()
	tmpDir := t.TempDir()
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

func TestAddCamera_DuplicateUploadCredentialsHTTP409(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
	})
	camJSON := func(displayName string) string {
		return fmt.Sprintf(`{
			"name": %q,
			"type": "http",
			"enabled": true,
			"snapshot_url": "http://example.com/s.jpg",
			"capture_interval_seconds": 60,
			"upload": {
				"host": "upload.example.com",
				"port": 2222,
				"username": "shared-sftp-user",
				"password": "p"
			}
		}`, displayName)
	}
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewBufferString(body))
		req.SetBasicAuth("admin", svc.GetWebPassword())
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)
		return w
	}
	if w := post(camJSON("Camera One")); w.Code != http.StatusCreated {
		t.Fatalf("first POST: %d %s", w.Code, w.Body.String())
	}
	if w := post(camJSON("Camera Two")); w.Code != http.StatusConflict {
		t.Fatalf("duplicate upload: want 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAddCamera_POST_RequiresUploadUsernameAndPassword(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
	})
	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewBufferString(body))
		req.SetBasicAuth("admin", svc.GetWebPassword())
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)
		return w.Code
	}
	noUser := `{"name":"Cam","type":"http","enabled":true,"snapshot_url":"http://example.com/s.jpg","capture_interval_seconds":60,"upload":{"host":"upload.example.com","port":2222,"username":"","password":"secret"}}`
	if code := post(noUser); code != http.StatusBadRequest {
		t.Fatalf("empty username: want 400, got %d", code)
	}
	noPass := `{"name":"Cam","type":"http","enabled":true,"snapshot_url":"http://example.com/s.jpg","capture_interval_seconds":60,"upload":{"host":"upload.example.com","port":2222,"username":"u1","password":""}}`
	if code := post(noPass); code != http.StatusBadRequest {
		t.Fatalf("empty password: want 400, got %d", code)
	}
}

func TestAddCamera_NormalizesUploadFields(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
	})
	body := `{"name":"Trim Upload","type":"http","enabled":true,"snapshot_url":"http://example.com/s.jpg","capture_interval_seconds":60,"upload":{"host":"  Upload.EXAMPLE.com  ","port":2222,"username":"  user1  ","password":"  secret  "}}`
	req := httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewBufferString(body))
	req.SetBasicAuth("admin", svc.GetWebPassword())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", w.Code, w.Body.String())
	}
	cam, err := svc.GetCamera("trim-upload")
	if err != nil {
		t.Fatalf("GetCamera: %v", err)
	}
	if cam.Upload.Host != "upload.example.com" {
		t.Errorf("upload.host: got %q", cam.Upload.Host)
	}
	if cam.Upload.Username != "user1" {
		t.Errorf("upload.username: got %q", cam.Upload.Username)
	}
	if cam.Upload.Password != "secret" {
		t.Errorf("upload.password: got %q", cam.Upload.Password)
	}
}

func TestAddCamera_ONVIF_RejectInvalidEndpoint(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
	})
	body := `{"name":"Bad Onvif","type":"onvif","enabled":true,"capture_interval_seconds":60,"onvif":{"endpoint":"   ","username":"u","password":"p"},"upload":{"host":"upload.example.com","port":2222,"username":"u1","password":"p1"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewBufferString(body))
	req.SetBasicAuth("admin", svc.GetWebPassword())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("POST onvif whitespace endpoint: want 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateCamera_PUT_Upload_RequiresUsername(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
	})
	postJSON := `{"name":"HTTP Cam","type":"http","enabled":true,"snapshot_url":"http://x/s.jpg","capture_interval_seconds":60,"upload":{"host":"upload.example.com","port":2222,"username":"u","password":"p"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewBufferString(postJSON))
	req.SetBasicAuth("admin", svc.GetWebPassword())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", w.Code, w.Body.String())
	}
	putJSON := `{"name":"HTTP Cam","type":"http","enabled":true,"snapshot_url":"http://x/s.jpg","capture_interval_seconds":60,"upload":{"host":"upload.example.com","port":2222,"username":"   ","password":"p"}}`
	req2 := httptest.NewRequest(http.MethodPut, "/api/cameras/http-cam", bytes.NewBufferString(putJSON))
	req2.SetBasicAuth("admin", svc.GetWebPassword())
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("PUT blank upload username: want 400, got %d: %s", w2.Code, w2.Body.String())
	}
}

func TestUpdateCamera_ONVIF_RejectEmptyEndpointWhenNoExistingEndpoint(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
	})
	postJSON := `{"name":"HTTP Cam","type":"http","enabled":true,"snapshot_url":"http://x/s.jpg","capture_interval_seconds":60,"upload":{"host":"upload.example.com","port":2222,"username":"u","password":"p"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewBufferString(postJSON))
	req.SetBasicAuth("admin", svc.GetWebPassword())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", w.Code, w.Body.String())
	}
	putJSON := `{"name":"HTTP Cam","type":"http","enabled":true,"snapshot_url":"http://x/s.jpg","capture_interval_seconds":60,"upload":{"host":"upload.example.com","port":2222,"username":"u","password":"p"},"onvif":{"endpoint":"   ","username":"a","password":"b"}}`
	req2 := httptest.NewRequest(http.MethodPut, "/api/cameras/http-cam", bytes.NewBufferString(putJSON))
	req2.SetBasicAuth("admin", svc.GetWebPassword())
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("PUT empty ONVIF endpoint: want 400, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestAddCamera_ONVIF_NormalizesDuplicateEndpoint verifies POST /api/cameras persists a
// single-scheme ONVIF endpoint when the client sends duplicated http:// prefixes.
func TestAddCamera_ONVIF_NormalizesDuplicateEndpoint(t *testing.T) {
	tmpDir := t.TempDir()
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
	})
	camJSON := `{
		"name": "Onvif Dup Endpoint",
		"type": "onvif",
		"enabled": true,
		"capture_interval_seconds": 60,
		"onvif": {
			"endpoint": "http://http://192.168.1.50/onvif/device_service",
			"username": "admin",
			"password": "secret"
		},
		"upload": {
			"host": "upload.example.com",
			"port": 2222,
			"username": "onvif-dup-user",
			"password": "pass"
		}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/cameras", bytes.NewBufferString(camJSON))
	req.SetBasicAuth("admin", svc.GetWebPassword())
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST: %d %s", w.Code, w.Body.String())
	}
	cam, err := svc.GetCamera("onvif-dup-endpoint")
	if err != nil {
		t.Fatalf("GetCamera: %v", err)
	}
	if cam.ONVIF == nil {
		t.Fatal("expected ONVIF config")
	}
	want := "http://192.168.1.50/onvif/device_service"
	if cam.ONVIF.Endpoint != want {
		t.Errorf("onvif.endpoint = %q, want %q", cam.ONVIF.Endpoint, want)
	}

	updateJSON := `{
		"id": "onvif-dup-endpoint",
		"name": "Onvif Dup Endpoint",
		"type": "onvif",
		"enabled": true,
		"capture_interval_seconds": 60,
		"onvif": {
			"endpoint": "https://https://10.0.0.2/other/onvif",
			"username": "admin",
			"password": "secret"
		},
		"upload": {
			"host": "upload.example.com",
			"port": 2222,
			"username": "onvif-dup-user",
			"password": ""
		}
	}`
	req2 := httptest.NewRequest(http.MethodPut, "/api/cameras/onvif-dup-endpoint", bytes.NewBufferString(updateJSON))
	req2.SetBasicAuth("admin", svc.GetWebPassword())
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", w2.Code, w2.Body.String())
	}
	cam2, err := svc.GetCamera("onvif-dup-endpoint")
	if err != nil {
		t.Fatalf("GetCamera after PUT: %v", err)
	}
	wantHTTPS := "https://10.0.0.2/other/onvif"
	if cam2.ONVIF.Endpoint != wantHTTPS {
		t.Errorf("after PUT onvif.endpoint = %q, want %q", cam2.ONVIF.Endpoint, wantHTTPS)
	}
}

// TestCameraAddUpdateDelete tests full camera lifecycle
func TestCameraAddUpdateDelete(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := config.NewService(tmpDir)

	workerStatus := make(map[string]map[string]interface{})

	server := NewServer(ServerConfig{
		ConfigService: svc,
		GetStatus: func() interface{} {
			return map[string]interface{}{"status": "ok"}
		},
		GetWorkerStatus: func(cameraID string) map[string]interface{} {
			if status, ok := workerStatus[cameraID]; ok {
				return status
			}
			return map[string]interface{}{
				"worker_running": false,
				"worker_error":   "Not started",
			}
		},
	})

	makeRequest := func(method, path string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBuffer(body))
		req.SetBasicAuth("admin", svc.GetWebPassword())
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)
		return w
	}

	// Add camera
	t.Run("AddCamera", func(t *testing.T) {
		camJSON := `{
			"name": "Test Camera",
			"type": "http",
			"enabled": true,
			"snapshot_url": "http://example.com/snap.jpg",
			"capture_interval_seconds": 60,
			"upload": {
				"host": "upload.example.com",
				"port": 2222,
				"username": "user",
				"password": "pass"
			}
		}`

		w := makeRequest("POST", "/api/cameras", []byte(camJSON))
		if w.Code != http.StatusCreated {
			t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
		}

		var created map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode POST response: %v", err)
		}
		if id, _ := created["id"].(string); id != "test-camera" {
			t.Fatalf("response id: want test-camera, got %v", created["id"])
		}

		// Verify persisted (id derived from display name)
		cam, err := svc.GetCamera("test-camera")
		if err != nil {
			t.Fatalf("Camera not found: %v", err)
		}
		if cam.Name != "Test Camera" {
			t.Errorf("Expected name 'Test Camera', got %s", cam.Name)
		}
	})

	// Update camera (preserve password)
	t.Run("UpdateCamera_PreservePassword", func(t *testing.T) {
		updateJSON := `{
			"id": "test-camera",
			"name": "Updated Camera",
			"type": "http",
			"enabled": true,
			"snapshot_url": "http://example.com/snap2.jpg",
			"capture_interval_seconds": 120,
			"upload": {
				"host": "upload.example.com",
				"port": 2222,
				"username": "user",
				"password": ""
			}
		}`

		w := makeRequest("PUT", "/api/cameras/test-camera", []byte(updateJSON))
		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}

		// Verify password was preserved
		cam, _ := svc.GetCamera("test-camera")
		if cam.Upload.Password != "pass" {
			t.Errorf("Expected password to be preserved, got %s", cam.Upload.Password)
		}
		if cam.Name != "Updated Camera" {
			t.Errorf("Expected name 'Updated Camera', got %s", cam.Name)
		}
	})

	// List cameras
	t.Run("ListCameras", func(t *testing.T) {
		w := makeRequest("GET", "/api/cameras", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d", w.Code)
		}

		var cameras []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &cameras); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if len(cameras) != 1 {
			t.Errorf("Expected 1 camera, got %d", len(cameras))
		}
	})

	// Delete camera
	t.Run("DeleteCamera", func(t *testing.T) {
		w := makeRequest("DELETE", "/api/cameras/test-camera", nil)
		if w.Code != http.StatusNoContent {
			t.Fatalf("Expected 204, got %d", w.Code)
		}

		// Verify deleted
		_, err := svc.GetCamera("test-camera")
		if err == nil {
			t.Error("Camera should have been deleted")
		}
	})
}

// TestConfigServicePersistence tests that all changes persist to disk
func TestConfigServicePersistence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create service and make changes
	svc1, _ := config.NewService(tmpDir)
	svc1.UpdateGlobal(func(g *config.GlobalSettings) error {
		g.Timezone = "America/New_York"
		return nil
	})

	cam := config.Camera{
		ID:      "persist-test",
		Name:    "Persistence Test",
		Type:    "http",
		Enabled: true,
		Upload: &config.Upload{
			Host:     "upload.example.com",
			Port:     2121,
			Username: "testuser",
			Password: "testpass",
		},
	}
	if _, err := svc1.AddCamera(cam); err != nil {
		t.Fatalf("AddCamera: %v", err)
	}

	// Create new service instance (simulates restart)
	svc2, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatalf("Failed to reload config: %v", err)
	}

	// Verify global config persisted
	global := svc2.GetGlobal()
	if global.Timezone != "America/New_York" {
		t.Errorf("Expected timezone America/New_York, got %s", global.Timezone)
	}

	// Verify camera persisted
	cam2, err := svc2.GetCamera("persist-test")
	if err != nil {
		t.Fatalf("Camera not found after reload: %v", err)
	}
	if cam2.Name != "Persistence Test" {
		t.Errorf("Expected name 'Persistence Test', got %s", cam2.Name)
	}
	if cam2.Upload.Password != "testpass" {
		t.Error("Password did not persist")
	}
}

// TestConfigServiceEventNotifications tests async event notifications
func TestConfigServiceEventNotifications(t *testing.T) {
	tmpDir := t.TempDir()
	svc, _ := config.NewService(tmpDir)

	events := make(chan config.ConfigEvent, 10)
	svc.Subscribe(func(event config.ConfigEvent) {
		events <- event
	})

	// Add camera - should trigger event
	cam := config.Camera{
		ID:      "event-test",
		Name:    "Event Test",
		Type:    "http",
		Enabled: true,
	}
	if _, err := svc.AddCamera(cam); err != nil {
		t.Fatalf("AddCamera: %v", err)
	}

	// Wait for event
	select {
	case event := <-events:
		if event.Type != "camera_added" {
			t.Errorf("Expected camera_added, got %s", event.Type)
		}
		if event.CameraID != "event-test" {
			t.Errorf("Expected camera ID event-test, got %s", event.CameraID)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for event")
	}

	// Update camera - should trigger event
	svc.UpdateCamera("event-test", func(c *config.Camera) error {
		c.Name = "Updated Name"
		return nil
	})

	select {
	case event := <-events:
		if event.Type != "camera_updated" {
			t.Errorf("Expected camera_updated, got %s", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for update event")
	}

	// Delete camera - should trigger event
	svc.DeleteCamera("event-test")

	select {
	case event := <-events:
		if event.Type != "camera_deleted" {
			t.Errorf("Expected camera_deleted, got %s", event.Type)
		}
	case <-time.After(1 * time.Second):
		t.Error("Timeout waiting for delete event")
	}
}

// testServerWithAuth creates a Server with auth (password: "test") for endpoint tests
func testServerWithAuth(t *testing.T, cfg ServerConfig) *Server {
	t.Helper()
	tmpDir := t.TempDir()
	svc, err := config.NewService(tmpDir)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if err := svc.UpdateGlobal(func(g *config.GlobalSettings) error {
		g.WebConsole = &config.WebConsole{Enabled: true, Password: "test"}
		return nil
	}); err != nil {
		t.Fatalf("UpdateGlobal: %v", err)
	}
	cfg.ConfigService = svc
	if cfg.GetStatus == nil {
		cfg.GetStatus = func() interface{} { return map[string]interface{}{"status": "ok"} }
	}
	return NewServer(cfg)
}

// TestHandleTestCamera tests the POST /api/test/camera endpoint
func TestHandleTestCamera(t *testing.T) {
	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 0xD9} // Minimal valid JPEG

	t.Run("success returns image", func(t *testing.T) {
		server := testServerWithAuth(t, ServerConfig{
			TestCamera: func(cam config.Camera) ([]byte, error) {
				if cam.Type != "http" || cam.SnapshotURL != "http://example.com/snap.jpg" {
					return nil, nil
				}
				return fakeJPEG, nil
			},
		})

		camJSON := `{"id":"test","type":"http","snapshot_url":"http://example.com/snap.jpg"}`
		req := httptest.NewRequest("POST", "/api/test/camera", bytes.NewBufferString(camJSON))
		req.SetBasicAuth("admin", "test")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); ct != "image/jpeg" {
			t.Errorf("Expected Content-Type image/jpeg, got %s", ct)
		}
		if !bytes.Equal(w.Body.Bytes(), fakeJPEG) {
			t.Error("Response body should match returned image")
		}
	})

	t.Run("failure returns 500 with error", func(t *testing.T) {
		server := testServerWithAuth(t, ServerConfig{
			TestCamera: func(config.Camera) ([]byte, error) {
				return nil, fmt.Errorf("connection refused")
			},
		})

		camJSON := `{"id":"test","type":"http","snapshot_url":"http://example.com/snap.jpg"}`
		req := httptest.NewRequest("POST", "/api/test/camera", bytes.NewBufferString(camJSON))
		req.SetBasicAuth("admin", "test")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected 500, got %d", w.Code)
		}
		if !strings.Contains(w.Body.String(), "connection refused") {
			t.Errorf("Expected error in body, got %s", w.Body.String())
		}
	})

	t.Run("invalid JSON returns 400", func(t *testing.T) {
		server := testServerWithAuth(t, ServerConfig{
			TestCamera: func(config.Camera) ([]byte, error) { return fakeJPEG, nil },
		})

		req := httptest.NewRequest("POST", "/api/test/camera", bytes.NewBufferString("not json"))
		req.SetBasicAuth("admin", "test")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("Expected 400, got %d", w.Code)
		}
	})

	t.Run("nil callback returns 503", func(t *testing.T) {
		server := testServerWithAuth(t, ServerConfig{})
		// TestCamera not set

		camJSON := `{"id":"test","type":"http","snapshot_url":"http://example.com/snap.jpg"}`
		req := httptest.NewRequest("POST", "/api/test/camera", bytes.NewBufferString(camJSON))
		req.SetBasicAuth("admin", "test")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected 503, got %d", w.Code)
		}
	})
}

// TestCameraPreview tests GET /api/cameras/{id}/preview
func TestCameraPreview(t *testing.T) {
	fakeJPEG := []byte{0xFF, 0xD8, 0xFF, 0xD9}

	t.Run("success returns image", func(t *testing.T) {
		server := testServerWithAuth(t, ServerConfig{
			GetCameraImage: func(cameraID string) ([]byte, error) {
				if cameraID != "preview-cam" {
					return nil, fmt.Errorf("unknown camera")
				}
				return fakeJPEG, nil
			},
		})
		svc := server.configService
		if _, err := svc.AddCamera(config.Camera{
			ID:      "preview-cam",
			Name:    "Preview Test",
			Type:    "http",
			Enabled: true,
			Upload:  &config.Upload{Host: "upload.example.com", Port: 2222, Username: "u", Password: "p"},
		}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest("GET", "/api/cameras/preview-cam/preview", nil)
		req.SetBasicAuth("admin", "test")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); ct != "image/jpeg" {
			t.Errorf("Expected Content-Type image/jpeg, got %s", ct)
		}
		if !bytes.Equal(w.Body.Bytes(), fakeJPEG) {
			t.Error("Response body should match returned image")
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache, no-store, must-revalidate" {
			t.Errorf("Expected Cache-Control no-cache, got %s", cc)
		}
	})

	t.Run("nil callback returns 503", func(t *testing.T) {
		server := testServerWithAuth(t, ServerConfig{})
		svc := server.configService
		if _, err := svc.AddCamera(config.Camera{
			ID:      "preview-cam",
			Name:    "Preview Test",
			Type:    "http",
			Enabled: true,
			Upload:  &config.Upload{Host: "upload.example.com", Port: 2222, Username: "u", Password: "p"},
		}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest("GET", "/api/cameras/preview-cam/preview", nil)
		req.SetBasicAuth("admin", "test")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected 503, got %d", w.Code)
		}
	})

	t.Run("unknown camera returns 404", func(t *testing.T) {
		server := testServerWithAuth(t, ServerConfig{
			GetCameraImage: func(string) ([]byte, error) { return fakeJPEG, nil },
		})

		req := httptest.NewRequest("GET", "/api/cameras/nonexistent/preview", nil)
		req.SetBasicAuth("admin", "test")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("Expected 404, got %d", w.Code)
		}
	})

	t.Run("no image available returns 204", func(t *testing.T) {
		server := testServerWithAuth(t, ServerConfig{
			GetCameraImage: func(cameraID string) ([]byte, error) {
				return nil, fmt.Errorf("no image available yet")
			},
		})
		svc := server.configService
		if _, err := svc.AddCamera(config.Camera{
			ID:      "preview-cam",
			Name:    "Preview Test",
			Type:    "http",
			Enabled: true,
			Upload:  &config.Upload{Host: "upload.example.com", Port: 2222, Username: "u", Password: "p"},
		}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest("GET", "/api/cameras/preview-cam/preview", nil)
		req.SetBasicAuth("admin", "test")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("Expected 204, got %d", w.Code)
		}
	})

	t.Run("empty image returns 204", func(t *testing.T) {
		server := testServerWithAuth(t, ServerConfig{
			GetCameraImage: func(cameraID string) ([]byte, error) {
				return []byte{}, nil
			},
		})
		svc := server.configService
		if _, err := svc.AddCamera(config.Camera{
			ID:      "preview-cam",
			Name:    "Preview Test",
			Type:    "http",
			Enabled: true,
			Upload:  &config.Upload{Host: "upload.example.com", Port: 2222, Username: "u", Password: "p"},
		}); err != nil {
			t.Fatal(err)
		}

		req := httptest.NewRequest("GET", "/api/cameras/preview-cam/preview", nil)
		req.SetBasicAuth("admin", "test")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)

		if w.Code != http.StatusNoContent {
			t.Errorf("Expected 204, got %d", w.Code)
		}
	})
}

// TestHandleTestUpload tests the POST /api/test/upload endpoint
func TestHandleTestUpload(t *testing.T) {
	t.Run("success returns ok", func(t *testing.T) {
		server := testServerWithAuth(t, ServerConfig{
			TestUpload: func(config.Upload) error { return nil },
		})

		uploadJSON := `{"host":"upload.example.com","port":2222,"username":"u","password":"p"}`
		req := httptest.NewRequest("POST", "/api/test/upload", bytes.NewBufferString(uploadJSON))
		req.SetBasicAuth("admin", "test")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
		}
		var result map[string]string
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("Invalid JSON: %v", err)
		}
		if result["status"] != "ok" {
			t.Errorf("Expected status ok, got %s", result["status"])
		}
	})

	t.Run("failure returns 500", func(t *testing.T) {
		server := testServerWithAuth(t, ServerConfig{
			TestUpload: func(config.Upload) error {
				return fmt.Errorf("connection refused")
			},
		})

		uploadJSON := `{"host":"upload.example.com","port":2222,"username":"u","password":"p"}`
		req := httptest.NewRequest("POST", "/api/test/upload", bytes.NewBufferString(uploadJSON))
		req.SetBasicAuth("admin", "test")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			t.Errorf("Expected 500, got %d", w.Code)
		}
	})
}
