package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/paths"
)

const updateTriggerName = "trigger-update"

func TestHandleUpdate_writesTriggerAtVolumeRoot(t *testing.T) {
	// Regression: docker-compose and Pi mounts use /data as the data volume root.
	// The trigger must not live under /data/aviationwx inside the container.
	dataDir := t.TempDir()
	t.Setenv("AVIATIONWX_DATA_DIR", dataDir)

	server := testServerWithAuth(t, ServerConfig{})
	req := httptest.NewRequest(http.MethodPost, "/api/update", nil)
	req.SetBasicAuth("admin", "test")
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}

	triggerPath := filepath.Join(dataDir, updateTriggerName)
	data, err := os.ReadFile(triggerPath)
	if err != nil {
		t.Fatalf("read trigger file: %v", err)
	}
	if string(data) != "force" {
		t.Fatalf("trigger content = %q, want force", data)
	}
	legacy := filepath.Join(dataDir, "aviationwx", updateTriggerName)
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy nested trigger path must not exist: %v", err)
	}
}

func TestHandleUpdate_createsMissingDataDir(t *testing.T) {
	parent := t.TempDir()
	dataDir := filepath.Join(parent, "fresh", "volume")
	t.Setenv("AVIATIONWX_DATA_DIR", dataDir)

	server := testServerWithAuth(t, ServerConfig{})
	req := httptest.NewRequest(http.MethodPost, "/api/update", nil)
	req.SetBasicAuth("admin", "test")
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dataDir, updateTriggerName)); err != nil {
		t.Fatalf("expected trigger file after MkdirAll: %v", err)
	}
}

func TestHandleUpdate_respectsAviationwxDataDir(t *testing.T) {
	customDir := t.TempDir()
	t.Setenv("AVIATIONWX_DATA_DIR", customDir)

	server := testServerWithAuth(t, ServerConfig{})
	req := httptest.NewRequest(http.MethodPost, "/api/update", nil)
	req.SetBasicAuth("admin", "test")
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(filepath.Join(customDir, updateTriggerName)); err != nil {
		t.Fatalf("trigger not written to AVIATIONWX_DATA_DIR: %v", err)
	}
}

func TestHandleUpdate_methodNotAllowed(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AVIATIONWX_DATA_DIR", dataDir)

	server := testServerWithAuth(t, ServerConfig{})
	req := httptest.NewRequest(http.MethodGet, "/api/update", nil)
	req.SetBasicAuth("admin", "test")
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d, want 405", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dataDir, updateTriggerName)); !os.IsNotExist(err) {
		t.Fatal("GET must not create trigger file")
	}
}

func TestHandleUpdate_requiresAuth(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AVIATIONWX_DATA_DIR", dataDir)

	server := testServerWithAuth(t, ServerConfig{})
	req := httptest.NewRequest(http.MethodPost, "/api/update", nil)
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status %d, want 401", w.Code)
	}
	if _, err := os.Stat(filepath.Join(dataDir, updateTriggerName)); !os.IsNotExist(err) {
		t.Fatal("unauthenticated request must not create trigger file")
	}
}

func TestHandleUpdate_successResponse(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AVIATIONWX_DATA_DIR", dataDir)

	server := testServerWithAuth(t, ServerConfig{})
	req := httptest.NewRequest(http.MethodPost, "/api/update", nil)
	req.SetBasicAuth("admin", "test")
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200, body %s", w.Code, w.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "ok" {
		t.Fatalf("status field = %q", resp["status"])
	}
	if !strings.Contains(resp["message"], "supervisor") {
		t.Fatalf("message = %q", resp["message"])
	}
}

func TestHandleUpdate_writeFailureWhenDataDirIsFile(t *testing.T) {
	parent := t.TempDir()
	filePath := filepath.Join(parent, "not-a-dir")
	if err := os.WriteFile(filePath, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AVIATIONWX_DATA_DIR", filePath)

	server := testServerWithAuth(t, ServerConfig{})
	req := httptest.NewRequest(http.MethodPost, "/api/update", nil)
	req.SetBasicAuth("admin", "test")
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status %d, want 500", w.Code)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["status"] != "error" {
		t.Fatalf("status field = %q", resp["status"])
	}
	if !strings.Contains(resp["error"], "Failed to trigger update") {
		t.Fatalf("error = %q", resp["error"])
	}
}

func TestUpdateTriggerPath_matchesHostSupervisorLayout(t *testing.T) {
	dataDir := t.TempDir()
	t.Setenv("AVIATIONWX_DATA_DIR", dataDir)

	got := filepath.Join(paths.HostDataDir(), updateTriggerName)
	want := filepath.Join(dataDir, updateTriggerName)
	if got != want {
		t.Fatalf("trigger path = %q, want %q", got, want)
	}
}
