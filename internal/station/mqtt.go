package station

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
	paho "github.com/eclipse/paho.mqtt.golang"
)

const mqttAPIMeta = "mqtt_v1"

// mqttQoS is the subscription QoS ceiling. Weather feeds tolerate re-delivery
// but not loss, so we accept up to at-least-once.
const mqttQoS = 1

// mqttClientIDPrefix keeps bridge client ids recognizable on the broker.
const mqttClientIDPrefix = "aviationwx-bridge-"

// mqttDisconnectQuiesceMs is the graceful disconnect quiesce window.
const mqttDisconnectQuiesceMs = 250

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

// mqttClientOptions builds paho options from a station config. Isolated so the
// wiring (broker, credentials, client id) is testable without a live broker.
func mqttClientOptions(cfg config.Station) *paho.ClientOptions {
	opts := paho.NewClientOptions()
	opts.AddBroker(cfg.BrokerURL)
	opts.SetClientID(mqttClientIDPrefix + cfg.ID)
	opts.SetCleanSession(true)
	opts.SetAutoReconnect(true)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}
	return opts
}

// MQTTSource holds an MQTT subscription to one broker topic and forwards raw
// publishes to handle. It owns no observation or POST logic; the manager wires
// those in through handle.
type MQTTSource struct {
	cfg    config.Station
	client paho.Client
	handle func(topic string, payload []byte, receivedAt time.Time)
}

func newMQTTSource(cfg config.Station, handle func(topic string, payload []byte, receivedAt time.Time)) *MQTTSource {
	return &MQTTSource{cfg: cfg, handle: handle}
}

// Start connects and subscribes. paho re-subscribes on reconnect via the
// OnConnect handler, so Start is safe to call when the broker is not yet up.
func (s *MQTTSource) Start() error {
	opts := mqttClientOptions(s.cfg)
	opts.SetOnConnectHandler(func(c paho.Client) {
		// Subscribe on every connect, including reconnect after a drop.
		c.Subscribe(s.cfg.Topic, mqttQoS, nil)
	})
	opts.SetDefaultPublishHandler(func(_ paho.Client, m paho.Message) {
		s.handle(m.Topic(), m.Payload(), time.Now())
	})
	s.client = paho.NewClient(opts)
	tok := s.client.Connect()
	tok.Wait()
	return tok.Error()
}

// Stop disconnects gracefully. Safe to call on a source that never connected.
func (s *MQTTSource) Stop() {
	if s.client != nil && s.client.IsConnected() {
		s.client.Disconnect(mqttDisconnectQuiesceMs)
	}
}
