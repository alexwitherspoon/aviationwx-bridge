package station

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

const tempestAPIMeta = "weatherflow_tempest_udp_v1"

// buildTempestObservation maps a WeatherFlow Tempest UDP datagram into an
// Observation. observed_at is the packet's own observation epoch (index 0 of
// obs/ob/evt), never the bridge clock; datagrams without an observation time
// (device_status / hub_status) are rejected rather than posted.
func buildTempestObservation(cfg config.Station, raw json.RawMessage) (*Observation, error) {
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return nil, fmt.Errorf("tempest datagram is not a JSON object: %w", err)
	}
	if rawMap == nil {
		return nil, fmt.Errorf("tempest datagram is JSON null, want an object")
	}
	epoch, ok := tempestEpoch(raw)
	if !ok {
		return nil, fmt.Errorf("tempest datagram has no observation time")
	}
	return &Observation{
		SourceID:   cfg.ID,
		Provider:   ProviderTempestUDP,
		ObservedAt: time.Unix(epoch, 0).UTC(),
		ProviderMeta: map[string]interface{}{
			"api": tempestAPIMeta,
			"raw": rawMap,
		},
	}, nil
}

// tempestEpoch extracts the observation epoch. Observation messages embed it at
// obs[0][0] (obs_st/obs_air/obs_sky), ob[0] (rapid_wind), or evt[0] (events).
func tempestEpoch(raw json.RawMessage) (int64, bool) {
	var top struct {
		Obs [][]interface{} `json:"obs"`
		Ob  []interface{}   `json:"ob"`
		Evt []interface{}   `json:"evt"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return 0, false
	}
	var v interface{}
	switch {
	case len(top.Obs) > 0 && len(top.Obs[0]) > 0:
		v = top.Obs[0][0]
	case len(top.Ob) > 0:
		v = top.Ob[0]
	case len(top.Evt) > 0:
		v = top.Evt[0]
	default:
		return 0, false
	}
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return int64(f), true
}
