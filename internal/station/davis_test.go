package station

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

func TestDavisPollFixture(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "davis_current_conditions.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/current_conditions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	davis := NewDavis()
	davis.client = srv.Client()
	txid := 1
	cfg := config.Station{
		ID:            "station-test",
		Name:          "Test",
		Type:          config.StationTypeDavisWeatherLinkLive,
		Enabled:       true,
		Host:          srv.URL,
		WindReference: config.WindReferenceTrue,
		Txid:          &txid,
	}

	obs, err := davis.Poll(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if obs.DID != "001D0A700002" {
		t.Errorf("DID = %q", obs.DID)
	}
	if obs.ProviderMeta["txid"] != 1 {
		t.Errorf("provider_meta txid = %v", obs.ProviderMeta["txid"])
	}
	if obs.ProviderMeta["api"] != "weatherlink_live_local_v1" {
		t.Errorf("provider_meta api = %v", obs.ProviderMeta["api"])
	}
	raw, ok := obs.ProviderMeta["raw"].(map[string]interface{})
	if !ok {
		t.Fatalf("provider_meta.raw type = %T", obs.ProviderMeta["raw"])
	}
	conds, ok := raw["conditions"].([]interface{})
	if !ok || len(conds) == 0 {
		t.Fatalf("raw.conditions = %#v", raw["conditions"])
	}
	iss0, ok := conds[0].(map[string]interface{})
	if !ok {
		t.Fatalf("raw condition[0] = %#v", conds[0])
	}
	if temp, ok := iss0["temp"].(float64); !ok || math.Abs(temp-62.7) > 1e-9 {
		t.Errorf("raw ISS temp (native °F) = %v, want 62.7", iss0["temp"])
	}
	if spd, ok := iss0["wind_speed_last"].(float64); !ok || math.Abs(spd-10) > 1e-9 {
		t.Errorf("raw wind_speed_last (mph) = %v, want 10", iss0["wind_speed_last"])
	}
	if len(obs.Transmitters) < 1 || obs.Transmitters[0].Txid != 1 {
		t.Errorf("transmitters = %+v", obs.Transmitters)
	}
}

func TestDavisPollWithoutTxidListsTransmitters(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "davis_current_conditions.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	davis := NewDavis()
	davis.client = srv.Client()
	cfg := config.Station{
		ID:   "station-test",
		Name: "Test",
		Type: config.StationTypeDavisWeatherLinkLive,
		Host: srv.Listener.Addr().String(),
	}

	obs, err := davis.Poll(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(obs.Transmitters) == 0 {
		t.Fatal("expected transmitters for ISS pick")
	}
}

func TestDavisMissingTxidError(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "davis_current_conditions.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	davis := NewDavis()
	davis.client = srv.Client()
	bad := 99
	cfg := config.Station{
		ID:   "station-test",
		Type: config.StationTypeDavisWeatherLinkLive,
		Host: srv.Listener.Addr().String(),
		Txid: &bad,
	}
	_, err = davis.Poll(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for missing txid")
	}
}

func TestDavisMissingTimestampLeavesObservedAtZero(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "davis_missing_ts.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	davis := NewDavis()
	davis.client = srv.Client()
	txid := 1
	obs, err := davis.Poll(context.Background(), config.Station{
		ID:   "station-test",
		Type: config.StationTypeDavisWeatherLinkLive,
		Host: srv.Listener.Addr().String(),
		Txid: &txid,
	})
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if !obs.ObservedAt.IsZero() {
		t.Fatalf("ObservedAt = %v, want zero when ts missing", obs.ObservedAt)
	}
	if obs.ProviderMeta["raw"] == nil {
		t.Fatal("expected raw even when ts missing")
	}
}

func TestDavisCurrentConditionsURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"192.168.1.50", "http://192.168.1.50:80/v1/current_conditions"},
		{"192.168.1.50:8080", "http://192.168.1.50:8080/v1/current_conditions"},
		{"http://example.local:8080/ignored", "http://example.local:8080/v1/current_conditions"},
		{"2001:db8::1", "http://[2001:db8::1]:80/v1/current_conditions"},
		{"[2001:db8::1]", "http://[2001:db8::1]:80/v1/current_conditions"},
		{"[2001:db8::1]:8080", "http://[2001:db8::1]:8080/v1/current_conditions"},
		{"http://[2001:db8::2]/other", "http://[2001:db8::2]/v1/current_conditions"},
	}
	for _, tc := range cases {
		u, err := davisCurrentConditionsURL(tc.in)
		if err != nil {
			t.Fatalf("%q: %v", tc.in, err)
		}
		if u != tc.want {
			t.Fatalf("%q: got %s want %s", tc.in, u, tc.want)
		}
	}
}
