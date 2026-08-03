package station

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/bridgeapi"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/logger"
)

// WeatherPoster posts weather observations to the AviationWX bridge API.
type WeatherPoster interface {
	PostWeather(ctx context.Context, req bridgeapi.WeatherRequest) error
	APIConfigured() bool
}

// Manager runs LAN poll loops for configured stations and optionally posts weather.
type Manager struct {
	cfgService *config.Service
	poster     WeatherPoster
	log        *logger.Logger
	providers  map[string]Provider

	mu      sync.Mutex
	workers map[string]*stationWorker
	status  map[string]StationStatus
	ring    *outboundRing

	// recentPayloads is an in-memory newest-first log for console (no disk).
	recentMu       sync.Mutex
	recentPayloads []PayloadLogEntry
}

// PayloadLogEntry is one line for the Weather page raw payload log (newest first).
type PayloadLogEntry struct {
	At         time.Time   `json:"at"`
	ObservedAt time.Time   `json:"observed_at,omitempty"`
	StationID  string      `json:"station_id"`
	LANOK      bool        `json:"lan_ok"`
	Posted     *bool       `json:"posted,omitempty"` // set when an API weather POST was attempted
	Message    string      `json:"message,omitempty"`
	Raw        interface{} `json:"raw,omitempty"` // station-native payload as received
}

const maxPayloadLog = 40

// ManagerConfig wires a Manager.
type ManagerConfig struct {
	ConfigService *config.Service
	Poster        WeatherPoster
	Log           *logger.Logger
}

// NewManager creates a station manager. Call SyncFromConfig to start workers.
func NewManager(cfg ManagerConfig) *Manager {
	log := cfg.Log
	if log == nil {
		log = logger.Default()
	}
	return &Manager{
		cfgService: cfg.ConfigService,
		poster:     cfg.Poster,
		log:        log.With("component", "station"),
		providers: map[string]Provider{
			ProviderDavisWeatherLinkLive: NewDavis(),
		},
		workers: make(map[string]*stationWorker),
		status:  make(map[string]StationStatus),
		ring:    newOutboundRing(),
	}
}

// SyncFromConfig starts, restarts, or stops poll workers to match config.
func (m *Manager) SyncFromConfig() {
	stations := m.cfgService.ListStations()
	wanted := make(map[string]config.Station, len(stations))
	for _, st := range stations {
		wanted[st.ID] = st
	}

	var toStop []*stationWorker

	m.mu.Lock()
	for id, w := range m.workers {
		st, ok := wanted[id]
		if !ok || !st.Enabled || st.Txid == nil || !samePollConfig(w.cfg, st) {
			toStop = append(toStop, w)
			delete(m.workers, id)
		}
	}
	m.mu.Unlock()

	for _, w := range toStop {
		w.stop()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for id, st := range wanted {
		m.status[id] = m.statusFromConfig(st, m.status[id])
		if !st.Enabled {
			continue
		}
		if st.Txid == nil {
			s := m.status[id]
			s.WaitingForTxid = true
			m.status[id] = s
			continue
		}
		if _, running := m.workers[id]; running {
			continue
		}
		prov, err := m.providerFor(st.Type)
		if err != nil {
			m.log.Warn("unsupported station type", "station", id, "type", st.Type, "error", err)
			continue
		}
		w := newStationWorker(m, st, prov)
		m.workers[id] = w
		go w.run()
		m.log.Info("station poll started",
			"station", id,
			"type", st.Type,
			"host", st.Host,
			"interval_s", pollInterval(st).Seconds())
	}

	for id := range m.status {
		if _, ok := wanted[id]; !ok {
			delete(m.status, id)
		}
	}
}

// Stop halts all poll workers.
func (m *Manager) Stop() {
	m.mu.Lock()
	toStop := make([]*stationWorker, 0, len(m.workers))
	for id, w := range m.workers {
		toStop = append(toStop, w)
		delete(m.workers, id)
	}
	m.mu.Unlock()
	for _, w := range toStop {
		w.stop()
	}
}

// StatusSnapshot returns per-station runtime status.
func (m *Manager) StatusSnapshot() []StationStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]StationStatus, 0, len(m.status))
	for _, st := range m.cfgService.ListStations() {
		s, ok := m.status[st.ID]
		if !ok {
			s = m.statusFromConfig(st, StationStatus{})
		}
		s.OutboundQueued = m.ring.Len()
		out = append(out, s)
	}
	return out
}

