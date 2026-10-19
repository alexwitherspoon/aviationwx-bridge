package station

import (
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