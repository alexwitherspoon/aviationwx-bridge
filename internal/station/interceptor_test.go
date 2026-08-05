package station

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

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

	resp, err := http.PostForm("http://"+addr+st.ListenPath, url.Values{
		"dateutc": {"2024-06-15 12:00:00"},
		"tempf":   {"70.1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "success") {
		t.Fatalf("good post: status=%d body=%q", resp.StatusCode, body)
	}

	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		poster.mu.Lock()
		n := len(poster.posts)
		poster.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	poster.mu.Lock()
	if len(poster.posts) != 1 {
		poster.mu.Unlock()
		t.Fatalf("expected 1 weather POST, got %d", len(poster.posts))
	}
	got := poster.posts[0]
	poster.mu.Unlock()
	if got.Provider != ProviderHTTPInterceptor || got.SourceID != st.ID {
		t.Fatalf("post = %+v", got)
	}
	if got.Sample != nil {
		t.Fatal("wire must omit sample")
	}

	// Coalesce within 1 Hz: still ACK the device, no second POST.
	resp, err = http.PostForm("http://"+addr+st.ListenPath, url.Values{
		"dateutc": {"2024-06-15 12:00:01"},
		"tempf":   {"71"},
	})
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "success") {
		t.Fatalf("coalesced post: status=%d body=%q", resp.StatusCode, body)
	}
	time.Sleep(50 * time.Millisecond)
	poster.mu.Lock()
	n := len(poster.posts)
	poster.mu.Unlock()
	if n != 1 {
		t.Fatalf("1 Hz coalesce should skip second POST; posts=%d", n)
	}
}

func TestInjectInterceptorMissingDateutc(t *testing.T) {
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

	obs, err := mgr.InjectInterceptorRequest(st, map[string]string{"tempf": "71"})
	if err != nil {
		t.Fatal(err)
	}
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

func TestPreviewInterceptorRequest(t *testing.T) {
	mgr := NewManager(ManagerConfig{})
	obs, err := mgr.PreviewInterceptorRequest(config.Station{
		Name: "WU",
		Type: config.StationTypeHTTPInterceptor,
	}, map[string]string{"dateutc": "2024-06-15 12:00:00", "tempf": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if obs.ObservedAt.IsZero() {
		t.Fatal("expected observed_at")
	}
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
