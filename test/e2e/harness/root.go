//go:build e2e

package harness

import (
	"os"
	"path/filepath"
)

// RepoRoot returns the repository root for E2E path resolution.
// Set AVIATIONWX_E2E_ROOT in scripts/e2e-run.sh; otherwise walk up from cwd for go.mod.
func RepoRoot() string {
	if v := os.Getenv("AVIATIONWX_E2E_ROOT"); v != "" {
		return v
	}
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

// E2EPath joins path segments under the repository root.
func E2EPath(parts ...string) string {
	return filepath.Join(append([]string{RepoRoot()}, parts...)...)
}
