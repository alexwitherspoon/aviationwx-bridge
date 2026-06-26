//go:build e2e

package e2e

import (
	"net/http"
	"testing"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/test/e2e/harness"
)

func TestSmoke_BridgeReadyz(t *testing.T) {
	harness.RequireE2EStack(t)
	if err := harness.WaitHTTPGET(harness.BridgeWebURL()+"/readyz", http.StatusOK, harness.DefaultWaitTimeout); err != nil {
		t.Fatal(err)
	}
}
