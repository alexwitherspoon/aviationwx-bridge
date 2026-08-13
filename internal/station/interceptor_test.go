package station

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/bridgeapi"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

func TestParseWundergroundDateUTC(t *testing.T) {
	cases := []struct {
		in   string
		want time.Time
		zero bool
	}{
		{in: "2024-06-15 12:00:00", want: time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)},
		{in: "2024-06-15T12:00:00Z", want: time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)},
		{in: "now", zero: true},
		{in: "", zero: true},
		{in: "not-a-date", zero: true},
	}
	for _, tc := range cases {
		got := parseWundergroundDateUTC(tc.in)
		if tc.zero {
			if !got.IsZero() {
				t.Errorf("parseWundergroundDateUTC(%q) = %v, want zero", tc.in, got)
			}
			continue
		}
		if !got.Equal(tc.want) {
			t.Errorf("parseWundergroundDateUTC(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestBuildInterceptorObservation(t *testing.T) {
	st := config.Station{
		ID:         "station-wu",
		Name:       "WU",
		Type:       config.StationTypeHTTPInterceptor,
		ListenPath: "/weatherstation/updateweatherstation.php",
		Dialect:    config.HTTPInterceptorDialectWunderground,
	}
	obs := buildInterceptorObservation(st, map[string]string{
		"dateutc":  "2024-06-15 12:00:00",
		"tempf":    "72.5",
		"PASSWORD": "secret-value",
		"ID":       "TEST",
	})
	if obs.Provider != ProviderHTTPInterceptor {
		t.Fatalf("provider = %s", obs.Provider)
	}
	if obs.SourceID != "station-wu" {
		t.Fatalf("source_id = %s", obs.SourceID)
	}
	if obs.ObservedAt.IsZero() {
		t.Fatal("expected observed_at")
	}
	raw, ok := obs.ProviderMeta["raw"].(map[string]interface{})
	if !ok || raw["tempf"] != "72.5" {
		t.Fatalf("raw = %#v", obs.ProviderMeta["raw"])
	}
	if raw["PASSWORD"] != interceptorRedacted {
		t.Fatalf("PASSWORD should be redacted, got %#v", raw["PASSWORD"])
	}
	if raw["ID"] != "TEST" {
		t.Fatalf("ID should remain, got %#v", raw["ID"])
	}
	if _, hasSample := obs.ProviderMeta["sample"]; hasSample {
		t.Fatal("must not set sample")
	}
}

func TestInterceptorHubReceiveAndSkipMissingDateutc(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{configured: true}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	defer mgr.Stop()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	st, err := svc.AddStation(config.Station{
		ID:         "station-wu",
		Name:       "WU",
		Type:       config.StationTypeHTTPInterceptor,
		Enabled:    true,
		ListenAddr: addr,
		ListenPath: "/weatherstation/updateweatherstation.php",
		Dialect:    config.HTTPInterceptorDialectWunderground,
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.SyncFromConfig()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		ready := mgr.hub != nil
		mgr.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Bad dateutc ACKs but must not burn the 1 Hz slot.
	resp, err := http.PostForm("http://"+addr+st.ListenPath, url.Values{
		"dateutc": {"now"},
		"tempf":   {"70.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "success") {
		t.Fatalf("missing dateutc: status=%d body=%q", resp.StatusCode, body)
	}
	time.Sleep(80 * time.Millisecond)
	poster.mu.Lock()
	if len(poster.posts) != 0 {
		t.Fatalf("missing dateutc must not POST; posts=%d", len(poster.posts))
	}
	poster.mu.Unlock()

	obsAt := time.Now().UTC().Add(-2 * time.Second).Format("2006-01-02 15:04:05")
	resp, err = http.PostForm("http://"+addr+st.ListenPath, url.Values{
		"dateutc": {obsAt},
		"tempf":   {"71"},
	})
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		poster.mu.Lock()
		n := len(poster.posts)
		poster.mu.Unlock()
		if n == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	poster.mu.Lock()
	n := len(poster.posts)
	poster.mu.Unlock()
	t.Fatalf("valid sample after bad dateutc posts=%d, want 1", n)
}

func TestPreviewInterceptorMissingDateutc(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{configured: true}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	st := config.Station{
		ID:         "station-wu",
		Name:       "WU",
		Type:       config.StationTypeHTTPInterceptor,
		Enabled:    true,
		ListenAddr: "127.0.0.1:8090",
		ListenPath: "/weatherstation/updateweatherstation.php",
		Dialect:    config.HTTPInterceptorDialectWunderground,
	}
	if _, err := svc.AddStation(st); err != nil {
		t.Fatal(err)
	}
	mgr.SyncFromConfig()
	defer mgr.Stop()

	obs, err := mgr.PreviewInterceptorRequest(st, map[string]string{"tempf": "71"})
	if err != nil {
		t.Fatal(err)
	}
	mgr.handleInterceptorObservation(mgr.runCtx, st, obs)
	if !obs.ObservedAt.IsZero() {
		t.Fatal("expected zero observed_at")
	}
	poster.mu.Lock()
	n := len(poster.posts)
	poster.mu.Unlock()
	if n != 0 {
		t.Fatalf("missing dateutc must skip POST; posts=%d", n)
	}
	var stStatus StationStatus
	for _, s := range mgr.StatusSnapshot() {
		if s.ID == st.ID {
			stStatus = s
			break
		}
	}
	if !stStatus.Degraded || stStatus.LastPollError == "" {
		t.Fatalf("expected degraded status: %+v", stStatus)
	}
}

func TestInterceptorHubLatestWinsUnderSlowPost(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	poster := &blockingPoster{configured: true, release: release, posts: make([]bridgeapi.WeatherRequest, 0)}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	defer mgr.Stop()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	st, err := svc.AddStation(config.Station{
		ID:         "station-wu",
		Name:       "WU",
		Type:       config.StationTypeHTTPInterceptor,
		Enabled:    true,
		ListenAddr: addr,
		ListenPath: "/weatherstation/updateweatherstation.php",
		Dialect:    config.HTTPInterceptorDialectWunderground,
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.SyncFromConfig()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		ready := mgr.hub != nil
		mgr.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	post := func(tempf, dateutc string) {
		t.Helper()
		resp, err := http.PostForm("http://"+addr+st.ListenPath, url.Values{
			"dateutc": {dateutc},
			"tempf":   {tempf},
		})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
	}

	// First job blocks inside PostWeather.
	post("1.0", "2024-06-15 12:00:00")
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		poster.mu.Lock()
		n := poster.entered
		poster.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Still inside 1 Hz window - ACK only.
	post("2.0", "2024-06-15 12:00:01")

	// After 1 Hz window, pending is latest-wins while the first post is blocked.
	time.Sleep(GlobalMinPollInterval + 50*time.Millisecond)
	post("3.0", "2024-06-15 12:00:02")
	time.Sleep(GlobalMinPollInterval + 50*time.Millisecond)
	post("9.9", "2024-06-15 12:00:03")

	close(release)

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		poster.mu.Lock()
		n := len(poster.posts)
		poster.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	poster.mu.Lock()
	defer poster.mu.Unlock()
	if len(poster.posts) < 2 {
		t.Fatalf("expected at least 2 posts (in-flight + latest), got %d", len(poster.posts))
	}
	if len(poster.posts) > 3 {
		t.Fatalf("expected latest-wins to bound posts, got %d", len(poster.posts))
	}
	last := poster.posts[len(poster.posts)-1]
	raw, _ := last.ProviderMeta["raw"].(map[string]interface{})
	if raw["tempf"] != "9.9" {
		t.Fatalf("last post should be latest pending tempf=9.9, got %#v", raw)
	}
}

type blockingPoster struct {
	mu         sync.Mutex
	configured bool
	release    chan struct{}
	entered    int
	posts      []bridgeapi.WeatherRequest
}

func (b *blockingPoster) APIConfigured() bool { return b.configured }

func (b *blockingPoster) PostWeather(ctx context.Context, req bridgeapi.WeatherRequest) error {
	b.mu.Lock()
	b.entered++
	b.mu.Unlock()
	select {
	case <-b.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	b.mu.Lock()
	b.posts = append(b.posts, req)
	b.mu.Unlock()
	return nil
}

func TestSyncInterceptorHubSkipsListenAddrMismatch(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(ManagerConfig{ConfigService: svc})
	defer mgr.Stop()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	keep, err := svc.AddStation(config.Station{
		ID:         "station-wu-a",
		Name:       "WU A",
		Type:       config.StationTypeHTTPInterceptor,
		Enabled:    true,
		ListenAddr: addr,
		ListenPath: "/a",
		Dialect:    config.HTTPInterceptorDialectWunderground,
	})
	if err != nil {
		t.Fatal(err)
	}
	skip, err := svc.AddStation(config.Station{
		ID:         "station-wu-b",
		Name:       "WU B",
		Type:       config.StationTypeHTTPInterceptor,
		Enabled:    true,
		ListenAddr: "127.0.0.1:1",
		ListenPath: "/b",
		Dialect:    config.HTTPInterceptorDialectWunderground,
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.SyncFromConfig()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		ready := mgr.hub != nil
		mgr.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	mgr.mu.Lock()
	hub := mgr.hub
	mgr.mu.Unlock()
	if hub == nil {
		t.Fatal("expected hub")
	}
	hub.mu.RLock()
	_, hasKeep := hub.routes[keep.ListenPath]
	_, hasSkip := hub.routes[skip.ListenPath]
	hub.mu.RUnlock()
	if !hasKeep || hasSkip {
		t.Fatalf("routes keep=%v skip=%v", hasKeep, hasSkip)
	}
	var skipStatus StationStatus
	for _, s := range mgr.StatusSnapshot() {
		if s.ID == skip.ID {
			skipStatus = s
			break
		}
	}
	if !skipStatus.Degraded || skipStatus.LastPollError == "" {
		t.Fatalf("expected degraded mismatch status: %+v", skipStatus)
	}
}

func TestSyncInterceptorHubMarksDegradedOnBindFailure(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(ManagerConfig{ConfigService: svc})
	defer mgr.Stop()

	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	addr := blocker.Addr().String()

	st, err := svc.AddStation(config.Station{
		ID:         "station-wu-bindfail",
		Name:       "WU BindFail",
		Type:       config.StationTypeHTTPInterceptor,
		Enabled:    true,
		ListenAddr: addr,
		ListenPath: "/weatherstation/updateweatherstation.php",
		Dialect:    config.HTTPInterceptorDialectWunderground,
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.SyncFromConfig()

	mgr.mu.Lock()
	hub := mgr.hub
	mgr.mu.Unlock()
	if hub != nil {
		t.Fatal("expected hub nil after bind failure")
	}
	var status StationStatus
	for _, s := range mgr.StatusSnapshot() {
		if s.ID == st.ID {
			status = s
			break
		}
	}
	if !status.Degraded || status.LANOK || !strings.Contains(status.LastPollError, "listen failed") {
		t.Fatalf("expected degraded bind-failure status: %+v", status)
	}
}

func TestSyncInterceptorHubClearsRoutingErrorWhenRoutable(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	mgr := NewManager(ManagerConfig{ConfigService: svc})
	defer mgr.Stop()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	st, err := svc.AddStation(config.Station{
		ID:         "station-wu-recover",
		Name:       "WU Recover",
		Type:       config.StationTypeHTTPInterceptor,
		Enabled:    true,
		ListenAddr: addr,
		ListenPath: "/recover",
		Dialect:    config.HTTPInterceptorDialectWunderground,
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.setStatus(st.ID, func(s *StationStatus) {
		s.Degraded = true
		s.LANOK = false
		s.LastPollError = fmt.Sprintf("listen_addr %q does not match active bind %q", "127.0.0.1:1", addr)
	})
	mgr.SyncFromConfig()

	var status StationStatus
	for _, s := range mgr.StatusSnapshot() {
		if s.ID == st.ID {
			status = s
			break
		}
	}
	if status.Degraded || status.LastPollError != "" {
		t.Fatalf("expected routing error cleared: %+v", status)
	}
}

func TestInterceptorHubReplaceTransfersPending(t *testing.T) {
	dir := t.TempDir()
	svc, err := config.NewService(dir)
	if err != nil {
		t.Fatal(err)
	}
	poster := &fakePoster{configured: true}
	mgr := NewManager(ManagerConfig{ConfigService: svc, Poster: poster})
	defer mgr.Stop()

	ln1, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr1 := ln1.Addr().String()
	_ = ln1.Close()

	st, err := svc.AddStation(config.Station{
		ID:         "station-wu",
		Name:       "WU",
		Type:       config.StationTypeHTTPInterceptor,
		Enabled:    true,
		ListenAddr: addr1,
		ListenPath: "/weatherstation/updateweatherstation.php",
		Dialect:    config.HTTPInterceptorDialectWunderground,
	})
	if err != nil {
		t.Fatal(err)
	}
	mgr.SyncFromConfig()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mgr.mu.Lock()
		ok := mgr.hub != nil && mgr.hub.alive()
		mgr.mu.Unlock()
		if ok {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	obs := buildInterceptorObservation(st, map[string]string{
		"dateutc": time.Now().UTC().Add(-time.Second).Format("2006-01-02 15:04:05"),
		"tempf":   "99",
	})
	mgr.mu.Lock()
	hub := mgr.hub
	mgr.mu.Unlock()
	if hub == nil {
		t.Fatal("hub nil")
	}
	hub.enqueue(st, obs)

	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr2 := ln2.Addr().String()
	_ = ln2.Close()
	if err := svc.UpdateStation(st.ID, func(s *config.Station) error {
		s.ListenAddr = addr2
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	mgr.SyncFromConfig()

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		poster.mu.Lock()
		posts := append([]bridgeapi.WeatherRequest(nil), poster.posts...)
		poster.mu.Unlock()
		for _, p := range posts {
			raw, _ := p.ProviderMeta["raw"].(map[string]interface{})
			if raw["tempf"] == "99" {
				return
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	poster.mu.Lock()
	defer poster.mu.Unlock()
	t.Fatalf("expected handed-off pending tempf=99 in posts=%+v", poster.posts)
}
