package bridgeapi

import (
	"context"
	"sync"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/logger"
)

const (
	defaultHeartbeatInterval = 60 * time.Second
	maxErrorFingerprints     = 32
)

// HealthBuilder builds a health request from live bridge state.
type HealthBuilder func() HealthRequest

// Reporter runs periodic health posts and caches the last bootstrap result.
type Reporter struct {
	mu sync.Mutex

	cfgService  *config.Service
	version     string
	commit      string
	log         *logger.Logger
	buildHealth HealthBuilder

	client   *Client
	cancel   context.CancelFunc
	done     chan struct{}
	interval time.Duration

	bootstrap       *BootstrapResponse
	lastHealthOK    time.Time
	lastHealthErr   string
	bridgeAPIStatus string

	errorsMu sync.Mutex
	errors   map[string]*ErrorFingerprint
}

// ReporterConfig wires a Reporter.
type ReporterConfig struct {
	ConfigService *config.Service
	Version       string
	Commit        string
	Log           *logger.Logger
	BuildHealth   HealthBuilder
	Interval      time.Duration
}

// NewReporter creates a reporter. Call SyncFromConfig after construction and on global_updated.
func NewReporter(cfg ReporterConfig) *Reporter {
	log := cfg.Log
	if log == nil {
		log = logger.Default()
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = defaultHeartbeatInterval
	}
	return &Reporter{
		cfgService:      cfg.ConfigService,
		version:         cfg.Version,
		commit:          cfg.Commit,
		log:             log.With("component", "bridge-api"),
		buildHealth:     cfg.BuildHealth,
		interval:        interval,
		errors:          make(map[string]*ErrorFingerprint),
		bridgeAPIStatus: StatusDown,
	}
}

// SyncFromConfig starts, restarts, or stops the heartbeat based on api settings.
func (r *Reporter) SyncFromConfig() {
	r.mu.Lock()
	global := r.cfgService.GetGlobal()
	if !config.APIConfigured(global.API) {
		cancel, done := r.detachLoopLocked()
		r.client = nil
		r.bootstrap = nil
		r.bridgeAPIStatus = StatusDown
		r.lastHealthErr = ""
		r.mu.Unlock()
		waitLoop(cancel, done)
		return
	}

	base := config.EffectiveAPIBaseURL(global.API)
	client, err := NewClient(ClientConfig{
		BaseURL: base,
		APIKey:  global.API.Key,
		Version: r.version,
	})
	if err != nil {
		cancel, done := r.detachLoopLocked()
		r.client = nil
		r.bridgeAPIStatus = StatusDown
		r.lastHealthErr = err.Error()
		r.mu.Unlock()
		waitLoop(cancel, done)
		r.log.Error("bridge api client config invalid", "error", err)
		return
	}

	cancel, done := r.detachLoopLocked()
	r.client = client
	r.mu.Unlock()
	waitLoop(cancel, done)

	r.mu.Lock()
	ctx, ccancel := context.WithCancel(context.Background())
	r.cancel = ccancel
	r.done = make(chan struct{})
	go r.run(ctx)
	r.mu.Unlock()
}

// Stop halts the heartbeat loop.
func (r *Reporter) Stop() {
	r.mu.Lock()
	cancel, done := r.detachLoopLocked()
	r.mu.Unlock()
	waitLoop(cancel, done)
}

// detachLoopLocked clears loop handles. Caller must wait without holding r.mu.
func (r *Reporter) detachLoopLocked() (context.CancelFunc, chan struct{}) {
	cancel := r.cancel
	done := r.done
	r.cancel = nil
	r.done = nil
	return cancel, done
}

