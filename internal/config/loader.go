package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// Load loads configuration from the specified file path
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Apply defaults
	applyDefaults(&config)

	// Validate
	if err := Validate(&config); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &config, nil
}

// applyDefaults sets default values for optional fields
func applyDefaults(c *Config) {
	// Upload defaults
	if c.Upload.Port == 0 {
		c.Upload.Port = 2121
	}
	if !c.Upload.TLS {
		c.Upload.TLS = true
	}
	if !c.Upload.TLSVerify {
		c.Upload.TLSVerify = true
	}
	if c.Upload.TimeoutConnectSeconds == 0 {
		c.Upload.TimeoutConnectSeconds = 10
	}
	if c.Upload.TimeoutUploadSeconds == 0 {
		c.Upload.TimeoutUploadSeconds = 30
	}

	// Global defaults
	if c.Global == nil {
		c.Global = &Global{}
	}
	if c.Global.CaptureTimeoutSeconds == 0 {
		c.Global.CaptureTimeoutSeconds = 15
	}
	if c.Global.RTSPTimeoutSeconds == 0 {
		c.Global.RTSPTimeoutSeconds = 20
	}

	// Camera defaults
	for i := range c.Cameras {
		cam := &c.Cameras[i]
		if cam.IntervalSeconds == 0 {
			cam.IntervalSeconds = 60
		}
		if !cam.Enabled {
			cam.Enabled = true
		}
	}

	// SNTP defaults
	if c.SNTP == nil {
		c.SNTP = &SNTP{}
	}
	if c.SNTP.Enabled && len(c.SNTP.Servers) == 0 {
		c.SNTP.Servers = []string{"pool.ntp.org"}
	}
	if c.SNTP.CheckIntervalSeconds == 0 {
		c.SNTP.CheckIntervalSeconds = 300
	}
	if c.SNTP.MaxOffsetSeconds == 0 {
		c.SNTP.MaxOffsetSeconds = 5
	}
	if c.SNTP.TimeoutSeconds == 0 {
		c.SNTP.TimeoutSeconds = 5
	}

	// Web console defaults
	if c.WebConsole == nil {
		c.WebConsole = &WebConsole{}
	}
	if !c.WebConsole.Enabled {
		c.WebConsole.Enabled = true
	}
	if c.WebConsole.Port == 0 {
		c.WebConsole.Port = 8080
	}
}
