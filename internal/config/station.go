package config

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Station type identifiers (epic #102 / #103).
const (
	StationTypeDavisWeatherLinkLive = "davis_weatherlink_live"
	StationTypeHTTPInterceptor      = "http_interceptor"
)

// DefaultDavisPollIntervalSeconds is the Davis Local API current_conditions floor.
const DefaultDavisPollIntervalSeconds = 10

// DefaultHTTPInterceptorListenAddr is the shared ingest bind for interceptor stations.
const DefaultHTTPInterceptorListenAddr = "0.0.0.0:8090"

// DefaultHTTPInterceptorListenPath is the classic Weather Underground update path.
const DefaultHTTPInterceptorListenPath = "/weatherstation/updateweatherstation.php"

// HTTPInterceptorDialectWunderground is the first supported interceptor dialect.
const HTTPInterceptorDialectWunderground = "wunderground"

// Station is a LAN weather station configuration (file-per-station under stations/).
// Wind is assumed true-north install; there is no magnetic mode.
type Station struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`

	// Davis (poll) fields.
	Host                string `json:"host,omitempty"`
	PollIntervalSeconds int    `json:"poll_interval_seconds,omitempty"`
	Txid                *int   `json:"txid,omitempty"` // required for Davis once user picks

	// HTTP interceptor (listen) fields.
	ListenAddr string `json:"listen_addr,omitempty"`
	ListenPath string `json:"listen_path,omitempty"`
	Dialect    string `json:"dialect,omitempty"`
}

// UnmarshalJSON defaults enabled to true when the field is omitted (CONFIG_SCHEMA).
func (st *Station) UnmarshalJSON(data []byte) error {
	aux := struct {
		ID                  string `json:"id"`
		Name                string `json:"name"`
		Type                string `json:"type"`
		Enabled             *bool  `json:"enabled"`
		Host                string `json:"host,omitempty"`
		PollIntervalSeconds int    `json:"poll_interval_seconds,omitempty"`
		Txid                *int   `json:"txid,omitempty"`
		ListenAddr          string `json:"listen_addr,omitempty"`
		ListenPath          string `json:"listen_path,omitempty"`
		Dialect             string `json:"dialect,omitempty"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	st.ID = aux.ID
	st.Name = aux.Name
	st.Type = aux.Type
	st.Host = aux.Host
	st.PollIntervalSeconds = aux.PollIntervalSeconds
	st.Txid = aux.Txid
	st.ListenAddr = aux.ListenAddr
	st.ListenPath = aux.ListenPath
	st.Dialect = aux.Dialect
	if aux.Enabled == nil {
		st.Enabled = true
	} else {
		st.Enabled = *aux.Enabled
	}
	return nil
}

// NormalizeStationDefaults fills defaults for known station types.
func NormalizeStationDefaults(st *Station) {
	if st == nil {
		return
	}
	switch st.Type {
	case StationTypeDavisWeatherLinkLive:
		if st.PollIntervalSeconds <= 0 {
			st.PollIntervalSeconds = DefaultDavisPollIntervalSeconds
		}
	case StationTypeHTTPInterceptor:
		if strings.TrimSpace(st.ListenAddr) == "" {
			st.ListenAddr = DefaultHTTPInterceptorListenAddr
		}
		if strings.TrimSpace(st.ListenPath) == "" {
			st.ListenPath = DefaultHTTPInterceptorListenPath
		}
		if strings.TrimSpace(st.Dialect) == "" {
			st.Dialect = HTTPInterceptorDialectWunderground
		}
	}
}

// ValidateStation reports whether a station config is persistable.
func ValidateStation(st Station) error {
	if err := ValidateStationID(st.ID); err != nil {
		return err
	}
	if strings.TrimSpace(st.Name) == "" {
		return fmt.Errorf("station name is required")
	}
	switch st.Type {
	case StationTypeDavisWeatherLinkLive:
		return validateDavisStation(st)
	case StationTypeHTTPInterceptor:
		return validateHTTPInterceptorStation(st)
	case "":
		return fmt.Errorf("station type is required")
	default:
		return fmt.Errorf("unsupported station type %q", st.Type)
	}
}

func validateDavisStation(st Station) error {
	if strings.TrimSpace(st.Host) == "" {
		return fmt.Errorf("station host is required")
	}
	if st.PollIntervalSeconds > 0 && st.PollIntervalSeconds < DefaultDavisPollIntervalSeconds {
		return fmt.Errorf("davis poll_interval_seconds must be >= %d", DefaultDavisPollIntervalSeconds)
	}
	return nil
}

func validateHTTPInterceptorStation(st Station) error {
	addr := strings.TrimSpace(st.ListenAddr)
	if addr == "" {
		addr = DefaultHTTPInterceptorListenAddr
	}
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("listen_addr must be host:port (got %q)", st.ListenAddr)
	}
	if portStr == "" {
		return fmt.Errorf("listen_addr missing port")
	}
	if host == "" {
		return fmt.Errorf("listen_addr missing host (use 0.0.0.0 or 127.0.0.1)")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return fmt.Errorf("listen_addr port must be an integer (got %q)", portStr)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("listen_addr port must be 1-65535 (got %d)", port)
	}
	path := strings.TrimSpace(st.ListenPath)
	if path == "" {
		path = DefaultHTTPInterceptorListenPath
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("listen_path must start with /")
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("listen_path must not contain path traversal segments")
	}
	dialect := strings.TrimSpace(st.Dialect)
	if dialect == "" {
		dialect = HTTPInterceptorDialectWunderground
	}
	if dialect != HTTPInterceptorDialectWunderground {
		return fmt.Errorf("unsupported interceptor dialect %q (supported: %s)", dialect, HTTPInterceptorDialectWunderground)
	}
	return nil
}
