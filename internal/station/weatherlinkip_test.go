package station

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/alexwitherspoon/aviationwx-bridge/internal/config"
)

func TestVantageCRC16(t *testing.T) {
	// Documented Davis test vector: crc16([C6 CE A2 03]) == 0xe2b4.
	if got := vantageCRC16([]byte{0xC6, 0xCE, 0xA2, 0x03}); got != 0xe2b4 {
		t.Fatalf("crc16 = %#x, want 0xe2b4", got)
	}
}

func TestVantagePacketCRC(t *testing.T) {
	// A valid packet (data + big-endian CRC) yields zero.
	data := []byte("LOOP packet test data")
	crc := vantageCRC16(data)
	pkt := append(append([]byte{}, data...), byte(crc>>8), byte(crc))
	if got := vantageCRC16(pkt); got != 0 {
		t.Fatalf("crc16(packet) = %#x, want 0", got)
	}
}

func TestParseVantageTime(t *testing.T) {
	raw := []byte{30, 15, 14, 25, 11, 125} // 2025-11-25 14:15:30
	got, ok := parseVantageTime(raw)
	if !ok {
		t.Fatal("parse failed")
	}
	want := time.Date(2025, 11, 25, 14, 15, 30, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("time = %v, want %v", got, want)
	}
	if _, ok := parseVantageTime([]byte{99, 99, 99, 99, 99, 125}); ok {
		t.Fatal("expected invalid time")
	}
}

func TestWeatherLinkIPPoll(t *testing.T) {
	timeBytes := []byte{30, 15, 14, 25, 11, 125} // 2025-11-25 14:15:30
	loopData := make([]byte, 97)
	copy(loopData, "LOO")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Wake: read '\n', write '\n\r'.
		buf := make([]byte, 1)
		_, _ = io.ReadFull(conn, buf)
		_, _ = conn.Write([]byte("\n\r"))

		// GETTIME: read "GETTIME\n", write 8 bytes (6 time + 2 CRC).
		cmd := make([]byte, 8)
		_, _ = io.ReadFull(conn, cmd)
		timeCRC := vantageCRC16(timeBytes)
		_, _ = conn.Write(append(append([]byte{}, timeBytes...), byte(timeCRC>>8), byte(timeCRC)))

		// LOOP 1: read "LOOP 1\n", write 99 bytes (97 data + 2 CRC).
		loopCmd := make([]byte, 7)
		_, _ = io.ReadFull(conn, loopCmd)
		loopCRC := vantageCRC16(loopData)
		_, _ = conn.Write(append(append([]byte{}, loopData...), byte(loopCRC>>8), byte(loopCRC)))
	}()

	st := config.Station{ID: "station-wlip", Type: config.StationTypeWeatherLinkIP, Host: ln.Addr().String()}
	obs, err := NewWeatherLinkIP().Poll(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if obs.Provider != ProviderWeatherLinkIP {
		t.Errorf("provider = %q", obs.Provider)
	}
	if !obs.ObservedAt.Equal(time.Date(2025, 11, 25, 14, 15, 30, 0, time.UTC)) {
		t.Errorf("observed_at = %v", obs.ObservedAt)
	}
	if obs.ProviderMeta["raw"] == nil {
		t.Error("expected raw loop packet")
	}
}
