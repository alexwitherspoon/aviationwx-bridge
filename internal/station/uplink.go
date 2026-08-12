package station

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/bridgeapi"
)

const (
	weatherPostTimeout = 15 * time.Second
	uplinkOpenAfter    = 2
	uplinkBackoffStart = 5 * time.Second
	uplinkBackoffCap   = 60 * time.Second
)

// weatherUplink is a process-local circuit breaker for api.aviationwx.org weather POSTs.
// Transient WAN loss is expected; the breaker fail-fasts into the ring and probes on backoff.
type weatherUplink struct {
	mu        sync.Mutex
	open      bool
	failures  int
	openUntil time.Time
	backoff   time.Duration
}

func newWeatherUplink() *weatherUplink {
	return &weatherUplink{backoff: uplinkBackoffStart}
}

func isTransientWeatherErr(err error) bool {
	if err == nil {
		return false
	}
	// Intentional cancel (Stop / hub reload) is not a WAN outage.
	if errors.Is(err, context.Canceled) {
		return false
	}
	code := bridgeapi.StatusCode(err)
	if code == 429 || code >= 500 {
		return true
	}
	if code != 0 {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	// StatusCode 0 with no typed net error: treat as transport-class
	// (bridgeapi wraps dial failures as "bridge api request: ...").
	return true
}

func isPermanentWeatherErr(err error) bool {
	if err == nil {
		return false
	}
	code := bridgeapi.StatusCode(err)
	return code >= 400 && code < 500 && code != 429
}

func (u *weatherUplink) isOpen() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.open
}

// probeDue reports whether an open breaker may attempt a POST (does not consume the slot).
func (u *weatherUplink) probeDue() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.open {
		return true
	}
	return !time.Now().Before(u.openUntil)
}

func (u *weatherUplink) allowAttempt() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if !u.open {
		return true
	}
	now := time.Now()
	if now.Before(u.openUntil) {
		return false
	}
	// Probe window: allow one attempt; postMu serializes callers.
	u.openUntil = now.Add(u.backoff)
	return true
}

func (u *weatherUplink) noteSuccess() (wasOpen bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	wasOpen = u.open
	u.open = false
	u.failures = 0
	u.backoff = uplinkBackoffStart
	u.openUntil = time.Time{}
	return wasOpen
}

func (u *weatherUplink) noteTransientFailure() (opened bool) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.failures++
	if !u.open && u.failures < uplinkOpenAfter {
		return false
	}
	opened = !u.open
	u.open = true
	if u.backoff <= 0 {
		u.backoff = uplinkBackoffStart
	}
	u.openUntil = time.Now().Add(u.backoff)
	next := u.backoff * 2
	if next > uplinkBackoffCap {
		next = uplinkBackoffCap
	}
	u.backoff = next
	return opened
}
