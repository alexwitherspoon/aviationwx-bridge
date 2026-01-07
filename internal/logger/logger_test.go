package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name:   "text format",
			config: Config{Level: "info", Format: "text"},
		},
		{
			name:   "json format",
			config: Config{Level: "debug", Format: "json"},
		},
		{
			name:   "default level",
			config: Config{Level: "invalid", Format: "text"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			tt.config.Output = buf
			l := New(tt.config)
			if l == nil {
				t.Fatal("New() returned nil")
			}
		})
	}
}

func TestLogger_Levels(t *testing.T) {
	buf := &bytes.Buffer{}
	l := New(Config{
		Level:  "debug",
		Format: "text",
		Output: buf,
	})

	tests := []struct {
		name  string
		fn    func(string, ...interface{})
		level string
	}{
		{"debug", l.Debug, "DEBUG"},
		{"info", l.Info, "INFO"},
		{"warn", l.Warn, "WARN"},
		{"error", l.Error, "ERROR"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			tt.fn("test message", "key", "value")
			output := buf.String()
			if !strings.Contains(output, tt.level) {
				t.Errorf("output should contain %s, got: %s", tt.level, output)
			}
			if !strings.Contains(output, "test message") {
				t.Errorf("output should contain message, got: %s", output)
			}
		})
	}
}

func TestLogger_With(t *testing.T) {
	buf := &bytes.Buffer{}
	l := New(Config{
		Level:  "info",
		Format: "text",
		Output: buf,
	})

	child := l.With("component", "test")
	if child == nil {
		t.Fatal("With() returned nil")
	}

	child.Info("test message")
	output := buf.String()
	if !strings.Contains(output, "component") {
		t.Errorf("output should contain component context, got: %s", output)
	}
}

func TestLogger_DebugFiltering(t *testing.T) {
	buf := &bytes.Buffer{}
	l := New(Config{
		Level:  "info", // Debug should be filtered
		Format: "text",
		Output: buf,
	})

	l.Debug("debug message")
	if buf.Len() > 0 {
		t.Errorf("debug message should be filtered at info level, got: %s", buf.String())
	}

	l.Info("info message")
	if buf.Len() == 0 {
		t.Error("info message should not be filtered")
	}
}

func TestLogger_JSONFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	l := New(Config{
		Level:  "info",
		Format: "json",
		Output: buf,
	})

	l.Info("test message", "key", "value")
	output := buf.String()

	// JSON output should contain these elements
	if !strings.Contains(output, `"msg":"test message"`) && !strings.Contains(output, `"msg": "test message"`) {
		t.Errorf("JSON output should contain msg field, got: %s", output)
	}
	if !strings.Contains(output, `"key":"value"`) && !strings.Contains(output, `"key": "value"`) {
		t.Errorf("JSON output should contain key/value, got: %s", output)
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Level != "info" {
		t.Errorf("Level = %s, want info", cfg.Level)
	}
	if cfg.Format != "text" {
		t.Errorf("Format = %s, want text", cfg.Format)
	}
}

func TestInit(t *testing.T) {
	// Just ensure it doesn't panic
	Init()
}

func TestPackageLevelFunctions(t *testing.T) {
	// Initialize with a test buffer
	buf := &bytes.Buffer{}
	SetDefault(New(Config{
		Level:  "debug",
		Format: "text",
		Output: buf,
	}))

	// Test all package-level functions
	Debug("debug msg")
	Info("info msg")
	Warn("warn msg")
	Error("error msg")

	output := buf.String()
	if !strings.Contains(output, "debug msg") {
		t.Error("should contain debug msg")
	}
	if !strings.Contains(output, "info msg") {
		t.Error("should contain info msg")
	}
	if !strings.Contains(output, "warn msg") {
		t.Error("should contain warn msg")
	}
	if !strings.Contains(output, "error msg") {
		t.Error("should contain error msg")
	}
}
