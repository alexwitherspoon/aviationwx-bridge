package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

func TestStationsCRUD(t *testing.T) {
	server := testServerWithAuth(t, ServerConfig{})
	pass := server.configService.GetWebPassword()

	t.Run("list empty", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/stations", nil)
		req.SetBasicAuth("admin", pass)
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d", w.Code)
		}
		var list []map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		if len(list) != 0 {
			t.Fatalf("len = %d", len(list))
		}
	})

	var createdID string
	t.Run("create", func(t *testing.T) {
		body := map[string]interface{}{
			"name":                  "Scappoose Davis",
			"type":                  config.StationTypeDavisWeatherLinkLive,
			"enabled":               true,
			"host":                  "192.168.1.50",
			"poll_interval_seconds": 10,
			"txid":                  1,
		}
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/stations", bytes.NewReader(raw))
		req.SetBasicAuth("admin", pass)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
		var got map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		createdID, _ = got["id"].(string)
		if createdID == "" {
			t.Fatal("missing id")
		}
		if got["host"] != "192.168.1.50" {
			t.Fatalf("host = %v", got["host"])
		}
	})

	t.Run("update", func(t *testing.T) {
		body := map[string]interface{}{
			"name":                  "Scappoose Davis North",
			"type":                  config.StationTypeDavisWeatherLinkLive,
			"enabled":               false,
			"host":                  "192.168.1.51",
			"poll_interval_seconds": 20,
			"txid":                  2,
		}
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/api/stations/"+createdID, bytes.NewReader(raw))
		req.SetBasicAuth("admin", pass)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
		st, err := server.configService.GetStation(createdID)
		if err != nil {
			t.Fatal(err)
		}
		if st.Host != "192.168.1.51" || st.Enabled || st.Txid == nil || *st.Txid != 2 {
			t.Fatalf("updated = %+v", st)
		}
	})

	t.Run("update omits txid keeps transmitter", func(t *testing.T) {
		body := map[string]interface{}{
			"name": "Scappoose Davis Renamed",
			"host": "192.168.1.51",
		}
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPut, "/api/stations/"+createdID, bytes.NewReader(raw))
		req.SetBasicAuth("admin", pass)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", w.Code, w.Body.String())
		}
		st, err := server.configService.GetStation(createdID)
		if err != nil {
			t.Fatal(err)
		}
		if st.Name != "Scappoose Davis Renamed" || st.Txid == nil || *st.Txid != 2 {
			t.Fatalf("txid should be preserved: %+v", st)
		}
	})

	t.Run("delete", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/api/stations/"+createdID, nil)
		req.SetBasicAuth("admin", pass)
		w := httptest.NewRecorder()
		server.GetMux().ServeHTTP(w, req)
		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d", w.Code)
		}
		if _, err := server.configService.GetStation(createdID); err == nil {
			t.Fatal("expected not found after delete")
		}
	})
}

func TestStationsCreateRejectsFastPoll(t *testing.T) {
	server := testServerWithAuth(t, ServerConfig{})
	pass := server.configService.GetWebPassword()
	body := map[string]interface{}{
		"name":                  "Fast",
		"type":                  config.StationTypeDavisWeatherLinkLive,
		"enabled":               true,
		"host":                  "192.168.1.50",
		"poll_interval_seconds": 5,
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/stations", bytes.NewReader(raw))
	req.SetBasicAuth("admin", pass)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.GetMux().ServeHTTP(w, req)
	if w.Code == http.StatusCreated {
		t.Fatal("expected rejection for poll < 10s")
	}
}
