package station

import (
	"net"
	"testing"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

func TestTempestStatusDatagramDoesNotDegrade(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Catch-all station (no serial filter) receives every datagram.
	if _, err := svc.AddStation(config.Station{
		ID:         "station-tempest",
		Name:       "Tempest",
		Type:       config.StationTypeTempestUDP,
		Enabled:    true,
		ListenAddr: "127.0.0.1:0",
	}); err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{configured: true}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	mgr.SyncFromConfig()
	defer mgr.Stop()

	if mgr.tempestHub == nil {
		t.Fatal("tempest hub not started")
	}
	mgr.tempestHub.handleDatagram([]byte(`{"serial_number":"AR-00004049","type":"device_status","hub_sn":"HB-00000001","timestamp":1510855923,"uptime":2189,"voltage":3.50}`))

	for _, s := range mgr.StatusSnapshot() {
		if s.ID != "station-tempest" {
			continue
		}
		if s.Degraded {
			t.Fatalf("station degraded by status-only datagram: %q", s.LastPollError)
		}
		return
	}
	t.Fatal("station-tempest missing from status")
}

func TestTempestUDPListenerPostsObservation(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := svc.AddStation(config.Station{
		ID:         "station-tempest",
		Name:       "Tempest",
		Type:       config.StationTypeTempestUDP,
		Enabled:    true,
		ListenAddr: "127.0.0.1:0",
	})
	if err != nil {
		t.Fatal(err)
	}

	poster := &fakePoster{configured: true}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	mgr.SyncFromConfig()
	defer mgr.Stop()

	if mgr.tempestHub == nil || mgr.tempestHub.localAddr() == nil {
		t.Fatal("tempest hub not started")
	}
	addr := mgr.tempestHub.localAddr()

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(`{"serial_number":"ST-00000512","type":"obs_st","hub_sn":"HB-00013030","obs":[[1588948614,0.18,0.22,0.27,144,6,1017.57,22.37,50.26,328,0.03,3,0.0,0,0,0,2.410,1]],"firmware_revision":129}`)); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		poster.mu.Lock()
		n := len(poster.posts)
		poster.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	poster.mu.Lock()
	defer poster.mu.Unlock()
	if len(poster.posts) == 0 {
		t.Fatal("expected weather POST from UDP datagram")
	}
	got := poster.posts[0]
	if got.SourceID != st.ID {
		t.Errorf("source_id = %s", got.SourceID)
	}
	if got.Provider != ProviderTempestUDP {
		t.Errorf("provider = %s", got.Provider)
	}
	if got.ObservedAt.Unix() != 1588948614 {
		t.Errorf("observed_at epoch = %d", got.ObservedAt.Unix())
	}
	if got.ProviderMeta == nil || got.ProviderMeta["raw"] == nil {
		t.Error("expected provider_meta.raw on wire")
	}
}