// WeatherSubsystemHealth builds the weather subsystem block for API health.
func (m *Manager) WeatherSubsystemHealth() (bridgeapi.SubsystemHealth, bool) {
	stations := m.cfgService.ListStations()
	if len(stations) == 0 {
		return bridgeapi.SubsystemHealth{}, false
	}
	statuses := m.StatusSnapshot()
	lanOK := true
	postOK := true
	degraded := false
	waiting := 0
	enabled := 0
	for _, s := range statuses {
		if !s.Enabled {
			continue
		}
		enabled++
		if s.WaitingForTxid {
			waiting++
			continue
		}
		if !s.LANOK && !s.LastPollAt.IsZero() {
			lanOK = false
		}
		if s.Degraded {
			degraded = true
		}
		if s.LastPostError != "" && s.LastPostAt.IsZero() {
			postOK = false
		}
		if !s.LastPostAt.IsZero() && s.LastPostError != "" {
			postOK = false
		}
	}
	status := bridgeapi.StatusOperational
	if enabled == 0 {
		status = bridgeapi.StatusOperational
	} else if !lanOK {
		status = bridgeapi.StatusDown
	} else if waiting == enabled || degraded {
		status = bridgeapi.StatusDegraded
	} else if m.poster != nil && m.poster.APIConfigured() && !postOK {
		status = bridgeapi.StatusDegraded
	}
	detail := map[string]interface{}{
		"lan_ok":           lanOK,
		"stations_enabled": enabled,
		"waiting_for_txid": waiting,
		"outbound_queued":  m.ring.Len(),
		"degraded":         degraded,
	}
	if m.poster != nil {
		detail["api_configured"] = m.poster.APIConfigured()
	}
	return bridgeapi.SubsystemHealth{Status: status, Detail: detail}, true
}

// TestPoll runs a single poll (for console Test poll / ISS list). Does not require txid.
func (m *Manager) TestPoll(ctx context.Context, cfg config.Station) (*Observation, error) {
	NormalizeForPoll(&cfg)
	prov, err := m.providerFor(cfg.Type)
	if err != nil {
		return nil, err
	}
	return prov.Poll(ctx, cfg)
}

// RecentPayloads returns newest-first payload log lines.
func (m *Manager) RecentPayloads() []PayloadLogEntry {
	m.recentMu.Lock()
	defer m.recentMu.Unlock()
	out := make([]PayloadLogEntry, len(m.recentPayloads))
	copy(out, m.recentPayloads)
	return out
}

func (m *Manager) providerFor(typ string) (Provider, error) {
	p, ok := m.providers[typ]
	if !ok {
		return nil, fmt.Errorf("unsupported station type %q", typ)
	}
	return p, nil
}

func (m *Manager) statusFromConfig(st config.Station, prev StationStatus) StationStatus {
	prev.ID = st.ID
	prev.Name = st.Name
	prev.Type = st.Type
	prev.Enabled = st.Enabled
	prev.WaitingForTxid = st.Enabled && st.Txid == nil
	return prev
}

