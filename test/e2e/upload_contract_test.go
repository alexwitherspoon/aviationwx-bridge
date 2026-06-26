//go:build e2e

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"testing"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/test/e2e/harness"
)

func TestUploadContract_RosterHTTPS(t *testing.T) {
	harness.RequireE2EStack(t)

	client, err := harness.RosterHTTPClient()
	if err != nil {
		t.Fatalf("client: %v", err)
	}

	resp, err := client.Get(harness.UploadRosterURL())
	if err != nil {
		t.Fatalf("GET roster: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d body=%s", resp.StatusCode, body)
	}
	if cc := resp.Header.Get("Cache-Control"); cc == "" || !containsNoStore(cc) {
		t.Fatalf("Cache-Control = %q", cc)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version  int      `json:"version"`
		Hostname string   `json:"hostname"`
		Port     int      `json:"port"`
		SHA256   []string `json:"sha256"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("json: %v", err)
	}
	if doc.Version != 1 {
		t.Fatalf("version = %d", doc.Version)
	}
	if doc.Hostname != harness.UploadHost {
		t.Fatalf("hostname = %q", doc.Hostname)
	}
	if doc.Port != 2222 {
		t.Fatalf("port = %d", doc.Port)
	}
	if len(doc.SHA256) == 0 {
		t.Fatal("empty sha256 roster")
	}
	fpRe := regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]+=*$`)
	for _, fp := range doc.SHA256 {
		if !fpRe.MatchString(fp) {
			t.Fatalf("bad fingerprint %q", fp)
		}
	}
}

func TestUploadContract_SFTPPortOpen(t *testing.T) {
	harness.RequireE2EStack(t)
	if err := harness.WaitTCP(harness.UploadSFTPAddr(), harness.DefaultWaitTimeout); err != nil {
		t.Fatal(err)
	}
}

func containsNoStore(cc string) bool {
	return len(cc) >= 8 && (cc == "no-store" || containsSubstring(cc, "no-store"))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
