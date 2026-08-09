package station

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

const (
	interceptorAPIMeta     = "http_interceptor_wunderground_v1"
	interceptorMaxBody     = 64 << 10
	interceptorIdleTimeout = 60 * time.Second
	interceptorRedacted    = "[redacted]"
)

// parseWundergroundDateUTC parses WU dateutc ("2000-01-01 00:00:00" or
// "now"). Empty / unparsable / "now" yield zero time (caller must skip POST).
func parseWundergroundDateUTC(raw string) time.Time {
	s := strings.TrimSpace(raw)
	if s == "" || strings.EqualFold(s, "now") {
		return time.Time{}
	}
	// Classic WU: YYYY-MM-DD HH:MM:SS as UTC.
	if t, err := time.Parse("2006-01-02 15:04:05", s); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

// sensitiveInterceptorRawKey reports WU/query keys that must not leave the Pi
// in weather POST provider_meta.raw or console payload logs.
func sensitiveInterceptorRawKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "password", "pass", "key", "apikey", "api_key", "token", "secret", "auth":
		return true
	default:
		return false
	}
}

// buildInterceptorObservation maps WU form/query fields into an Observation.
// rawValues should already be flattened key -> single string (first value).
// Credential-like keys are redacted in provider_meta.raw.
func buildInterceptorObservation(cfg config.Station, rawValues map[string]string) *Observation {
	metaRaw := make(map[string]interface{}, len(rawValues))
	for k, v := range rawValues {
		if sensitiveInterceptorRawKey(k) {
			metaRaw[k] = interceptorRedacted
			continue
		}
		metaRaw[k] = v
	}
	obs := &Observation{
		SourceID: cfg.ID,
		Provider: ProviderHTTPInterceptor,
		ProviderMeta: map[string]interface{}{
			"api":     interceptorAPIMeta,
			"path":    cfg.ListenPath,
			"dialect": cfg.Dialect,
			"raw":     metaRaw,
		},
	}
	if cfg.Dialect == "" {
		obs.ProviderMeta["dialect"] = config.HTTPInterceptorDialectWunderground
	}
	obs.ObservedAt = parseWundergroundDateUTC(rawValues["dateutc"])
	return obs
}

func formOrQueryValues(r *http.Request) (map[string]string, error) {
	switch r.Method {
	case http.MethodGet:
		return flattenURLValues(r.URL.Query()), nil
	case http.MethodPost, http.MethodPut:
		ct := r.Header.Get("Content-Type")
		if strings.Contains(ct, "application/x-www-form-urlencoded") || ct == "" {
			body, err := io.ReadAll(io.LimitReader(r.Body, interceptorMaxBody+1))
			if err != nil {
				return nil, err
			}
			if len(body) > interceptorMaxBody {
				return nil, fmt.Errorf("body too large")
			}
			vals, err := url.ParseQuery(string(body))
			if err != nil {
				return nil, err
			}
			// Merge query string (some devices put ID on query).
			for k, vs := range r.URL.Query() {
				if _, ok := vals[k]; !ok {
					vals[k] = vs
				}
			}
			return flattenURLValues(vals), nil
		}
		return nil, fmt.Errorf("unsupported Content-Type %q", ct)
	default:
		return nil, fmt.Errorf("method not allowed")
	}
}

func flattenURLValues(v url.Values) map[string]string {
	out := make(map[string]string, len(v))
	for k, vs := range v {
		if len(vs) > 0 {
			out[k] = vs[0]
		}
	}
	return out
}

// interceptorRoute is one listen_path -> station binding.
type interceptorRoute struct {
	station config.Station
}

type interceptorJob struct {
	station config.Station
	obs     *Observation
}

// interceptorHub serves Weather Underground-compatible ingest for one listen_addr.
// Under API backpressure, pending keeps at most one job per station (latest wins)
// and a single worker posts so Serve can ACK without spawning a goroutine per request.
type interceptorHub struct {
	mgr  *Manager
	addr string

	mu       sync.RWMutex
	routes   map[string]interceptorRoute // path -> station
	lastEmit map[string]time.Time        // station id -> last accepted emit (1 Hz)
	pending  map[string]interceptorJob   // station id -> latest job

	server     *http.Server
	done       chan struct{} // closed when Serve exits
	workerStop chan struct{}
	workerDone chan struct{}
	wake       chan struct{}
	postCtx    context.Context
	postCancel context.CancelFunc
	stopOnce   sync.Once
}

