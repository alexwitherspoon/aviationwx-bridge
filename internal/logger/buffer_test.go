package logger

import (
	"testing"
	"time"
)

func TestBuffer_GetLast_capsRequest(t *testing.T) {
	buf := NewBuffer(10)
	for i := 0; i < 5; i++ {
		buf.Add(LogEntry{Message: "entry"})
	}

	got := buf.GetLast(2000)
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5 (buffer size)", len(got))
	}
}

func TestBuffer_GetLast_respectsBufferSize(t *testing.T) {
	buf := NewBuffer(3)
	for _, msg := range []string{"a", "b", "c"} {
		buf.Add(LogEntry{Timestamp: time.Now(), Message: msg})
	}

	got := buf.GetLast(10)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
}

func TestBuffer_GetLast_negativeReturnsEmpty(t *testing.T) {
	buf := NewBuffer(3)
	buf.Add(LogEntry{Message: "x"})
	if got := buf.GetLast(-1); len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}
