package update

import "testing"

func TestParseUploadSSHHostKeysSHA256(t *testing.T) {
	body := `## Release

<!-- AVIATIONWX_RELEASE_META {"version": "2.9.4", "min_host_version": "2.3", "upload_ssh_host_keys_sha256": ["SHA256:abc123", "SHA256:def456"]} -->
`
	got := ParseUploadSSHHostKeysSHA256(body)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0] != "SHA256:abc123" || got[1] != "SHA256:def456" {
		t.Fatalf("got %v", got)
	}
}

func TestParseUploadSSHHostKeysSHA256_missing(t *testing.T) {
	if got := ParseUploadSSHHostKeysSHA256("no metadata"); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}
