package update

import (
	"path/filepath"
	"testing"
)

func TestLoadTrustedUploadHostKeys_roundTrip(t *testing.T) {
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
