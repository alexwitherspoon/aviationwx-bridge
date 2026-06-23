// Package paths resolves host-visible data directory paths inside the container.
package paths

import "os"

// HostDataDir is where supervisor and host scripts persist state. On Pi installs
// /data/aviationwx is bind-mounted to /data in the container; local docker-compose
// mounts ./data at /data directly.
func HostDataDir() string {
	if d := os.Getenv("AVIATIONWX_DATA_DIR"); d != "" {
		return d
	}
	return "/data"
}
