package web

import (
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/scheduler"
)

// healthIndicators are fields extracted from bridge getStatus() for /healthz.
type healthIndicators struct {
	orchestratorPresent bool
	orchestratorRunning bool
	camerasActive       int
	camerasTotal        int
	uploadsRecent       int
	queueHealth         string
	ntpHealthy          bool
	hostRecovery        map[string]interface{}
}

func extractHealthIndicators(raw interface{}) healthIndicators {
	hi := healthIndicators{
		queueHealth: "unknown",
		ntpHealthy:  true,
	}
	statusMap, ok := raw.(map[string]interface{})
	if !ok {
		return hi
	}

	hi.camerasActive = intFromStatus(statusMap["cameras"])
	hi.camerasTotal = intFromStatus(statusMap["total_cameras"])
	if hi.camerasTotal == 0 {
		hi.camerasTotal = hi.camerasActive
	}

	if hr, ok := statusMap["host_recovery"].(map[string]interface{}); ok {
		hi.hostRecovery = hr
	}

	if th, ok := statusMap["time_health"].(map[string]interface{}); ok {
		if healthy, ok := th["healthy"].(bool); ok {
			hi.ntpHealthy = healthy
		}
	}

	if orchRaw, ok := statusMap["orchestrator"]; ok && orchRaw != nil {
		hi.orchestratorPresent = true
		switch orch := orchRaw.(type) {
		case scheduler.OrchestratorStatus:
			applyOrchestratorIndicators(&hi, orch)
		case map[string]interface{}:
			applyOrchestratorMapIndicators(&hi, orch)
		}
	}

	return hi
}

func applyOrchestratorIndicators(hi *healthIndicators, orch scheduler.OrchestratorStatus) {
	hi.orchestratorRunning = orch.Running
	if orch.CameraCount > hi.camerasTotal {
		hi.camerasTotal = orch.CameraCount
	}
	hi.uploadsRecent = int(orch.UploadStats.UploadsSuccess)
	hi.queueHealth = worstQueueHealthFromStats(orch.CameraStats)
	hi.ntpHealthy = orch.TimeInfo.TimeHealthy
}

func applyOrchestratorMapIndicators(hi *healthIndicators, orch map[string]interface{}) {
	if running, ok := orch["running"].(bool); ok {
		hi.orchestratorRunning = running
	}
	if n, ok := orch["camera_count"].(float64); ok {
		if int(n) > hi.camerasTotal {
			hi.camerasTotal = int(n)
		}
	}
	if upload, ok := orch["upload_stats"].(map[string]interface{}); ok {
		if success, ok := upload["uploads_success"].(float64); ok {
			hi.uploadsRecent = int(success)
		}
	}
}

func worstQueueHealthFromStats(stats []scheduler.CameraStatus) string {
	worst := "healthy"
	for _, cs := range stats {
		switch cs.QueueStats.HealthLevel {
		case "critical":
			return "critical"
		case "degraded", "catching_up":
			if worst != "critical" {
				worst = "degraded"
			}
		}
	}
	return worst
}

func intFromStatus(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
