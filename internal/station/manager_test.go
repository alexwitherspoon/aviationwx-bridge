package station

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/bridgeapi"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

type fakePoster struct {
	mu         sync.Mutex
	configured bool
	posts      []bridgeapi.WeatherRequest
	failN      int
	calls      int
}

func (f *fakePoster) APIConfigured() bool { return f.configured }

func (f *fakePoster) PostWeather(ctx context.Context, req bridgeapi.WeatherRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failN > 0 {
		f.failN--
		return errors.New("post failed")
	}
	f.posts = append(f.posts, req)
	return nil
}

func TestManagerPollAndPost(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "davis_current_conditions.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	txid := 1
	st, err := svc.AddStation(config.Station{
		ID:                  "station-davis",
		Name:                "Davis",
		Type:                config.StationTypeDavisWeatherLinkLive,
		Enabled:             true,
		Host:                srv.Listener.Addr().String(),
		PollIntervalSeconds: 10,
		Txid:                &txid,
	})
	if err != nil {
		t.Fatal(err)
	}

	poster := &fakePoster{configured: true}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	// Inject Davis client that hits httptest (host is addr without scheme).
	davis := NewDavis()
	davis.client = srv.Client()
	mgr.providers[ProviderDavisWeatherLinkLive] = davis

	mgr.SyncFromConfig()
	defer mgr.Stop()

	deadline := time.Now().Add(2 * time.Second)
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
	if len(poster.posts) == 0 {
		poster.mu.Unlock()
		t.Fatal("expected weather POST")
	}
	got := poster.posts[0]
	if got.SourceID != st.ID {
		t.Errorf("source_id = %s", got.SourceID)
	}
	if got.Provider != ProviderDavisWeatherLinkLive {
		t.Errorf("provider = %s", got.Provider)
	}
	if got.Sample != nil {
		t.Errorf("sample should be omitted on wire, got %+v", got.Sample)
	}
	if got.ProviderMeta == nil || got.ProviderMeta["raw"] == nil {
		t.Error("expected provider_meta.raw on wire")
	}
	if _, err := json.Marshal(got); err != nil {
		t.Fatal(err)
	}
	poster.mu.Unlock()

	sub, ok := mgr.WeatherSubsystemHealth()
	if !ok || sub.Status != bridgeapi.StatusOperational {
		t.Fatalf("weather subsystem = %+v ok=%v", sub, ok)
	}

	deadline = time.Now().Add(2 * time.Second)
	var payloads []PayloadLogEntry
	for time.Now().Before(deadline) {
		payloads = mgr.RecentPayloads()
		if len(payloads) > 0 && payloads[0].Raw != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(payloads) == 0 || payloads[0].Raw == nil {
		t.Fatal("expected raw payload log entry")
	}
	if !payloads[0].LANOK {
		t.Fatal("expected lan_ok on payload log")
	}
	if payloads[0].ObservedAt.IsZero() {
		t.Fatal("expected observed_at on payload log")
	}
}

func TestOutboundRingDropOldest(t *testing.T) {
	r := newOutboundRing()
	for i := 0; i < MaxOutboundWeatherSamples+5; i++ {
		r.Push(bridgeapi.WeatherRequest{SourceID: string(rune('a' + (i % 26)))})
	}
	if r.Len() != MaxOutboundWeatherSamples {
		t.Fatalf("len = %d", r.Len())
	}
}

func TestManagerQueuesWhenPostFails(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "davis_current_conditions.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	txid := 1
	if _, err := svc.AddStation(config.Station{
		ID:                  "station-davis",
		Name:                "Davis",
		Type:                config.StationTypeDavisWeatherLinkLive,
		Enabled:             true,
		Host:                srv.Listener.Addr().String(),
		PollIntervalSeconds: 10,
		Txid:                &txid,
	}); err != nil {
		t.Fatal(err)
	}

	poster := &fakePoster{configured: true, failN: 1}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	davis := NewDavis()
	davis.client = srv.Client()
	mgr.providers[ProviderDavisWeatherLinkLive] = davis
	mgr.SyncFromConfig()
	defer mgr.Stop()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.ring.Len() > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("expected outbound ring entry after post failure")
}

func TestManagerSkipsPostWhenTimestampMissing(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "davis_missing_ts.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	txid := 1
	if _, err := svc.AddStation(config.Station{
		ID:                  "station-davis",
		Name:                "Davis",
		Type:                config.StationTypeDavisWeatherLinkLive,
		Enabled:             true,
		Host:                srv.Listener.Addr().String(),
		PollIntervalSeconds: 10,
		Txid:                &txid,
	}); err != nil {
		t.Fatal(err)
	}

	poster := &fakePoster{configured: true}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	davis := NewDavis()
	davis.client = srv.Client()
	mgr.providers[ProviderDavisWeatherLinkLive] = davis
	mgr.SyncFromConfig()
	defer mgr.Stop()

	deadline := time.Now().Add(2 * time.Second)
	var statuses []StationStatus
	for time.Now().Before(deadline) {
		statuses = mgr.StatusSnapshot()
		if len(statuses) == 1 && statuses[0].Degraded {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(statuses) != 1 || !statuses[0].Degraded {
		t.Fatalf("status = %+v, want degraded", statuses)
	}
	if !statuses[0].LANOK {
		t.Fatal("LAN should still be OK (device responded)")
	}
	if poster.calls != 0 {
		t.Fatalf("posts = %d, want 0 when ts missing", poster.calls)
	}
	sub, ok := mgr.WeatherSubsystemHealth()
	if !ok || sub.Status != bridgeapi.StatusDegraded {
		t.Fatalf("weather subsystem = %+v ok=%v", sub, ok)
	}
}

func TestManagerNoPostWithoutAPI(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "davis_current_conditions.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	txid := 1
	if _, err := svc.AddStation(config.Station{
		ID:      "station-davis",
		Name:    "Davis",
		Type:    config.StationTypeDavisWeatherLinkLive,
		Enabled: true,
		Host:    srv.Listener.Addr().String(),
		Txid:    &txid,
	}); err != nil {
		t.Fatal(err)
	}

	poster := &fakePoster{configured: false}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	davis := NewDavis()
	davis.client = srv.Client()
	mgr.providers[ProviderDavisWeatherLinkLive] = davis
	mgr.SyncFromConfig()
	defer mgr.Stop()

	time.Sleep(100 * time.Millisecond)
	if poster.calls != 0 {
		t.Fatalf("posts = %d, want 0 without API", poster.calls)
	}
	statuses := mgr.StatusSnapshot()
	if len(statuses) != 1 || !statuses[0].LANOK {
		// May still be racing first poll
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			statuses = mgr.StatusSnapshot()
			if len(statuses) == 1 && statuses[0].LANOK {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("status = %+v", statuses)
	}
}
