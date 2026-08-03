package station

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	davisMDNSService    = "_weatherlinklive._tcp"
	discoverMDNSTimeout = 4 * time.Second
	discoverProbeBudget = 12 * time.Second
	probeHTTPTimeout    = 400 * time.Millisecond
	probeMaxConcurrency = 8
	probeMaxHostsPerNet = 254
	ptrLookupTimeout    = 800 * time.Millisecond
	probeMinPrefixLen   = 24
	probeMaxPrefixLen   = 30
)

// DiscoverCandidate is a LAN device suggested for the Host field.
// Prefer Host as a DNS/mDNS name; IP holds the numeric address when Host is a name.
type DiscoverCandidate struct {
	Host   string `json:"host"`
	IP     string `json:"ip,omitempty"`
	Port   int    `json:"port,omitempty"`
	Name   string `json:"name,omitempty"`
	Method string `json:"method"` // mdns | http_probe
	DID    string `json:"did,omitempty"`
}

// DiscoverEvent is one progress update for user-initiated discovery (SSE).
type DiscoverEvent struct {
	Type      string             `json:"type"` // phase | progress | candidate | note | done | error
	Phase     string             `json:"phase,omitempty"`
	Message   string             `json:"message,omitempty"`
	Candidate *DiscoverCandidate `json:"candidate,omitempty"`
	Done      int                `json:"done,omitempty"`  // progress: hosts completed
	Total     int                `json:"total,omitempty"` // progress: hosts to probe
}

// DiscoverEmit receives discovery progress. Callers must not block long (SSE flush).
type DiscoverEmit func(DiscoverEvent)

// DiscoverOptions configures user-initiated LAN discovery.
type DiscoverOptions struct {
	Type   string // station type / provider; default davis_weatherlink_live
	Subnet string // IPv4 CIDR for HTTP probe (required for scan; /24-/30)
}

// DiscoverStationsStream runs user-initiated LAN discovery and emits progress.
// Only call from an explicit console action (mDNS + rate-limited HTTP probe of Subnet).
func DiscoverStationsStream(ctx context.Context, opts DiscoverOptions, emit DiscoverEmit) error {
	if emit == nil {
		return fmt.Errorf("discover emit is required")
	}
	if opts.Type == "" {
		opts.Type = ProviderDavisWeatherLinkLive
	}
	switch opts.Type {
	case ProviderDavisWeatherLinkLive:
		return discoverDavisStream(ctx, opts.Subnet, emit)
	default:
		return fmt.Errorf("discovery not supported for station type %q", opts.Type)
	}
}

// DiscoverStations collects a full result (tests / non-streaming callers).
func DiscoverStations(ctx context.Context, opts DiscoverOptions) (*DiscoverResult, error) {
	out := &DiscoverResult{}
	err := DiscoverStationsStream(ctx, opts, func(ev DiscoverEvent) {
		switch ev.Type {
		case "candidate":
			if ev.Candidate != nil {
				out.Candidates = append(out.Candidates, *ev.Candidate)
			}
		case "note":
			if ev.Message != "" {
				out.Notes = append(out.Notes, ev.Message)
			}
		}
	})
	return out, err
}

// DiscoverResult is a non-streaming discovery snapshot.
type DiscoverResult struct {
	Candidates []DiscoverCandidate `json:"candidates"`
	Notes      []string            `json:"notes,omitempty"`
}

