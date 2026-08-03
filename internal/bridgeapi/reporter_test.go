package bridgeapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/logger"
)

func testConfigService(t *testing.T, key, base string) *config.Service {
	t.Helper()
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	err = svc.UpdateGlobal(func(g *config.GlobalSettings) error {
		g.API = &config.APISettings{
			Enabled: true,
			Key:     key,
			BaseURL: base,
		}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateGlobal: %v", err)
	}
	return svc
}

func TestReporterClearsBootstrapOnClientRebuild(t *testing.T) {
	t.Setenv("AVIATIONWX_API_TLS_INSECURE", "1")

	var bootN atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bridge/bootstrap":
			n := bootN.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data": map[string]interface{}{
					"airport":                    map[string]string{"id": "KXX", "name": "Mock"},
					"bridge_id":                  fmt.Sprintf("br_%d", n),
					"declination_deg":            -1.0,
					"heartbeat_interval_seconds": 60,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/bridge/health":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	keyA := "awxb_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKL"
	keyB := "awxb_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuv"
	svc := testConfigService(t, keyA, srv.URL)

	r := NewReporter(ReporterConfig{
		ConfigService: svc,
		Version:       "test",
		Log:           logger.Default(),
		Interval:      time.Hour,
	})
	r.SyncFromConfig()
	defer r.Stop()

	// Wait for initial tick bootstrap.
	deadline := time.Now().Add(3 * time.Second)
	var id1 string
	for time.Now().Before(deadline) {
		if snap := r.Snapshot(); snap.Bootstrap != nil {
			id1 = snap.Bootstrap.BridgeID
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if id1 == "" {
		t.Fatal("expected bootstrap after SyncFromConfig")
	}

	_ = svc.UpdateGlobal(func(g *config.GlobalSettings) error {
		g.API.Key = keyB
		return nil
	})
	r.SyncFromConfig()

	if snap := r.Snapshot(); snap.Bootstrap != nil {
		t.Fatal("bootstrap should be nil immediately after client rebuild")
	}

	deadline = time.Now().Add(3 * time.Second)
	var id2 string
	for time.Now().Before(deadline) {
		if snap := r.Snapshot(); snap.Bootstrap != nil {
			id2 = snap.Bootstrap.BridgeID
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if id2 == "" {
		t.Fatal("expected re-bootstrap after key change")
	}
	if id2 == id1 {
		t.Fatalf("expected new bridge id after key change, still %q", id1)
	}
	if bootN.Load() < 2 {
		t.Fatalf("expected at least 2 bootstrap calls, got %d", bootN.Load())
	}
}

func TestReporterRestoresErrorsOnHealthFailure(t *testing.T) {
	t.Setenv("AVIATIONWX_API_TLS_INSECURE", "1")

	healthN := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bridge/bootstrap":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data": map[string]interface{}{
					"airport":   map[string]string{"id": "KXX"},
					"bridge_id": "br_1",
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/bridge/health":
			healthN++
			http.Error(w, "fail", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	key := "awxb_abcdefghijklmnopqrstuvwxyz0123456789ABCDEFGHIJKL"
	svc := testConfigService(t, key, srv.URL)
	r := NewReporter(ReporterConfig{
		ConfigService: svc,
		Version:       "test",
		Log:           logger.Default(),
		Interval:      time.Hour,
	})
	client, err := NewClient(ClientConfig{
		BaseURL:    srv.URL,
		APIKey:     key,
		Version:    "test",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.client = client
	r.mu.Unlock()

	r.NoteError("capture:cam1", "timeout")
	r.tick(context.Background())

	r.mu.Lock()
	_, ok := r.errors["capture:cam1"]
	r.mu.Unlock()
	if !ok {
		t.Fatal("capture error fingerprint should be restored after failed health POST")
	}
}

func TestReporterSyncFromConfigClearsBootstrapWhenDisabled(t *testing.T) {
	svc, err := config.NewService(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := NewReporter(ReporterConfig{
		ConfigService: svc,
		Version:       "test",
		Log:           logger.Default(),
		Interval:      time.Hour,
	})
	r.mu.Lock()
	r.bootstrap = &BootstrapResponse{BridgeID: "stale"}
	r.client = &Client{}
	r.mu.Unlock()

	r.SyncFromConfig()
	if r.Snapshot().Bootstrap != nil {
		t.Fatal("bootstrap should clear when API disabled")
	}
}
