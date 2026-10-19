package station

import (
	"net"
	"testing"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
	paho "github.com/eclipse/paho.mqtt.golang"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

// startTestMQTTBroker runs an in-memory MQTT broker and returns its broker URL.
func startTestMQTTBroker(t *testing.T) string {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := lis.Addr().String()
	lis.Close()

	server := mqtt.New(nil)
	if err := server.AddHook(new(auth.Hook), &auth.Options{
		Ledger: &auth.Ledger{Auth: auth.AuthRules{{Remote: "*", Allow: true}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := server.AddListener(listeners.NewTCP(listeners.Config{ID: "t1", Address: addr})); err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })
	return "tcp://" + addr
}

// connectPublisher returns a connected paho publisher for the test broker.
func connectPublisher(t *testing.T, brokerURL string) paho.Client {
	t.Helper()
	opts := paho.NewClientOptions().AddBroker(brokerURL).SetClientID("test-publisher")
	pub := paho.NewClient(opts)
	tok := pub.Connect()
	tok.Wait()
	if err := tok.Error(); err != nil {
		t.Fatalf("publisher connect: %v", err)
	}
	t.Cleanup(func() { pub.Disconnect(100) })
	return pub
}

func TestMQTTSubscriberPostsObservation(t *testing.T) {
	brokerURL := startTestMQTTBroker(t)

	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := svc.AddStation(config.Station{
		ID:        "station-mqtt",
		Name:      "MQTT",
		Type:      config.StationTypeMQTT,
		Enabled:   true,
		BrokerURL: brokerURL,
		Topic:     "weather/station",
	})
	if err != nil {
		t.Fatal(err)
	}

	poster := &fakePoster{configured: true}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	mgr.SyncFromConfig()
	defer mgr.Stop()

	pub := connectPublisher(t, brokerURL)

	// The subscriber connects asynchronously, so retry the publish until a
	// POST lands (an early publish before SUBACK is dropped, which is fine).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		pub.Publish("weather/station", 0, false, `{"temp":72.5,"humidity":40}`)
		time.Sleep(100 * time.Millisecond)
		poster.mu.Lock()
		n := len(poster.posts)
		poster.mu.Unlock()
		if n > 0 {
			break
		}
	}

	poster.mu.Lock()
	defer poster.mu.Unlock()
	if len(poster.posts) == 0 {
		t.Fatal("expected weather POST from MQTT publish")
	}
	got := poster.posts[0]
	if got.SourceID != st.ID {
		t.Errorf("source_id = %s", got.SourceID)
	}
	if got.Provider != ProviderMQTT {
		t.Errorf("provider = %s", got.Provider)
	}
	if got.ObservedAt.IsZero() {
		t.Error("expected observed_at from arrival time")
	}
	if got.ProviderMeta == nil || got.ProviderMeta["raw"] == nil {
		t.Error("expected provider_meta.raw on wire")
	}
}

func TestMQTTSubscriberRejectsNonJSON(t *testing.T) {
	brokerURL := startTestMQTTBroker(t)

	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := svc.AddStation(config.Station{
		ID:        "station-mqtt",
		Name:      "MQTT",
		Type:      config.StationTypeMQTT,
		Enabled:   true,
		BrokerURL: brokerURL,
		Topic:     "weather/station",
	})
	if err != nil {
		t.Fatal(err)
	}

	poster := &fakePoster{configured: true}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	mgr.SyncFromConfig()
	defer mgr.Stop()

	pub := connectPublisher(t, brokerURL)

	deadline := time.Now().Add(5 * time.Second)
	degraded := false
	for time.Now().Before(deadline) {
		pub.Publish("weather/station", 0, false, "not-json")
		time.Sleep(100 * time.Millisecond)
		for _, s := range mgr.StatusSnapshot() {
			if s.ID == st.ID && s.Degraded {
				degraded = true
			}
		}
		if degraded {
			break
		}
	}
	if !degraded {
		t.Fatal("expected station to degrade on non-JSON payload")
	}
	poster.mu.Lock()
	defer poster.mu.Unlock()
	if len(poster.posts) != 0 {
		t.Fatalf("expected no POST for non-JSON payload, got %d", len(poster.posts))
	}
}
