package station

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

const mqttAPIMeta = "mqtt_v1"

// buildMQTTObservation maps a received MQTT publish into an Observation.
//
// observed_at is the message arrival time, not a payload field: MQTT is a
// low-latency push, so arrival is a tight upper bound on observation age. This
// is the deliberate exception to the "no bridge-clock time" rule that applies
// only to push providers, not the poll models (Davis/Ecowitt).
//
// The payload must be a JSON object; anything else is not forwardable
// station-native data and is rejected. Retained-message delivery is handled at
// subscribe time (see the subscriber), not here.
func buildMQTTObservation(cfg config.Station, topic string, payload []byte, receivedAt time.Time) (*Observation, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("mqtt payload is not a JSON object: %w", err)
	}
	return &Observation{
		SourceID:   cfg.ID,
		Provider:   ProviderMQTT,
		ObservedAt: receivedAt.UTC(),
		ProviderMeta: map[string]interface{}{
			"api":   mqttAPIMeta,
			"topic": topic,
			"raw":   raw,
		},
	}, nil
}