func discoverDavisStream(ctx context.Context, subnet string, emit DiscoverEmit) error {
	var (
		mu     sync.Mutex
		emitMu sync.Mutex
		seen   = map[string]struct{}{}
	)
	safeEmit := func(ev DiscoverEvent) {
		emitMu.Lock()
		defer emitMu.Unlock()
		emit(ev)
	}
	emitCandidate := func(c DiscoverCandidate) {
		c = normalizeCandidate(c)
		key := candidateKey(c)
		if key == "" {
			return
		}
		mu.Lock()
		if _, ok := seen[key]; ok {
			mu.Unlock()
			return
		}
		seen[key] = struct{}{}
		mu.Unlock()

		preferDNSName(ctx, &c)
		cp := c
		safeEmit(DiscoverEvent{Type: "candidate", Candidate: &cp})
	}

	safeEmit(DiscoverEvent{Type: "phase", Phase: "mdns", Message: "Browsing mDNS for WeatherLink Live..."})
	mdnsCtx, cancel := context.WithTimeout(ctx, discoverMDNSTimeout)
	var mdnsCountMu sync.Mutex
	mdnsCount := 0
	discoverDavisMDNS(mdnsCtx, func(c DiscoverCandidate) {
		mdnsCountMu.Lock()
		mdnsCount++
		mdnsCountMu.Unlock()
		emitCandidate(c)
	})
	cancel()

	mdnsCountMu.Lock()
	foundMDNS := mdnsCount
	mdnsCountMu.Unlock()
	if foundMDNS == 0 {
		safeEmit(DiscoverEvent{Type: "note", Message: "No WeatherLink Live devices via mDNS. Multicast often fails across Docker bridge networking."})
	}

	targets, err := probeTargetsFromSubnet(subnet)
	if err != nil {
		safeEmit(DiscoverEvent{Type: "note", Message: err.Error()})
	} else if len(targets) == 0 {
		safeEmit(DiscoverEvent{Type: "note", Message: "Enter a network CIDR (e.g. 192.168.1.0/24) to scan for WeatherLink Live over HTTP."})
	} else {
		safeEmit(DiscoverEvent{Type: "phase", Phase: "http_probe", Message: fmt.Sprintf("Probing %d host(s) on %s for WeatherLink Live HTTP API...", len(targets), strings.TrimSpace(subnet))})
		safeEmit(DiscoverEvent{Type: "progress", Done: 0, Total: len(targets), Message: fmt.Sprintf("0/%d", len(targets))})
		probeCtx, probeCancel := context.WithTimeout(ctx, discoverProbeBudget)
		discoverDavisHTTPProbe(probeCtx, targets, emitCandidate, func(done, total int) {
			safeEmit(DiscoverEvent{
				Type:    "progress",
				Done:    done,
				Total:   total,
				Message: fmt.Sprintf("%d/%d", done, total),
			})
		})
		probeCancel()
	}

	mu.Lock()
	count := len(seen)
	mu.Unlock()
	if count == 0 {
		safeEmit(DiscoverEvent{Type: "note", Message: "No candidates found. Enter the station IP or hostname manually, then use Test poll."})
	}
	safeEmit(DiscoverEvent{Type: "done", Message: fmt.Sprintf("%d candidate(s)", count)})
	return nil
}

func normalizeCandidate(c DiscoverCandidate) DiscoverCandidate {
	c.Host = strings.TrimSpace(c.Host)
	c.IP = strings.TrimSpace(c.IP)
	if c.Port == 0 {
		c.Port = 80
	}
	if c.IP != "" && c.IP == c.Host {
		c.IP = ""
	}
	return c
}

func candidateKey(c DiscoverCandidate) string {
	key := c.IP
	if key == "" {
		key = c.Host
	}
	if key == "" {
		return ""
	}
	if c.Port > 0 && c.Port != 80 {
		return fmt.Sprintf("%s:%d", key, c.Port)
	}
	return key
}

func preferDNSName(ctx context.Context, c *DiscoverCandidate) {
	if c == nil {
		return
	}
	ipStr := c.IP
	if ipStr == "" {
		if net.ParseIP(c.Host) == nil {
			return
		}
		ipStr = c.Host
	}
	lookupCtx, cancel := context.WithTimeout(ctx, ptrLookupTimeout)
	defer cancel()
	names, err := net.DefaultResolver.LookupAddr(lookupCtx, ipStr)
	if err != nil || len(names) == 0 {
		return
	}
	name := strings.TrimSuffix(strings.TrimSpace(names[0]), ".")
	if name == "" || name == ipStr {
		return
	}
	c.IP = ipStr
	c.Host = name
}

