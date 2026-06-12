package upload

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"os"
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
	dir := t.TempDir()
	path := filepath.Join(dir, "ssh_known_hosts")
	store, err := newHostKeyStore(path, dir)
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

func TestHostKeyStore_trustedRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ssh_known_hosts")
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}

	key1 := testHostKey(t)
	cb := store.callback("upload.example.com", 2222)
	if err := cb("ignored", nil, key1); err != nil {
		t.Fatalf("first connect: %v", err)
	}

	key2 := testHostKey(t)
	fp2 := ssh.FingerprintSHA256(key2)
	trustedPath := filepath.Join(dir, "upload_ssh_trusted_keys.json")
	payload := fmt.Sprintf(`{"sha256":["%s"]}`, fp2)
	if err := os.WriteFile(trustedPath, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}

	if err := cb("ignored", nil, key2); err != nil {
		t.Fatalf("trusted rotation: %v", err)
	}
	if err := cb("ignored", nil, key2); err != nil {
		t.Fatalf("after rotation: %v", err)
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
