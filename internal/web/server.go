package web

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/camera"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/logger"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/paths"
)

//go:embed static/*
var staticFiles embed.FS

// healthStatusCollectTimeout bounds getStatus() in /healthz (overridable in tests).
var healthStatusCollectTimeout = 5 * time.Second

// statusAPICollectTimeout bounds getStatus() in /api/status (overridable in tests).
var statusAPICollectTimeout = 10 * time.Second

// normalizeUploadForAPI trims upload host/username/password, lowercases host, and applies the
// default host when the trimmed value is empty so SFTP identity keys stay consistent.
func normalizeUploadForAPI(u *config.Upload) {
	if u == nil {
		return
	}
	u.Host = strings.TrimSpace(u.Host)
	u.Username = strings.TrimSpace(u.Username)
	u.Password = strings.TrimSpace(u.Password)
	if u.Host == "" {
		u.Host = "upload.aviationwx.org"
	}
	u.Host = strings.ToLower(u.Host)
}

var allowedCameraTypes = map[string]struct{}{
	"http":  {},
	"onvif": {},
	"rtsp":  {},
}

// normalizeCameraType trims, lowercases, and validates camera type for persistence (factory expects lowercase).
func normalizeCameraType(t string) (string, error) {
	t = strings.ToLower(strings.TrimSpace(t))
	if t == "" {
		return "", errors.New("camera type is required")
	}
	if _, ok := allowedCameraTypes[t]; !ok {
		return "", fmt.Errorf("unsupported camera type %q (use http, onvif, or rtsp)", t)
	}
	return t, nil
}

// validateONVIFCameraForPost ensures ONVIF cameras have a usable endpoint and credentials after normalization.
func validateONVIFCameraForPost(cam *config.Camera) error {
	if cam.ONVIF == nil {
		return errors.New("ONVIF settings are required for type onvif")
	}
	ep := strings.TrimSpace(cam.ONVIF.Endpoint)
	if ep != "" {
		ep = camera.NormalizeONVIFEndpoint(ep)
	}
	cam.ONVIF.Endpoint = ep
	if ep == "" {
		return errors.New("ONVIF endpoint is required for type onvif")
	}
	u := strings.TrimSpace(cam.ONVIF.Username)
	if u == "" {
		return errors.New("ONVIF username is required for type onvif")
	}
	if strings.TrimSpace(cam.ONVIF.Password) == "" {
		return errors.New("ONVIF password is required for type onvif")
	}
	cam.ONVIF.Username = u
	cam.ONVIF.Password = strings.TrimSpace(cam.ONVIF.Password)
	return nil
}

// Server provides the web console HTTP server
type Server struct {
	configService *config.Service
	mux           *http.ServeMux
	server        *http.Server
	log           *logger.Logger

	// Callbacks to bridge services
	getStatus           func() interface{}
	getCaptureReadiness func() (ok bool, reason string)
	testCamera          func(camConfig config.Camera) ([]byte, error)
	testUpload          func(uploadConfig config.Upload) error
	getCameraImage      func(cameraID string) ([]byte, error)
	getWorkerStatus     func(cameraID string) map[string]interface{}
}

// ServerConfig configures the web server
type ServerConfig struct {
	ConfigService *config.Service
	GetStatus     func() interface{}
	// GetCaptureReadiness, if non-nil, backs GET /readyz for host-side capture health checks (no auth).
	GetCaptureReadiness func() (ok bool, reason string)
	TestCamera          func(camConfig config.Camera) ([]byte, error)
	TestUpload          func(uploadConfig config.Upload) error
	GetCameraImage      func(cameraID string) ([]byte, error)
	GetWorkerStatus     func(cameraID string) map[string]interface{}
}

