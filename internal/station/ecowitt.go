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
	ecowittDefaultHTTPPort = "80"
	ecowittLivedataPath    = "/get_livedata_info"
	ecowittVersionPath     = "/get_version"
	ecowittAPIMeta         = "ecowitt_gateway_http_v1"
	ecowittItemTime        = "0x18"
)

// Ecowitt polls Ecowitt / Ambient Fine Offset gateways over LAN HTTP.
type Ecowitt struct {
	client *http.Client
}

// NewEcowitt returns an Ecowitt gateway provider.
func NewEcowitt() *Ecowitt {
	return &Ecowitt{client: NewLANClient(LANPollTimeout)}
}

// Poll implements Provider.
func (e *Ecowitt) Poll(ctx context.Context, cfg config.Station) (*Observation, error) {
	raw, err := e.fetchLivedata(ctx, cfg.Host)
	if err != nil {
		return nil, err
	}
	return buildEcowittObservation(cfg, raw)
}

func (e *Ecowitt) fetchLivedata(ctx context.Context, host string) (json.RawMessage, error) {
	u, err := ecowittURL(host, ecowittLivedataPath)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ecowitt fetch: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("ecowitt read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ecowitt HTTP %d", resp.StatusCode)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("ecowitt response is not JSON")
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return nil, fmt.Errorf("ecowitt decode: %w", err)
	}
	if _, ok := top["common_list"]; !ok {
		if _, ok := top["wh25"]; !ok {
			return nil, fmt.Errorf("ecowitt response missing common_list/wh25")
		}
	}
	return json.RawMessage(body), nil
}

func ecowittURL(host, path string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("ecowitt host is empty")
	}
	if strings.Contains(host, "://") {
		u, err := url.Parse(host)
		if err != nil {
			return "", fmt.Errorf("ecowitt host URL: %w", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", fmt.Errorf("ecowitt host scheme must be http or https")
		}
		if u.Host == "" {
			return "", fmt.Errorf("ecowitt host URL missing host")
		}
		u.Path = path
		u.RawQuery = ""
		u.Fragment = ""
		return u.String(), nil
	}
	if strings.ContainsAny(host, "/?#") {
		return "", fmt.Errorf("ecowitt host must be hostname or IP[:port]")
	}
	if h, p, err := net.SplitHostPort(host); err == nil {
		return "http://" + net.JoinHostPort(h, p) + path, nil
	}
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return "http://" + net.JoinHostPort(host, ecowittDefaultHTTPPort) + path, nil
}

func buildEcowittObservation(cfg config.Station, raw json.RawMessage) (*Observation, error) {
	var rawObj interface{}
	if err := json.Unmarshal(raw, &rawObj); err != nil {
		return nil, fmt.Errorf("ecowitt raw decode: %w", err)
	}
	obs := &Observation{
		SourceID: cfg.ID,
		Provider: ProviderEcowittGateway,
		ProviderMeta: map[string]interface{}{
			"api":  ecowittAPIMeta,
			"path": ecowittLivedataPath,
			"raw":  rawObj,
		},
	}
	if t, ok := parseEcowittObservedAt(raw); ok {
		obs.ObservedAt = t
	}
	// Missing station time: leave ObservedAt zero so Manager skips weather POST.
	return obs, nil
}

type ecowittListItem struct {
	ID  string `json:"id"`
	Val string `json:"val"`
}

func parseEcowittObservedAt(raw json.RawMessage) (time.Time, bool) {
	var top struct {
		CommonList []ecowittListItem `json:"common_list"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		return time.Time{}, false
	}
	for _, item := range top.CommonList {
		id := strings.ToLower(strings.TrimSpace(item.ID))
		if id != ecowittItemTime && id != "24" {
			continue
		}
		if t, ok := parseEcowittTimeVal(item.Val); ok {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseEcowittTimeVal(val string) (time.Time, bool) {
	val = strings.TrimSpace(val)
	if val == "" {
		return time.Time{}, false
	}
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006/01/02 15:04:05",
		time.RFC3339,
	}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, val, time.UTC); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
