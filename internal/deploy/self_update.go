// Package deploy reads deployment-mode settings from the process environment.
package deploy

import (
	"os"
	"strconv"
)

const selfUpdateEnv = "AVIATIONWX_SELF_UPDATE"

// SelfUpdateEnabled reports whether the web UI may trigger a host supervisor update.
// Pi installs default AVIATIONWX_SELF_UPDATE=1 in aviationwx-container-start.sh (host may override).
// Docker-only deployments leave it unset so operators update via their own tooling.
func SelfUpdateEnabled() bool {
	v := os.Getenv(selfUpdateEnv)
	if v == "" {
		return false
	}
	ok, err := strconv.ParseBool(v)
	return err == nil && ok
}
