package upload

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/update"
	"golang.org/x/crypto/ssh"
)

// TestHostKeyRotation_endToEnd verifies release metadata sync plus trusted rotation.
func TestHostKeyRotation_endToEnd(t *testing.T) {
	dir, path := testHostKeyDir(t)
	key1 := testHostKey(t)
	key2 := testHostKey(t)
	fp2 := ssh.FingerprintSHA256(key2)

	body := `<!-- AVIATIONWX_RELEASE_META {"upload_ssh_host_keys_sha256": ["` + fp2 + `"]} -->`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"body": body})
	}))
	defer server.Close()

	prev := update.TrustedHostKeysReleasesURLForTest()
	update.SetTrustedHostKeysReleasesURLForTest(server.URL)
	t.Cleanup(func() { update.SetTrustedHostKeysReleasesURLForTest(prev) })

	if err := update.SyncTrustedUploadHostKeys(dir); err != nil {
		t.Fatalf("sync: %v", err)
	}

	store, err := newHostKeyStore(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	cb := store.callback("upload.aviationwx.org", 2222)
	if err := cb("ignored", nil, key1); err != nil {
		t.Fatalf("tofu: %v", err)
	}
	if err := cb("ignored", nil, key2); err != nil {
		t.Fatalf("trusted rotation after sync: %v", err)
	}
}
