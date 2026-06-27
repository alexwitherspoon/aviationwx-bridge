//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/test/e2e/harness"
	"golang.org/x/crypto/ssh"
)

func TestSFTPIdentity_TestUploadAPI(t *testing.T) {
	harness.RequireE2EStack(t)

	payload, _ := json.Marshal(map[string]interface{}{
		"host":     harness.UploadHost,
		"port":     2222,
		"username": harness.E2EUploadSFTPUser(),
		"password": harness.E2EUploadSFTPPassword(),
	})
	req, err := http.NewRequest(http.MethodPost, harness.BridgeWebURL()+"/api/test/upload", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.SetBasicAuth(harness.BridgeUser, harness.BridgePass)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST test/upload: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d body=%s", resp.StatusCode, body)
	}
}

func TestSFTPIdentity_AuthWithFixtureUser(t *testing.T) {
	harness.RequireE2EStack(t)

	config := &ssh.ClientConfig{
		User: harness.E2EUploadSFTPUser(),
		Auth: []ssh.AuthMethod{
			ssh.Password(harness.E2EUploadSFTPPassword()),
		},
		// codeql[go/insecure-hostkeycallback]: contract test confirms SFTP auth only.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         30 * time.Second,
	}
	client, err := ssh.Dial("tcp", harness.UploadSFTPAddr(), config)
	if err != nil {
		t.Fatalf("ssh dial: %v", err)
	}
	defer func() { _ = client.Close() }()
}
