package upload

import (
	"crypto/rand"
	"crypto/rsa"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
)

func testKnownHostsPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "ssh_known_hosts")
}

func testHostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ssh.NewPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return pub
}

func TestHostKeyStore_TOFUAndMismatch(t *testing.T) {
	path := testKnownHostsPath(t)
	store, err := newHostKeyStore(path)
	if err != nil {
		t.Fatal(err)
	}

	key1 := testHostKey(t)
	cb := store.callback("upload.example.com", 2222)
	if err := cb("ignored", nil, key1); err != nil {
		t.Fatalf("first connect: %v", err)
	}

	key2 := testHostKey(t)
	if err := cb("ignored", nil, key2); err == nil {
		t.Fatal("expected host key mismatch on second key")
	}

	if err := cb("ignored", nil, key1); err != nil {
		t.Fatalf("repeat connect with same key: %v", err)
	}
}

func TestKnownHostLabel(t *testing.T) {
	if got := knownHostLabel("host.example", 22); got != "host.example" {
		t.Fatalf("port 22: got %q", got)
	}
	if got := knownHostLabel("host.example", 2222); got != "[host.example]:2222" {
		t.Fatalf("port 2222: got %q", got)
	}
}
