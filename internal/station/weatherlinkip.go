package station

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

const (
	weatherLinkIPDefaultPort = 22222
	weatherLinkIPAPIMeta     = "vantage_serial_v1"
	weatherLinkIPLoopSize    = 99
	weatherLinkIPTimeSize    = 8
)

// vantageCRCTable is the Davis CRC-CCITT (poly 0x1021) lookup table.
var vantageCRCTable = buildVantageCRCTable()

func buildVantageCRCTable() [256]uint16 {
	var t [256]uint16
	for i := range t {
		crc := uint16(i) << 8
		for j := 0; j < 8; j++ {
			if crc&0x8000 != 0 {
				crc = (crc << 1) ^ 0x1021
			} else {
				crc <<= 1
			}
		}
		t[i] = crc
	}
	return t
}

// vantageCRC16 computes the Davis Vantage 16-bit CRC. A valid packet (data +
// its trailing 2 CRC bytes) yields zero.
func vantageCRC16(data []byte) uint16 {
	var crc uint16
	for _, b := range data {
		crc = (vantageCRCTable[(crc>>8)^uint16(b)] ^ (crc << 8)) & 0xffff
	}
	return crc
}

// WeatherLinkIP polls a legacy Davis IP logger over the Vantage serial
// protocol (raw TCP on port 22222). It is distinct from WeatherLink Live Local
// HTTP (davis_weatherlink_live).
type WeatherLinkIP struct {
	dial func(ctx context.Context, addr string) (net.Conn, error)
}

// NewWeatherLinkIP returns a legacy Davis IP logger provider.
func NewWeatherLinkIP() *WeatherLinkIP {
	return &WeatherLinkIP{dial: defaultWeatherLinkIPDial}
}

func defaultWeatherLinkIPDial(ctx context.Context, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: LANPollTimeout}
	return d.DialContext(ctx, "tcp", addr)
}

// Poll implements Provider. Each call opens a fresh TCP session (wake + GETTIME
// + LOOP) and then closes it so WeatherLink.com uploads can proceed.
func (w *WeatherLinkIP) Poll(ctx context.Context, cfg config.Station) (*Observation, error) {
	addr, err := weatherLinkIPAddr(cfg.Host)
	if err != nil {
		return nil, err
	}
	conn, err := w.dial(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("weatherlink_ip dial: %w", err)
	}
	defer func() { _ = conn.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if err := weatherLinkIPWake(conn); err != nil {
		return nil, err
	}
	consoleTime, err := weatherLinkIPGetTime(conn)
	if err != nil {
		return nil, err
	}
	loop, err := weatherLinkIPLoop(conn)
	if err != nil {
		return nil, err
	}

	return &Observation{
		SourceID:   cfg.ID,
		Provider:   ProviderWeatherLinkIP,
		ObservedAt: consoleTime,
		ProviderMeta: map[string]interface{}{
			"api":  weatherLinkIPAPIMeta,
			"port": weatherLinkIPDefaultPort,
			"raw": map[string]interface{}{
				"loop_base64": base64.StdEncoding.EncodeToString(loop),
				"loop_hex":    fmt.Sprintf("%x", loop),
			},
		},
	}, nil
}

func weatherLinkIPAddr(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", fmt.Errorf("weatherlink_ip host is empty")
	}
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host, nil
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", weatherLinkIPDefaultPort)), nil
}

// weatherLinkIPWake sends the wake byte and waits for the \n\r acknowledgement.
func weatherLinkIPWake(conn net.Conn) error {
	if _, err := conn.Write([]byte("\n")); err != nil {
		return fmt.Errorf("weatherlink_ip wake write: %w", err)
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return fmt.Errorf("weatherlink_ip wake read: %w", err)
	}
	if buf[0] != '\n' || buf[1] != '\r' {
		return fmt.Errorf("weatherlink_ip wake ack %q", buf)
	}
	return nil
}

// weatherLinkIPGetTime requests and parses the console clock.
func weatherLinkIPGetTime(conn net.Conn) (time.Time, error) {
	if _, err := conn.Write([]byte("GETTIME\n")); err != nil {
		return time.Time{}, fmt.Errorf("weatherlink_ip gettime write: %w", err)
	}
	raw := make([]byte, weatherLinkIPTimeSize)
	if _, err := io.ReadFull(conn, raw); err != nil {
		return time.Time{}, fmt.Errorf("weatherlink_ip gettime read: %w", err)
	}
	if vantageCRC16(raw) != 0 {
		return time.Time{}, fmt.Errorf("weatherlink_ip gettime CRC mismatch")
	}
	t, ok := parseVantageTime(raw[:6])
	if !ok {
		return time.Time{}, fmt.Errorf("weatherlink_ip gettime invalid time")
	}
	return t, nil
}

// weatherLinkIPLoop requests one LOOP packet and verifies its CRC.
func weatherLinkIPLoop(conn net.Conn) ([]byte, error) {
	if _, err := conn.Write([]byte("LOOP 1\n")); err != nil {
		return nil, fmt.Errorf("weatherlink_ip loop write: %w", err)
	}
	raw := make([]byte, weatherLinkIPLoopSize)
	if _, err := io.ReadFull(conn, raw); err != nil {
		return nil, fmt.Errorf("weatherlink_ip loop read: %w", err)
	}
	if vantageCRC16(raw) != 0 {
		return nil, fmt.Errorf("weatherlink_ip loop CRC mismatch")
	}
	return raw, nil
}

// parseVantageTime parses the 6 time bytes of a GETTIME response.
//
// NOTE: GETTIME returns the console's local wall clock, not UTC. We label it
// UTC pending real-hardware validation; if consoles run in the site timezone,
// observed_at is offset and needs a timezone correction.
func parseVantageTime(raw []byte) (time.Time, bool) {
	if len(raw) != 6 {
		return time.Time{}, false
	}
	sec := int(int8(raw[0]))
	minute := int(int8(raw[1]))
	hr := int(int8(raw[2]))
	day := int(int8(raw[3]))
	mon := int(int8(raw[4]))
	yr := int(int8(raw[5])) + 1900
	if mon < 1 || mon > 12 || day < 1 || day > 31 || hr < 0 || hr > 23 || minute < 0 || minute > 59 || sec < 0 || sec > 60 {
		return time.Time{}, false
	}
	return time.Date(yr, time.Month(mon), day, hr, minute, sec, 0, time.UTC), true
}
