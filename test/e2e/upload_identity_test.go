//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/test/e2e/harness"
)

func TestUploadIdentity_RosterCachedAndAPIOk(t *testing.T) {
	harness.RequireE2EStack(t)

	trustedPath := harness.E2EPath("testdata", "e2e", "bridge", "upload_ssh_trusted_keys.json")
	deadline := time.Now().Add(harness.DefaultWaitTimeout)
	var raw []byte
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(trustedPath)
		if err == nil && len(data) > 10 {
			raw = data
			break
		}
		time.Sleep(harness.DefaultPoll)
	}
	if len(raw) == 0 {
		raw, _ = os.ReadFile(trustedPath)
	}
	if len(raw) == 0 {
		t.Fatalf("trusted roster file missing on host mount: %v", trustedPath)
	}
	source, fps := trustedRosterForEndpoint(t, raw, harness.UploadHost, 2222)
	if source != "https-roster" || len(fps) == 0 {
		t.Fatalf("trusted roster for %s:2222 source=%q sha256=%v", harness.UploadHost, source, fps)
	}

	client := &http.Client{Timeout: 20 * time.Second}
	req, err := http.NewRequest(http.MethodGet, harness.BridgeWebURL()+"/api/upload/ssh-host-keys", nil)
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
		if resp.StatusCode != http.StatusOK {
			time.Sleep(harness.DefaultPoll)
			continue
		}
		var out struct {
			Endpoints []struct {
				Status              string   `json:"status"`
				Host                string   `json:"host"`
				Port                int      `json:"port"`
				TrustedSource       string   `json:"trusted_source"`
				TrustedRosterSHA256 []string `json:"trusted_roster_sha256"`
			} `json:"endpoints"`
		}
		if err := json.Unmarshal(body, &out); err != nil || len(out.Endpoints) == 0 {
			time.Sleep(harness.DefaultPoll)
			continue
		}
		for _, ep := range out.Endpoints {
			if ep.Host != harness.UploadHost || ep.Port != 2222 {
				continue
			}
			if ep.Status != "ok" {
				break
			}
			if ep.TrustedSource != "https-roster" || len(ep.TrustedRosterSHA256) == 0 {
				t.Fatalf("ssh-host-keys API trusted roster for %s: source=%q sha256=%v",
					harness.UploadHost, ep.TrustedSource, ep.TrustedRosterSHA256)
			}
			return
		}
		time.Sleep(harness.DefaultPoll)
	}
	t.Fatalf("ssh-host-keys API did not reach ok for %s:2222; last body=%s", harness.UploadHost, lastBody)
}

func trustedRosterForEndpoint(t *testing.T, raw []byte, host string, port int) (source string, sha256 []string) {
	t.Helper()
	key := fmt.Sprintf("%s:%d", strings.ToLower(strings.TrimSpace(host)), port)

	var v2 struct {
		Version   int `json:"version"`
		Endpoints map[string]struct {
			Source string   `json:"source"`
			SHA256 []string `json:"sha256"`
		} `json:"endpoints"`
	}
	if err := json.Unmarshal(raw, &v2); err != nil {
		t.Fatalf("trusted json: %v", err)
	}
	if v2.Version >= 2 && v2.Endpoints != nil {
		ep, ok := v2.Endpoints[key]
		if !ok {
			keys := make([]string, 0, len(v2.Endpoints))
			for k := range v2.Endpoints {
				keys = append(keys, k)
			}
			t.Fatalf("trusted roster missing endpoint %q (have %v)", key, keys)
		}
		return ep.Source, ep.SHA256
	}

	var v1 struct {
		Source string   `json:"source"`
		SHA256 []string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &v1); err != nil {
		t.Fatalf("trusted json: %v", err)
	}
	return v1.Source, v1.SHA256
}
