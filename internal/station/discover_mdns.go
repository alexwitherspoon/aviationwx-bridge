//go:build !race

package station

import (
	"context"
	"net"
	"strings"
	"sync"

	"github.com/hashicorp/mdns"
)

// discoverDavisMDNS browses for WeatherLink Live via mDNS.
// Entries are snapshotted into DiscoverCandidate immediately so we never retain
// library pointers (hashicorp/mdns mutates ServiceEntry after send).
func discoverDavisMDNS(ctx context.Context, onFound func(DiscoverCandidate)) {
	rawCh := make(chan *mdns.ServiceEntry, 8)
	candCh := make(chan DiscoverCandidate, 8)

	var queryWG sync.WaitGroup
	queryWG.Add(1)
	go func() {
		defer queryWG.Done()
		params := mdns.DefaultParams(davisMDNSService)
		params.Entries = rawCh
		params.Timeout = discoverMDNSTimeout
		params.DisableIPv6 = true
		_ = mdns.Query(params)
		close(rawCh)
	}()

	var copyWG sync.WaitGroup
	copyWG.Add(1)
	go func() {
		defer copyWG.Done()
		for e := range rawCh {
			if c, ok := snapshotMDNSEntry(e); ok {
				select {
				case candCh <- c:
				case <-ctx.Done():
					// Drop remaining; Query continues until timeout.
				}
			}
		}
		close(candCh)
	}()

	defer func() {
		// Always wait for Query so it cannot block forever on a full rawCh.
		for range candCh {
		}
		copyWG.Wait()
		queryWG.Wait()
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case c, ok := <-candCh:
			if !ok {
				return
			}
			if onFound != nil {
				onFound(c)
			}
		}
	}
}

// snapshotMDNSEntry copies scalar fields from a library entry into our type.
// Callers must not retain e after return.
func snapshotMDNSEntry(e *mdns.ServiceEntry) (DiscoverCandidate, bool) {
	if e == nil {
		return DiscoverCandidate{}, false
	}
	// Copy IP bytes before String() so a concurrent mutation is less likely to
	// tear the net.IP header (still best-effort vs library races).
	ip := ""
	if e.AddrV4 != nil {
		ip = append(net.IP(nil), e.AddrV4...).String()
	} else if len(e.AddrV6) > 0 {
		ip = append(net.IP(nil), e.AddrV6...).String()
	}
	host := strings.TrimSuffix(e.Host, ".")
	name := e.Name
	port := e.Port
	if host == "" {
		host = ip
	}
	if host == "" {
		return DiscoverCandidate{}, false
	}
	if port == 0 {
		port = 80
	}
	return DiscoverCandidate{
		Host:   host,
		IP:     ip,
		Port:   port,
		Name:   name,
		Method: "mdns",
	}, true
}
