package station

import (
	"context"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

// Provider type strings (match config.Station.Type / wire provider field).
const (
	ProviderDavisWeatherLinkLive = config.StationTypeDavisWeatherLinkLive
)

// GlobalMinPollInterval is the hard ceiling: never sample faster than 1 Hz.
const GlobalMinPollInterval = time.Second

// TransmitterInfo summarizes a Davis radio transmitter for ISS picker UX.
type TransmitterInfo struct {
	Txid              int      `json:"txid"`
	DataStructureType int      `json:"data_structure_type"`
	TempF             *float64 `json:"temp_f,omitempty"`
	HumidityPct       *float64 `json:"humidity_pct,omitempty"`
	RXState           *int     `json:"rx_state,omitempty"`
}

// Observation is one LAN poll result. ProviderMeta.raw carries station-native
// payload for core adapters and console display. No bridge-normalized sample.
// ObservedAt is set only from the station timestamp (e.g. WLL ts); zero means
// missing - do not POST (wrong time is worse than a gap).
type Observation struct {
	ObservedAt   time.Time
	SourceID     string
	Provider     string
	ProviderMeta map[string]interface{}
	DID          string
	Transmitters []TransmitterInfo
}

// Provider polls a LAN station and returns an observation.
type Provider interface {
	// Poll fetches current conditions. When cfg.Txid is nil, Transmitters is
	// still populated for ISS selection.
	Poll(ctx context.Context, cfg config.Station) (*Observation, error)
}

// StationStatus is runtime state for health / console (no derived weather readout).
type StationStatus struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Type           string    `json:"type"`
	Enabled        bool      `json:"enabled"`
	WaitingForTxid bool      `json:"waiting_for_txid"`
	LANOK          bool      `json:"lan_ok"`
	Degraded       bool      `json:"degraded,omitempty"` // LAN reachable but observation not trustworthy for POST
	LastPollAt     time.Time `json:"last_poll_at,omitempty"`
	LastPollError  string    `json:"last_poll_error,omitempty"`
	LastPostAt     time.Time `json:"last_post_at,omitempty"`
	LastPostError  string    `json:"last_post_error,omitempty"`
	LastObservedAt time.Time `json:"last_observed_at,omitempty"`
	OutboundQueued int       `json:"outbound_queued,omitempty"`
}
