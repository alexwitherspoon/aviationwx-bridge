package web

import (
	"testing"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/queue"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/scheduler"
	timepkg "github.com/alexwitherspoon/AviationWX.org-Bridge/internal/time"
)

func TestExtractHealthIndicators_orchestratorRunning(t *testing.T) {
	raw := map[string]interface{}{
		"cameras":       2,
		"total_cameras": 3,
		"time_health": map[string]interface{}{
			"healthy": true,
		},
		"orchestrator": scheduler.OrchestratorStatus{
			Running:     true,
			CameraCount: 2,
			CameraStats: []scheduler.CameraStatus{
				{QueueStats: queue.QueueStats{HealthLevel: "healthy"}},
			},
			UploadStats: scheduler.UploadStats{UploadsSuccess: 10},
		},
	}

	hi := extractHealthIndicators(raw)
	if !hi.orchestratorPresent || !hi.orchestratorRunning {
		t.Fatalf("orchestrator: present=%v running=%v", hi.orchestratorPresent, hi.orchestratorRunning)
	}
	if hi.camerasActive != 2 || hi.camerasTotal != 3 {
		t.Fatalf("cameras: active=%d total=%d", hi.camerasActive, hi.camerasTotal)
	}
	if hi.uploadsRecent != 10 || hi.queueHealth != "healthy" {
		t.Fatalf("uploads=%d queue=%q", hi.uploadsRecent, hi.queueHealth)
	}
}

func TestExtractHealthIndicators_orchestratorStoppedDegraded(t *testing.T) {
	raw := map[string]interface{}{
		"cameras": 1,
		"orchestrator": scheduler.OrchestratorStatus{
			Running: false,
			Uptime:  time.Minute,
		},
	}

	hi := extractHealthIndicators(raw)
	if !hi.orchestratorPresent || hi.orchestratorRunning {
		t.Fatal("expected present, not running")
	}

	health := map[string]interface{}{"status": "healthy"}
	if hi.orchestratorPresent && !hi.orchestratorRunning {
		health["status"] = "degraded"
	}
	if health["status"] != "degraded" {
		t.Fatal("expected degraded when orchestrator stopped")
	}
}

func TestExtractHealthIndicators_orchestratorTimeUnhealthy(t *testing.T) {
	raw := map[string]interface{}{
		"cameras": 1,
		"time_health": map[string]interface{}{
			"healthy": true,
		},
		"orchestrator": scheduler.OrchestratorStatus{
			Running:  true,
			TimeInfo: timepkg.TimeInfo{TimeHealthy: false},
		},
	}

	hi := extractHealthIndicators(raw)
	if hi.ntpHealthy {
		t.Fatal("expected ntpHealthy false when orchestrator TimeInfo is unhealthy")
	}
}
