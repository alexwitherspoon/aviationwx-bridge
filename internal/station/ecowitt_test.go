package station

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

func TestEcowittPollFixture(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "ecowitt_get_livedata_info.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ecowittLivedataPath {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	e := NewEcowitt()
	e.client = srv.Client()
	obs, err := e.Poll(context.Background(), config.Station{
		ID:   "station-ecowitt",
		Type: config.StationTypeEcowittGateway,
		Host: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if obs.Provider != ProviderEcowittGateway {
		t.Fatalf("provider = %q", obs.Provider)
	}
	if obs.ObservedAt.IsZero() {
		t.Fatal("expected ObservedAt from 0x18")
	}
	want := time.Date(2023, 2, 3, 14, 22, 11, 0, time.UTC)
	if !obs.ObservedAt.Equal(want) {
		t.Fatalf("ObservedAt = %v, want %v", obs.ObservedAt, want)
	}
	raw, ok := obs.ProviderMeta["raw"].(map[string]interface{})
	if !ok || raw["common_list"] == nil {
		t.Fatalf("provider_meta.raw = %#v", obs.ProviderMeta["raw"])
	}
	if obs.ProviderMeta["api"] != ecowittAPIMeta {
		t.Fatalf("api = %v", obs.ProviderMeta["api"])
	}
}

func TestEcowittPollMissingTimeSkipsObservedAt(t *testing.T) {
	body := []byte(`{"common_list":[{"id":"0x02","val":"20.0","unit":"C"}],"wh25":[{"intemp":"21.0","unit":"C","inhumi":"50%","abs":"1000 hPa","rel":"1000 hPa"}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	e := NewEcowitt()
	e.client = srv.Client()
	obs, err := e.Poll(context.Background(), config.Station{
		ID: "s1", Type: config.StationTypeEcowittGateway, Host: srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !obs.ObservedAt.IsZero() {
		t.Fatalf("ObservedAt = %v, want zero without 0x18", obs.ObservedAt)
	}
}

func TestEcowittPollRejectsNonLivedata(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":"Version: GW2000B_V2.1.4","platform":"ecowitt"}`))
	}))
	defer srv.Close()
	e := NewEcowitt()
	e.client = srv.Client()
	if _, err := e.Poll(context.Background(), config.Station{Host: srv.URL}); err == nil {
		t.Fatal("expected error for get_version-shaped body")
	}
}

func TestParseEcowittTimeVal(t *testing.T) {
	got, ok := parseEcowittTimeVal("2024-11-25T20:41:16")
	if !ok || got.Year() != 2024 {
		t.Fatalf("got %v ok=%v", got, ok)
	}
	if _, ok := parseEcowittTimeVal(""); ok {
		t.Fatal("empty must fail")
	}
}
