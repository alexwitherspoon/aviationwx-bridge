//go:build e2e

package harness

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

// RequireDocker skips the test when the Docker daemon is unavailable.
func RequireDocker(t *testing.T) {
	t.Helper()
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker daemon not available")
	}
}

// RequireE2EStack skips when the E2E compose stack is not running.
func RequireE2EStack(t *testing.T) {
	t.Helper()
	RequireDocker(t)
	if os.Getenv("AVIATIONWX_E2E_STACK") != "1" {
		t.Skip("E2E stack not started (run make e2e or scripts/e2e-run.sh)")
	}
}

// WaitTCP waits until addr (host:port) accepts a TCP connection or timeout.
func WaitTCP(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(DefaultPoll)
	}
	return fmt.Errorf("tcp %s not ready within %s", addr, timeout)
}

// WaitHTTPGET waits for url to return wantStatus.
func WaitHTTPGET(url string, wantStatus int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}
	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == wantStatus {
				return nil
			}
		}
		time.Sleep(DefaultPoll)
	}
	return fmt.Errorf("GET %s did not return %d within %s", url, wantStatus, timeout)
}
