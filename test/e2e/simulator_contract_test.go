//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/test/e2e/harness"
)

func TestSimulatorContract_HealthAndSnapshot(t *testing.T) {
	harness.RequireE2EStack(t)
	sim := "http://127.0.0.1:18080"
	if err := harness.WaitHTTPGET(sim+"/healthz", http.StatusOK, harness.DefaultWaitTimeout); err != nil {
		t.Fatal(err)
	}
	if err := harness.WaitHTTPGET(sim+"/http/cam-a/snapshot.jpg", http.StatusOK, harness.DefaultWaitTimeout); err != nil {
		t.Fatal(err)
	}
}
