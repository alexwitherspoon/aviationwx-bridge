// stale_sync_behavior_test.go — specification-style (TDD) tests for NTP staleness.
//
// Behavior under test:
//   - "Good sync" = NTP response with |offset| <= max_offset (lastGoodSync advances).
//   - Response with |offset| > max_offset does not advance lastGoodSync and is unhealthy.
//   - When every server fails: unhealthy if never synced; healthy if last good sync within
//     stale_threshold_hours; unhealthy if that window has elapsed.
//
// QueryHook supplies deterministic NTP results without the network.

package time

import (
	"errors"
	"testing"
	"time"
)

func TestTimeHealth_StaleSyncSpec_InBoundsResponseSetsHealthyAndLastGoodSync(t *testing.T) {
	t.Parallel()
	hook := func(string) (time.Duration, error) { return 2 * time.Second, nil }
	th := NewTimeHealth(Config{
		Servers:          []string{"ntp.test"},
		MaxOffsetSeconds: 5,
		QueryHook:        hook,
	})
	th.check()

	if !th.IsHealthy() {
		t.Fatal("want healthy after in-bounds NTP response")
	}
	st := th.GetStatus()
	if st.LastGoodSync.IsZero() {
		t.Fatal("want last_good_sync set after in-bounds sync")
	}
	if st.Offset != 2*time.Second {
		t.Fatalf("offset = %v, want 2s", st.Offset)
	}
}

func TestTimeHealth_StaleSyncSpec_OutOfBoundsResponseDoesNotSetLastGoodSync(t *testing.T) {
	t.Parallel()
	hook := func(string) (time.Duration, error) { return 100 * time.Second, nil }
	th := NewTimeHealth(Config{
		Servers:          []string{"ntp.test"},
		MaxOffsetSeconds: 5,
		QueryHook:        hook,
	})
	th.check()

	if th.IsHealthy() {
		t.Fatal("want unhealthy when offset exceeds max_offset")
	}
	if !th.GetStatus().LastGoodSync.IsZero() {
		t.Fatal("want last_good_sync unset when offset out of bounds")
	}
}

func TestTimeHealth_StaleSyncSpec_AllServersFail_NeverSynced(t *testing.T) {
	t.Parallel()
	hook := func(string) (time.Duration, error) { return 0, errors.New("unreachable") }
	th := NewTimeHealth(Config{
		Servers:             []string{"a", "b"},
		StaleThresholdHours: 24,
		QueryHook:           hook,
	})
	th.check()

	if th.IsHealthy() {
		t.Fatal("want unhealthy when NTP never succeeded and all servers fail")
	}
}

func TestTimeHealth_StaleSyncSpec_TransientOutage_StaysHealthyAfterPriorGoodSync(t *testing.T) {
	t.Parallel()
	calls := 0
	hook := func(string) (time.Duration, error) {
		calls++
		if calls == 1 {
			return 1 * time.Second, nil
		}
		return 0, errors.New("network down")
	}
	th := NewTimeHealth(Config{
		Servers:          []string{"ntp.test"},
		MaxOffsetSeconds: 5,
		QueryHook:        hook,
	})
	th.check()
	if calls != 1 {
		t.Fatalf("first check: want 1 hook call, got %d", calls)
	}
	if !th.IsHealthy() {
		t.Fatal("want healthy after first good sync")
	}
	if th.GetStatus().LastGoodSync.IsZero() {
		t.Fatal("want last_good_sync after good sync")
	}

	th.check()
	if calls != 2 {
		t.Fatalf("second check: want 2 hook calls, got %d", calls)
	}
	if !th.IsHealthy() {
		t.Fatal("want healthy while last good sync is within stale window (transient outage)")
	}
}

func TestTimeHealth_StaleSyncSpec_Table_AllServersFail_GivenLastGoodSyncAge(t *testing.T) {
	t.Parallel()
	errAll := errors.New("ntp down")

	tests := []struct {
		name        string
		staleHours  int
		lastGoodAge time.Duration // age of lastGoodSync relative to time.Now() before check(); 0 = skip seed (never synced)
		wantHealthy bool
		wantReason  string // appears in failure message
	}{
		{
			name:        "within_default_24h_window",
			staleHours:  24,
			lastGoodAge: -1 * time.Hour,
			wantHealthy: true,
			wantReason:  "last good sync 1h ago should still be healthy when NTP fails",
		},
		{
			name:        "beyond_default_24h_window",
			staleHours:  24,
			lastGoodAge: -25 * time.Hour,
			wantHealthy: false,
			wantReason:  "last good sync 25h ago should be unhealthy when NTP fails",
		},
		{
			name:        "within_custom_2h_window",
			staleHours:  2,
			lastGoodAge: -90 * time.Minute,
			wantHealthy: true,
			wantReason:  "last good sync 90m ago within 2h window should be healthy when NTP fails",
		},
		{
			name:        "beyond_custom_2h_window",
			staleHours:  2,
			lastGoodAge: -3 * time.Hour,
			wantHealthy: false,
			wantReason:  "last good sync 3h ago beyond 2h window should be unhealthy when NTP fails",
		},
		{
			name:        "never_synced_lastGoodZero",
			staleHours:  24,
			lastGoodAge: 0,
			wantHealthy: false,
			wantReason:  "never achieved in-bounds sync should be unhealthy when NTP fails",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			th := NewTimeHealth(Config{
				Servers:             []string{"ntp.test"},
				StaleThresholdHours: tt.staleHours,
				QueryHook:           func(string) (time.Duration, error) { return 0, errAll },
			})
			if tt.lastGoodAge != 0 {
				th.mu.Lock()
				th.lastGoodSync = time.Now().Add(tt.lastGoodAge)
				th.mu.Unlock()
			}

			th.check()

			got := th.IsHealthy()
			if got != tt.wantHealthy {
				t.Fatalf("healthy=%v, want %v — %s", got, tt.wantHealthy, tt.wantReason)
			}
		})
	}
}

func TestTimeHealth_StaleSyncSpec_ZeroStaleHoursUses24hDefault(t *testing.T) {
	t.Parallel()
	th := NewTimeHealth(Config{
		Servers:             []string{"ntp.test"},
		StaleThresholdHours: 0,
		QueryHook:           func(string) (time.Duration, error) { return 0, errors.New("down") },
	})
	if th.staleMaxAge != 24*time.Hour {
		t.Fatalf("staleMaxAge = %v, want 24h default", th.staleMaxAge)
	}
	th.mu.Lock()
	th.lastGoodSync = time.Now().Add(-23 * time.Hour)
	th.mu.Unlock()
	th.check()
	if !th.IsHealthy() {
		t.Fatal("want healthy at 23h with default 24h window")
	}
	th.mu.Lock()
	th.lastGoodSync = time.Now().Add(-25 * time.Hour)
	th.mu.Unlock()
	th.check()
	if th.IsHealthy() {
		t.Fatal("want unhealthy beyond default 24h window")
	}
}
