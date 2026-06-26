package upload

import (
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func testKnownHostsPath(t *testing.T) string {
	t.Helper()
	_, path := testHostKeyDir(t)
	return path
}

func testHostKeyDir(t *testing.T) (dir, knownHostsPath string) {
	t.Helper()
	dir = t.TempDir()
	return dir, filepath.Join(dir, "ssh_known_hosts")
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

func writeTrustedJSON(t *testing.T, dir string, fps ...string) {
	t.Helper()
	var b strings.Builder
	b.WriteString(`{"sha256":[`)
	for i, fp := range fps {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%q", fp)
	}
	b.WriteString(`]}`)
	path := filepath.Join(dir, "upload_ssh_trusted_keys.json")
	if err := os.WriteFile(path, []byte(b.String()), 0600); err != nil {
		t.Fatal(err)
	}
}

func countKnownHostLines(t *testing.T, path, label string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatal(err)
	}
	n := 0
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if hostFieldMatches(fields[0], label) {
			n++
		}
	}
	return n
}

func TestNewHostKeyStore_requiresPath(t *testing.T) {
	if _, err := newHostKeyStore("", t.TempDir()); err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestNewHostKeyStore_defaultConfigDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ssh_known_hosts")
	store, err := newHostKeyStore(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if store.configDir != dir {
		t.Fatalf("configDir = %q, want %q", store.configDir, dir)
	}
}

func TestVerify_rejectsInvalidInput(t *testing.T) {
	dir, path := testHostKeyDir(t)
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	key := testHostKey(t)
	if err := store.verify("", key, "", 0); err == nil {
		t.Fatal("expected error for empty label")
	}
	if err := store.verify("[h]:2222", nil, "", 0); err == nil {
		t.Fatal("expected error for nil key")
	}
}

func TestHostKeyStore_TOFUAndMismatch(t *testing.T) {
	dir, path := testHostKeyDir(t)
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}

	key1 := testHostKey(t)
	cb := store.callback("upload.example.com", 2222)
	if err := cb("ignored", nil, key1); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	if countKnownHostLines(t, path, "[upload.example.com]:2222") != 1 {
		t.Fatal("expected one known_hosts entry after TOFU")
	}

	key2 := testHostKey(t)
	if err := cb("ignored", nil, key2); err == nil {
		t.Fatal("expected host key mismatch on untrusted second key")
	}

	if err := cb("ignored", nil, key1); err != nil {
		t.Fatalf("repeat connect with same key: %v", err)
	}
}

func TestHostKeyStore_trustedRotation(t *testing.T) {
	dir, path := testHostKeyDir(t)
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
	writeTrustedJSON(t, dir, ssh.FingerprintSHA256(key2))

	if err := cb("ignored", nil, key2); err != nil {
		t.Fatalf("trusted rotation: %v", err)
	}
	if countKnownHostLines(t, path, "[upload.example.com]:2222") != 1 {
		t.Fatal("replace should leave exactly one line for host")
	}
	if err := cb("ignored", nil, key2); err != nil {
		t.Fatalf("after rotation: %v", err)
	}
	if err := cb("ignored", nil, key1); err == nil {
		t.Fatal("old key should be rejected after rotation")
	}
}

func TestHostKeyStore_trustedRotation_caseInsensitiveFingerprint(t *testing.T) {
	dir, path := testHostKeyDir(t)
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	cb := store.callback("upload.example.com", 2222)
	if err := cb("ignored", nil, testHostKey(t)); err != nil {
		t.Fatal(err)
	}

	key2 := testHostKey(t)
	fp := strings.ToLower(ssh.FingerprintSHA256(key2))
	writeTrustedJSON(t, dir, fp)

	if err := cb("ignored", nil, key2); err != nil {
		t.Fatalf("case-insensitive trusted fp: %v", err)
	}
	_ = path
}

