package station

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

// TestWeatherRequestGoldenDavisWLL locks the exact POST /v1/bridge/weather body
// shape (raw-only, no sample) derived from the Davis WLL Local API fixture.
func TestWeatherRequestGoldenDavisWLL(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "davis_current_conditions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(fixture, &top); err != nil {
		t.Fatal(err)
	}
	rawData := top["data"]
	var env davisEnvelope
	if err := json.Unmarshal(fixture, &env); err != nil || env.Data == nil {
		t.Fatalf("fixture: %v data=%v", err, env.Data)
	}

	txid := 1
	cfg := config.Station{
		ID:      "station-davis-wll",
		Name:    "Davis WLL",
		Type:    config.StationTypeDavisWeatherLinkLive,
		Enabled: true,
		Host:    "192.168.1.50",
		Txid:    &txid,
	}
	obs, err := buildDavisObservation(cfg, env.Data, rawData)
	if err != nil {
		t.Fatal(err)
	}
	req := weatherRequestFromObs(obs)
	gotBytes, err := json.MarshalIndent(req, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	gotBytes = append(gotBytes, '\n')
	if strings.Contains(string(gotBytes), `"sample"`) {
		t.Fatalf("wire JSON must omit sample; got:\n%s", gotBytes)
	}

	goldenPath := filepath.Join("..", "bridgeapi", "testdata", "weather_post_davis_wll.example.json")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, gotBytes, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}

	wantBytes, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden (run UPDATE_GOLDEN=1 to create): %v", err)
	}
	var want, got interface{}
	if err := json.Unmarshal(wantBytes, &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(gotBytes, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("weather POST wire drift.\nwant:\n%s\ngot:\n%s", wantBytes, gotBytes)
	}
}
