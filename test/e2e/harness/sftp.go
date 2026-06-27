//go:build e2e

package harness

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// WaitForUploadedBridgeJPEG waits for a JPEG under {sftpRoot}/{user}/files stamped by the bridge.
func WaitForUploadedBridgeJPEG(sftpRoot, user string, timeout time.Duration) (string, error) {
	root := filepath.Join(sftpRoot, user, "files")
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if path, ok := findBridgeJPEG(root); ok {
			return path, nil
		}
		time.Sleep(DefaultPoll)
	}
	return "", fmt.Errorf("no uploaded bridge JPEG under %s within %s", root, timeout)
}

func findBridgeJPEG(root string) (string, bool) {
	var match string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".jpg") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil || len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
			return nil
		}
		out, err := exec.Command("exiftool", "-UserComment", "-b", path).Output()
		if err != nil || !strings.Contains(string(out), "AviationWX-Bridge") {
			return nil
		}
		match = path
		return fs.SkipAll
	})
	if match == "" {
		return "", false
	}
	return match, true
}
