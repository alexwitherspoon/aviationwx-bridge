package upload

import (
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// TestHostKeyRotation_endToEnd verifies HTTPS roster refresh plus trusted rotation.
func TestHostKeyRotation_endToEnd(t *testing.T) {
	dir, path := testHostKeyDir(t)
	key1 := testHostKey(t)
	key2 := testHostKey(t)

	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	cb := store.callback("upload.aviationwx.org", 2222)
	if err := cb("ignored", nil, key1); err != nil {
		t.Fatalf("tofu: %v", err)
	}

	store.syncHTTPS = func(configDir, host string, port int) error {
		writeTrustedJSON(t, configDir, ssh.FingerprintSHA256(key2))
		return nil
	}
	store.lastTrustedFetch = time.Time{}

	if err := cb("ignored", nil, key2); err != nil {
		t.Fatalf("trusted rotation after https sync: %v", err)
	}
}
