package station

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alexwitherspoon/AviationWX.org-Bridge/internal/config"
)

const (
	davisDefaultHTTPPort = "80"
	davisPath            = "/v1/current_conditions"

	davisStructISS      = 1
	davisStructLeafSoil = 2
	davisStructTempHum  = 4
)

// Davis polls WeatherLink Live Local API over HTTP.
type Davis struct {
	client *http.Client
}

// NewDavis returns a Davis WLL provider.
func NewDavis() *Davis {
	return &Davis{
		client: NewLANClient(LANPollTimeout),
	}
}

// Poll implements Provider.
func (d *Davis) Poll(ctx context.Context, cfg config.Station) (*Observation, error) {
	data, rawData, err := d.fetch(ctx, cfg.Host)
	if err != nil {
		return nil, err
	}
	return buildDavisObservation(cfg, data, rawData)
}

type davisEnvelope struct {
	Data  *davisData `json:"data"`
	Error *string    `json:"error"`
}

type davisData struct {
	DID        string            `json:"did"`
	TS         int64             `json:"ts"`
	Conditions []json.RawMessage `json:"conditions"`
}

type davisConditionHead struct {
	DataStructureType int `json:"data_structure_type"`
	Txid              int `json:"txid"`
	LSID              int `json:"lsid"`
}

type davisISS struct {
	DataStructureType int      `json:"data_structure_type"`
	Txid              int      `json:"txid"`
	Temp              *float64 `json:"temp"`
	Hum               *float64 `json:"hum"`
	RXState           *int     `json:"rx_state"`
}

func (d *Davis) fetch(ctx context.Context, host string) (*davisData, json.RawMessage, error) {
	u, err := davisCurrentConditionsURL(host)
	if err != nil {
		return nil, nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("davis fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("davis read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("davis HTTP %d", resp.StatusCode)
	}

	var env davisEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, nil, fmt.Errorf("davis decode: %w", err)
	}
	if env.Error != nil && *env.Error != "" {
		return nil, nil, fmt.Errorf("davis API error: %s", *env.Error)
	}
	if env.Data == nil {
		return nil, nil, fmt.Errorf("davis response missing data")
	}

	// Preserve station-native JSON for core adapters (aviationwx#274 pivot).
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, nil, fmt.Errorf("davis raw envelope: %w", err)
	}
	rawData := envelope["data"]
	if len(rawData) == 0 {
		rawData, err = json.Marshal(env.Data)
		if err != nil {
			return nil, nil, fmt.Errorf("davis rematerialize raw: %w", err)
		}
	}
	return env.Data, rawData, nil
}

func davisCurrentConditionsURL(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("davis host is empty")
	}
	// Allow paste of full URL or bare host[:port] / [IPv6]:port.
	if strings.Contains(host, "://") {
		u, err := url.Parse(host)
		if err != nil {
			return "", fmt.Errorf("davis host URL: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", fmt.Errorf("davis host scheme must be http or https")
		}
		if u.Host == "" {
			return "", fmt.Errorf("davis host URL missing host")
		}
		u.Path = davisPath
		u.RawQuery = ""
		u.Fragment = ""
		return u.String(), nil
	}
	if strings.ContainsAny(host, "/?#") {
		return "", fmt.Errorf("davis host must be hostname or IP[:port]")
	}
	if h, p, err := net.SplitHostPort(host); err == nil {
		return "http://" + net.JoinHostPort(h, p) + davisPath, nil
	}
	// Bare hostname, IPv4, or IPv6. Strip optional brackets so JoinHostPort
	// does not produce [[addr]]:port.
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return "http://" + net.JoinHostPort(host, davisDefaultHTTPPort) + davisPath, nil
}

func buildDavisObservation(cfg config.Station, data *davisData, rawData json.RawMessage) (*Observation, error) {
	var rawObj interface{}
	if len(rawData) > 0 {
		if err := json.Unmarshal(rawData, &rawObj); err != nil {
			return nil, fmt.Errorf("davis raw decode: %w", err)
		}
	}

	obs := &Observation{
		SourceID: cfg.ID,
		Provider: ProviderDavisWeatherLinkLive,
		DID:      data.DID,
		ProviderMeta: map[string]interface{}{
			"api":  "weatherlink_live_local_v1",
			"path": davisPath,
			"did":  data.DID,
			"raw":  rawObj,
		},
	}
	if data.TS > 0 {
		obs.ObservedAt = time.Unix(data.TS, 0).UTC()
	}
	// Missing ts: leave ObservedAt zero. Caller must skip weather POST
	// (bridge-clock fallback is unsafe for staleness).

	var issByTxid = map[int]*davisISS{}

	for _, raw := range data.Conditions {
		var head davisConditionHead
		if err := json.Unmarshal(raw, &head); err != nil {
			continue
		}
		switch head.DataStructureType {
		case davisStructISS:
			var iss davisISS
			if err := json.Unmarshal(raw, &iss); err != nil {
				continue
			}
			issByTxid[iss.Txid] = &iss
			info := TransmitterInfo{
				Txid:              iss.Txid,
				DataStructureType: davisStructISS,
				TempF:             iss.Temp,
				HumidityPct:       iss.Hum,
				RXState:           iss.RXState,
			}
			obs.Transmitters = append(obs.Transmitters, info)
		case davisStructLeafSoil, davisStructTempHum:
			if head.Txid > 0 {
				obs.Transmitters = append(obs.Transmitters, TransmitterInfo{
					Txid:              head.Txid,
					DataStructureType: head.DataStructureType,
				})
			}
		}
	}

	if cfg.Txid == nil {
		return obs, nil
	}
	txid := *cfg.Txid
	obs.ProviderMeta["txid"] = txid

	if _, ok := issByTxid[txid]; !ok {
		return obs, fmt.Errorf("davis txid %d not present in current_conditions", txid)
	}
	return obs, nil
}
