package camera

import "strings"

// NormalizeONVIFEndpoint removes duplicate http:// or https:// prefixes (e.g. when a full URL
// was pasted into a field that already included a scheme). The scheme of the original value
// is preserved: if the string began with https://, the result uses https://; otherwise http://.
func NormalizeONVIFEndpoint(raw string) string {
	orig := strings.TrimSpace(raw)
	if orig == "" {
		return ""
	}
	preferHTTPS := strings.HasPrefix(strings.ToLower(orig), "https://")
	s := orig
	for range 10 {
		lower := strings.ToLower(s)
		if strings.HasPrefix(lower, "https://") {
			s = s[len("https://"):]
			continue
		}
		if strings.HasPrefix(lower, "http://") {
			s = s[len("http://"):]
			continue
		}
		break
	}
	s = strings.TrimSpace(s)
	s = strings.TrimLeft(s, "/")
	if s == "" {
		return ""
	}
	if preferHTTPS {
		return "https://" + s
	}
	return "http://" + s
}
