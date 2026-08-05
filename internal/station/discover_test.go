package station

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEnumerateIPv4HostsBounded(t *testing.T) {
	_, ipnet, err := net.ParseCIDR("192.168.1.10/24")
	if err != nil {
		t.Fatal(err)
	}
	hosts := enumerateIPv4Hosts(ipnet, 5)
	if len(hosts) != 5 {
		t.Fatalf("len = %d", len(hosts))
	}
	if hosts[0] != "192.168.1.1" {
		t.Fatalf("first = %s", hosts[0])
	}
}

func TestProbeTargetsFromSubnet(t *testing.T) {
	hosts, err := probeTargetsFromSubnet("192.168.1.0/24")
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 254 {
		t.Fatalf("len = %d", len(hosts))
	}
	if hosts[0] != "192.168.1.1" || hosts[253] != "192.168.1.254" {
		t.Fatalf("range = %s..%s", hosts[0], hosts[253])
	}

	if _, err := probeTargetsFromSubnet("10.0.0.0/16"); err == nil {
		t.Fatal("expected /16 rejected")
	}
	if _, err := probeTargetsFromSubnet("not-a-cidr"); err == nil {
		t.Fatal("expected invalid rejected")
	}
	hosts, err = probeTargetsFromSubnet("")
	if err != nil || hosts != nil {
		t.Fatalf("empty: hosts=%v err=%v", hosts, err)
	}
}

func TestPreferDNSNameLeavesIPWhenNoPTR(t *testing.T) {
	c := DiscoverCandidate{Host: "127.0.0.1", Method: "http_probe"}
	preferDNSName(context.Background(), &c)
	if c.Host == "" {
		t.Fatal("host cleared")
	}
}

func TestProbeDavisURLAcceptsFixture(t *testing.T) {
	fixture := []byte(`{"data":{"did":"001D0A700002","ts":1,"conditions":[]},"error":null}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != davisPath {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	c, ok := probeDavisURL(context.Background(), srv.Client(), srv.URL+davisPath)
	if !ok {
		t.Fatal("expected ok")
	}
	if c.DID != "001D0A700002" {
		t.Fatalf("did = %s", c.DID)
	}
}

func TestDiscoverStationsUnsupportedType(t *testing.T) {
	_, err := DiscoverStations(context.Background(), DiscoverOptions{Type: "ecowitt"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDiscoverStationsStreamEmitsPhases(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var types []string
	var notes []string
	err := DiscoverStationsStream(ctx, DiscoverOptions{Type: ProviderDavisWeatherLinkLive}, func(ev DiscoverEvent) {
		types = append(types, ev.Type)
		if ev.Type == "note" {
			notes = append(notes, ev.Message)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(types) == 0 || types[len(types)-1] != "done" {
		t.Fatalf("events = %v", types)
	}
	joined := strings.Join(notes, " ")
	if !strings.Contains(joined, "CIDR") {
		t.Fatalf("expected CIDR guidance note, got %v", notes)
	}
}

func TestDiscoverStationsStreamEmitsProgress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var progress []DiscoverEvent
	err := DiscoverStationsStream(ctx, DiscoverOptions{
		Type:   ProviderDavisWeatherLinkLive,
		Subnet: "127.0.0.0/30",
	}, func(ev DiscoverEvent) {
		if ev.Type == "progress" {
			progress = append(progress, ev)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(progress) == 0 {
		t.Fatal("expected progress events")
	}
	if progress[0].Total != 2 {
		t.Fatalf("total = %d", progress[0].Total)
	}
	last := progress[len(progress)-1]
	if last.Done != last.Total || last.Total != 2 {
		t.Fatalf("last progress = %+v", last)
	}
}

func TestDiscoverStationsNotesWhenEmptySubnet(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := DiscoverStations(ctx, DiscoverOptions{Type: ProviderDavisWeatherLinkLive})
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	if len(res.Candidates) == 0 && len(res.Notes) == 0 {
		t.Fatal("expected notes when no candidates")
	}
}