func newInterceptorHub(mgr *Manager, addr string) *interceptorHub {
	parent := context.Background()
	if mgr != nil && mgr.runCtx != nil {
		parent = mgr.runCtx
	}
	ctx, cancel := context.WithCancel(parent)
	return &interceptorHub{
		mgr:        mgr,
		addr:       addr,
		routes:     make(map[string]interceptorRoute),
		lastEmit:   make(map[string]time.Time),
		pending:    make(map[string]interceptorJob),
		done:       make(chan struct{}),
		workerStop: make(chan struct{}),
		workerDone: make(chan struct{}),
		wake:       make(chan struct{}, 1),
		postCtx:    ctx,
		postCancel: cancel,
	}
}

func (h *interceptorHub) setRoutes(routes map[string]interceptorRoute) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.routes = routes
	keep := make(map[string]struct{}, len(routes))
	for _, r := range routes {
		keep[r.station.ID] = struct{}{}
	}
	for id := range h.lastEmit {
		if _, ok := keep[id]; !ok {
			delete(h.lastEmit, id)
		}
	}
	for id := range h.pending {
		if _, ok := keep[id]; !ok {
			delete(h.pending, id)
		}
	}
}

func (h *interceptorHub) alive() bool {
	if h == nil || h.done == nil {
		return false
	}
	select {
	case <-h.done:
		return false
	default:
		return true
	}
}

func (h *interceptorHub) enqueue(st config.Station, obs *Observation) {
	h.mu.Lock()
	h.pending[st.ID] = interceptorJob{station: st, obs: obs}
	h.mu.Unlock()
	select {
	case h.wake <- struct{}{}:
	default:
	}
}

func (h *interceptorHub) takePending() []interceptorJob {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.pending) == 0 {
		return nil
	}
	out := make([]interceptorJob, 0, len(h.pending))
	for id, job := range h.pending {
		out = append(out, job)
		delete(h.pending, id)
	}
	return out
}

func (h *interceptorHub) runWorker() {
	defer close(h.workerDone)
	for {
		select {
		case <-h.workerStop:
			h.dropPending()
			return
		case <-h.wake:
			for {
				select {
				case <-h.workerStop:
					h.dropPending()
					return
				default:
				}
				jobs := h.takePending()
				if len(jobs) == 0 {
					break
				}
				for _, job := range jobs {
					select {
					case <-h.workerStop:
						h.dropPending()
						return
					default:
					}
					h.mgr.handleInterceptorObservationCtx(h.postCtx, job.station, job.obs)
				}
			}
		}
	}
}

// dropPending clears queued jobs on shutdown. In-flight posts use hub.postCtx
// (child of Manager.runCtx) so Stop and hub reload can cancel them.
func (h *interceptorHub) dropPending() {
	h.mu.Lock()
	h.pending = make(map[string]interceptorJob)
	h.mu.Unlock()
}

func (h *interceptorHub) start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", h.serve)
	h.server = &http.Server{
		Addr:              h.addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       interceptorIdleTimeout,
	}
	ln, err := net.Listen("tcp", h.addr)
	if err != nil {
		return err
	}
	go h.runWorker()
	go func() {
		defer close(h.done)
		err := h.server.Serve(ln)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.mgr.log.Warn("interceptor listener stopped unexpectedly",
				"addr", h.addr, "error", err)
			h.mu.RLock()
			routes := make([]interceptorRoute, 0, len(h.routes))
			for _, r := range h.routes {
				routes = append(routes, r)
			}
			h.mu.RUnlock()
			for _, route := range routes {
				stID := route.station.ID
				errMsg := fmt.Sprintf("interceptor listen failed on %s: %v", h.addr, err)
				h.mgr.setStatus(stID, func(s *StationStatus) {
					s.Degraded = true
					s.LANOK = false
					s.LastPollError = errMsg
				})
			}
		}
	}()
	return nil
}

