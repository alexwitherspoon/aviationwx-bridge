package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseUploadSSHHostKeysSHA256_releaseWorkflowFormat(t *testing.T) {
	body := `## AviationWX.org Bridge v2.9.4

<!-- AVIATIONWX_RELEASE_META {"version": "2.9.4", "min_host_version": "2.3", "upload_ssh_host_keys_sha256": ["SHA256:abc123", "SHA256:def456"]} -->
`
	got := ParseUploadSSHHostKeysSHA256(body)
	if len(got) != 2 || got[0] != "SHA256:abc123" {
		t.Fatalf("got %v", got)
	}
}

func TestParseUploadSSHHostKeysSHA256_releaseTemplateFenceFormat(t *testing.T) {
	body := `## What's Changed

## AVIATIONWX_METADATA

` + "```json\n" + `{
  "min_host_version": "2.0",
  "upload_ssh_host_keys_sha256": ["SHA256:fence123"]
}
` + "```\n"
	got := ParseUploadSSHHostKeysSHA256(body)
	if len(got) != 1 || got[0] != "SHA256:fence123" {
		t.Fatalf("got %v", got)
	}
}

func TestParseUploadSSHHostKeysSHA256_missing(t *testing.T) {
	if got := ParseUploadSSHHostKeysSHA256("no metadata"); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestParseUploadSSHHostKeysSHA256_malformedJSON(t *testing.T) {
	body := `<!-- AVIATIONWX_RELEASE_META {not json} -->`
	if got := ParseUploadSSHHostKeysSHA256(body); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestParseUploadSSHHostKeysSHA256_wrongType(t *testing.T) {
	body := `<!-- AVIATIONWX_RELEASE_META {"upload_ssh_host_keys_sha256": "SHA256:only-one"} -->`
	if got := ParseUploadSSHHostKeysSHA256(body); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestParseUploadSSHHostKeysSHA256_skipsBlankEntries(t *testing.T) {
	body := `<!-- AVIATIONWX_RELEASE_META {"upload_ssh_host_keys_sha256": ["SHA256:ok", "", "  "]} -->`
	got := ParseUploadSSHHostKeysSHA256(body)
	if len(got) != 1 || got[0] != "SHA256:ok" {
		t.Fatalf("got %v", got)
	}
}

func TestNormalizeFingerprint(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  ", ""},
		{"abc", "SHA256:abc"},
		{"SHA256:abc", "SHA256:abc"},
		{"sha256:abc", "SHA256:abc"},
		{"SHA256:SHA256:abc", "SHA256:abc"},
	}
	for _, tt := range tests {
		if got := normalizeFingerprint(tt.in); got != tt.want {
			t.Fatalf("normalizeFingerprint(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestNormalizeFingerprintList_dedupes(t *testing.T) {
	got := normalizeFingerprintList([]string{"SHA256:x", "sha256:x", "SHA256:y"})
	if len(got) != 2 || got[0] != "SHA256:x" || got[1] != "SHA256:y" {
		t.Fatalf("got %v", got)
	}
}

func TestWriteTrustedHostKeysFile_roundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, trustedHostKeysFile)
	if err := writeTrustedHostKeysFile(path, []string{"SHA256:abc", "sha256:def"}, "test"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTrustedUploadHostKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "SHA256:abc" || got[1] != "SHA256:def" {
		t.Fatalf("got %v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&077 != 0 {
		t.Fatalf("file mode %o, want 0600", info.Mode().Perm())
	}
}

func TestWriteTrustedHostKeysFile_emptyListNoWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, trustedHostKeysFile)
	if err := writeTrustedHostKeysFile(path, nil, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected no file for empty fingerprint list")
	}
}

func TestLoadTrustedUploadHostKeys_missingFile(t *testing.T) {
	got, err := LoadTrustedUploadHostKeys(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestLoadTrustedUploadHostKeys_invalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, trustedHostKeysFile)
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTrustedUploadHostKeys(dir); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestSyncTrustedUploadHostKeys_requiresConfigDir(t *testing.T) {
	if err := SyncTrustedUploadHostKeys("  "); err == nil {
		t.Fatal("expected error")
	}
}

func TestSyncTrustedUploadHostKeys_fromMockRelease(t *testing.T) {
	dir := t.TempDir()
	body := `<!-- AVIATIONWX_RELEASE_META {"upload_ssh_host_keys_sha256": ["SHA256:from-release", "SHA256:backup"]} -->`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github.v3+json" {
			t.Errorf("Accept header = %q", r.Header.Get("Accept"))
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"body": body})
	}))
	defer server.Close()

	prev := trustedHostKeysReleasesURL
	trustedHostKeysReleasesURL = server.URL
	t.Cleanup(func() { trustedHostKeysReleasesURL = prev })

	if err := SyncTrustedUploadHostKeys(dir); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTrustedUploadHostKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "SHA256:from-release" {
		t.Fatalf("got %v", got)
	}
	raw, err := os.ReadFile(filepath.Join(dir, trustedHostKeysFile))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"source": "github-release"`) {
		t.Fatalf("missing source in %s", raw)
	}
}

func TestSyncTrustedUploadHostKeys_noKeysInRelease(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"body": "no metadata"})
	}))
	defer server.Close()

	prev := trustedHostKeysReleasesURL
	trustedHostKeysReleasesURL = server.URL
	t.Cleanup(func() { trustedHostKeysReleasesURL = prev })

	if err := SyncTrustedUploadHostKeys(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, trustedHostKeysFile)); !os.IsNotExist(err) {
		t.Fatal("expected no trusted keys file when metadata omits fingerprints")
	}
}

func TestSyncTrustedUploadHostKeys_github404(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	prev := trustedHostKeysReleasesURL
	trustedHostKeysReleasesURL = server.URL
	t.Cleanup(func() { trustedHostKeysReleasesURL = prev })

	if err := SyncTrustedUploadHostKeys(dir); err != nil {
		t.Fatal(err)
	}
}

func TestSyncTrustedUploadHostKeys_overwritesPriorFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, trustedHostKeysFile)
	if err := writeTrustedHostKeysFile(path, []string{"SHA256:old"}, "test"); err != nil {
		t.Fatal(err)
	}

	body := `<!-- AVIATIONWX_RELEASE_META {"upload_ssh_host_keys_sha256": ["SHA256:new"]} -->`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"body": body})
	}))
	defer server.Close()

	prev := trustedHostKeysReleasesURL
	trustedHostKeysReleasesURL = server.URL
	t.Cleanup(func() { trustedHostKeysReleasesURL = prev })

	if err := SyncTrustedUploadHostKeys(dir); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTrustedUploadHostKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "SHA256:new" {
		t.Fatalf("got %v", got)
	}
}
