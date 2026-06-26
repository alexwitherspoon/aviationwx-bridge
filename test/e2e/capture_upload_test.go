//go:build e2e

package e2e

import (
	"os"
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/test/e2e/harness"
)

func TestCaptureUpload_HTTPProgression(t *testing.T) {
	harness.RequireE2EStack(t)

	sim := os.Getenv("E2E_SIMULATOR_URL")
	if sim == "" {
		sim = "http://127.0.0.1:18080"
	}
	if _, err := harness.HTTPPost(sim+"/control/reset", nil); err != nil {
		t.Fatalf("simulator reset: %v", err)
	}

	sftpRoot := harness.E2EPath("testdata", "e2e", "sftp")
	path, err := harness.WaitForUploadedBridgeJPEG(sftpRoot, harness.E2EUploadSFTPUser(), 3*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("uploaded file: %s", path)
}
