package station

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/bridgeapi"
)

func TestIsTransientWeatherErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"canceled", context.Canceled, false},
		{"deadline", context.DeadlineExceeded, true},
		{"429", bridgeapi.NewStatusError(429, "slow"), true},
		{"503", bridgeapi.NewStatusError(503, "down"), true},
		{"400", bridgeapi.NewStatusError(400, "bad"), false},
		{"401", bridgeapi.NewStatusError(401, "auth"), false},
		{"dns", &net.DNSError{Err: "no such host", Name: "api.example"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientWeatherErr(tc.err); got != tc.want {
				t.Fatalf("isTransientWeatherErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestWeatherUplinkProbeDueDoesNotConsume(t *testing.T) {
	u := newWeatherUplink()
	u.noteTransientFailure()
	u.noteTransientFailure() // open
	if !u.isOpen() {
		t.Fatal("expected open")
	}
	u.mu.Lock()
	u.openUntil = time.Now().Add(-time.Millisecond)
	u.mu.Unlock()
	if !u.probeDue() {
		t.Fatal("probe should be due")
	}
	if !u.probeDue() {
		t.Fatal("probeDue must be idempotent")
	}
	if !u.allowAttempt() {
		t.Fatal("allowAttempt should succeed")
	}
	if u.probeDue() {
		t.Fatal("after allowAttempt, next probe should wait backoff")
	}
}

func TestIsPermanentWeatherErr(t *testing.T) {
	if !isPermanentWeatherErr(bridgeapi.NewStatusError(400, "x")) {
		t.Fatal("400 should be permanent")
	}
	if isPermanentWeatherErr(bridgeapi.NewStatusError(429, "x")) {
		t.Fatal("429 should not be permanent")
	}
	if isPermanentWeatherErr(errors.New("dial")) {
		t.Fatal("transport should not be permanent")
	}
}