func TestHostKeyStore_trustedRotation_ignoresUntrustedList(t *testing.T) {
	dir, path := testHostKeyDir(t)
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	cb := store.callback("upload.example.com", 2222)
	if err := cb("ignored", nil, testHostKey(t)); err != nil {
		t.Fatal(err)
	}

	writeTrustedJSON(t, dir, "SHA256:notTheServersKey")
	if err := cb("ignored", nil, testHostKey(t)); err == nil {
		t.Fatal("expected mismatch when trusted list does not include server key")
	}
	_ = path
}

func TestHostKeyStore_trustedFirstConnectRejectsUnknownKey(t *testing.T) {
	dir, path := testHostKeyDir(t)
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	writeTrustedJSON(t, dir, "SHA256:notTheServersKey")
	cb := store.callback("upload.example.com", 2222)
	if err := cb("ignored", nil, testHostKey(t)); err == nil {
		t.Fatal("expected rejection when trusted roster exists but server key is not listed")
	}
}

func TestHostKeyStore_firstConnectFetchesRosterBeforeTOFU(t *testing.T) {
	dir, path := testHostKeyDir(t)
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	serverKey := testHostKey(t)
	store.syncHTTPS = func(configDir, host string, port int) error {
		writeTrustedJSON(t, configDir, ssh.FingerprintSHA256(serverKey))
		return nil
	}
	cb := store.callback("upload.example.com", 2222)
	if err := cb("ignored", nil, serverKey); err != nil {
		t.Fatalf("expected TOFU after roster fetch: %v", err)
	}
	_ = path
}

func TestHostKeyStore_firstConnectRejectsAfterRosterFetch(t *testing.T) {
	dir, path := testHostKeyDir(t)
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	store.syncHTTPS = func(configDir, host string, port int) error {
		writeTrustedJSON(t, configDir, "SHA256:notTheServersKey")
		return nil
	}
	cb := store.callback("upload.example.com", 2222)
	if err := cb("ignored", nil, testHostKey(t)); err == nil {
		t.Fatal("expected rejection after roster fetch on first connect")
	}
	_ = path
}

func TestHostKeyStore_refreshTrustedOnMismatch(t *testing.T) {
	dir, path := testHostKeyDir(t)
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}

	key1 := testHostKey(t)
	key2 := testHostKey(t)
	cb := store.callback("upload.example.com", 2222)
	if err := cb("ignored", nil, key1); err != nil {
		t.Fatal(err)
	}

	store.syncHTTPS = func(configDir, host string, port int) error {
		writeTrustedJSON(t, configDir, ssh.FingerprintSHA256(key2))
		return nil
	}
	store.lastTrustedFetch = time.Time{} // allow refresh

	if err := cb("ignored", nil, key2); err != nil {
		t.Fatalf("expected sync+rotate: %v", err)
	}
}

func TestHostKeyStore_refreshTrustedForceOnMismatch(t *testing.T) {
	dir, path := testHostKeyDir(t)
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	cb := store.callback("upload.example.com", 2222)
	if err := cb("ignored", nil, testHostKey(t)); err != nil {
		t.Fatal(err)
	}

	called := 0
	store.syncHTTPS = func(string, string, int) error {
		called++
		return nil
	}
	store.lastTrustedFetch = time.Now()

	if err := cb("ignored", nil, testHostKey(t)); err == nil {
		t.Fatal("expected mismatch")
	}
	if called != 1 {
		t.Fatalf("force refresh on mismatch: sync called %d times, want 1", called)
	}
	_ = path
}

func TestHostKeyStore_refreshRetriesAfterSyncFailure(t *testing.T) {
	dir, path := testHostKeyDir(t)
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	cb := store.callback("upload.example.com", 2222)
	if err := cb("ignored", nil, testHostKey(t)); err != nil {
		t.Fatal(err)
	}

	called := 0
	store.syncHTTPS = func(string, string, int) error {
		called++
		return fmt.Errorf("network down")
	}
	store.lastTrustedFetch = time.Time{}

	if err := cb("ignored", nil, testHostKey(t)); err == nil {
		t.Fatal("expected mismatch")
	}
	if called != 1 {
		t.Fatalf("sync called %d times, want 1", called)
	}
	if !store.lastTrustedFetch.IsZero() {
		t.Fatal("lastTrustedFetch should not advance after failed sync")
	}

	if err := cb("ignored", nil, testHostKey(t)); err == nil {
		t.Fatal("expected mismatch on retry")
	}
	if called != 2 {
		t.Fatalf("sync called %d times after retry, want 2", called)
	}
	_ = path
}

