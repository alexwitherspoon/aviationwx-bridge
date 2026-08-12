package station

import (
	"net"
	"net/http"
	"time"
)

// LANPollTimeout bounds a single poll of a LAN weather sensor.
const LANPollTimeout = 8 * time.Second

const lanDialTimeout = 3 * time.Second

// NewLANClient returns an HTTP client for on-prem sensor polls (short dial + overall bound).
// Proxy is disabled so HTTP_PROXY cannot redirect LAN device traffic.
func NewLANClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = LANPollTimeout
	}
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: (&net.Dialer{
				Timeout:   lanDialTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			TLSHandshakeTimeout: lanDialTimeout,
			MaxIdleConns:        4,
			IdleConnTimeout:     90 * time.Second,
			ForceAttemptHTTP2:   false,
		},
	}
}
