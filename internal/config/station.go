package config

import (
	"fmt"
	"strings"
)

// Station type identifiers (epic #102 / #103).
const (
	StationTypeDavisWeatherLinkLive = "davis_weatherlink_live"
)

// DefaultDavisPollIntervalSeconds is the Davis Local API current_conditions floor.
const DefaultDavisPollIntervalSeconds = 10

// Station is a LAN weather station configuration (file-per-station under stations/).
// Wind from Davis is assumed true-north install; there is no magnetic mode.
type Station struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`

	Host                string `json:"host,omitempty"`
	PollIntervalSeconds int    `json:"poll_interval_seconds,omitempty"`
	Txid                *int   `json:"txid,omitempty"` // required for Davis once user picks
}

// NormalizeStationDefaults fills defaults for known station types.
func NormalizeStationDefaults(st *Station) {
	if st == nil {
		return
	}
	if st.Type == StationTypeDavisWeatherLinkLive {
		if st.PollIntervalSeconds <= 0 {
			st.PollIntervalSeconds = DefaultDavisPollIntervalSeconds
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
