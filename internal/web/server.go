package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
	"github.com/alexwitherspoon/aviationwx-bridge/internal/logger"
)

//go:embed static/*
var staticFiles embed.FS

// Server provides the web console HTTP server
type Server struct {
	configService *config.Service
	mux           *http.ServeMux
	server        *http.Server
	log           *logger.Logger

	// Callbacks to bridge services
	getStatus       func() interface{}
	testCamera      func(camConfig config.Camera) ([]byte, error)
	testUpload      func(uploadConfig config.Upload) error
	getCameraImage  func(cameraID string) ([]byte, error)
	getWorkerStatus func(cameraID string) map[string]interface{}
}

// ServerConfig configures the web server
type ServerConfig struct {
	ConfigService   *config.Service
	GetStatus       func() interface{}
	TestCamera      func(camConfig config.Camera) ([]byte, error)
	TestUpload      func(uploadConfig config.Upload) error
	GetCameraImage  func(cameraID string) ([]byte, error)
	GetWorkerStatus func(cameraID string) map[string]interface{}
}

// NewServer creates a new web server
func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		configService:   cfg.ConfigService,
		mux:             http.NewServeMux(),
		log:             logger.Default(),
		getStatus:       cfg.GetStatus,
		testCamera:      cfg.TestCamera,
		testUpload:      cfg.TestUpload,
		getCameraImage:  cfg.GetCameraImage,
		getWorkerStatus: cfg.GetWorkerStatus,
	}

	s.setupRoutes()
	return s
}

func (s *Server) setupRoutes() {
	// API routes (require auth)
	s.mux.HandleFunc("/api/status", s.authMiddleware(s.handleStatus))
	s.mux.HandleFunc("/api/config", s.authMiddleware(s.handleConfig))
	s.mux.HandleFunc("/api/cameras", s.authMiddleware(s.handleCameras))
	s.mux.HandleFunc("/api/cameras/", s.authMiddleware(s.handleCamera))
	s.mux.HandleFunc("/api/time", s.authMiddleware(s.handleTime))
	s.mux.HandleFunc("/api/test/camera", s.authMiddleware(s.handleTestCamera))
	s.mux.HandleFunc("/api/test/upload", s.authMiddleware(s.handleTestUpload))
	s.mux.HandleFunc("/api/update", s.authMiddleware(s.handleUpdate))

	// Health check (no auth)
	s.mux.HandleFunc("/healthz", s.handleHealthz)
	s.mux.HandleFunc("/api/logs", s.authMiddleware(http.HandlerFunc(s.handleLogs)))

	// Static files (require auth except for login assets)
	staticFS, _ := fs.Sub(staticFiles, "static")
	fileServer := http.FileServer(http.FS(staticFS))
	s.mux.HandleFunc("/", s.staticMiddleware(fileServer))
}

// Start starts the web server
func (s *Server) Start() error {
	port := s.configService.GetWebPort()
	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      s.mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return s.server.ListenAndServe()
}

// Stop stops the web server gracefully
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

// GetMux returns the HTTP mux for testing
func (s *Server) GetMux() *http.ServeMux {
	return s.mux
}

