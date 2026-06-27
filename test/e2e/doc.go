// Package e2e contains local integration tests for the bridge harness.
//
// Run via make e2e or scripts/e2e-run.sh. Tests use //go:build e2e and are excluded
// from default go test ./...
//
// Add new suites as *_test.go files in this package. Use test/e2e/harness for shared
// helpers. Contract tests should fail when aviationwx.org or camera-simulator shape
// drifts; fix harness fixtures before changing bridge production code.
package e2e
