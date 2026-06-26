//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/test/e2e/harness"
)

func TestUploadIdentity_RosterCachedAndAPIOk(t *testing.T) {
	harness.RequireE2EStack(t)

	trustedPath := harness.E2EPath("testdata", "e2e", "bridge", "upload_ssh_trusted_keys.json")
	deadline := time.Now().Add(harness.DefaultWaitTimeout)
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(trustedPath)
		if err == nil && len(raw) > 10 {
			break
		}
		time.Sleep(harness.DefaultPoll)
	}
	raw, err := os.ReadFile(trustedPath)
	if err != nil {
		t.Fatalf("trusted roster file missing on host mount: %v", err)
	}
	var trusted struct {
		Source string   `json:"source"`
		SHA256 []string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &trusted); err != nil {
		t.Fatalf("trusted json: %v", err)
	}
	if trusted.Source != "https-roster" || len(trusted.SHA256) == 0 {
		t.Fatalf("trusted roster: %+v", trusted)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest(http.MethodGet, harness.BridgeWebURL+"/api/upload/ssh-host-keys", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth(harness.BridgeUser, harness.BridgePass)

	var lastBody string
	for time.Now().Before(deadline) {
		resp, err := client.Do(req)
		if err != nil {
			time.Sleep(harness.DefaultPoll)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		lastBody = string(body)
		if resp.StatusCode == http.StatusOK {
			var out struct {
				Endpoints []struct {
					Status string `json:"status"`
					Host   string `json:"host"`
				} `json:"endpoints"`
			}
			if json.Unmarshal(body, &out) == nil && len(out.Endpoints) > 0 && out.Endpoints[0].Status == "ok" {
				return
			}
		}
		time.Sleep(harness.DefaultPoll)
	}
	t.Fatalf("ssh-host-keys API did not reach ok; last body=%s", lastBody)
}
