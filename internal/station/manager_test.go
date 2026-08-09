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

func TestFlushRingPacesCatchupOnePerSecondPerSource(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{configured: true}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	defer mgr.Stop()

	now := time.Now().UTC()
	mgr.postMu.Lock()
	for i := 0; i < 3; i++ {
		mgr.ring.Push(bridgeapi.WeatherRequest{
			SourceID:   "src-a",
			ObservedAt: now.Add(-time.Duration(i) * time.Second),
			Provider:   "test",
		})
	}

	ctx := context.Background()
	mgr.flushRing(ctx)
	mgr.postMu.Unlock()

	poster.mu.Lock()
	n := len(poster.posts)
	poster.mu.Unlock()
	if n != 1 {
		t.Fatalf("after first flush posts = %d, want 1", n)
	}
	if mgr.ring.Len() != 2 {
		t.Fatalf("ring len = %d, want 2", mgr.ring.Len())
	}

	mgr.postMu.Lock()
	mgr.flushRing(ctx)
	nRing := mgr.ring.Len()
	mgr.postMu.Unlock()
	poster.mu.Lock()
	n = len(poster.posts)
	poster.mu.Unlock()
	if n != 1 {
		t.Fatalf("second flush within interval posts = %d, want still 1", n)
	}
	if nRing != 2 {
		t.Fatalf("ring len after paced skip = %d, want 2", nRing)
	}

	mgr.postMu.Lock()
	mgr.lastCatchupPost["src-a"] = time.Now().UTC().Add(-GlobalMinPollInterval)
	mgr.flushRing(ctx)
	mgr.postMu.Unlock()
	poster.mu.Lock()
	n = len(poster.posts)
	poster.mu.Unlock()
	if n != 2 {
		t.Fatalf("after interval posts = %d, want 2", n)
	}
}

func TestFlushRingCatchupBudgetIndependentOfLive(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{configured: true}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	defer mgr.Stop()

	now := time.Now().UTC()
	ctx := context.Background()
	st := config.Station{ID: "station-a", Type: config.StationTypeDavisWeatherLinkLive}
	if err := mgr.postObservation(ctx, st, &Observation{
		SourceID:   "src-a",
		ObservedAt: now,
		Provider:   ProviderDavisWeatherLinkLive,
		ProviderMeta: map[string]interface{}{
			"api": "test",
			"raw": map[string]interface{}{"temp": 1},
		},
	}); err != nil {
		t.Fatal(err)
	}

	mgr.postMu.Lock()
	mgr.ring.Push(bridgeapi.WeatherRequest{
		SourceID:   "src-a",
		ObservedAt: now.Add(-time.Second),
		Provider:   "test",
	})
	mgr.flushRing(ctx)
	mgr.postMu.Unlock()

	poster.mu.Lock()
	defer poster.mu.Unlock()
	if len(poster.posts) != 2 {
		t.Fatalf("posts = %d, want live + catch-up (separate budgets)", len(poster.posts))
	}
}

func TestCatchupTickerDrainsWithoutLive(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{configured: true}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	defer mgr.Stop()

	now := time.Now().UTC()
	mgr.postMu.Lock()
	mgr.ring.Push(bridgeapi.WeatherRequest{
		SourceID:   "src-a",
		ObservedAt: now.Add(-2 * time.Second),
		Provider:   "test",
	})
	mgr.ring.Push(bridgeapi.WeatherRequest{
		SourceID:   "src-a",
		ObservedAt: now.Add(-time.Second),
		Provider:   "test",
	})
	mgr.postMu.Unlock()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		poster.mu.Lock()
		n := len(poster.posts)
		poster.mu.Unlock()
		if n >= 2 && mgr.ring.Len() == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	poster.mu.Lock()
	n := len(poster.posts)
	poster.mu.Unlock()
	t.Fatalf("ticker drain posts=%d ring=%d, want 2 posts and empty ring", n, mgr.ring.Len())
}

func TestOutboundRingDropsByObservedAtAge(t *testing.T) {
	r := newOutboundRing()
	now := time.Now().UTC()
	r.Push(bridgeapi.WeatherRequest{
		SourceID:   "old",
		ObservedAt: now.Add(-OutboundWeatherMaxAge - time.Minute),
	})
	r.Push(bridgeapi.WeatherRequest{
		SourceID:   "fresh",
		ObservedAt: now.Add(-time.Minute),
	})
	r.Push(bridgeapi.WeatherRequest{
		SourceID:   "zero",
		ObservedAt: time.Time{},
	})
	r.Push(bridgeapi.WeatherRequest{
		SourceID:   "future",
		ObservedAt: now.Add(outboundFutureSkew + time.Minute),
	})
	if r.Len() != 1 {
		t.Fatalf("len = %d, want 1 (fresh only)", r.Len())
	}
	got := r.PopAll()
	if len(got) != 1 || got[0].SourceID != "fresh" {
		t.Fatalf("pop = %+v", got)
	}
}

func TestOutboundRingSoftMaxDropsOldest(t *testing.T) {
	r := newOutboundRing()
	now := time.Now().UTC()
	for i := 0; i < OutboundWeatherSoftMax+5; i++ {
		// All within the age window so SoftMax (not age) decides drops.
		r.Push(bridgeapi.WeatherRequest{
			SourceID:   string(rune('a' + (i % 26))),
			ObservedAt: now.Add(-time.Duration(i) * time.Millisecond),
		})
	}
	if r.Len() != OutboundWeatherSoftMax {
		t.Fatalf("len = %d, want soft max %d", r.Len(), OutboundWeatherSoftMax)
	}
}

func TestManagerQueuesWhenPostFails(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "davis_current_conditions.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Fixture ts is historical; rewrite to "now" so a failed POST is still
	// within OutboundWeatherMaxAge for ring retention.
	var top map[string]interface{}
	if err := json.Unmarshal(fixture, &top); err != nil {
		t.Fatal(err)
	}
	data, ok := top["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("fixture data: want map, got %T", top["data"])
	}
	data["ts"] = float64(time.Now().UTC().Unix())
	fixture, err = json.Marshal(top)
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

func TestManagerMissingTxidIsDegradedNotDown(t *testing.T) {
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
	txid := 99 // not present in fixture (txid 1 only)
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
		if len(statuses) == 1 && statuses[0].LastPollError != "" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(statuses) != 1 {
		t.Fatalf("status = %+v", statuses)
	}
	if !statuses[0].LANOK || !statuses[0].Degraded {
		t.Fatalf("want LANOK+degraded on missing txid, got %+v", statuses[0])
	}
	if poster.calls != 0 {
		t.Fatalf("posts = %d, want 0", poster.calls)
	}
	payloads := mgr.RecentPayloads()
	if len(payloads) < 1 || payloads[0].Raw == nil {
		t.Fatalf("payload log should keep raw: %+v", payloads)
	}
	sub, ok := mgr.WeatherSubsystemHealth()
	if !ok || sub.Status != bridgeapi.StatusDegraded {
		t.Fatalf("weather subsystem = %+v ok=%v, want degraded", sub, ok)
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
