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
	failErr    error // when failN > 0; default "post failed"
	calls      int
}

func (f *fakePoster) APIConfigured() bool { return f.configured }

func (f *fakePoster) PostWeather(ctx context.Context, req bridgeapi.WeatherRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.failN > 0 {
		f.failN--
		if f.failErr != nil {
			return f.failErr
		}
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
	ctx := context.Background()
	// Hold postMu so the catch-up ticker cannot race assertions.
	mgr.postMu.Lock()
	defer mgr.postMu.Unlock()
	for i := 0; i < 3; i++ {
		mgr.ring.Push(bridgeapi.WeatherRequest{
			SourceID:   "src-a",
			ObservedAt: now.Add(-time.Duration(i) * time.Second),
			Provider:   "test",
		})
	}

	mgr.flushRing(ctx)
	poster.mu.Lock()
	n := len(poster.posts)
	poster.mu.Unlock()
	if n != 1 {
		t.Fatalf("after first flush posts = %d, want 1", n)
	}
	if mgr.ring.Len() != 2 {
		t.Fatalf("ring len = %d, want 2", mgr.ring.Len())
	}

	mgr.lastCatchupPost["src-a"] = time.Now().UTC()
	mgr.flushRing(ctx)
	poster.mu.Lock()
	n = len(poster.posts)
	poster.mu.Unlock()
	if n != 1 {
		t.Fatalf("second flush within interval posts = %d, want still 1", n)
	}
	if mgr.ring.Len() != 2 {
		t.Fatalf("ring len after paced skip = %d, want 2", mgr.ring.Len())
	}

	mgr.lastCatchupPost["src-a"] = time.Now().UTC().Add(-GlobalMinPollInterval)
	mgr.flushRing(ctx)
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
		// First poll may still be in flight.
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

func TestCatchupSuccessClearsLastPostError(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	txid := 1
	if _, err := svc.AddStation(config.Station{
		ID:      "station-a",
		Name:    "A",
		Type:    config.StationTypeDavisWeatherLinkLive,
		Enabled: true,
		Host:    "127.0.0.1",
		Txid:    &txid,
	}); err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{configured: true, failN: 1}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	defer mgr.Stop()
	mgr.SyncFromConfig()

	now := time.Now().UTC()
	_ = mgr.postObservation(context.Background(), config.Station{ID: "station-a"}, &Observation{
		SourceID:     "station-a",
		ObservedAt:   now,
		Provider:     ProviderDavisWeatherLinkLive,
		ProviderMeta: map[string]interface{}{"api": "test", "raw": map[string]interface{}{"t": 1}},
	})
	var found *StationStatus
	for _, s := range mgr.StatusSnapshot() {
		if s.ID == "station-a" {
			found = &s
			break
		}
	}
	if found == nil || found.LastPostError == "" {
		t.Fatalf("setup: want LastPostError, got %+v", found)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if mgr.ring.Len() == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if mgr.ring.Len() != 0 {
		t.Fatalf("ring len = %d, want drained", mgr.ring.Len())
	}
	for _, s := range mgr.StatusSnapshot() {
		if s.ID != "station-a" {
			continue
		}
		if s.LastPostError != "" {
			t.Fatalf("LastPostError still %q after catch-up", s.LastPostError)
		}
		if s.LastPostAt.IsZero() {
			t.Fatal("LastPostAt still zero after catch-up")
		}
		return
	}
	t.Fatal("station-a missing")
}

func TestUplinkBreakerFailFastQueuesWithoutDial(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{
		configured: true,
		failN:      2,
		failErr:    context.DeadlineExceeded,
	}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	defer mgr.Stop()

	now := time.Now().UTC()
	st := config.Station{ID: "station-a"}
	obs := func(i int) *Observation {
		return &Observation{
			SourceID:     "station-a",
			ObservedAt:   now.Add(-time.Duration(i) * time.Second),
			Provider:     ProviderDavisWeatherLinkLive,
			ProviderMeta: map[string]interface{}{"api": "test", "raw": map[string]interface{}{"i": i}},
		}
	}
	_ = mgr.postObservation(context.Background(), st, obs(0))
	_ = mgr.postObservation(context.Background(), st, obs(1))
	if !mgr.uplink.isOpen() {
		t.Fatal("expected uplink breaker open after consecutive transport failures")
	}
	poster.mu.Lock()
	callsAfterOpen := poster.calls
	poster.mu.Unlock()

	_ = mgr.postObservation(context.Background(), st, obs(2))
	poster.mu.Lock()
	calls := poster.calls
	poster.mu.Unlock()
	if calls != callsAfterOpen {
		t.Fatalf("breaker open still dialed: calls %d -> %d", callsAfterOpen, calls)
	}
	if mgr.ring.Len() < 1 {
		t.Fatalf("ring len = %d, want queued samples", mgr.ring.Len())
	}
}

func TestPermanent4xxNotRequeued(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{
		configured: true,
		failN:      1,
		failErr:    bridgeapi.NewStatusError(400, "bad request"),
	}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	defer mgr.Stop()

	err = mgr.postObservation(context.Background(), config.Station{ID: "station-a"}, &Observation{
		SourceID:     "station-a",
		ObservedAt:   time.Now().UTC(),
		Provider:     ProviderDavisWeatherLinkLive,
		ProviderMeta: map[string]interface{}{"api": "test", "raw": map[string]interface{}{"t": 1}},
	})
	if err == nil {
		t.Fatal("expected 400 error")
	}
	if mgr.ring.Len() != 0 {
		t.Fatalf("ring len = %d, want 0 for permanent 4xx", mgr.ring.Len())
	}
	if mgr.uplink.isOpen() {
		t.Fatal("4xx must not open WAN breaker")
	}
}

func TestRemovedStationDropsRing(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	txid := 1
	if _, err := svc.AddStation(config.Station{
		ID: "gone", Name: "Gone", Type: config.StationTypeDavisWeatherLinkLive,
		Enabled: true, Host: "127.0.0.1", Txid: &txid,
	}); err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{configured: true}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	defer mgr.Stop()
	mgr.SyncFromConfig()

	mgr.postMu.Lock()
	mgr.ring.Push(bridgeapi.WeatherRequest{
		SourceID: "gone", ObservedAt: time.Now().UTC(), Provider: "test",
	})
	mgr.postMu.Unlock()

	if err := svc.DeleteStation("gone"); err != nil {
		t.Fatal(err)
	}
	mgr.SyncFromConfig()
	if mgr.ring.Len() != 0 {
		t.Fatalf("ring len = %d after delete, want 0", mgr.ring.Len())
	}
}

func TestFlushRingPushFrontRestoresFIFO(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{configured: true, failN: 1}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	defer mgr.Stop()

	now := time.Now().UTC()
	mgr.postMu.Lock()
	mgr.ring.Push(bridgeapi.WeatherRequest{SourceID: "a", ObservedAt: now.Add(-2 * time.Second), BridgeID: "first"})
	mgr.ring.Push(bridgeapi.WeatherRequest{SourceID: "b", ObservedAt: now.Add(-time.Second), BridgeID: "second"})
	mgr.flushRing(context.Background())
	if mgr.ring.Len() != 2 {
		t.Fatalf("ring len = %d after mid-flush fail, want 2", mgr.ring.Len())
	}
	mgr.lastCatchupPost = map[string]time.Time{}
	poster.failN = 0
	mgr.flushRing(context.Background())
	mgr.postMu.Unlock()

	poster.mu.Lock()
	defer poster.mu.Unlock()
	if len(poster.posts) < 1 {
		t.Fatal("expected restored catch-up POST")
	}
	if poster.posts[0].BridgeID != "first" {
		t.Fatalf("FIFO restore broken: first post BridgeID=%q", poster.posts[0].BridgeID)
	}
}

func TestFlushRingEmptyDoesNotConsumeProbe(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{
		configured: true,
		failN:      2,
		failErr:    context.DeadlineExceeded,
	}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	defer mgr.Stop()

	now := time.Now().UTC()
	st := config.Station{ID: "station-a"}
	obs := &Observation{
		SourceID:     "station-a",
		ObservedAt:   now,
		Provider:     ProviderDavisWeatherLinkLive,
		ProviderMeta: map[string]interface{}{"api": "test", "raw": map[string]interface{}{"t": 1}},
	}
	_ = mgr.postObservation(context.Background(), st, obs)
	_ = mgr.postObservation(context.Background(), st, obs)
	if !mgr.uplink.isOpen() {
		t.Fatal("expected breaker open")
	}

	// Force probe due with an empty due-set (all items still pacing).
	mgr.postMu.Lock()
	mgr.uplink.mu.Lock()
	mgr.uplink.openUntil = time.Now().Add(-time.Millisecond)
	mgr.uplink.mu.Unlock()
	for id := range mgr.lastCatchupPost {
		mgr.lastCatchupPost[id] = time.Now().UTC()
	}
	callsBefore := poster.calls
	// Drain ring so flush has nothing ready... actually ring has items but paced.
	// Empty PopReady path: clear ring.
	mgr.ring = newOutboundRing()
	mgr.flushRing(context.Background())
	mgr.postMu.Unlock()

	poster.mu.Lock()
	calls := poster.calls
	poster.mu.Unlock()
	if calls != callsBefore {
		t.Fatalf("empty flush dialed poster: %d -> %d", callsBefore, calls)
	}
	if !mgr.uplink.probeDue() {
		t.Fatal("empty flush must not consume probe slot")
	}
}

func TestSplitTimeoutsLiveGetsFreshBudget(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	slow := &phasePoster{configured: true, catchupDelay: 200 * time.Millisecond}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: slow})
	defer mgr.Stop()

	now := time.Now().UTC()
	mgr.postMu.Lock()
	mgr.ring.Push(bridgeapi.WeatherRequest{SourceID: "src-a", ObservedAt: now.Add(-time.Second), Provider: "test"})
	mgr.postMu.Unlock()

	// Parent still has budget after a slow flush phase because live uses its own timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = mgr.postObservation(ctx, config.Station{ID: "live"}, &Observation{
		SourceID:     "src-live",
		ObservedAt:   now,
		Provider:     ProviderDavisWeatherLinkLive,
		ProviderMeta: map[string]interface{}{"api": "test", "raw": map[string]interface{}{"t": 1}},
	})
	if err != nil {
		t.Fatalf("live post: %v", err)
	}
	slow.mu.Lock()
	defer slow.mu.Unlock()
	if slow.liveOK != 1 {
		t.Fatalf("liveOK = %d, want 1", slow.liveOK)
	}
}