func discoverDavisHTTPProbe(ctx context.Context, targets []string, onFound func(DiscoverCandidate), onProgress func(done, total int)) {
	if len(targets) == 0 {
		return
	}
	total := len(targets)
	client := &http.Client{Timeout: probeHTTPTimeout}
	sem := make(chan struct{}, probeMaxConcurrency)
	var (
		wg       sync.WaitGroup
		doneMu   sync.Mutex
		done     int
		lastEmit time.Time
	)
	// Throttle progress SSE so a /24 does not flood the console (always emit first and last).
	report := func() {
		if onProgress == nil {
			return
		}
		doneMu.Lock()
		n := done
		now := time.Now()
		emit := n == total || n == 1 || now.Sub(lastEmit) >= 100*time.Millisecond
		if emit {
			lastEmit = now
		}
		doneMu.Unlock()
		if emit {
			onProgress(n, total)
		}
	}
	for _, ip := range targets {
		if ctx.Err() != nil {
			break
		}
		ip := ip
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				doneMu.Lock()
				done++
				doneMu.Unlock()
				report()
			}()
			if ctx.Err() != nil {
				return
			}
			c, ok := probeDavisHost(ctx, client, ip)
			if ok {
				onFound(c)
			}
		}()
	}
	wg.Wait()
	if onProgress != nil {
		doneMu.Lock()
		n := done
		doneMu.Unlock()
		onProgress(n, total)
	}
}

func probeDavisHost(ctx context.Context, client *http.Client, ip string) (DiscoverCandidate, bool) {
	c, ok := probeDavisURL(ctx, client, "http://"+ip+davisPath)
	if !ok {
		return DiscoverCandidate{}, false
	}
	c.Host = ip
	c.Port = 80
	c.Method = "http_probe"
	return c, true
}

func probeDavisURL(ctx context.Context, client *http.Client, rawURL string) (DiscoverCandidate, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return DiscoverCandidate{}, false
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return DiscoverCandidate{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return DiscoverCandidate{}, false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return DiscoverCandidate{}, false
	}
	var env struct {
		Data *struct {
			DID string `json:"did"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Data == nil {
		return DiscoverCandidate{}, false
	}
	return DiscoverCandidate{DID: env.Data.DID}, true
}

// probeTargetsFromSubnet parses an operator-supplied IPv4 CIDR (/24-/30) into host IPs.
func probeTargetsFromSubnet(subnet string) ([]string, error) {
	subnet = strings.TrimSpace(subnet)
	if subnet == "" {
		return nil, nil
	}
	_, ipnet, err := net.ParseCIDR(subnet)
	if err != nil {
		return nil, fmt.Errorf("invalid network CIDR %q: use form 192.168.1.0/24", subnet)
	}
	if ipnet.IP.To4() == nil {
		return nil, fmt.Errorf("network CIDR must be IPv4")
	}
	ones, bits := ipnet.Mask.Size()
	if bits != 32 || ones < probeMinPrefixLen || ones > probeMaxPrefixLen {
		return nil, fmt.Errorf("network CIDR prefix must be /%d to /%d (got /%d)", probeMinPrefixLen, probeMaxPrefixLen, ones)
	}
	return enumerateIPv4Hosts(ipnet, probeMaxHostsPerNet), nil
}

func enumerateIPv4Hosts(ipnet *net.IPNet, maxHosts int) []string {
	ip := ipnet.IP.To4()
	if ip == nil {
		return nil
	}
	network := make(net.IP, 4)
	broadcast := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		network[i] = ip[i] & ipnet.Mask[i]
		broadcast[i] = ip[i] | ^ipnet.Mask[i]
	}
	start := ipv4ToUint32(network) + 1
	end := ipv4ToUint32(broadcast)
	if end <= start {
		return nil
	}
	var out []string
	for n := start; n < end && len(out) < maxHosts; n++ {
		out = append(out, uint32ToIPv4(n).String())
	}
	return out
}

func ipv4ToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func uint32ToIPv4(n uint32) net.IP {
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
}
