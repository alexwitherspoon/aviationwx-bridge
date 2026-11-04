package station

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

const tempestAPIMeta = "weatherflow_tempest_udp_v1"

// tempestMaxDatagram bounds a single UDP datagram; Tempest JSON is well under 1 KB.
const tempestMaxDatagram = 8 << 10

// buildTempestObservation maps a WeatherFlow Tempest UDP datagram into an
// Observation. observed_at is the packet's own observation epoch (index 0 of
// obs/ob/evt), never the bridge clock; datagrams without an observation time
// (device_status / hub_status) are rejected rather than posted.
func buildTempestObservation(cfg config.Station, raw json.RawMessage) (*Observation, error) {
	var rawMap map[string]json.RawMessage
	if err := json.Unmarshal(raw, &rawMap); err != nil {
		return nil, fmt.Errorf("tempest datagram is not a JSON object: %w", err)
	}
	if rawMap == nil {
		return nil, fmt.Errorf("tempest datagram is JSON null, want an object")
	}
	epoch, ok := tempestEpoch(raw)
	if !ok {
		return nil, fmt.Errorf("tempest datagram has no observation time")
	}
	return &Observation{
		SourceID:   cfg.ID,
		Provider:   ProviderTempestUDP,
		ObservedAt: time.Unix(epoch, 0).UTC(),
		ProviderMeta: map[string]interface{}{
			"api": tempestAPIMeta,
			"raw": rawMap,
		},
	}, nil
}

// tempestEpoch extracts the observation epoch. Observation messages embed it at
// obs[0][0] (obs_st/obs_air/obs_sky), ob[0] (rapid_wind), or evt[0] (events).
func tempestEpoch(raw json.RawMessage) (int64, bool) {
	var top struct {
		Obs [][]interface{} `json:"obs"`
		Ob  []interface{}   `json:"ob"`
		Evt []interface{}   `json:"evt"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return 0, false
	}
	var v interface{}
	switch {
	case len(top.Obs) > 0 && len(top.Obs[0]) > 0:
		v = top.Obs[0][0]
	case len(top.Ob) > 0:
		v = top.Ob[0]
	case len(top.Evt) > 0:
		v = top.Evt[0]
	default:
		return 0, false
	}
	f, ok := v.(float64)
	if !ok {
		return 0, false
	}
	return int64(f), true
}

// TempestHub owns the shared UDP listener for tempest stations and routes each
// datagram to the station whose serial_number matches the packet, or to a
// catch-all station with no serial filter.
type TempestHub struct {
	mgr  *Manager
	mu   sync.Mutex
	conn *net.UDPConn
	addr string

	bySerial map[string]config.Station
	catchAll []config.Station

	jobs       chan tempestJob
	done       chan struct{}
	workerDone chan struct{}
}

type tempestJob struct {
	st  config.Station
	obs *Observation
}

func newTempestHub(m *Manager) *TempestHub {
	return &TempestHub{
		mgr:        m,
		bySerial:   make(map[string]config.Station),
		jobs:       make(chan tempestJob, 32),
		done:       make(chan struct{}),
		workerDone: make(chan struct{}),
	}
}

// setRoutes rebuilds the serial routing table.
func (h *TempestHub) setRoutes(stations []config.Station) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.bySerial = make(map[string]config.Station)
	h.catchAll = h.catchAll[:0]
	for _, st := range stations {
		if strings.TrimSpace(st.SerialNumber) == "" {
			h.catchAll = append(h.catchAll, st)
			continue
		}
		if _, exists := h.bySerial[st.SerialNumber]; !exists {
			h.bySerial[st.SerialNumber] = st
		}
	}
}

// stationFor returns the station that should receive a datagram from a device.
func (h *TempestHub) stationFor(serial string) (config.Station, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if st, ok := h.bySerial[serial]; ok {
		return st, true
	}
	if len(h.catchAll) > 0 {
		return h.catchAll[0], true
	}
	return config.Station{}, false
}

// start binds the UDP socket and launches the read and delivery workers.
func (h *TempestHub) start(addr string) error {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("tempest resolve listen addr: %w", err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("tempest listen: %w", err)
	}
	h.mu.Lock()
	h.conn = conn
	h.addr = addr
	h.mu.Unlock()
	go h.readLoop()
	go h.runWorker()
	return nil
}

// localAddr returns the bound address (used by tests that bind an ephemeral port).
func (h *TempestHub) localAddr() *net.UDPAddr {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == nil {
		return nil
	}
	return h.conn.LocalAddr().(*net.UDPAddr)
}

func (h *TempestHub) readLoop() {
	buf := make([]byte, tempestMaxDatagram)
	for {
		h.mu.Lock()
		conn := h.conn
		h.mu.Unlock()
		if conn == nil {
			return
		}
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-h.done:
				return
			default:
			}
			continue
		}
		h.handleDatagram(buf[:n])
	}
}

func (h *TempestHub) handleDatagram(b []byte) {
	var meta struct {
		Serial string `json:"serial_number"`
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return
	}
	st, ok := h.stationFor(meta.Serial)
	if !ok {
		return
	}
	obs, err := buildTempestObservation(st, b)
	if err != nil {
		h.mgr.setStatus(st.ID, func(s *StationStatus) {
			s.Degraded = true
			s.LastPollError = err.Error()
		})
		return
	}
	select {
	case h.jobs <- tempestJob{st, obs}:
	default:
		// UDP is lossy by nature; dropping under backpressure is acceptable.
	}
}

func (h *TempestHub) runWorker() {
	defer close(h.workerDone)
	for {
		select {
		case <-h.done:
			return
		case j := <-h.jobs:
			h.mgr.handleTempestObservation(j.st, j.obs)
		}
	}
}

func (h *TempestHub) stop() {
	close(h.done)
	h.mu.Lock()
	conn := h.conn
	h.mu.Unlock()
	if conn != nil {
		conn.Close()
	}
	<-h.workerDone
}

// handleTempestObservation records status and posts a received observation.
func (m *Manager) handleTempestObservation(st config.Station, obs *Observation) {
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

	m.setStatus(st.ID, func(s *StationStatus) {
		s.LANOK = true
		s.Degraded = false
		s.WaitingForTxid = false
		s.LastPollAt = now
		s.LastPollError = ""
		s.LastObservedAt = obs.ObservedAt
	})

	if m.poster != nil && m.poster.APIConfigured() {
		ctx, cancel := context.WithTimeout(context.Background(), weatherPostTimeout)
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
