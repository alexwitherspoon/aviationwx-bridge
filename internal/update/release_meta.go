package update

import (
	"encoding/json"
	"regexp"
	"strings"
)

var (
	releaseMetaHTMLRE = regexp.MustCompile(`AVIATIONWX_RELEASE_META\s+(\{[^}]+\})`)
	metadataFenceRE   = regexp.MustCompile("(?is)##\\s*AVIATIONWX_METADATA\\s*\\n+```json\\s*\\n(\\{.*?\\})\\s*```")
)

// ParseUploadSSHHostKeysSHA256 extracts upload_ssh_host_keys_sha256 from a GitHub release body.
// Fingerprints use OpenSSH form, e.g. SHA256:base64...
func ParseUploadSSHHostKeysSHA256(releaseBody string) []string {
	meta := parseReleaseMetaJSON(releaseBody)
	if meta == nil {
		return nil
	}
	raw, ok := meta["upload_ssh_host_keys_sha256"]
	if !ok {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		s, ok := item.(string)
		if !ok {
			continue
		}
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseReleaseMetaJSON(releaseBody string) map[string]interface{} {
	for _, re := range []*regexp.Regexp{releaseMetaHTMLRE, metadataFenceRE} {
		m := re.FindStringSubmatch(releaseBody)
		if len(m) < 2 {
			continue
		}
		var meta map[string]interface{}
		if err := json.Unmarshal([]byte(m[1]), &meta); err != nil {
			continue
		}
		return meta
	}
	return nil
}
