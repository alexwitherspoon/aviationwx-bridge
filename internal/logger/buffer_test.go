package logger

import (
	"testing"
	"time"
)

func TestBuffer_GetLast_capsRequest(t *testing.T) {
	buf := NewBuffer(maxLogEntriesRequested + 10)
	for i := 0; i < maxLogEntriesRequested+5; i++ {
		buf.Add(LogEntry{Message: "entry"})
	}

	got := buf.GetLast(2000)
	if len(got) != maxLogEntriesRequested {
		t.Fatalf("len = %d, want %d", len(got), maxLogEntriesRequested)
	}
}

func TestBuffer_GetLast_newestFirst(t *testing.T) {
	buf := NewBuffer(3)
	for _, msg := range []string{"a", "b", "c"} {
		buf.Add(LogEntry{Message: msg})
	}

	got := buf.GetLast(2)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Message != "c" || got[1].Message != "b" {
		t.Fatalf("order = [%s, %s], want [c, b]", got[0].Message, got[1].Message)
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

func TestBuffer_GetLast_wrapAround(t *testing.T) {
	buf := NewBuffer(3)
	for _, msg := range []string{"a", "b", "c", "d"} {
		buf.Add(LogEntry{Message: msg})
	}

	got := buf.GetLast(3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Message != "d" || got[1].Message != "c" || got[2].Message != "b" {
		t.Fatalf("got [%s, %s, %s], want [d, c, b]", got[0].Message, got[1].Message, got[2].Message)
	}

	got = buf.GetLast(2)
	if len(got) != 2 || got[0].Message != "d" || got[1].Message != "c" {
		t.Fatalf("tail-2 after wrap: got [%s, %s], want [d, c]", got[0].Message, got[1].Message)
	}
}

func TestBuffer_GetLast_negativeReturnsEmpty(t *testing.T) {
	buf := NewBuffer(3)
	buf.Add(LogEntry{Message: "x"})
	if got := buf.GetLast(-1); len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}
