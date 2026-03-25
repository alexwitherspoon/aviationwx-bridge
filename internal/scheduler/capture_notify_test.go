package scheduler

import (
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/queue"
)

func TestEligibleForPendingGateWake(t *testing.T) {
	tmpDir := t.TempDir()
	q, err := queue.NewQueue("cam", tmpDir, queue.DefaultQueueConfig(), nil)
	if err != nil {
		t.Fatal(err)
	}
	w := NewCaptureWorker(CaptureWorkerConfig{
		Camera:       &mockCamera{id: "cam"},
		CameraConfig: CameraConfig{ID: "cam", Enabled: true},
		Queue:        q,
		IntervalSecs: 60,
	})
	if w.eligibleForPendingGateWake() {
		t.Fatal("expected not eligible without pending")
	}
	w.mu.Lock()
	w.pendingCapture = true
	w.state.NextAttempt = time.Now().Add(-time.Second)
	w.mu.Unlock()
	if !w.eligibleForPendingGateWake() {
		t.Fatal("expected eligible when pending, not busy, not paused, backoff elapsed")
	}
	w.mu.Lock()
	w.state.NextAttempt = time.Now().Add(time.Hour)
	w.mu.Unlock()
	if w.eligibleForPendingGateWake() {
		t.Fatal("expected not eligible during backoff")
	}
}
