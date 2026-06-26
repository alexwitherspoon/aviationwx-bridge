package update

import (
	"os"
	"path/filepath"
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
	path := filepath.Join(dir, trustedHostKeysFile)
	if err := writeTrustedHostKeysFile(path, []string{"SHA256:abc", "sha256:def"}, "https-roster"); err != nil {
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
