// Package logger provides structured logging for AviationWX Bridge
package logger

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Logger wraps slog for compatibility with existing interfaces
type Logger struct {
	slog   *slog.Logger
	level  slog.Level
	format string
	buffer *Buffer
}

// Global log buffer for web UI access
var globalBuffer *Buffer

// Config holds logger configuration
type Config struct {
	Level  string // debug, info, warn, error
	Format string // json, text
	Output io.Writer
}

// DefaultConfig returns default logger configuration
func DefaultConfig() Config {
	return Config{
		Level:  "info",
		Format: "text",
		Output: os.Stdout,
	}
}

// ConfigFromEnv creates config from environment variables
func ConfigFromEnv() Config {
	cfg := DefaultConfig()

	if level := os.Getenv("LOG_LEVEL"); level != "" {
		cfg.Level = strings.ToLower(level)
	}
	if format := os.Getenv("LOG_FORMAT"); format != "" {
		cfg.Format = strings.ToLower(format)
	}

	return cfg
}

// New creates a new logger with the given configuration
func New(cfg Config) *Logger {
	// Initialize global buffer if not already done
	if globalBuffer == nil {
		globalBuffer = NewBuffer(1000) // Keep last 1000 log entries
	}

	// Parse level
	var level slog.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "info":
		level = slog.LevelInfo
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	// Set output
	output := cfg.Output
	if output == nil {
		output = os.Stdout
	}

	// Create handler
	var handler slog.Handler
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Shorten time format
			if a.Key == slog.TimeKey {
				return slog.Attr{
					Key:   a.Key,
					Value: slog.StringValue(a.Value.Time().Format("15:04:05")),
				}
			}
			return a
		},
	}

	switch strings.ToLower(cfg.Format) {
	case "json":
		handler = slog.NewJSONHandler(output, opts)
	default:
		handler = slog.NewTextHandler(output, opts)
	}

	return &Logger{
		slog:   slog.New(handler),
		level:  level,
		format: cfg.Format,
		buffer: globalBuffer,
	}
}

// Debug logs a debug message
func (l *Logger) Debug(msg string, keysAndValues ...interface{}) {
	l.slog.Debug(msg, keysAndValues...)
	l.addToBuffer("DEBUG", msg, keysAndValues...)
}

// Info logs an info message
func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.slog.Info(msg, keysAndValues...)
	l.addToBuffer("INFO", msg, keysAndValues...)
}

// Warn logs a warning message
func (l *Logger) Warn(msg string, keysAndValues ...interface{}) {
	l.slog.Warn(msg, keysAndValues...)
	l.addToBuffer("WARN", msg, keysAndValues...)
}

// Error logs an error message
func (l *Logger) Error(msg string, keysAndValues ...interface{}) {
	l.slog.Error(msg, keysAndValues...)
	l.addToBuffer("ERROR", msg, keysAndValues...)
}

// addToBuffer adds a log entry to the in-memory buffer
func (l *Logger) addToBuffer(level, msg string, keysAndValues ...interface{}) {
	if l.buffer == nil {
		return
	}

	attrs := make(map[string]interface{})
	for i := 0; i < len(keysAndValues)-1; i += 2 {
		if key, ok := keysAndValues[i].(string); ok {
			attrs[key] = keysAndValues[i+1]
		}
	}

	l.buffer.Add(LogEntry{
		Timestamp: time.Now(),
		Level:     level,
		Message:   msg,
		Attrs:     attrs,
	})
}

// With returns a new logger with additional context
func (l *Logger) With(keysAndValues ...interface{}) *Logger {
	return &Logger{
		slog:   l.slog.With(keysAndValues...),
		level:  l.level,
		format: l.format,
		buffer: l.buffer,
	}
}

// GetSlog returns the underlying slog.Logger
func (l *Logger) GetSlog() *slog.Logger {
	return l.slog
}

// Package-level default logger
var defaultLogger = New(DefaultConfig())

// Init initializes the default logger from environment
func Init() {
	defaultLogger = New(ConfigFromEnv())
	slog.SetDefault(defaultLogger.slog)
}

// SetDefault sets the default logger
func SetDefault(logger *Logger) {
	defaultLogger = logger
	slog.SetDefault(logger.slog)
}

// Default returns the default logger
func Default() *Logger {
	return defaultLogger
}

// GetRecentLogs returns the last N log entries from the global buffer
func GetRecentLogs(n int) []LogEntry {
	if globalBuffer == nil {
		return nil
	}
	return globalBuffer.GetLast(n)
}

// Package-level convenience functions

// Debug logs a debug message using the default logger
func Debug(msg string, keysAndValues ...interface{}) {
	defaultLogger.Debug(msg, keysAndValues...)
}

// Info logs an info message using the default logger
func Info(msg string, keysAndValues ...interface{}) {
	defaultLogger.Info(msg, keysAndValues...)
}

// Warn logs a warning message using the default logger
func Warn(msg string, keysAndValues ...interface{}) {
	defaultLogger.Warn(msg, keysAndValues...)
}

// Error logs an error message using the default logger
func Error(msg string, keysAndValues ...interface{}) {
	defaultLogger.Error(msg, keysAndValues...)
}