func (h *interceptorHub) stop() {
	h.stopOnce.Do(func() {
		if h.postCancel != nil {
			h.postCancel()
		}
		if h.server == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := h.server.Shutdown(ctx); err != nil {
			// Shutdown timed out or failed - force close so Serve exits and done closes.
			_ = h.server.Close()
		}
		<-h.done
		close(h.workerStop)
		<-h.workerDone
	})
}

func (h *interceptorHub) serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if path == "" {
		path = "/"
	}
	h.mu.RLock()
	route, ok := h.routes[path]
	h.mu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, interceptorMaxBody)
	vals, err := formOrQueryValues(r)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || strings.Contains(err.Error(), "body too large") {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	st := route.station
	obs := buildInterceptorObservation(st, vals)
	now := time.Now().UTC()

	// Global ≤1 Hz ceiling per station.
	h.mu.Lock()
	last := h.lastEmit[st.ID]
	if !last.IsZero() && now.Sub(last) < GlobalMinPollInterval {
		h.mu.Unlock()
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success\n"))
		return
	}
	h.lastEmit[st.ID] = now
	h.mu.Unlock()

	// ACK immediately. Weather POST can block up to tens of seconds; WU-style
	// devices retry or drop if the listener stalls on the response.
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("success\n"))

	h.enqueue(st, obs)
}

// PreviewInterceptorRequest parses a WU-style payload without posting (console Test).
func (m *Manager) PreviewInterceptorRequest(st config.Station, values map[string]string) (*Observation, error) {
	config.NormalizeStationDefaults(&st)
	if st.Type != config.StationTypeHTTPInterceptor {
		return nil, fmt.Errorf("station type is not http_interceptor")
	}
	if strings.TrimSpace(st.ID) == "" {
		st.ID = "preview"
	}
	if err := config.ValidateStation(st); err != nil {
		return nil, err
	}
	if values == nil {
		values = map[string]string{}
	}
	return buildInterceptorObservation(st, values), nil
}

// InjectInterceptorRequest applies one WU ingest (status + optional weather POST).
func (m *Manager) InjectInterceptorRequest(st config.Station, values map[string]string) (*Observation, error) {
	obs, err := m.PreviewInterceptorRequest(st, values)
	if err != nil {
		return nil, err
	}
	config.NormalizeStationDefaults(&st)
	if strings.TrimSpace(st.ID) == "" {
		st.ID = "preview"
	}
	m.handleInterceptorObservationCtx(m.runCtx, st, obs)
	return obs, nil
}

func (m *Manager) handleInterceptorObservationCtx(parent context.Context, st config.Station, obs *Observation) {
	now := time.Now().UTC()
	entry := PayloadLogEntry{
		At:         now,
		ObservedAt: obs.ObservedAt,
		StationID:  st.ID,
		LANOK:      true,
	}
	if obs.ProviderMeta != nil {
		entry.Raw = obs.ProviderMeta["raw"]
	}

	if obs.ObservedAt.IsZero() {
		msg := "missing observation timestamp (dateutc); skipped weather POST"
		m.setStatus(st.ID, func(s *StationStatus) {
			s.LANOK = true
			s.Degraded = true
			s.WaitingForTxid = false
			s.LastPollAt = now
			s.LastPollError = msg
			s.LastObservedAt = time.Time{}
		})
		entry.Message = msg
		m.notePayload(entry)
		m.log.Warn("interceptor ingest degraded", "station", st.ID, "reason", "missing_dateutc")
		return
	}

	m.setStatus(st.ID, func(s *StationStatus) {
		s.LANOK = true
		s.Degraded = false
		s.WaitingForTxid = false
		s.LastPollAt = now
		s.LastPollError = ""
		s.LastObservedAt = obs.ObservedAt
	})

	if m.poster != nil && m.poster.APIConfigured() {
		if parent == nil {
			parent = context.Background()
		}
		ctx, cancel := context.WithTimeout(parent, 30*time.Second)
		postErr := m.postObservation(ctx, st, obs)
		cancel()
		posted := postErr == nil
		entry.Posted = &posted
		if postErr != nil {
			entry.Message = postErr.Error()
		}
	}
	m.notePayload(entry)
}