// Middleware

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check for basic auth
		_, password, ok := r.BasicAuth()
		if !ok || password != s.configService.GetWebPassword() {
			w.Header().Set("WWW-Authenticate", `Basic realm="AviationWX Bridge"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) staticMiddleware(fileServer http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Allow access to root and static assets without auth for login page
		if r.URL.Path == "/" || strings.HasPrefix(r.URL.Path, "/css/") ||
			strings.HasPrefix(r.URL.Path, "/js/") {
			fileServer.ServeHTTP(w, r)
			return
		}

		// All other static files require auth
		_, password, ok := r.BasicAuth()
		if !ok || password != s.configService.GetWebPassword() {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		fileServer.ServeHTTP(w, r)
	}
}

// API Handlers

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status := s.getStatus()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		global := s.configService.GetGlobal()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(global)

	case http.MethodPut:
		var updates config.GlobalSettings
		if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		err := s.configService.UpdateGlobal(func(g *config.GlobalSettings) error {
			// Update fields
			if updates.Timezone != "" {
				g.Timezone = updates.Timezone
			}
			if updates.WebConsole != nil {
				g.WebConsole = updates.WebConsole
			}
			if updates.Global != nil {
				g.Global = updates.Global
			}
			if updates.Queue != nil {
				g.Queue = updates.Queue
			}
			if updates.SNTP != nil {
				g.SNTP = updates.SNTP
			}
			return nil
		})

		if err != nil {
			http.Error(w, "Failed to update config: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCameras(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listCameras(w, r)
	case http.MethodPost:
		s.addCamera(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) listCameras(w http.ResponseWriter, r *http.Request) {
	cameras := s.configService.ListCameras()
	global := s.configService.GetGlobal()

	// Convert to map format for frontend
	result := make([]map[string]interface{}, 0, len(cameras))
	for _, cam := range cameras {
		camMap := s.cameraToMap(cam, global.Timezone)
		result = append(result, camMap)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (s *Server) addCamera(w http.ResponseWriter, r *http.Request) {
	var cam config.Camera
	if err := json.NewDecoder(r.Body).Decode(&cam); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if cam.ID == "" {
		cam.ID = fmt.Sprintf("cam-%d", time.Now().Unix())
	}
	if cam.Name == "" {
		cam.Name = cam.ID
	}
	if cam.Upload == nil {
		http.Error(w, "Upload credentials are required", http.StatusBadRequest)
		return
	}

	// Set defaults
	if cam.CaptureIntervalSeconds == 0 {
		cam.CaptureIntervalSeconds = 60
	}
	if cam.Upload.Host == "" {
		cam.Upload.Host = "upload.aviationwx.org"
	}
	if cam.Upload.Port == 0 {
		cam.Upload.Port = 2121
	}
	cam.Upload.TLS = true

	// Add camera via ConfigService
	if err := s.configService.AddCamera(cam); err != nil {
		s.log.Error("Failed to add camera via API",
			"camera", cam.ID,
			"error", err,
			"camera_type", cam.Type)
		http.Error(w, fmt.Sprintf("Failed to add camera %s: %v", cam.ID, err), http.StatusInternalServerError)
		return
	}

	s.log.Info("Camera added via API", "camera", cam.ID, "type", cam.Type)

	global := s.configService.GetGlobal()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(s.cameraToMap(cam, global.Timezone))
}

func (s *Server) handleCamera(w http.ResponseWriter, r *http.Request) {
	// Extract camera ID from path
	path := strings.TrimPrefix(r.URL.Path, "/api/cameras/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.Error(w, "Camera ID required", http.StatusBadRequest)
		return
	}

	cameraID := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	switch {
	case action == "preview" && r.Method == http.MethodGet:
		s.getCameraPreview(w, r, cameraID)
	case action == "" && r.Method == http.MethodGet:
		s.getCamera(w, r, cameraID)
	case action == "" && r.Method == http.MethodPut:
		s.updateCamera(w, r, cameraID)
	case action == "" && r.Method == http.MethodDelete:
		s.deleteCamera(w, r, cameraID)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getCamera(w http.ResponseWriter, r *http.Request, cameraID string) {
	cam, err := s.configService.GetCamera(cameraID)
	if err != nil {
		http.Error(w, "Camera not found", http.StatusNotFound)
		return
	}

	global := s.configService.GetGlobal()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.cameraToMap(*cam, global.Timezone))
}

func (s *Server) updateCamera(w http.ResponseWriter, r *http.Request, cameraID string) {
	var updates config.Camera
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	err := s.configService.UpdateCamera(cameraID, func(cam *config.Camera) error {
		// Preserve passwords if empty
		if updates.Upload != nil && updates.Upload.Password == "" && cam.Upload != nil {
			updates.Upload.Password = cam.Upload.Password
		}
		if updates.Auth != nil && updates.Auth.Password == "" && cam.Auth != nil {
			updates.Auth.Password = cam.Auth.Password
		}
		if updates.RTSP != nil && updates.RTSP.Password == "" && cam.RTSP != nil {
			updates.RTSP.Password = cam.RTSP.Password
		}
		if updates.ONVIF != nil && updates.ONVIF.Password == "" && cam.ONVIF != nil {
			updates.ONVIF.Password = cam.ONVIF.Password
		}

		// Update fields
		cam.Name = updates.Name
		cam.Type = updates.Type
		cam.Enabled = updates.Enabled
		cam.SnapshotURL = updates.SnapshotURL
		cam.CaptureIntervalSeconds = updates.CaptureIntervalSeconds
		cam.Auth = updates.Auth
		cam.ONVIF = updates.ONVIF
		cam.RTSP = updates.RTSP
		cam.Image = updates.Image
		cam.Upload = updates.Upload
		cam.Queue = updates.Queue

		return nil
	})

	if err != nil {
		http.Error(w, "Failed to update camera: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Get updated camera
	cam, _ := s.configService.GetCamera(cameraID)
	global := s.configService.GetGlobal()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.cameraToMap(*cam, global.Timezone))
}

func (s *Server) deleteCamera(w http.ResponseWriter, r *http.Request, cameraID string) {
	if err := s.configService.DeleteCamera(cameraID); err != nil {
		http.Error(w, "Failed to delete camera: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getCameraPreview(w http.ResponseWriter, r *http.Request, cameraID string) {
	// Check if camera exists
	if _, err := s.configService.GetCamera(cameraID); err != nil {
		http.Error(w, "Camera not found", http.StatusNotFound)
		return
	}

	// Get image from callback
	if s.getCameraImage == nil {
		http.Error(w, "Preview not available", http.StatusServiceUnavailable)
		return
	}

	imageData, err := s.getCameraImage(cameraID)
	if err != nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if len(imageData) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(imageData)
}

func (s *Server) handleTime(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		global := s.configService.GetGlobal()

		response := map[string]interface{}{
			"system_time":         time.Now().UTC().Format(time.RFC3339),
			"configured_timezone": global.Timezone,
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)

	case http.MethodPut:
		var update struct {
			Timezone string `json:"timezone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Update timezone via ConfigService
		err := s.configService.UpdateGlobal(func(g *config.GlobalSettings) error {
			g.Timezone = update.Timezone
			return nil
		})

		if err != nil {
			http.Error(w, "Failed to update timezone: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})

	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTestCamera(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var cam config.Camera
	if err := json.NewDecoder(r.Body).Decode(&cam); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if s.testCamera == nil {
		http.Error(w, "Test not available", http.StatusServiceUnavailable)
		return
	}

	imageData, err := s.testCamera(cam)
	if err != nil {
		http.Error(w, "Test failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(imageData)
}

func (s *Server) handleTestUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var upload config.Upload
	if err := json.NewDecoder(r.Body).Decode(&upload); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if s.testUpload == nil {
		http.Error(w, "Test not available", http.StatusServiceUnavailable)
		return
	}

	if err := s.testUpload(upload); err != nil {
		http.Error(w, "Upload test failed: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	// Enhanced health check with actual system status
	// Returns 200 OK if operational, 503 if unhealthy

	status := s.buildHealthStatus()

	// Set HTTP status code based on health
	statusCode := http.StatusOK
	if status["status"] == "unhealthy" {
		statusCode = http.StatusServiceUnavailable
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(status)
}

func (s *Server) buildHealthStatus() map[string]interface{} {
	// Start with basic health
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	// Get orchestrator status if available
	if s.getStatus != nil {
		rawStatus := s.getStatus()
		if statusMap, ok := rawStatus.(map[string]interface{}); ok {
			// Extract key health indicators
			orchestratorRunning := false
			camerasActive := 0
			camerasTotal := 0
			uploadsRecent := 0
			queueHealth := "unknown"
			ntpHealthy := true

			if orch, ok := statusMap["orchestrator"].(map[string]interface{}); ok {
				if running, ok := orch["running"].(bool); ok {
					orchestratorRunning = running
				}
			}

			if cameras, ok := statusMap["cameras"].([]interface{}); ok {
				camerasTotal = len(cameras)
				for _, cam := range cameras {
					if camMap, ok := cam.(map[string]interface{}); ok {
						if enabled, ok := camMap["enabled"].(bool); ok && enabled {
							camerasActive++
						}
					}
				}
			}

			if queue, ok := statusMap["queue"].(map[string]interface{}); ok {
				if qh, ok := queue["health"].(string); ok {
					queueHealth = qh
				}
			}

			if upload, ok := statusMap["upload"].(map[string]interface{}); ok {
				if stats, ok := upload["stats"].(map[string]interface{}); ok {
					if success, ok := stats["uploads_success"].(int64); ok {
						uploadsRecent = int(success)
					}
				}
			}

			if timeInfo, ok := statusMap["time"].(map[string]interface{}); ok {
				if healthy, ok := timeInfo["ntp_healthy"].(bool); ok {
					ntpHealthy = healthy
				}
			}

			// Determine overall health status
			details := []string{}

			if !orchestratorRunning {
				health["status"] = "degraded"
				details = append(details, "orchestrator not running")
			}

			if camerasTotal > 0 && camerasActive == 0 {
				health["status"] = "degraded"
				details = append(details, "no active cameras")
			}

			if queueHealth == "critical" {
				health["status"] = "degraded"
				details = append(details, "queue critical")
			}

			// Populate health details
			health["orchestrator_running"] = orchestratorRunning
			health["cameras_active"] = camerasActive
			health["cameras_total"] = camerasTotal
			health["uploads_recent"] = uploadsRecent
			health["queue_health"] = queueHealth
			health["ntp_healthy"] = ntpHealthy

			if len(details) > 0 {
				health["details"] = strings.Join(details, "; ")
			}
		}
	}

	return health
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.log.Info("Update triggered via web UI")

	// Trigger update by touching a file that the supervisor script watches
	updateTriggerFile := "/data/aviationwx/trigger-update"
	
	if err := os.WriteFile(updateTriggerFile, []byte("manual-trigger"), 0644); err != nil {
		s.log.Error("Failed to create update trigger file", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  fmt.Sprintf("Failed to trigger update: %v", err),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"message": "Update triggered successfully. The supervisor script will apply the update shortly.",
	})
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	// Get tail parameter (default 100 lines)
	tail := 100
	if tailStr := r.URL.Query().Get("tail"); tailStr != "" {
		if n, err := fmt.Sscanf(tailStr, "%d", &tail); err == nil && n == 1 {
			if tail > 1000 {
				tail = 1000 // Cap at 1000 lines
			}
		}
	}

	// Get recent logs from the global buffer
	entries := logger.GetRecentLogs(tail)

	w.Header().Set("Content-Type", "text/plain")

	if len(entries) == 0 {
		fmt.Fprintf(w, "# No logs available yet\n")
		return
	}

	// Format and return logs
	for _, entry := range entries {
		fmt.Fprintln(w, logger.FormatEntry(entry))
	}
}

// Helper functions

func (s *Server) cameraToMap(cam config.Camera, timezone string) map[string]interface{} {
	result := map[string]interface{}{
		"id":                       cam.ID,
		"name":                     cam.Name,
		"type":                     cam.Type,
		"enabled":                  cam.Enabled,
		"snapshot_url":             cam.SnapshotURL,
		"capture_interval_seconds": cam.CaptureIntervalSeconds,
		"timezone":                 timezone,
	}

	if cam.Auth != nil {
		result["auth"] = cam.Auth
	}
	if cam.ONVIF != nil {
		result["onvif"] = cam.ONVIF
	}
	if cam.RTSP != nil {
		result["rtsp"] = cam.RTSP
	}
	if cam.Image != nil {
		result["image"] = cam.Image
	}
	if cam.Upload != nil {
		result["upload"] = cam.Upload
	}
	if cam.Queue != nil {
		result["queue"] = cam.Queue
	}

	// Add worker status if available
	if s.getWorkerStatus != nil {
		status := s.getWorkerStatus(cam.ID)
		for k, v := range status {
			result[k] = v
		}
	}

	return result
}