func TestWeatherHealthIncludesWANUplinkOpen(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	txid := 1
	if _, err := svc.AddStation(config.Station{
		ID: "station-a", Name: "A", Type: config.StationTypeDavisWeatherLinkLive,
		Enabled: true, Host: "127.0.0.1", Txid: &txid,
	}); err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{
		configured: true,
		failN:      2,
		failErr:    context.DeadlineExceeded,
	}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	defer mgr.Stop()
	mgr.SyncFromConfig()

	sub, ok := mgr.WeatherSubsystemHealth()
	if !ok {
		t.Fatal("expected weather subsystem")
	}
	if open, _ := sub.Detail["wan_uplink_open"].(bool); open {
		t.Fatal("wan_uplink_open true before failures")
	}

	now := time.Now().UTC()
	st := config.Station{ID: "station-a"}
	obs := func(i int) *Observation {
		return &Observation{
			SourceID: "station-a", ObservedAt: now.Add(-time.Duration(i) * time.Second),
			Provider:     ProviderDavisWeatherLinkLive,
			ProviderMeta: map[string]interface{}{"api": "test", "raw": map[string]interface{}{"i": i}},
		}
	}
	_ = mgr.postObservation(context.Background(), st, obs(0))
	_ = mgr.postObservation(context.Background(), st, obs(1))
	if !mgr.uplink.isOpen() {
		t.Fatal("expected breaker open")
	}
	sub, ok = mgr.WeatherSubsystemHealth()
	if !ok {
		t.Fatal("expected weather subsystem")
	}
	if open, _ := sub.Detail["wan_uplink_open"].(bool); !open {
		t.Fatal("wan_uplink_open false while breaker open")
	}
}