func TestHostKeyStore_separateLabelsPerPort(t *testing.T) {
	dir, path := testHostKeyDir(t)
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	key22 := testHostKey(t)
	key2222 := testHostKey(t)

	if err := store.callback("host.example", 22)("h", nil, key22); err != nil {
		t.Fatal(err)
	}
	if err := store.callback("host.example", 2222)("h", nil, key2222); err != nil {
		t.Fatal(err)
	}
	if countKnownHostLines(t, path, "host.example") != 1 {
		t.Fatal("port 22 label")
	}
	if countKnownHostLines(t, path, "[host.example]:2222") != 1 {
		t.Fatal("port 2222 label")
	}
}

func TestHostFieldMatches_commaSeparated(t *testing.T) {
	if !hostFieldMatches("host.a,host.b", "host.b") {
		t.Fatal("expected match in comma list")
	}
	if hostFieldMatches("host.a,host.b", "host.c") {
		t.Fatal("unexpected match")
	}
}

func TestReplace_preservesCommentsAndOtherHosts(t *testing.T) {
	dir, path := testHostKeyDir(t)
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}

	other := testHostKey(t)
	initial := testHostKey(t)
	labelA := "[upload.a]:2222"
	labelB := "[upload.b]:2222"
	if err := store.append(labelA, initial); err != nil {
		t.Fatal(err)
	}
	if err := store.append(labelB, other); err != nil {
		t.Fatal(err)
	}
	comment := "# managed by bridge\n"
	if err := os.WriteFile(path, append([]byte(comment), mustRead(t, path)...), 0600); err != nil {
		t.Fatal(err)
	}

	rotated := testHostKey(t)
	if err := store.replace(labelA, rotated); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "# managed by bridge") {
		t.Fatal("comment removed")
	}
	if countKnownHostLines(t, path, labelA) != 1 {
		t.Fatal("label A should have one line")
	}
	if countKnownHostLines(t, path, labelB) != 1 {
		t.Fatal("label B should remain")
	}
	if err := store.verify(labelA, rotated, "upload.a", 2222); err != nil {
		t.Fatalf("rotated key should verify: %v", err)
	}
	if err := store.verify(labelA, initial, "upload.a", 2222); err == nil {
		t.Fatal("initial key should no longer verify")
	}
}

func TestGetSharedHostKeyStore_samePath(t *testing.T) {
	dir, path := testHostKeyDir(t)
	a, err := getSharedHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := getSharedHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("expected same hostKeyStore instance for identical known_hosts path")
	}
}

func TestHostKeyStore_concurrentVerify(t *testing.T) {
	dir, path := testHostKeyDir(t)
	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	store.syncHTTPS = func(string, string, int) error {
		return fmt.Errorf("offline")
	}
	key := testHostKey(t)
	cb := store.callback("upload.example.com", 2222)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = cb("ignored", nil, key)
		}()
	}
	wg.Wait()
	if countKnownHostLines(t, path, "[upload.example.com]:2222") != 1 {
		t.Fatal("expected single TOFU entry after concurrent verify")
	}
}

func TestKnownHostLabel(t *testing.T) {
	tests := []struct {
		host string
		port int
		want string
	}{
		{"host.example", 22, "host.example"},
		{"host.example", 0, "host.example"},
		{"host.example", 2222, "[host.example]:2222"},
		{"  ", 22, ""},
	}
	for _, tt := range tests {
		if got := knownHostLabel(tt.host, tt.port); got != tt.want {
			t.Fatalf("knownHostLabel(%q, %d) = %q, want %q", tt.host, tt.port, got, tt.want)
		}
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