func waitLoop(cancel context.CancelFunc, done chan struct{}) {
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// BootstrapNow performs an immediate bootstrap using the current client.
func (r *Reporter) BootstrapNow(ctx context.Context) (*BootstrapResponse, error) {
	r.mu.Lock()
	client := r.client
	r.mu.Unlock()
	if client == nil {
		return nil, errAPINotConfigured
	}
	boot, err := client.Bootstrap(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	if err != nil {
		r.bridgeAPIStatus = StatusDown
		r.lastHealthErr = err.Error()
		r.noteErrorLocked("bridge_api:bootstrap", err.Error())
		return nil, err
	}
	r.bootstrap = boot
	r.bridgeAPIStatus = StatusOperational
	r.lastHealthErr = ""
	return boot, nil
}

// PostWeather posts a weather sample when the API client is configured.
func (r *Reporter) PostWeather(ctx context.Context, req WeatherRequest) error {
	r.mu.Lock()
	client := r.client
	boot := r.bootstrap
	r.mu.Unlock()
	if client == nil {
		return errAPINotConfigured
	}
	if boot != nil && req.BridgeID == "" {
		req.BridgeID = boot.BridgeID
	}
	if err := client.PostWeather(ctx, req); err != nil {
		r.NoteError("weather:post", err.Error())
		return err
	}
	return nil
}

// APIConfigured reports whether the HTTPS client is active.
func (r *Reporter) APIConfigured() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.client != nil
}

// Snapshot returns link status for the console / local status API.
func (r *Reporter) Snapshot() LinkSnapshot {
	r.mu.Lock()
	defer r.mu.Unlock()
	snap := LinkSnapshot{
		Configured:   r.client != nil,
		Status:       r.bridgeAPIStatus,
		LastHealthOK: r.lastHealthOK,
		LastError:    r.lastHealthErr,
		Bootstrap:    r.bootstrap,
	}
	return snap
}

// LinkSnapshot is a read-only view of API link state.
type LinkSnapshot struct {
	Configured   bool
	Status       string
	LastHealthOK time.Time
	LastError    string
	Bootstrap    *BootstrapResponse
}

// NoteError records an error fingerprint for the next health POST.
func (r *Reporter) NoteError(fingerprint, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.noteErrorLocked(fingerprint, message)
}

func (r *Reporter) noteErrorLocked(fingerprint, message string) {
	r.errorsMu.Lock()
	defer r.errorsMu.Unlock()
	if existing, ok := r.errors[fingerprint]; ok {
		existing.Count++
		existing.LastMessage = truncateMsg(message)
		return
	}
	if len(r.errors) >= maxErrorFingerprints {
		return
	}
	r.errors[fingerprint] = &ErrorFingerprint{
		Fingerprint: fingerprint,
		Count:       1,
		LastMessage: truncateMsg(message),
	}
}

func (r *Reporter) drainErrorsLocked() []ErrorFingerprint {
	r.errorsMu.Lock()
	defer r.errorsMu.Unlock()
	out := make([]ErrorFingerprint, 0, len(r.errors))
	for _, e := range r.errors {
		out = append(out, *e)
	}
	r.errors = make(map[string]*ErrorFingerprint)
	return out
}

func (r *Reporter) run(ctx context.Context) {
	defer close(r.done)

	// Immediate bootstrap + health so link status is available without waiting a full interval.
	r.tick(ctx)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *Reporter) tick(ctx context.Context) {
	r.mu.Lock()
	client := r.client
	r.mu.Unlock()
	if client == nil {
		return
	}

	reqCtx, cancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer cancel()

	if r.Snapshot().Bootstrap == nil {
		if _, err := r.BootstrapNow(reqCtx); err != nil {
			r.log.Warn("bridge api bootstrap failed", "error", err)
			return
		}
	}

	req := HealthRequest{
		ObservedAt: time.Now().UTC(),
		Host: HostHealth{
			Status: StatusOperational,
			NTPOK:  true,
			Build: BuildInfo{
				Version: r.version,
				Commit:  r.commit,
			},
		},
		Inventory: Inventory{},
	}
	if r.buildHealth != nil {
		req = r.buildHealth()
		if req.ObservedAt.IsZero() {
			req.ObservedAt = time.Now().UTC()
		}
	}

	r.mu.Lock()
	if r.bootstrap != nil && req.BridgeID == "" {
		req.BridgeID = r.bootstrap.BridgeID
	}
	req.Errors = r.drainErrorsLocked()
	if req.Subsystems == nil {
		req.Subsystems = map[string]SubsystemHealth{}
	}
	req.Subsystems["bridge_api"] = SubsystemHealth{Status: StatusOperational}
	channel := ""
	if g := r.cfgService.GetGlobal(); g.UpdateChannel != "" {
		channel = g.UpdateChannel
	}
	req.Host.Build.Version = r.version
	req.Host.Build.Commit = r.commit
	req.Host.Build.Channel = channel
	client = r.client
	r.mu.Unlock()

	postCtx, postCancel := context.WithTimeout(ctx, defaultRequestTimeout)
	defer postCancel()
	if err := client.PostHealth(postCtx, req); err != nil {
		r.mu.Lock()
		r.bridgeAPIStatus = StatusDegraded
		if IsUnauthorized(err) {
			r.bridgeAPIStatus = StatusDown
		}
		r.lastHealthErr = err.Error()
		r.noteErrorLocked("bridge_api:health", err.Error())
		r.mu.Unlock()
		r.log.Warn("bridge api health post failed", "error", err, "status", StatusCode(err))
		return
	}

	r.mu.Lock()
	r.lastHealthOK = time.Now().UTC()
	r.lastHealthErr = ""
	r.bridgeAPIStatus = StatusOperational
	r.mu.Unlock()
	r.log.Debug("bridge api health posted")
}

func truncateMsg(s string) string {
	if len(s) <= 200 {
		return s
	}
	return s[:200]
}

var errAPINotConfigured = errString("bridge api is not configured")

type errString string

func (e errString) Error() string { return string(e) }
