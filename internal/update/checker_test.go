package update

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewChecker(t *testing.T) {
	c := NewChecker("v1.0.0", "abc1234")
	if c == nil {
		t.Fatal("NewChecker returned nil")
	}
	if c.currentVersion != "v1.0.0" {
		t.Errorf("currentVersion = %s, want v1.0.0", c.currentVersion)
	}
	if c.currentCommit != "abc1234" {
		t.Errorf("currentCommit = %s, want abc1234", c.currentCommit)
	}
}

func TestChecker_Status(t *testing.T) {
	c := NewChecker("v1.0.0", "abc1234")
	status := c.Status()

	if status.CurrentVersion != "v1.0.0" {
		t.Errorf("CurrentVersion = %s, want v1.0.0", status.CurrentVersion)
	}
	if status.CurrentCommit != "abc1234" {
		t.Errorf("CurrentCommit = %s, want abc1234", status.CurrentCommit)
	}
	if status.UpdateAvailable {
		t.Error("UpdateAvailable should be false initially")
	}
}

func TestChecker_isNewerVersion(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		tagName        string
		expected       bool
	}{
		{
			name:           "dev version - any release is newer",
			currentVersion: "dev",
			tagName:        "v1.0.0",
			expected:       true,
		},
		{
			name:           "same version",
			currentVersion: "v1.0.0",
			tagName:        "v1.0.0",
			expected:       false,
		},
		{
			name:           "newer version",
			currentVersion: "v1.0.0",
			tagName:        "v1.1.0",
			expected:       true,
		},
		{
			name:           "older version",
			currentVersion: "v1.1.0",
			tagName:        "v1.0.0",
			expected:       false,
		},
		{
			name:           "empty current version",
			currentVersion: "",
			tagName:        "v1.0.0",
			expected:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewChecker(tt.currentVersion, "abc")
			got := c.isNewerVersion(tt.tagName)
			if got != tt.expected {
				t.Errorf("isNewerVersion(%s) = %v, want %v", tt.tagName, got, tt.expected)
			}
		})
	}
}

func TestChecker_StartStop(t *testing.T) {
	c := NewChecker("v1.0.0", "abc")
	c.Start()

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	c.Stop()
}

func TestUpdateStatus_MarshalJSON(t *testing.T) {
	status := UpdateStatus{
		CurrentVersion:  "v1.0.0",
		CurrentCommit:   "abc1234",
		LatestVersion:   "v1.1.0",
		UpdateAvailable: true,
		LastCheck:       time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(status)
	if err != nil {
		t.Fatalf("MarshalJSON failed: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if result["current_version"] != "v1.0.0" {
		t.Errorf("current_version = %v, want v1.0.0", result["current_version"])
	}
	if result["update_available"] != true {
		t.Error("update_available should be true")
	}
}

func TestChecker_CheckWithMockServer(t *testing.T) {
	// Create a mock server that returns a release
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"tag_name": "v2.0.0",
			"html_url": "https://github.com/test/releases/v2.0.0",
		})
	}))
	defer server.Close()

	// Note: We can't easily test the actual Check() function because it uses
	// a hardcoded GitHub URL. In production code, we'd inject the URL.
	// For now, just test the status update mechanism.

	c := NewChecker("v1.0.0", "abc")
	c.setResult("v2.0.0", "https://test.com/release", true)

	status := c.Status()
	if !status.UpdateAvailable {
		t.Error("UpdateAvailable should be true after setResult")
	}
	if status.LatestVersion != "v2.0.0" {
		t.Errorf("LatestVersion = %s, want v2.0.0", status.LatestVersion)
	}
}

func TestChecker_SetError(t *testing.T) {
	c := NewChecker("v1.0.0", "abc")
	testErr := http.ErrBodyNotAllowed // Any error

	c.setError(testErr)

	status := c.Status()
	if status.LastError == nil {
		t.Error("LastError should be set")
	}
	if status.LastCheck.IsZero() {
		t.Error("LastCheck should be set")
	}
}
