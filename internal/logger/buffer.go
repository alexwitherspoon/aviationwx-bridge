package logger

import (
	"container/ring"
	"fmt"
	"sync"
	"time"
)

// LogEntry represents a single log entry
type LogEntry struct {
	Timestamp time.Time
	Level     string
	Message   string
	Attrs     map[string]interface{}
}

// Buffer is a thread-safe circular buffer for log entries
type Buffer struct {
	mu   sync.RWMutex
	ring *ring.Ring
	size int
}

// NewBuffer creates a new log buffer with the specified capacity
func NewBuffer(capacity int) *Buffer {
	return &Buffer{
		ring: ring.New(capacity),
		size: 0,
	}
}

// Add adds a log entry to the buffer
func (b *Buffer) Add(entry LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.ring.Value = entry
	b.ring = b.ring.Next()

	if b.size < b.ring.Len() {
		b.size++
	}
}

// GetLast returns the last N log entries (newest first)
func (b *Buffer) GetLast(n int) []LogEntry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if n > b.size {
		n = b.size
	}

	entries := make([]LogEntry, 0, n)

	// Start from current position (oldest) and go backwards
	r := b.ring
	for i := 0; i < n && i < b.size; i++ {
		r = r.Prev()
		if r.Value != nil {
			if entry, ok := r.Value.(LogEntry); ok {
				entries = append([]LogEntry{entry}, entries...) // Prepend for newest-first
			}
		}
	}

	return entries
}

// FormatEntry formats a log entry as a text line
func FormatEntry(e LogEntry) string {
	attrs := ""
	for k, v := range e.Attrs {
		attrs += fmt.Sprintf(" %s=%v", k, v)
	}
	return fmt.Sprintf("time=%s level=%s msg=%q%s",
		e.Timestamp.Format("15:04:05"),
		e.Level,
		e.Message,
		attrs,
	)
}
