package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidateAPISettings(t *testing.T) {
	if err := ValidateAPISettings(nil); err != nil {
		t.Fatalf("nil: %v", err)
	}
	if err := ValidateAPISettings(&APISettings{Enabled: false}); err != nil {
		t.Fatalf("disabled: %v", err)
	}
	if err := ValidateAPISettings(&APISettings{Enabled: true}); err == nil {
		t.Fatal("expected error for missing key")
	}
	if err := ValidateAPISettings(&APISettings{Enabled: true, Key: "not-a-real-key"}); err == nil {
		t.Fatal("expected error for bad prefix")
	}
	if err := ValidateAPISettings(&APISettings{Enabled: true, Key: "awxb_short"}); err == nil {
		t.Fatal("expected error for short key")
	}
	if err := ValidateAPISettings(&APISettings{Enabled: true, Key: "awxb_" + strings.Repeat("a", 49)}); err == nil {
		t.Fatal("expected error for long secret")
	}
	okKey := "awxb_" + strings.Repeat("a", 48)
	ok := &APISettings{Enabled: true, Key: okKey}
	if err := ValidateAPISettings(ok); err != nil {
		t.Fatalf("valid: %v", err)
	}
	if !ValidAPIKeyShape(okKey) {
		t.Fatal("ValidAPIKeyShape")
	}
	if err := ValidateAPISettings(&APISettings{
		Enabled: true,
		Key:     okKey,
		BaseURL: "http://localhost",
	}); err == nil {
		t.Fatal("expected https requirement")
	}
	if EffectiveAPIBaseURL(nil) != DefaultAPIBaseURL {
		t.Fatalf("default base URL")
	}
	if !APIConfigured(ok) {
		t.Fatal("expected configured")
	}
}

func TestStationCRUD(t *testing.T) {
	dir := t.TempDir()
	svc, err := NewService(dir)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	txid := 1
	st, err := svc.AddStation(Station{
		Name:    "Davis WLL",
		Type:    StationTypeDavisWeatherLinkLive,
		Enabled: true,
		Host:    "192.168.1.50",
		Txid:    &txid,
	})
	if err != nil {
		t.Fatalf("AddStation: %v", err)
	}
	if st.ID == "" || st.PollIntervalSeconds != DefaultDavisPollIntervalSeconds {
		t.Fatalf("defaults not applied: %+v", st)
	}

	list := svc.ListStations()
	if len(list) != 1 {
		t.Fatalf("list len = %d", len(list))
	}

	err = svc.UpdateStation(st.ID, func(s *Station) error {
		s.Host = "192.168.1.51"
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateStation: %v", err)
	}
	got, err := svc.GetStation(st.ID)
	if err != nil || got.Host != "192.168.1.51" {
		t.Fatalf("GetStation: %+v err=%v", got, err)
	}

	if err := svc.DeleteStation(st.ID); err != nil {
		t.Fatalf("DeleteStation: %v", err)
	}
	if len(svc.ListStations()) != 0 {
		t.Fatal("expected empty list")
	}
}

func TestStationRejectsFastDavisPoll(t *testing.T) {
	err := ValidateStation(Station{
		ID:                  "st-1",
		Name:                "Davis",
		Type:                StationTypeDavisWeatherLinkLive,
		Host:                "10.0.0.1",
		PollIntervalSeconds: 5,
	})
	if err == nil {
		t.Fatal("expected poll interval error")
	}
}

func TestAPIUpdateGlobalValidates(t *testing.T) {
	dir := t.TempDir()
	svc, err := NewService(dir)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	err = svc.UpdateGlobal(func(g *GlobalSettings) error {
		g.API = &APISettings{Enabled: true, Key: "bad"}
		return nil
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	err = svc.UpdateGlobal(func(g *GlobalSettings) error {
		g.API = &APISettings{Enabled: true, Key: "awxb_" + strings.Repeat("b", 48)}
		return nil
	})
	if err != nil {
		t.Fatalf("UpdateGlobal: %v", err)
	}
	if !APIConfigured(svc.GetGlobal().API) {
		t.Fatal("api not configured after update")
	}
}

func TestStationUnmarshalEnabledDefault(t *testing.T) {
	var st Station
	if err := json.Unmarshal([]byte(`{"id":"s1","name":"N","type":"davis_weatherlink_live","host":"1.2.3.4"}`), &st); err != nil {
		t.Fatal(err)
	}
	if !st.Enabled {
		t.Fatal("omitted enabled should default true")
	}
	if err := json.Unmarshal([]byte(`{"id":"s2","name":"N","type":"davis_weatherlink_live","host":"1.2.3.4","enabled":false}`), &st); err != nil {
		t.Fatal(err)
	}
	if st.Enabled {
		t.Fatal("explicit false must stick")
	}
}

func TestValidateEcowittStation(t *testing.T) {
	st := Station{
		ID:   "station-ecowitt",
		Name: "Ecowitt",
		Type: StationTypeEcowittGateway,
		Host: "192.168.1.60",
	}
	NormalizeStationDefaults(&st)
	if err := ValidateStation(st); err != nil {
		t.Fatal(err)
	}
	st.PollIntervalSeconds = 5
	if err := ValidateStation(st); err == nil {
		t.Fatal("expected poll floor error")
	}
}