// NewServer creates a new web server
func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		configService:       cfg.ConfigService,
		mux:                 http.NewServeMux(),
		log:                 logger.Default(),
		getStatus:           cfg.GetStatus,
		getCaptureReadiness: cfg.GetCaptureReadiness,
		testCamera:          cfg.TestCamera,
		testUpload:          cfg.TestUpload,
		getCameraImage:      cfg.GetCameraImage,
		getWorkerStatus:     cfg.GetWorkerStatus,
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
	s.mux.HandleFunc("/readyz", s.handleReadyz)
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
		expectedPassword := s.configService.GetWebPassword()
		// Use constant-time comparison to prevent timing attacks
		passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(expectedPassword)) == 1
		if !ok || !passwordMatch {
			w.Header().Set("WWW-Authenticate", `Basic realm="AviationWX.org Bridge"`)
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
		expectedPassword := s.configService.GetWebPassword()
		// Use constant-time comparison to prevent timing attacks
		passwordMatch := subtle.ConstantTimeCompare([]byte(password), []byte(expectedPassword)) == 1
		if !ok || !passwordMatch {
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
	if s.getStatus == nil {
		http.Error(w, "Status not available", http.StatusServiceUnavailable)
		return
	}

	status, ok := runWithTimeout(statusAPICollectTimeout, s.getStatus)
	if !ok || status == nil {
		http.Error(w, "Status temporarily unavailable", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		global := s.configService.GetGlobal()
		autoMCC := global.MaxConcurrentCaptures <= 0 &&
			(global.Global == nil || global.Global.MaxConcurrentCaptures <= 0)
		effectiveMCC := global.MaxConcurrentCaptures
		if effectiveMCC <= 0 {
			effectiveMCC = config.EffectiveMaxConcurrentCaptures(global)
		}
		raw, err := json.Marshal(global)
		if err != nil {
			http.Error(w, "Failed to encode config", http.StatusInternalServerError)
			return
		}
		var out map[string]interface{}
		if err := json.Unmarshal(raw, &out); err != nil {
			http.Error(w, "Failed to encode config", http.StatusInternalServerError)
			return
		}
		out["max_concurrent_captures"] = effectiveMCC
		out["max_concurrent_captures_auto"] = autoMCC
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)

	case http.MethodPut:
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}
		var keyPresence map[string]json.RawMessage
		if err := json.Unmarshal(bodyBytes, &keyPresence); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}
		var updates config.GlobalSettings
		if err := json.Unmarshal(bodyBytes, &updates); err != nil {
			http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
			return
		}

		if raw, ok := keyPresence["max_concurrent_captures"]; ok {
			var ptr *int
			if err := json.Unmarshal(raw, &ptr); err != nil {
				http.Error(w, "max_concurrent_captures: invalid value", http.StatusBadRequest)
				return
			}
			if ptr != nil && *ptr != 0 && (*ptr < 1 || *ptr > 10) {
				http.Error(w, fmt.Sprintf("max_concurrent_captures must be 0 (auto), null, or 1–10, got %d", *ptr), http.StatusBadRequest)
				return
			}
		}

		err = s.configService.UpdateGlobal(func(g *config.GlobalSettings) error {
			// Nested sections first (full replacements from JSON)
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
			// Top-level persisted fields (settings UI); apply after nested so PUT body scalars win
			if updates.UpdateChannel != "" {
				g.UpdateChannel = updates.UpdateChannel
			}
			if updates.MaxConcurrentUploads != 0 {
				g.MaxConcurrentUploads = updates.MaxConcurrentUploads
				if g.Global == nil {
					g.Global = &config.Global{}
				}
				g.Global.MaxConcurrentUploads = updates.MaxConcurrentUploads
			}
			if raw, ok := keyPresence["max_concurrent_captures"]; ok {
				var ptr *int
				if err := json.Unmarshal(raw, &ptr); err != nil {
					return err
				}
				if ptr == nil || *ptr == 0 {
					g.MaxConcurrentCaptures = 0
					if g.Global != nil {
						g.Global.MaxConcurrentCaptures = 0
					}
				} else {
					g.MaxConcurrentCaptures = *ptr
					if g.Global == nil {
						g.Global = &config.Global{}
					}
					g.Global.MaxConcurrentCaptures = *ptr
				}
			}
			if updates.TimeoutConnectSeconds != 0 {
				g.TimeoutConnectSeconds = updates.TimeoutConnectSeconds
			}
			if updates.TimeoutUploadSeconds != 0 {
				g.TimeoutUploadSeconds = updates.TimeoutUploadSeconds
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

	// Display name is required; camera id is derived from it (client-supplied id is ignored)
	if strings.TrimSpace(cam.Name) == "" {
		http.Error(w, "Display name is required", http.StatusBadRequest)
		return
	}
	camType, err := normalizeCameraType(cam.Type)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	cam.Type = camType
	cam.ID = ""
	if cam.Upload == nil {
		http.Error(w, "Upload credentials are required", http.StatusBadRequest)
		return
	}
	normalizeUploadForAPI(cam.Upload)

	// Set defaults
	if cam.CaptureIntervalSeconds == 0 {
		cam.CaptureIntervalSeconds = 60
	}
	if cam.Upload.Port == 0 {
		cam.Upload.Port = 2222
	}
	if strings.TrimSpace(cam.Upload.Username) == "" {
		http.Error(w, "Upload username is required", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(cam.Upload.Password) == "" {
		http.Error(w, "Upload password is required", http.StatusBadRequest)
		return
	}

	if cam.Type == "onvif" {
		if err := validateONVIFCameraForPost(&cam); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	} else if cam.ONVIF != nil && strings.TrimSpace(cam.ONVIF.Endpoint) != "" {
		cam.ONVIF.Endpoint = camera.NormalizeONVIFEndpoint(cam.ONVIF.Endpoint)
	}

	// Add camera via ConfigService (returns persisted camera with generated id)
	added, err := s.configService.AddCamera(cam)
	if err != nil {
		if errors.Is(err, config.ErrDuplicateUploadCredentials) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		s.log.Error("Failed to add camera via API",
			"camera_name", cam.Name,
			"error", err,
			"camera_type", cam.Type)
		http.Error(w, fmt.Sprintf("Failed to add camera %q: %v", cam.Name, err), http.StatusInternalServerError)
		return
	}

	s.log.Info("Camera added via API", "camera", added.ID, "type", added.Type)

	global := s.configService.GetGlobal()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(s.cameraToMap(added, global.Timezone))
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
	if err := config.ValidateCameraID(cameraID); err != nil {
		http.Error(w, "Invalid camera ID", http.StatusBadRequest)
		return
	}
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

	existing, err := s.configService.GetCamera(cameraID)
	if err != nil {
		http.Error(w, "Camera not found", http.StatusNotFound)
		return
	}

	if tt := strings.TrimSpace(updates.Type); tt != "" {
		nt, errNorm := normalizeCameraType(tt)
		if errNorm != nil {
			http.Error(w, errNorm.Error(), http.StatusBadRequest)
			return
		}
		updates.Type = nt
	} else {
		updates.Type = existing.Type
	}

	if updates.Upload != nil {
		normalizeUploadForAPI(updates.Upload)
	}

	if updates.ONVIF != nil {
		ep := strings.TrimSpace(updates.ONVIF.Endpoint)
		if ep != "" {
			ep = camera.NormalizeONVIFEndpoint(ep)
		} else if existing.ONVIF != nil && strings.TrimSpace(existing.ONVIF.Endpoint) != "" {
			ep = strings.TrimSpace(existing.ONVIF.Endpoint)
		}
		updates.ONVIF.Endpoint = ep
		if strings.TrimSpace(updates.ONVIF.Endpoint) == "" {
			http.Error(w, "ONVIF endpoint is required when ONVIF settings are present", http.StatusBadRequest)
			return
		}
	}

	if updates.Upload != nil {
		if strings.TrimSpace(updates.Upload.Username) == "" {
			http.Error(w, "Upload username is required", http.StatusBadRequest)
			return
		}
		if updates.Upload.Password == "" {
			if existing.Upload == nil || strings.TrimSpace(existing.Upload.Password) == "" {
				http.Error(w, "Upload password is required", http.StatusBadRequest)
				return
			}
		}
	}

	err = s.configService.UpdateCamera(cameraID, func(cam *config.Camera) error {
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
		if errors.Is(err, config.ErrDuplicateUploadCredentials) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
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

	if cam.ONVIF != nil && cam.ONVIF.Endpoint != "" {
		cam.ONVIF.Endpoint = camera.NormalizeONVIFEndpoint(cam.ONVIF.Endpoint)
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

// handleHealthz reports process and subsystem health. Returns 503 when unhealthy.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
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

// handleReadyz reports capture pipeline readiness. Returns 503 when enabled cameras lack a recent successful capture.
// No authentication. For host watchdog and Docker health checks.
func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.getCaptureReadiness == nil {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "unknown",
			"detail": "capture readiness not configured",
		})
		return
	}

	// Finish before Docker health timeout (5s) even under load.
	ok, reason, completed := runReadinessWithTimeout(4*time.Second, s.getCaptureReadiness)
	body := map[string]interface{}{
		"status":    "ready",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	if !completed || !ok {
		body["status"] = "not_ready"
		if reason != "" {
			body["reason"] = reason
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(body)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func (s *Server) buildHealthStatus() map[string]interface{} {
	// Start with basic health
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}

	if s.getStatus != nil {
		var hi healthIndicators
		details := []string{}
		if status, ok := runWithTimeout(healthStatusCollectTimeout, s.getStatus); ok && status != nil {
			hi = extractHealthIndicators(status)
		} else if !ok {
			health["status"] = "degraded"
			details = append(details, "status collection timed out")
		} else {
			health["status"] = "degraded"
			details = append(details, "status unavailable")
		}

		if hi.orchestratorPresent && !hi.orchestratorRunning {
			health["status"] = "degraded"
			details = append(details, "orchestrator not running")
		}

		if hi.camerasTotal > 0 && hi.camerasActive == 0 {
			health["status"] = "degraded"
			details = append(details, "no enabled cameras")
		}

		if hi.queueHealth == "critical" {
			health["status"] = "degraded"
			details = append(details, "queue critical")
		}

		health["orchestrator_running"] = hi.orchestratorRunning
		health["cameras_active"] = hi.camerasActive
		health["cameras_total"] = hi.camerasTotal
		health["uploads_recent"] = hi.uploadsRecent
		health["queue_health"] = hi.queueHealth
		health["ntp_healthy"] = hi.ntpHealthy

		for _, d := range details {
			appendHealthDetail(health, d)
		}

		if hi.hostRecovery != nil {
			health["host_recovery"] = hi.hostRecovery
		}
	}

	if s.getCaptureReadiness != nil {
		ok, reason := s.getCaptureReadiness()
		if !ok {
			health["status"] = "unhealthy"
			appendHealthDetail(health, "capture not ready: "+reason)
		}
	}

	if hostRecovery, ok := health["host_recovery"].(map[string]interface{}); ok {
		if exhausted, _ := hostRecovery["exhausted"].(bool); exhausted {
			health["status"] = "unhealthy"
			if reason, _ := hostRecovery["reason"].(string); reason != "" {
				appendHealthDetail(health, "host auto-recovery exhausted: "+reason)
			} else {
				appendHealthDetail(health, "host auto-recovery exhausted (manual intervention required)")
			}
		}
	}

	return health
}

// appendHealthDetail adds a semicolon-separated detail string to the health response.
func appendHealthDetail(health map[string]interface{}, detail string) {
	if detail == "" {
		return
	}
	if prev, ok := health["details"].(string); ok && prev != "" {
		health["details"] = prev + "; " + detail
		return
	}
	health["details"] = detail
}

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.log.Info("Update triggered via web UI")

	// Trigger update by creating a force-update trigger file at the mounted data volume
	// root (/data in the container; host /data/aviationwx on Pi). The supervisor checks
	// ${DATA_DIR}/trigger-update on the host.
	dataDir := paths.HostDataDir()
	updateTriggerFile := filepath.Join(dataDir, "trigger-update")

	// Write "force" to indicate we want to skip age checks
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		s.log.Error("Failed to create update trigger directory", "error", err, "dir", dataDir)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  fmt.Sprintf("Failed to trigger update: %v", err),
		})
		return
	}
	if err := os.WriteFile(updateTriggerFile, []byte("force"), 0644); err != nil {
		s.log.Error("Failed to create update trigger file", "error", err, "path", updateTriggerFile)
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
