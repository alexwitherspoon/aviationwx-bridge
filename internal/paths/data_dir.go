// Package paths resolves in-container paths for the mounted host data directory.
package paths

import "os"

// HostDataDir returns the container-visible mount for the host data directory.
// Host scripts default to /data/aviationwx (scripts/aviationwx-supervisor.sh) and
// bind-mount it to /data (-v "${DATA_DIR}:/data" in aviationwx-container-start.sh).
// Local docker-compose mounts ./data at /data directly. Override with AVIATIONWX_DATA_DIR.
func HostDataDir() string {
	if d := os.Getenv("AVIATIONWX_DATA_DIR"); d != "" {
		return d
	}
	return "/data"
}
