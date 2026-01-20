package health

import (
	"encoding/json"
	"net/http"
	"time"
)

// StatusProvider is a function that returns the current system health status
type StatusProvider func() HealthStatus

// HealthStatus represents the overall system health
type HealthStatus struct {
	Status          string    `json:"status"` // "healthy", "degraded", "unhealthy"
	Timestamp       time.Time `json:"timestamp"`
	Orchestrator    bool      `json:"orchestrator_running"`
	CamerasActive   int       `json:"cameras_active"`
	CamerasTotal    int       `json:"cameras_total"`
	UploadsLast5Min int       `json:"uploads_last_5min"`
	QueueHealth     string    `json:"queue_health"` // "healthy", "degraded", "critical"
	NTPHealthy      bool      `json:"ntp_healthy"`
	Details         string    `json:"details,omitempty"` // Human-readable explanation
}

// HealthHandler returns a simple health check endpoint for backward compatibility
// Always returns 200 OK if the web server is responsive
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// EnhancedHealthHandler returns a detailed health check with status codes
// - 200: Healthy (all systems operational)
// - 200: Degraded (some issues but functional)
// - 503: Unhealthy (critical failures)
func EnhancedHealthHandler(provider StatusProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		status := provider()

		// Set HTTP status code based on health
		statusCode := http.StatusOK
		if status.Status == "unhealthy" {
			statusCode = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		json.NewEncoder(w).Encode(status)
	}
}

// ReadyHandler returns readiness check endpoint
// Used by Kubernetes/orchestrators to determine if app is ready for traffic
func ReadyHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}
