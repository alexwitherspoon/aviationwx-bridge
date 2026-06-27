package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
	if err := writeTrustedHostKeysForEndpoint(dir, "upload.aviationwx.org", 2222, []string{"SHA256:abc", "sha256:def"}, "https-roster"); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTrustedUploadHostKeysForEndpoint(dir, "upload.aviationwx.org", 2222)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != "SHA256:abc" || got[1] != "SHA256:def" {
		t.Fatalf("got %v", got)
	}
	path := filepath.Join(dir, trustedHostKeysFile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&077 != 0 {
		t.Fatalf("file mode %o, want 0600", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"version": 2`) {
		t.Fatalf("expected v2 file: %s", raw)
	}
}

func TestWriteTrustedHostKeysForEndpoint_doesNotOverwriteOtherHost(t *testing.T) {
	dir := t.TempDir()
	if err := writeTrustedHostKeysForEndpoint(dir, "upload.a.test", 2222, []string{"SHA256:aaa"}, "https-roster"); err != nil {
		t.Fatal(err)
	}
	if err := writeTrustedHostKeysForEndpoint(dir, "upload.b.test", 2222, []string{"SHA256:bbb"}, "https-roster"); err != nil {
		t.Fatal(err)
	}
	gotA, err := LoadTrustedUploadHostKeysForEndpoint(dir, "upload.a.test", 2222)
	if err != nil {
		t.Fatal(err)
	}
	gotB, err := LoadTrustedUploadHostKeysForEndpoint(dir, "upload.b.test", 2222)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotA) != 1 || gotA[0] != "SHA256:aaa" {
		t.Fatalf("host A = %v", gotA)
	}
	if len(gotB) != 1 || gotB[0] != "SHA256:bbb" {
		t.Fatalf("host B = %v", gotB)
	}
}

func TestParseTrustedHostKeysFile_migratesV1ToDefaultEndpoint(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, trustedHostKeysFile)
	v1 := `{"sha256":["SHA256:legacy"],"updated_at":"2026-06-26T00:00:00Z","source":"https-roster"}`
	if err := os.WriteFile(path, []byte(v1), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadTrustedUploadHostKeys(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "SHA256:legacy" {
		t.Fatalf("got %v", got)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"version": 2`) {
		t.Fatal("expected on-disk migration to v2")
	}
	other, err := LoadTrustedUploadHostKeysForEndpoint(dir, "upload.other.test", 2222)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("other host should be empty until synced: %v", other)
	}
}

func TestWriteTrustedHostKeysFile_emptyListNoWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, trustedHostKeysFile)
	if err := writeTrustedHostKeysFile(path, nil, "https-roster"); err != nil {
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
