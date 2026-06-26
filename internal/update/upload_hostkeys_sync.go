package update

import (
	"strconv"
	"strings"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

// UploadEndpoint identifies an SFTP upload host for roster sync.
type UploadEndpoint struct {
	Host string
	Port int
}

// NormalizeUploadEndpoint trims host and applies default port 2222.
func NormalizeUploadEndpoint(host string, port int) UploadEndpoint {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" {
		host = "upload.aviationwx.org"
	}
	if port <= 0 {
		port = 2222
	}
	return UploadEndpoint{Host: host, Port: port}
}

// DistinctUploadEndpointsFromCameras returns unique upload targets from enabled cameras.
func DistinctUploadEndpointsFromCameras(cameras []config.Camera) []UploadEndpoint {
	seen := make(map[string]struct{})
	out := make([]UploadEndpoint, 0)
	for _, cam := range cameras {
		if !cam.Enabled || cam.Upload == nil {
			continue
		}
		ep := NormalizeUploadEndpoint(cam.Upload.Host, cam.Upload.Port)
		key := ep.Host + ":" + strconv.Itoa(ep.Port)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ep)
	}
	return out
}

// SyncUploadSSHHostKeysForCameras fetches HTTPS rosters for each distinct enabled camera upload host.
// Returns nil when at least one endpoint sync succeeds. Returns the last error when all fail.
func SyncUploadSSHHostKeysForCameras(configDir string, cameras []config.Camera) error {
	endpoints := DistinctUploadEndpointsFromCameras(cameras)
	if len(endpoints) == 0 {
		return nil
	}
	var lastErr error
	var anyOK bool
	for _, ep := range endpoints {
		if err := SyncUploadSSHHostKeysHTTPS(configDir, ep.Host, ep.Port); err != nil {
			lastErr = err
			continue
		}
		anyOK = true
	}
	if anyOK {
		return nil
	}
	return lastErr
}
