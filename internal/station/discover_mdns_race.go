//go:build race

package station

import "context"

// discoverDavisMDNS is a no-op under -race.
// github.com/hashicorp/mdns races with itself when consumers read ServiceEntry
// fields; HTTP probe discovery remains covered by race tests.
func discoverDavisMDNS(ctx context.Context, onFound func(DiscoverCandidate)) {
	<-ctx.Done()
}
