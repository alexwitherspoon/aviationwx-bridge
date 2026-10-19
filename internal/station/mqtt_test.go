package station

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

func TestBuildMQTTObservation(t *testing.T) {
	receivedAt := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	cfg := config.Station{ID: "station-mqtt", Type: config.StationTypeMQTT}

	tests := []struct {
		name    string
		topic   string
		payload []byte
		wantErr bool
	}{
		{name: "json object", topic: "weather/station", payload: []byte(`{"temp":72.5,"humidity":40}`)},
		{name: "empty object", topic: "w/s", payload: []byte(`{}`)},
		{name: "null rejected", topic: "w/s", payload: []byte(`null`), wantErr: true},
		{name: "array rejected", topic: "w/s", payload: []byte(`[1,2]`), wantErr: true},
		{name: "scalar rejected", topic: "w/s", payload: []byte(`72.5`), wantErr: true},
		{name: "non-json rejected", topic: "w/s", payload: []byte(`temp=72.5`), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs, err := buildMQTTObservation(cfg, tt.topic, tt.payload, receivedAt)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if obs.SourceID != cfg.ID {
				t.Errorf("source id = %q", obs.SourceID)
			}
			if obs.Provider != ProviderMQTT {
				t.Errorf("provider = %q", obs.Provider)
			}
			if !obs.ObservedAt.Equal(receivedAt) {
				t.Errorf("observed_at = %v, want %v", obs.ObservedAt, receivedAt)
			}
			if obs.ProviderMeta["topic"] != tt.topic {
				t.Errorf("topic = %v", obs.ProviderMeta["topic"])
			}
			if obs.ProviderMeta["raw"] == nil {
				t.Error("expected raw preserved")
			}
		})
	}
}

func TestMQTTClientOptions(t *testing.T) {
	cfg := config.Station{
		ID:        "station-mqtt",
		Type:      config.StationTypeMQTT,
		BrokerURL: "tcp://broker.local:1883",
		Topic:     "weather/station",
		Username:  "user",
		Password:  "secret",
	}
	opts := mqttClientOptions(cfg)
	if len(opts.Servers) != 1 {
		t.Fatalf("servers = %v", opts.Servers)
	}
	if got := opts.Servers[0].Scheme; got != "tcp" {
		t.Errorf("scheme = %q", got)
	}
	if got := opts.Servers[0].Host; got != "broker.local:1883" {
		t.Errorf("host = %q", got)
	}
	const clientIDPrefix = mqttClientIDPrefix + "station-mqtt-"
	if !strings.HasPrefix(opts.ClientID, clientIDPrefix) || len(opts.ClientID) <= len(clientIDPrefix) {
		t.Errorf("client id = %q, want prefix %q plus a random suffix", opts.ClientID, clientIDPrefix)
	}
	if opts.Username != "user" || opts.Password != "secret" {
		t.Errorf("creds = %q/%q", opts.Username, opts.Password)
	}
	if !opts.CleanSession {
		t.Error("expected clean session")
	}
	if !opts.AutoReconnect {
		t.Error("expected auto reconnect")
	}

	opts = mqttClientOptions(config.Station{ID: "s2", BrokerURL: "tcp://b:1883", Topic: "t"})
	if opts.Username != "" || opts.Password != "" {
		t.Errorf("expected empty creds, got %q/%q", opts.Username, opts.Password)
	}
}

func TestBuildMQTTObservationPreservesNumberPrecision(t *testing.T) {
	cfg := config.Station{ID: "s", Type: config.StationTypeMQTT}
	obs, err := buildMQTTObservation(cfg, "t", []byte(`{"device_id":9007199254740993}`), time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(obs.ProviderMeta["raw"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "9007199254740993") {
		t.Fatalf("raw lost integer precision: %s", b)
	}
}

func TestBuildMQTTObservationRejectsOversizedPayload(t *testing.T) {
	cfg := config.Station{ID: "s", Type: config.StationTypeMQTT}
	big := make([]byte, mqttMaxPayloadBytes+1)
	for i := range big {
		big[i] = ' '
	}
	big[0] = '{'
	big[len(big)-1] = '}'
	if _, err := buildMQTTObservation(cfg, "t", big, time.Unix(0, 0)); err == nil {
		t.Fatal("expected oversized payload error")
	}
}
