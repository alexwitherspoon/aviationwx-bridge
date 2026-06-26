package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestSyncUploadSSHHostKeysHTTPS_productionRosterFixture(t *testing.T) {
	dir := t.TempDir()
	const host = "upload.aviationwx.org"
	// Production roster shape captured 2026-06-26.
	body := `{
  "version": 1,
  "hostname": "upload.aviationwx.org",
  "port": 2222,
  "sha256": [
    "SHA256:e/16Fvzq8ZFnM9bfxU5bZKFlboKmsP3AbJN8W9jOLUI",
    "SHA256:fdya7uGmJ+FFtMwZRYSzEghY1IYSFp7tk1aklUftoII",
    "SHA256:vSPiVyfZoYXdW9nDDCPDedGg+UOuaSFEn231XNHGOsI"
  ],
  "updated_at": "2026-06-26T00:44:36Z"
}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	old := uploadHostKeysHTTPClient
	uploadHostKeysHTTPClient = &http.Client{
		Transport: &rewriteHostTransport{base: server.URL, inner: http.DefaultTransport},
	}
	defer func() { uploadHostKeysHTTPClient = old }()

	if err := SyncUploadSSHHostKeysHTTPS(dir, host, 2222); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, err := LoadTrustedUploadHostKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
}

func TestValidateUploadSSHHostKeysDocument(t *testing.T) {
	doc := &uploadSSHHostKeysDocument{
		Version:  1,
		Hostname: "upload.example.com",
		Port:     2222,
		SHA256:   []string{"SHA256:abc"},
	}
	if err := validateUploadSSHHostKeysDocument(doc, "upload.example.com", 2222); err != nil {
		t.Fatal(err)
	}
	if err := validateUploadSSHHostKeysDocument(doc, "other.example.com", 2222); err == nil {
		t.Fatal("expected hostname mismatch")
	}
	if err := validateUploadSSHHostKeysDocument(doc, "upload.example.com", 2223); err == nil {
		t.Fatal("expected port mismatch")
	}
}

func TestSyncUploadSSHHostKeysHTTPS_replacesStaleRoster(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, trustedHostKeysFile)
	if err := writeTrustedHostKeysFile(path, []string{"SHA256:stale1", "SHA256:stale2"}, "test"); err != nil {
		t.Fatal(err)
	}

	const host = "upload.test"
	body := `{"version":1,"hostname":"upload.test","port":2222,"sha256":["SHA256:current"],"updated_at":"2026-06-26T00:00:00Z"}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	old := uploadHostKeysHTTPClient
	uploadHostKeysHTTPClient = &http.Client{
		Transport: &rewriteHostTransport{base: server.URL, inner: http.DefaultTransport},
	}
	defer func() { uploadHostKeysHTTPClient = old }()

	if err := SyncUploadSSHHostKeysHTTPS(dir, host, 2222); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, err := LoadTrustedUploadHostKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "SHA256:current" {
		t.Fatalf("stale keys not replaced: got %v", got)
	}
}

func TestSyncUploadSSHHostKeysHTTPS_mockServer(t *testing.T) {
	dir := t.TempDir()
	const host = "upload.test"
	body := `{"version":1,"hostname":"upload.test","port":2222,"sha256":["SHA256:fromhttps"],"updated_at":"2026-06-26T00:00:00Z"}`

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	client := server.Client()
	SetUploadHostKeysHTTPClientForTest(client)
	t.Cleanup(func() { SetUploadHostKeysHTTPClientForTest(nil) })

	// Patch fetch to use test server URL - test via httptest with custom fetch is awkward because
	// fetch builds URL from host. Use integration-style override:
	old := uploadHostKeysHTTPClient
	uploadHostKeysHTTPClient = &http.Client{
		Transport: &rewriteHostTransport{base: server.URL, inner: server.Client().Transport},
	}
	defer func() { uploadHostKeysHTTPClient = old }()

	if err := SyncUploadSSHHostKeysHTTPS(dir, host, 2222); err != nil {
		t.Fatalf("sync: %v", err)
	}
	got, err := LoadTrustedUploadHostKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "SHA256:fromhttps" {
		t.Fatalf("got %v", got)
	}
	data, err := LoadTrustedUploadHostKeysFileData(dir)
	if err != nil || data == nil || data.Source != "https-roster" {
		t.Fatalf("metadata: %+v err=%v", data, err)
	}
}

func TestSyncUploadSSHHostKeysHTTPS_non200KeepsFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, trustedHostKeysFile)
	if err := writeTrustedHostKeysFile(path, []string{"SHA256:keep"}, "test"); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer server.Close()

	uploadHostKeysHTTPClient = &http.Client{
		Transport: &rewriteHostTransport{base: server.URL, inner: http.DefaultTransport},
	}
	defer func() { uploadHostKeysHTTPClient = nil }()

	if err := SyncUploadSSHHostKeysHTTPS(dir, "upload.test", 2222); err == nil {
		t.Fatal("expected error")
	}
	got, _ := LoadTrustedUploadHostKeys(dir)
	if len(got) != 1 || got[0] != "SHA256:keep" {
		t.Fatalf("file changed: %v", got)
	}
}

type rewriteHostTransport struct {
	base  string
	inner http.RoundTripper
}

func (t *rewriteHostTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.URL, _ = req.URL.Parse(t.base + uploadSSHHostKeysWellKnownPath)
	if t.inner == nil {
		t.inner = http.DefaultTransport
	}
	return t.inner.RoundTrip(req2)
}

func TestSyncUploadSSHHostKeysHTTPS_rejectsRedirect(t *testing.T) {
	dir := t.TempDir()
	const host = "upload.test"

	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/roster.json", http.StatusFound)
	}))
	defer redirect.Close()

	uploadHostKeysHTTPClient = &http.Client{
		Transport: &rewriteHostTransport{base: redirect.URL, inner: http.DefaultTransport},
	}
	defer func() { uploadHostKeysHTTPClient = nil }()

	if err := SyncUploadSSHHostKeysHTTPS(dir, host, 2222); err == nil {
		t.Fatal("expected redirect error")
	}
}

func TestUploadSSHHostKeysDocument_unmarshal(t *testing.T) {
	raw := `{"version":1,"hostname":"h","port":2222,"sha256":["SHA256:x"],"updated_at":"t"}`
	var doc uploadSSHHostKeysDocument
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.Port != 2222 {
		t.Fatalf("%+v", doc)
	}
}
