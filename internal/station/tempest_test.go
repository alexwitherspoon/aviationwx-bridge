package station

import (
	"testing"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

func TestBuildTempestObservation(t *testing.T) {
	cfg := config.Station{ID: "station-tempest", Type: config.StationTypeTempestUDP}

	tests := []struct {
		name    string
		payload string
		wantE   int64
		wantErr bool
	}{
		{
			name:    "obs_st",
			payload: `{"serial_number":"ST-00000512","type":"obs_st","hub_sn":"HB-00013030","obs":[[1588948614,0.18,0.22,0.27,144,6,1017.57,22.37,50.26,328,0.03,3,0.0,0,0,0,2.410,1]],"firmware_revision":129}`,
			wantE:   1588948614,
		},
		{
			name:    "obs_sky",
			payload: `{"serial_number":"SK-00008453","type":"obs_sky","hub_sn":"HB-00000001","obs":[[1493321340,9000,10,0.0,2.6,4.6,7.4,187,3.12,1,130,null,0,3]],"firmware_revision":29}`,
			wantE:   1493321340,
		},
		{
			name:    "rapid_wind",
			payload: `{"serial_number":"SK-00008453","type":"rapid_wind","hub_sn":"HB-00000001","ob":[1493322445,2.3,128]}`,
			wantE:   1493322445,
		},
		{
			name:    "evt_precip",
			payload: `{"serial_number":"SK-00008453","type":"evt_precip","hub_sn":"HB-00000001","evt":[1493322445]}`,
			wantE:   1493322445,
		},
		{
			name:    "device_status skipped",
			payload: `{"serial_number":"AR-00004049","type":"device_status","hub_sn":"HB-00000001","timestamp":1510855923,"uptime":2189,"voltage":3.50}`,
			wantErr: true,
		},
		{
			name:    "non-json rejected",
			payload: `not-json`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obs, err := buildTempestObservation(cfg, []byte(tt.payload))
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if obs.Provider != ProviderTempestUDP {
				t.Errorf("provider = %q", obs.Provider)
			}
			if obs.ObservedAt.Unix() != tt.wantE {
				t.Errorf("observed_at epoch = %d, want %d", obs.ObservedAt.Unix(), tt.wantE)
			}
			if obs.ProviderMeta["raw"] == nil {
				t.Error("expected raw preserved")
			}
		})
	}
}

func TestTempestObservedAtIsStationEpoch(t *testing.T) {
	cfg := config.Station{ID: "s", Type: config.StationTypeTempestUDP}
	obs, err := buildTempestObservation(cfg, []byte(`{"type":"obs_st","obs":[[1600000000,1.0]]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := time.Unix(1600000000, 0).UTC()
	if !obs.ObservedAt.Equal(want) {
		t.Fatalf("observed_at = %v, want %v", obs.ObservedAt, want)
	}
}
