package bridgeapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testAPIKey() string {
	return "awxb_" + strings.Repeat("a", 48)
}

func TestClientBootstrapAndHealth(t *testing.T) {
	var gotKey string
	var healthBody HealthRequest

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("X-Api-Key")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/bridge/bootstrap":
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"data": BootstrapResponse{
					Airport:        BootstrapAirport{ID: "KSPB", Name: "Scappoose"},
					BridgeID:       "bridge-spb-1",
					DeclinationDeg: -15.2,
				},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/bridge/health":
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err := json.Unmarshal(raw, &healthBody); err != nil {
				t.Errorf("health body: %v", err)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:    srv.URL,
		APIKey:     testAPIKey(),
		Version:    "test",
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	ctx := context.Background()
	boot, err := client.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	if boot.BridgeID != "bridge-spb-1" || boot.Airport.ID != "KSPB" {
		t.Fatalf("unexpected bootstrap: %+v", boot)
	}
	if gotKey != testAPIKey() {
		t.Fatalf("api key header = %q", gotKey)
	}

	err = client.PostHealth(ctx, HealthRequest{
		ObservedAt: time.Now().UTC(),
		Host: HostHealth{
			Status: StatusOperational,
			NTPOK:  true,
			Build:  BuildInfo{Version: "test", Commit: "abc"},
		},
		Inventory: Inventory{
			Cameras: []InventoryCamera{{ID: "cam-1", Name: "West", EnabledOnBridge: true}},
		},
	})
	if err != nil {
		t.Fatalf("PostHealth: %v", err)
	}
	if healthBody.Host.Build.Version != "test" {
		t.Fatalf("health build version = %q", healthBody.Host.Build.Version)
	}
	if len(healthBody.Inventory.Cameras) != 1 {
		t.Fatalf("inventory cameras = %d", len(healthBody.Inventory.Cameras))
	}
}

func TestClientBootstrapRejectsMissingData(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true})
	}))
	defer srv.Close()
	client, err := NewClient(ClientConfig{BaseURL: srv.URL, APIKey: testAPIKey(), HTTPClient: srv.Client()})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Bootstrap(context.Background()); err == nil {
		t.Fatal("expected error for missing data")
	}
}

func TestClientUnauthorized(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":"INVALID_API_KEY"}}`))
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:    srv.URL,
		APIKey:     testAPIKey(),
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.Bootstrap(context.Background())
	if !IsUnauthorized(err) {
		t.Fatalf("expected unauthorized, got %v", err)
	}
}

func TestClientRejectsHTTPBaseURL(t *testing.T) {
	_, err := NewClient(ClientConfig{
		BaseURL: "http://example.com",
		APIKey:  testAPIKey(),
	})
	if err == nil {
		t.Fatal("expected error for http base URL")
	}
}

func TestClientPostWeather(t *testing.T) {
	var got WeatherRequest
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client, err := NewClient(ClientConfig{
		BaseURL:    srv.URL,
		APIKey:     testAPIKey(),
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = client.PostWeather(context.Background(), WeatherRequest{
		ObservedAt: time.Now().UTC(),
		SourceID:   "station-1",
		Provider:   "davis_weatherlink_live",
		ProviderMeta: map[string]interface{}{
			"api": "weatherlink_live_local_v1",
			"raw": map[string]interface{}{"did": "x", "ts": 1},
		},
	})
	if err != nil {
		t.Fatalf("PostWeather: %v", err)
	}
	if got.SourceID != "station-1" {
		t.Fatalf("unexpected weather body: %+v", got)
	}
	if got.Sample != nil {
		t.Fatalf("sample should be omitted, got %+v", got.Sample)
	}
	if got.ProviderMeta["api"] != "weatherlink_live_local_v1" {
		t.Fatalf("provider_meta = %+v", got.ProviderMeta)
	}
}

func TestClientServerError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	client, err := NewClient(ClientConfig{
		BaseURL:    srv.URL,
		APIKey:     testAPIKey(),
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = client.PostHealth(context.Background(), HealthRequest{Host: HostHealth{Build: BuildInfo{Version: "t"}}})
	if StatusCode(err) != http.StatusInternalServerError {
		t.Fatalf("status = %d err=%v", StatusCode(err), err)
	}
}
