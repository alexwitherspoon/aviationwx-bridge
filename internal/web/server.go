package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

//go:embed static/*
var staticFiles embed.FS

// Server provides the web console HTTP server
type Server struct {
	config     *config.Config
	configPath string
	mux        *http.ServeMux
	server     *http.Server
	mu         sync.RWMutex

	// Callbacks for integration with orchestrator
	onConfigChange func(*config.Config) error
	getStatus      func() interface{}
	testCamera     func(camConfig config.Camera) ([]byte, error)
	testUpload     func(uploadConfig config.Upload) error
	getCameraImage func(cameraID string) ([]byte, error)
}

// ServerConfig configures the web server
type ServerConfig struct {
	Config         *config.Config
	ConfigPath     string
	OnConfigChange func(*config.Config) error
	GetStatus      func() interface{}
	TestCamera     func(camConfig config.Camera) ([]byte, error)
	TestUpload     func(uploadConfig config.Upload) error
	GetCameraImage func(cameraID string) ([]byte, error)
}

// NewServer creates a new web server
func NewServer(cfg ServerConfig) *Server {
	s := &Server{
		config:         cfg.Config,
		configPath:     cfg.ConfigPath,
		mux:            http.NewServeMux(),
		onConfigChange: cfg.OnConfigChange,
		getStatus:      cfg.GetStatus,
		testCamera:     cfg.TestCamera,
		testUpload:     cfg.TestUpload,
		getCameraImage: cfg.GetCameraImage,
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

	// Health check (no auth)
	s.mux.HandleFunc("/healthz", s.handleHealthz)

	// Static files (require auth except for login assets)
	staticFS, _ := fs.Sub(staticFiles, "static")
	fileServer := http.FileServer(http.FS(staticFS))
	s.mux.HandleFunc("/", s.staticMiddleware(fileServer))
}

// Start starts the web server
func (s *Server) Start() error {
	port := s.config.GetWebPort()
	addr := fmt.Sprintf(":%d", port)

	s.server = &http.Server{
		Addr:         addr,
		Handler:      s.mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
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

// UpdateConfig updates the config reference
func (s *Server) UpdateConfig(cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = cfg
}

// Middleware

func (s *Server) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check for basic auth
		_, password, ok := r.BasicAuth()
		if !ok || password != s.config.GetWebPassword() {
			w.Header().Set("WWW-Authenticate", `Basic realm="AviationWX Bridge"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) staticMiddleware(fileServer http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Allow access to login page without auth
		if r.URL.Path == "/" || r.URL.Path == "/index.html" ||
			strings.HasPrefix(r.URL.Path, "/css/") ||
			strings.HasPrefix(r.URL.Path, "/js/") {
			// Check auth - if not authenticated, serve login prompt
			_, password, ok := r.BasicAuth()
			if !ok || password != s.config.GetWebPassword() {
				w.Header().Set("WWW-Authenticate", `Basic realm="AviationWX Bridge"`)
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
		fileServer.ServeHTTP(w, r)
	}
}

// API Handlers

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	response := map[string]interface{}{
		"first_run":    s.config.IsFirstRun(),
		"timezone":     s.config.Timezone,
		"camera_count": len(s.config.Cameras),
		"time":         time.Now().UTC().Format(time.RFC3339),
	}

	if s.getStatus != nil {
		response["orchestrator"] = s.getStatus()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getConfig(w, r)
	case http.MethodPut:
		s.putConfig(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Return config with passwords masked
	safeConfig := s.maskPasswords(s.config)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(safeConfig)
}

func (s *Server) putConfig(w http.ResponseWriter, r *http.Request) {
	var newConfig config.Config
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate config
	if err := s.validateConfig(&newConfig); err != nil {
		http.Error(w, "Invalid config: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Apply changes
	if s.onConfigChange != nil {
		if err := s.onConfigChange(&newConfig); err != nil {
			http.Error(w, "Failed to apply config: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	s.mu.Lock()
	s.config = &newConfig
	s.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
	s.mu.RLock()
	defer s.mu.RUnlock()

	cameras := make([]map[string]interface{}, len(s.config.Cameras))
	for i, cam := range s.config.Cameras {
		cameras[i] = s.cameraToMap(cam)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cameras)
}

func (s *Server) addCamera(w http.ResponseWriter, r *http.Request) {
	var cam config.Camera
	if err := json.NewDecoder(r.Body).Decode(&cam); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate camera
	if cam.ID == "" {
		http.Error(w, "Camera ID is required", http.StatusBadRequest)
		return
	}
	if cam.Name == "" {
		cam.Name = cam.ID
	}
	if cam.Upload == nil {
		http.Error(w, "Upload credentials are required", http.StatusBadRequest)
		return
	}

	// Check for duplicate ID
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, existing := range s.config.Cameras {
		if existing.ID == cam.ID {
			http.Error(w, "Camera ID already exists", http.StatusConflict)
			return
		}
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

	s.config.Cameras = append(s.config.Cameras, cam)

	// Notify of config change
	if s.onConfigChange != nil {
		// Make a copy of the config to pass to the handler
		configCopy := s.copyConfig(s.config)
		if err := s.onConfigChange(configCopy); err != nil {
			// Rollback
			s.config.Cameras = s.config.Cameras[:len(s.config.Cameras)-1]
			http.Error(w, "Failed to apply: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(s.cameraToMap(cam))
}

func (s *Server) handleCamera(w http.ResponseWriter, r *http.Request) {
	// Extract camera ID from path: /api/cameras/{id}
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
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, cam := range s.config.Cameras {
		if cam.ID == cameraID {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(s.cameraToMap(cam))
			return
		}
	}

	http.Error(w, "Camera not found", http.StatusNotFound)
}

func (s *Server) updateCamera(w http.ResponseWriter, r *http.Request, cameraID string) {
	var update config.Camera
	if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for i, cam := range s.config.Cameras {
		if cam.ID == cameraID {
			// Preserve ID
			update.ID = cameraID
			
			// Preserve existing password if new one is empty
			if update.Upload != nil && update.Upload.Password == "" && cam.Upload != nil {
				update.Upload.Password = cam.Upload.Password
			}
			
			// Preserve auth password if new one is empty
			if update.Auth != nil && update.Auth.Password == "" && cam.Auth != nil {
				update.Auth.Password = cam.Auth.Password
			}
			
			// Preserve RTSP password if new one is empty
			if update.RTSP != nil && update.RTSP.Password == "" && cam.RTSP != nil {
				update.RTSP.Password = cam.RTSP.Password
			}
			
			// Preserve ONVIF password if new one is empty
			if update.ONVIF != nil && update.ONVIF.Password == "" && cam.ONVIF != nil {
				update.ONVIF.Password = cam.ONVIF.Password
			}
			
			s.config.Cameras[i] = update

			if s.onConfigChange != nil {
				// Make a copy of the config to pass to the handler
				configCopy := s.copyConfig(s.config)
				if err := s.onConfigChange(configCopy); err != nil {
					// Rollback
					s.config.Cameras[i] = cam
					http.Error(w, "Failed to apply: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(s.cameraToMap(update))
			return
		}
	}

	http.Error(w, "Camera not found", http.StatusNotFound)
}

func (s *Server) deleteCamera(w http.ResponseWriter, r *http.Request, cameraID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, cam := range s.config.Cameras {
		if cam.ID == cameraID {
			// Remove camera
			s.config.Cameras = append(s.config.Cameras[:i], s.config.Cameras[i+1:]...)

			if s.onConfigChange != nil {
				// Make a copy of the config to pass to the handler
				configCopy := s.copyConfig(s.config)
				if err := s.onConfigChange(configCopy); err != nil {
					// Rollback
					s.config.Cameras = append(s.config.Cameras[:i], append([]config.Camera{cam}, s.config.Cameras[i:]...)...)
					http.Error(w, "Failed to apply: "+err.Error(), http.StatusInternalServerError)
					return
				}
			}

			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	http.Error(w, "Camera not found", http.StatusNotFound)
}

func (s *Server) getCameraPreview(w http.ResponseWriter, r *http.Request, cameraID string) {
	// Check if camera exists
	s.mu.RLock()
	found := false
	for _, cam := range s.config.Cameras {
		if cam.ID == cameraID {
			found = true
			break
		}
	}
	s.mu.RUnlock()

	if !found {
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
		http.Error(w, "Failed to get image: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if len(imageData) == 0 {
		http.Error(w, "No image available yet", http.StatusNoContent)
		return
	}

	// Return image
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.Write(imageData)
}

func (s *Server) handleTime(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getTime(w, r)
	case http.MethodPut:
		s.setTimezone(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getTime(w http.ResponseWriter, r *http.Request) {
	now := time.Now()

	s.mu.RLock()
	timezone := s.config.Timezone
	s.mu.RUnlock()

	var localTime time.Time
	var tzName string
	var utcOffset string

	if timezone != "" {
		loc, err := time.LoadLocation(timezone)
		if err == nil {
			localTime = now.In(loc)
			tzName = timezone
			_, offset := localTime.Zone()
			hours := offset / 3600
			mins := (offset % 3600) / 60
			if mins < 0 {
				mins = -mins
			}
			utcOffset = fmt.Sprintf("%+03d:%02d", hours, mins)
		}
	}

	if localTime.IsZero() {
		localTime = now.Local()
		tzName = "Local"
		_, offset := localTime.Zone()
		hours := offset / 3600
		mins := (offset % 3600) / 60
		if mins < 0 {
			mins = -mins
		}
		utcOffset = fmt.Sprintf("%+03d:%02d", hours, mins)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"utc":        now.UTC().Format(time.RFC3339),
		"local":      localTime.Format(time.RFC3339),
		"timezone":   tzName,
		"utc_offset": utcOffset,
	})
}

func (s *Server) setTimezone(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Timezone string `json:"timezone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate timezone
	if req.Timezone != "" {
		_, err := time.LoadLocation(req.Timezone)
		if err != nil {
			http.Error(w, "Invalid timezone: "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	s.mu.Lock()
	s.config.Timezone = req.Timezone
	s.mu.Unlock()

	if s.onConfigChange != nil {
		if err := s.onConfigChange(s.config); err != nil {
			http.Error(w, "Failed to apply: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
		http.Error(w, "Camera testing not available", http.StatusServiceUnavailable)
		return
	}

	imageData, err := s.testCamera(cam)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	// Return image as base64 or just success
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":    true,
		"image_size": len(imageData),
	})
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
		http.Error(w, "Upload testing not available", http.StatusServiceUnavailable)
		return
	}

	err := s.testUpload(upload)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
	})
}

// Helper functions

func (s *Server) maskPasswords(cfg *config.Config) map[string]interface{} {
	// Create a safe copy with passwords masked
	result := map[string]interface{}{
		"version":  cfg.Version,
		"timezone": cfg.Timezone,
	}

	cameras := make([]map[string]interface{}, len(cfg.Cameras))
	for i, cam := range cfg.Cameras {
		cameras[i] = s.cameraToMap(cam)
	}
	result["cameras"] = cameras

	if cfg.WebConsole != nil {
		result["web_console"] = map[string]interface{}{
			"enabled":  cfg.WebConsole.Enabled,
			"port":     cfg.WebConsole.Port,
			"password": "********",
		}
	}

	return result
}

func (s *Server) cameraToMap(cam config.Camera) map[string]interface{} {
	m := map[string]interface{}{
		"id":                       cam.ID,
		"name":                     cam.Name,
		"type":                     cam.Type,
		"enabled":                  cam.Enabled,
		"capture_interval_seconds": cam.CaptureIntervalSeconds,
	}

	if cam.SnapshotURL != "" {
		m["snapshot_url"] = cam.SnapshotURL
	}

	if cam.Auth != nil {
		m["auth"] = map[string]interface{}{
			"type":     cam.Auth.Type,
			"username": cam.Auth.Username,
			"password": "********",
		}
	}

	if cam.RTSP != nil {
		m["rtsp"] = map[string]interface{}{
			"url":       cam.RTSP.URL,
			"username":  cam.RTSP.Username,
			"password":  "********",
			"substream": cam.RTSP.Substream,
		}
	}

	if cam.Upload != nil {
		m["upload"] = map[string]interface{}{
			"host":     cam.Upload.Host,
			"port":     cam.Upload.Port,
			"username": cam.Upload.Username,
			"password": "********",
			"tls":      cam.Upload.TLS,
		}
	}

	return m
}

func (s *Server) validateConfig(cfg *config.Config) error {
	// Basic validation
	if cfg.Version == 0 {
		cfg.Version = 2
	}

	for i, cam := range cfg.Cameras {
		if cam.ID == "" {
			return fmt.Errorf("camera %d: ID is required", i)
		}
		if cam.Type == "" {
			return fmt.Errorf("camera %s: type is required", cam.ID)
		}
		if cam.Type != "http" && cam.Type != "rtsp" && cam.Type != "onvif" {
			return fmt.Errorf("camera %s: invalid type %q", cam.ID, cam.Type)
		}
		if cam.Upload == nil {
			return fmt.Errorf("camera %s: upload credentials required", cam.ID)
		}
		if cam.Upload.Username == "" {
			return fmt.Errorf("camera %s: upload username required", cam.ID)
		}
		if cam.Upload.Password == "" {
			return fmt.Errorf("camera %s: upload password required", cam.ID)
		}
	}

	return nil
}

// copyConfig creates a deep copy of the config to prevent shared pointer issues
func (s *Server) copyConfig(cfg *config.Config) *config.Config {
	// Marshal to JSON and back to create a deep copy
	data, err := json.Marshal(cfg)
	if err != nil {
		// If marshal fails, return original (shouldn't happen)
		return cfg
	}
	
	var copy config.Config
	if err := json.Unmarshal(data, &copy); err != nil {
		// If unmarshal fails, return original (shouldn't happen)
		return cfg
	}
	
	return &copy
}