func (m *Manager) setStatus(id string, mut func(*StationStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.status[id]
	mut(&s)
	m.status[id] = s
}

func (m *Manager) notePayload(entry PayloadLogEntry) {
	m.recentMu.Lock()
	defer m.recentMu.Unlock()
	m.recentPayloads = append([]PayloadLogEntry{entry}, m.recentPayloads...)
	if len(m.recentPayloads) > maxPayloadLog {
		m.recentPayloads = m.recentPayloads[:maxPayloadLog]
	}
}

func (m *Manager) postObservation(ctx context.Context, st config.Station, obs *Observation) error {
	if m.poster == nil || !m.poster.APIConfigured() {
		return nil
	}
	req := weatherRequestFromObs(obs)
	m.flushRing(ctx)
	if err := m.poster.PostWeather(ctx, req); err != nil {
		m.ring.Push(req)
		m.setStatus(st.ID, func(s *StationStatus) {
			s.LastPostError = err.Error()
		})
		m.log.Warn("weather post failed", "station", st.ID, "error", err)
		return err
	}
	m.setStatus(st.ID, func(s *StationStatus) {
		s.LastPostAt = time.Now().UTC()
		s.LastPostError = ""
	})
	return nil
}

func (m *Manager) flushRing(ctx context.Context) {
	if m.poster == nil || !m.poster.APIConfigured() {
		return
	}
	pending := m.ring.PopAll()
	for i, req := range pending {
		if err := m.poster.PostWeather(ctx, req); err != nil {
			m.ring.PushFront(pending[i:]...)
			return
		}
	}
}

func weatherRequestFromObs(obs *Observation) bridgeapi.WeatherRequest {
	return bridgeapi.WeatherRequest{
		ObservedAt:   obs.ObservedAt,
		SourceID:     obs.SourceID,
		Provider:     obs.Provider,
		ProviderMeta: obs.ProviderMeta,
		// sample intentionally omitted - core owns units from provider_meta.raw
	}
}

type stationWorker struct {
	mgr    *Manager
	cfg    config.Station
	prov   Provider
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

func newStationWorker(mgr *Manager, cfg config.Station, prov Provider) *stationWorker {
	ctx, cancel := context.WithCancel(context.Background())
	return &stationWorker{
		mgr:    mgr,
		cfg:    cfg,
		prov:   prov,
		ctx:    ctx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
}

func (w *stationWorker) stop() {
	w.cancel()
	<-w.done
}

func (w *stationWorker) run() {
	defer close(w.done)

	interval := pollInterval(w.cfg)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	w.tick(w.ctx)

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.tick(w.ctx)
		}
	}
}

func (w *stationWorker) tick(ctx context.Context) {
	pollCtx, cancel := context.WithTimeout(ctx, davisHTTPTimeout+2*time.Second)
	defer cancel()

	obs, err := w.prov.Poll(pollCtx, w.cfg)
	now := time.Now().UTC()
	if err != nil {
		w.mgr.setStatus(w.cfg.ID, func(s *StationStatus) {
			s.LANOK = false
			s.LastPollAt = now
			s.LastPollError = err.Error()
			s.WaitingForTxid = false
		})
		w.mgr.notePayload(PayloadLogEntry{
			At:        now,
			StationID: w.cfg.ID,
			LANOK:     false,
			Message:   err.Error(),
		})
		w.mgr.log.Warn("station poll failed", "station", w.cfg.ID, "error", err)
		return
	}

	w.mgr.setStatus(w.cfg.ID, func(s *StationStatus) {
		s.LANOK = true
		s.LastPollAt = now
		s.WaitingForTxid = w.cfg.Txid == nil
		s.Degraded = false
		s.LastPollError = ""
		s.LastObservedAt = obs.ObservedAt
	})

	entry := PayloadLogEntry{
		At:         now,
		ObservedAt: obs.ObservedAt,
		StationID:  w.cfg.ID,
		LANOK:      true,
	}
	if obs.ProviderMeta != nil {
		entry.Raw = obs.ProviderMeta["raw"]
	}

	// Station timestamp required for POST. Gap is safer than inventing observed_at.
	if w.cfg.Txid != nil && obs.ObservedAt.IsZero() {
		msg := "missing observation timestamp (station ts); skipped weather POST"
		w.mgr.setStatus(w.cfg.ID, func(s *StationStatus) {
			s.Degraded = true
			s.LastPollError = msg
			s.LastObservedAt = time.Time{}
		})
		entry.Message = msg
		w.mgr.notePayload(entry)
		w.mgr.log.Warn("station poll degraded", "station", w.cfg.ID, "reason", "missing_ts")
		return
	}

	if w.cfg.Txid != nil && w.mgr.poster != nil && w.mgr.poster.APIConfigured() {
		postErr := w.mgr.postObservation(pollCtx, w.cfg, obs)
		posted := postErr == nil
		entry.Posted = &posted
		if postErr != nil {
			entry.Message = postErr.Error()
		}
	}

	w.mgr.notePayload(entry)
}

func pollInterval(st config.Station) time.Duration {
	sec := st.PollIntervalSeconds
	if sec <= 0 {
		sec = config.DefaultDavisPollIntervalSeconds
	}
	d := time.Duration(sec) * time.Second
	if st.Type == config.StationTypeDavisWeatherLinkLive && d < time.Duration(config.DefaultDavisPollIntervalSeconds)*time.Second {
		d = time.Duration(config.DefaultDavisPollIntervalSeconds) * time.Second
	}
	if d < GlobalMinPollInterval {
		d = GlobalMinPollInterval
	}
	return d
}

func samePollConfig(a, b config.Station) bool {
	if a.Host != b.Host || a.Type != b.Type || a.Enabled != b.Enabled {
		return false
	}
	if a.PollIntervalSeconds != b.PollIntervalSeconds || a.WindReference != b.WindReference {
		return false
	}
	if (a.Txid == nil) != (b.Txid == nil) {
		return false
	}
	if a.Txid != nil && b.Txid != nil && *a.Txid != *b.Txid {
		return false
	}
	return true
}

// NormalizeForPoll applies station defaults before a one-shot Test poll.
func NormalizeForPoll(st *config.Station) {
	config.NormalizeStationDefaults(st)
}
