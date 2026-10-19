package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// Station type identifiers (epic #102 / #103).
const (
	StationTypeDavisWeatherLinkLive = "davis_weatherlink_live"
	StationTypeHTTPInterceptor      = "http_interceptor"
	StationTypeEcowittGateway       = "ecowitt_gateway"
	StationTypeMQTT                 = "mqtt"
)

// DefaultDavisPollIntervalSeconds is the Davis Local API current_conditions floor.
const DefaultDavisPollIntervalSeconds = 10

// DefaultEcowittPollIntervalSeconds is the Ecowitt get_livedata_info poll floor.
const DefaultEcowittPollIntervalSeconds = 10

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

	// MQTT subscriber fields.
	BrokerURL string `json:"broker_url,omitempty"` // tcp://host[:port]; default port 1883
	Topic     string `json:"topic,omitempty"`
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
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
		BrokerURL           string `json:"broker_url,omitempty"`
		Topic               string `json:"topic,omitempty"`
		Username            string `json:"username,omitempty"`
		Password            string `json:"password,omitempty"`
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
	st.BrokerURL = aux.BrokerURL
	st.Topic = aux.Topic
	st.Username = aux.Username
	st.Password = aux.Password
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
	case StationTypeEcowittGateway:
		if st.PollIntervalSeconds <= 0 {
			st.PollIntervalSeconds = DefaultEcowittPollIntervalSeconds
		}
	case StationTypeHTTPInterceptor:
		st.ListenAddr = strings.TrimSpace(st.ListenAddr)
		st.ListenPath = strings.TrimSpace(st.ListenPath)
		st.Dialect = strings.TrimSpace(st.Dialect)
		if st.ListenAddr == "" {
			st.ListenAddr = DefaultHTTPInterceptorListenAddr
		}
		if st.ListenPath == "" {
			st.ListenPath = DefaultHTTPInterceptorListenPath
		}
		if st.Dialect == "" {
			st.Dialect = HTTPInterceptorDialectWunderground
		}
	case StationTypeMQTT:
		st.BrokerURL = normalizeMQTTBrokerURL(strings.TrimSpace(st.BrokerURL))
		st.Topic = strings.TrimSpace(st.Topic)
		st.Username = strings.TrimSpace(st.Username)
		// Password is left untrimmed: it is an opaque secret.
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
	case StationTypeEcowittGateway:
		return validateEcowittStation(st)
	case StationTypeHTTPInterceptor:
		return validateHTTPInterceptorStation(st)
	case StationTypeMQTT:
		return validateMQTTStation(st)
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

func validateEcowittStation(st Station) error {
	if strings.TrimSpace(st.Host) == "" {
		return fmt.Errorf("station host is required")
	}
	if st.PollIntervalSeconds > 0 && st.PollIntervalSeconds < DefaultEcowittPollIntervalSeconds {
		return fmt.Errorf("ecowitt poll_interval_seconds must be >= %d", DefaultEcowittPollIntervalSeconds)
	}
	return nil
}

func validateMQTTStation(st Station) error {
	broker := strings.TrimSpace(st.BrokerURL)
	if broker == "" {
		return fmt.Errorf("broker_url is required")
	}
	u, err := url.Parse(broker)
	if err != nil || u.Scheme != "tcp" || u.Host == "" {
		return fmt.Errorf("broker_url must be a tcp:// URL with a host (got %q)", redactURLUserInfo(st.BrokerURL))
	}
	if u.User != nil {
		return fmt.Errorf("broker_url must not contain credentials; use the username and password fields")
	}
	if strings.TrimSpace(st.Topic) == "" {
		return fmt.Errorf("topic is required")
	}
	return nil
}

// normalizeMQTTBrokerURL appends the default port 1883 to a portless tcp://
// URL. paho dials the URL host verbatim, so a missing port would fail to
// connect.
func normalizeMQTTBrokerURL(raw string) string {
	if !strings.HasPrefix(raw, "tcp://") {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	if u.Port() == "" {
		u.Host = net.JoinHostPort(u.Hostname(), "1883")
	}
	return u.String()
}

// redactURLUserInfo strips userinfo from a URL so validation errors never
// echo a credential-bearing broker_url. An unparseable URL is not echoed at
// all: it could still carry embedded credentials.
func redactURLUserInfo(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(unparseable url)"
	}
	u.User = nil
	return u.String()
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
