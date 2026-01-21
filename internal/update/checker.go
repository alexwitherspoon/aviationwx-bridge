// Package update provides update checking functionality
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const (
	// GitHub API URL for releases
	releasesURL = "https://api.github.com/repos/alexwitherspoon/aviationwx-bridge/releases/latest"

	// Check interval - check once per hour
	defaultCheckInterval = time.Hour

	// Timeout for API requests
	requestTimeout = 10 * time.Second
)

// Checker periodically checks for new releases
type Checker struct {
	currentVersion string
	currentCommit  string
	mu             sync.RWMutex
	latestVersion  string
	latestURL      string
	updateAvail    bool
	lastCheck      time.Time
	lastError      error
	checkInterval  time.Duration
	ctx            context.Context
	cancel         context.CancelFunc
}

// NewChecker creates a new update checker
func NewChecker(currentVersion, currentCommit string) *Checker {
	ctx, cancel := context.WithCancel(context.Background())
	return &Checker{
		currentVersion: currentVersion,
		currentCommit:  currentCommit,
		checkInterval:  defaultCheckInterval,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start begins periodic checking for updates
func (c *Checker) Start() {
	go c.run()
}

// Stop stops the update checker
func (c *Checker) Stop() {
	c.cancel()
}

// Check performs an immediate check for updates
func (c *Checker) Check() error {
	return c.checkNow()
}

// Status returns the current update status
func (c *Checker) Status() UpdateStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return UpdateStatus{
		CurrentVersion:  c.currentVersion,
		CurrentCommit:   c.currentCommit,
		LatestVersion:   c.latestVersion,
		LatestURL:       c.latestURL,
		UpdateAvailable: c.updateAvail,
		LastCheck:       c.lastCheck,
		LastError:       c.lastError,
	}
}

// UpdateStatus represents the current update check status
type UpdateStatus struct {
	CurrentVersion  string    `json:"current_version"`
	CurrentCommit   string    `json:"current_commit"`
	LatestVersion   string    `json:"latest_version,omitempty"`
	LatestURL       string    `json:"latest_url,omitempty"`
	UpdateAvailable bool      `json:"update_available"`
	LastCheck       time.Time `json:"last_check,omitempty"`
	LastError       error     `json:"-"`
	ErrorMessage    string    `json:"error,omitempty"`
}

// MarshalJSON custom marshaler to include error message
func (s UpdateStatus) MarshalJSON() ([]byte, error) {
	type Alias UpdateStatus
	aux := struct {
		Alias
		LastCheck string `json:"last_check,omitempty"`
	}{
		Alias: Alias(s),
	}
	if s.LastError != nil {
		aux.ErrorMessage = s.LastError.Error()
	}
	if !s.LastCheck.IsZero() {
		aux.LastCheck = s.LastCheck.Format(time.RFC3339)
	}
	return json.Marshal(aux)
}

func (c *Checker) run() {
	// Initial check after 30 seconds
	timer := time.NewTimer(30 * time.Second)

	for {
		select {
		case <-c.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			_ = c.checkNow() // Ignore error, we'll retry
			timer.Reset(c.checkInterval)
		}
	}
}

func (c *Checker) checkNow() error {
	ctx, cancel := context.WithTimeout(c.ctx, requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releasesURL, nil)
	if err != nil {
		c.setError(err)
		return err
	}

	// Set headers
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "aviationwx-bridge/"+c.currentVersion)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.setError(err)
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			return
		}
	}()

	if resp.StatusCode == http.StatusNotFound {
		// No releases yet
		c.setResult("", "", false)
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		err := fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
		c.setError(err)
		return err
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		c.setError(err)
		return err
	}

	// Compare versions
	updateAvailable := c.isNewerVersion(release.TagName)

	c.setResult(release.TagName, release.HTMLURL, updateAvailable)
	return nil
}

// isNewerVersion checks if the release tag is newer than current version
func (c *Checker) isNewerVersion(tagName string) bool {
	// If current is "dev" or "unknown", don't show updates
	// (dev/edge builds should be managed manually)
	if c.currentVersion == "dev" || c.currentVersion == "" {
		return false
	}

	// Strip 'v' prefix from both versions for comparison
	currentVer := c.currentVersion
	if len(currentVer) > 0 && currentVer[0] == 'v' {
		currentVer = currentVer[1:]
	}

	latestVer := tagName
	if len(latestVer) > 0 && latestVer[0] == 'v' {
		latestVer = latestVer[1:]
	}

	// If versions are identical, no update needed
	if currentVer == latestVer {
		return false
	}

	// Simple semantic version comparison (major.minor.patch)
	// For proper semver comparison, consider github.com/Masterminds/semver
	return compareVersions(latestVer, currentVer) > 0
}

// compareVersions compares two semantic version strings
// Returns: 1 if v1 > v2, -1 if v1 < v2, 0 if equal
func compareVersions(v1, v2 string) int {
	// Split versions into components
	parts1 := parseVersion(v1)
	parts2 := parseVersion(v2)

	// Compare each component
	for i := 0; i < 3; i++ {
		if parts1[i] > parts2[i] {
			return 1
		}
		if parts1[i] < parts2[i] {
			return -1
		}
	}

	return 0 // Equal
}

// parseVersion parses a version string like "2.0.3" into [2, 0, 3]
func parseVersion(v string) [3]int {
	var parts [3]int
	var current int
	var idx int

	for i := 0; i < len(v) && idx < 3; i++ {
		if v[i] >= '0' && v[i] <= '9' {
			current = current*10 + int(v[i]-'0')
		} else if v[i] == '.' {
			parts[idx] = current
			current = 0
			idx++
		}
	}

	if idx < 3 {
		parts[idx] = current
	}

	return parts
}

func (c *Checker) setError(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastError = err
	c.lastCheck = time.Now()
}

func (c *Checker) setResult(version, url string, updateAvailable bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latestVersion = version
	c.latestURL = url
	c.updateAvail = updateAvailable
	c.lastCheck = time.Now()
	c.lastError = nil
}
