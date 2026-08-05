package bridgeapi

import "time"

// BootstrapResponse is the data object from GET /v1/bridge/bootstrap
// (core wraps it as { "success": true, "data": ... } per OpenAPI).
type BootstrapResponse struct {
	Airport                  BootstrapAirport `json:"airport"`
	BridgeID                 string           `json:"bridge_id"`
	DeclinationDeg           float64          `json:"declination_deg"`
	DeclinationSource        string           `json:"declination_source,omitempty"` // override|global|wmm|none
	HeartbeatIntervalSeconds int              `json:"heartbeat_interval_seconds,omitempty"`
	EnabledSources           []EnabledSource  `json:"enabled_sources,omitempty"`
}

// bootstrapEnvelope matches the core {success, data} response wrapper.
type bootstrapEnvelope struct {
	Success bool               `json:"success"`
	Data    *BootstrapResponse `json:"data"`
}

// BootstrapAirport is airport identity from bootstrap.
type BootstrapAirport struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// EnabledSource is a core-enabled binding returned by bootstrap.
type EnabledSource struct {
	Kind           string `json:"kind"`
	BridgeSourceID string `json:"bridge_source_id"`
	CoreStationID  string `json:"core_station_id,omitempty"`
	Enabled        bool   `json:"enabled"`
}

// HealthRequest is POST /v1/bridge/health.
type HealthRequest struct {
	ObservedAt time.Time                  `json:"observed_at"`
	BridgeID   string                     `json:"bridge_id,omitempty"`
	Host       HostHealth                 `json:"host"`
	Subsystems map[string]SubsystemHealth `json:"subsystems,omitempty"`
	Inventory  Inventory                  `json:"inventory"`
	Errors     []ErrorFingerprint         `json:"errors,omitempty"`
}

// HostHealth is appliance self-health.
type HostHealth struct {
	Status            string         `json:"status"` // operational, degraded, down
	NTPOK             bool           `json:"ntp_ok"`
	NTPFailureSeconds int64          `json:"ntp_failure_seconds,omitempty"`
	Build             BuildInfo      `json:"build"`
	Resources         *HostResources `json:"resources,omitempty"`
}

// BuildInfo is always sent with health.
type BuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Channel string `json:"channel,omitempty"`
}

// HostResources is optional host resource snapshot.
type HostResources struct {
	MemAvailableMB int    `json:"mem_available_mb,omitempty"`
	QueuePath      string `json:"queue_path,omitempty"`
	QueueDepth     int    `json:"queue_depth,omitempty"`
}

// SubsystemHealth is one nested subsystem block.
type SubsystemHealth struct {
	Status string                 `json:"status"`
	Detail map[string]interface{} `json:"detail,omitempty"`
}

// Inventory lists configured sources for ops discovery.
type Inventory struct {
	Cameras  []InventoryCamera  `json:"cameras,omitempty"`
	Stations []InventoryStation `json:"stations,omitempty"`
}

// InventoryCamera is a camera advertised in health.
type InventoryCamera struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	EnabledOnBridge bool   `json:"enabled_on_bridge"`
}

// InventoryStation is a weather station advertised in health.
type InventoryStation struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Type            string `json:"type"`
	EnabledOnBridge bool   `json:"enabled_on_bridge"`
}

// ErrorFingerprint aggregates repeated errors since the last successful heartbeat.
type ErrorFingerprint struct {
	Fingerprint string `json:"fingerprint"`
	Count       int    `json:"count"`
	LastMessage string `json:"last_message,omitempty"`
	Subsystem   string `json:"subsystem,omitempty"`
}

// WeatherRequest is POST /v1/bridge/weather.
// Canonical Davis (and future LAN) correctness is provider_meta.raw.
// Sample is omitted on the wire (aviationwx#274); core must accept raw-only.
type WeatherRequest struct {
	ObservedAt   time.Time              `json:"observed_at"`
	BridgeID     string                 `json:"bridge_id,omitempty"`
	SourceID     string                 `json:"source_id"`
	Provider     string                 `json:"provider"`
	Sample       *WeatherSample         `json:"sample,omitempty"` // deprecated; do not send from bridge
	ProviderMeta map[string]interface{} `json:"provider_meta,omitempty"`
}

// WeatherSample is a legacy thin canonical view (°C, kt, inHg).
// Bridge no longer populates this; retained for decoding older fixtures/tests.
type WeatherSample struct {
	TempC        *float64 `json:"temp_c,omitempty"`
	HumidityPct  *float64 `json:"humidity_pct,omitempty"`
	WindSpeedKt  *float64 `json:"wind_speed_kt,omitempty"`
	WindGustKt   *float64 `json:"wind_gust_kt,omitempty"`
	WindDirDeg   *float64 `json:"wind_dir_deg,omitempty"`
	PressureInHg *float64 `json:"pressure_inhg,omitempty"`
	RainIn       *float64 `json:"rain_in,omitempty"`
}

// Status values for host/subsystem health.
const (
	StatusOperational = "operational"
	StatusDegraded    = "degraded"
	StatusDown        = "down"
	StatusMaintenance = "maintenance"
)
