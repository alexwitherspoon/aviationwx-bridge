package station

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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

// mqttConnectRetryInterval paces background reconnects when the broker is not
// yet reachable at startup.
const mqttConnectRetryInterval = 5 * time.Second

// mqttTestConnectTimeout bounds the console Test broker connect attempt.
const mqttTestConnectTimeout = 5 * time.Second

// mqttTestCaptureTimeout bounds how long the console Test waits for a message.
const mqttTestCaptureTimeout = 3 * time.Second

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
	if raw == nil {
		return nil, fmt.Errorf("mqtt payload is JSON null, want an object")
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
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(mqttConnectRetryInterval)
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
		if m.Retained() {
			return
		}
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

// TestMQTTSubscribe connects to the configured broker, subscribes, and returns
// the first received observation, or an error if none arrives before the
// capture timeout. It is a one-shot console Test, not a live source.
func (m *Manager) TestMQTTSubscribe(ctx context.Context, st config.Station) (*Observation, error) {
	config.NormalizeStationDefaults(&st)
	if st.Type != config.StationTypeMQTT {
		return nil, fmt.Errorf("station type is not mqtt")
	}
	if err := config.ValidateStation(st); err != nil {
		return nil, err
	}

	type result struct {
		obs *Observation
		err error
	}
	ch := make(chan result, 1)

	opts := mqttClientOptions(st)
	// Test is a single bounded attempt, not the retrying live source.
	opts.SetConnectRetry(false)
	opts.SetConnectTimeout(mqttTestConnectTimeout)
	opts.SetDefaultPublishHandler(func(_ paho.Client, msg paho.Message) {
		if msg.Retained() {
			return
		}
		obs, err := buildMQTTObservation(st, msg.Topic(), msg.Payload(), time.Now())
		select {
		case ch <- result{obs, err}:
		default:
		}
	})

	client := paho.NewClient(opts)
	if tok := client.Connect(); tok.Wait() && tok.Error() != nil {
		return nil, fmt.Errorf("mqtt connect: %w", tok.Error())
	}
	defer client.Disconnect(mqttDisconnectQuiesceMs)
	if tok := client.Subscribe(st.Topic, mqttQoS, nil); tok.Wait() && tok.Error() != nil {
		return nil, fmt.Errorf("mqtt subscribe: %w", tok.Error())
	}

	select {
	case r := <-ch:
		return r.obs, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(mqttTestCaptureTimeout):
		return nil, fmt.Errorf("no message received on %q within %s (is the station publishing?)", st.Topic, mqttTestCaptureTimeout)
	}
}

// shouldRunMQTTStation reports whether SyncFromConfig should keep an MQTT
// subscription for st.
func shouldRunMQTTStation(st config.Station) bool {
	if !st.Enabled || st.Type != config.StationTypeMQTT {
		return false
	}
	return strings.TrimSpace(st.BrokerURL) != "" && strings.TrimSpace(st.Topic) != ""
}

// sameMQTTConfig reports whether two station configs need no reconnect.
func sameMQTTConfig(a, b config.Station) bool {
	return a.BrokerURL == b.BrokerURL &&
		a.Topic == b.Topic &&
		a.Username == b.Username &&
		a.Password == b.Password
}

// syncMQTTSources starts, restarts, or stops MQTT broker subscriptions in step
// with the configured stations. Start runs in a goroutine so an unreachable
// broker never blocks config sync; paho retries in the background and
// re-subscribes on every connect.
func (m *Manager) syncMQTTSources(wanted map[string]config.Station) {
	var toStop []*MQTTSource

	m.mu.Lock()
	for id, src := range m.mqttSources {
		st, ok := wanted[id]
		if !ok || !shouldRunMQTTStation(st) || !sameMQTTConfig(src.cfg, st) {
			toStop = append(toStop, src)
			delete(m.mqttSources, id)
			delete(m.mqttLastEmit, id)
		}
	}
	for id, st := range wanted {
		if !shouldRunMQTTStation(st) {
			continue
		}
		if _, running := m.mqttSources[id]; running {
			continue
		}
		src := newMQTTSource(st, m.mqttHandler(st))
		m.mqttSources[id] = src
		go func(id string, src *MQTTSource) {
			if err := src.Start(); err != nil {
				m.log.Warn("mqtt connect failed", "station", id, "error", err)
			}
		}(id, src)
		m.log.Info("mqtt subscribed", "station", id, "broker", st.BrokerURL, "topic", st.Topic)
	}
	m.mu.Unlock()

	for _, src := range toStop {
		src.Stop()
	}
}

// mqttHandler returns the deliver callback for a station's subscription. It
// thins to the global emit ceiling and forwards each fresh publish.
func (m *Manager) mqttHandler(st config.Station) func(topic string, payload []byte, receivedAt time.Time) {
	return func(topic string, payload []byte, receivedAt time.Time) {
		now := receivedAt.UTC()

		// Thin to ≤1 Hz per station: a misbehaving publisher must not flood the
		// bridge or the core.
		m.mu.Lock()
		if last, ok := m.mqttLastEmit[st.ID]; ok && now.Sub(last) < GlobalMinPollInterval {
			m.mu.Unlock()
			return
		}
		m.mqttLastEmit[st.ID] = now
		m.mu.Unlock()

		obs, err := buildMQTTObservation(st, topic, payload, receivedAt)
		if err != nil {
			m.setStatus(st.ID, func(s *StationStatus) {
				s.LANOK = true
				s.Degraded = true
				s.LastPollAt = now
				s.LastPollError = err.Error()
			})
			m.log.Warn("mqtt payload rejected", "station", st.ID, "error", err)
			return
		}
		m.deliverMQTTObservation(st, obs, now)
	}
}

// deliverMQTTObservation records status and posts a received observation,
// mirroring the interceptor live-ingest path.
func (m *Manager) deliverMQTTObservation(st config.Station, obs *Observation, now time.Time) {
	entry := PayloadLogEntry{
		At:         now,
		ObservedAt: obs.ObservedAt,
		StationID:  st.ID,
		LANOK:      true,
	}
	if obs.ProviderMeta != nil {
		entry.Raw = obs.ProviderMeta["raw"]
	}

	m.setStatus(st.ID, func(s *StationStatus) {
		s.LANOK = true
		s.Degraded = false
		s.WaitingForTxid = false
		s.LastPollAt = now
		s.LastPollError = ""
		s.LastObservedAt = obs.ObservedAt
	})

	if m.poster != nil && m.poster.APIConfigured() {
		ctx, cancel := context.WithTimeout(context.Background(), weatherPostTimeout)
		postErr := m.postObservation(ctx, st, obs)
		cancel()
		posted := postErr == nil
		entry.Posted = &posted
		if postErr != nil {
			entry.Message = postErr.Error()
		}
	}
	m.notePayload(entry)
}