func TestHealPostStatusAfterDrainClearsStaleWAN(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	txid := 1
	if _, err := svc.AddStation(config.Station{
		ID: "station-a", Name: "A", Type: config.StationTypeDavisWeatherLinkLive,
		Enabled: true, Host: "127.0.0.1", Txid: &txid,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddStation(config.Station{
		ID: "station-b", Name: "B", Type: config.StationTypeDavisWeatherLinkLive,
		Enabled: true, Host: "127.0.0.1", Txid: &txid,
	}); err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: &fakePoster{configured: true}})
	defer mgr.Stop()
	mgr.SyncFromConfig()

	mgr.setStatus("station-a", func(s *StationStatus) {
		s.LastPostError = "weather WAN unreachable; queued"
	})
	mgr.setStatus("station-b", func(s *StationStatus) {
		s.LastPostError = "HTTP 400: bad request"
	})
	mgr.healPostStatusAfterDrain()

	for _, s := range mgr.StatusSnapshot() {
		switch s.ID {
		case "station-a":
			if s.LastPostError != "" {
				t.Fatalf("station-a LastPostError=%q, want cleared", s.LastPostError)
			}
		case "station-b":
			if s.LastPostError != "HTTP 400: bad request" {
				t.Fatalf("station-b LastPostError=%q, want permanent 4xx kept", s.LastPostError)
			}
		}
	}
}

func TestStaleWANPostReason(t *testing.T) {
	if !staleWANPostReason("weather WAN unreachable; queued") {
		t.Fatal("want fail-fast queued reason")
	}
	if !staleWANPostReason("bridge api request: dial tcp: i/o timeout") {
		t.Fatal("want transport wrapper")
	}
	if staleWANPostReason("HTTP 400: bad request") {
		t.Fatal("permanent 4xx must not be stale WAN")
	}
	if staleWANPostReason("") {
		t.Fatal("empty must not be stale")
	}
}

type phasePoster struct {
	configured   bool
	catchupDelay time.Duration
	mu           sync.Mutex
	liveOK       int
}

func (p *phasePoster) APIConfigured() bool { return p.configured }

func (p *phasePoster) PostWeather(ctx context.Context, req bridgeapi.WeatherRequest) error {
	if req.SourceID == "src-live" {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			p.mu.Lock()
			p.liveOK++
			p.mu.Unlock()
			return nil
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(p.catchupDelay):
		return nil
	}
}
