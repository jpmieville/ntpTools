package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"net"
	"testing"
	"time"
)

// ─── decodeFirstByte ────────────────────────────────────────────────────────

func TestDecodeFirstByte(t *testing.T) {
	tests := []struct {
		name     string
		byte1    uint8
		wantLI   uint8
		wantVN   uint8
		wantMode uint8
	}{
		{
			name:     "LI=0 VN=4 Mode=4 (server response v4)",
			byte1:    0x24,
			wantLI:   0,
			wantVN:   4,
			wantMode: 4,
		},
		{
			name:     "LI=0 VN=4 Mode=3 (client request v4)",
			byte1:    0x23,
			wantLI:   0,
			wantVN:   4,
			wantMode: 3,
		},
		{
			name:     "LI=0 VN=3 Mode=3 (client request v3)",
			byte1:    0x1B,
			wantLI:   0,
			wantVN:   3,
			wantMode: 3,
		},
		{
			name:     "LI=3 VN=4 Mode=4 (alarm, server v4)",
			byte1:    0xE4,
			wantLI:   3,
			wantVN:   4,
			wantMode: 4,
		},
		{
			name:     "all zeros",
			byte1:    0x00,
			wantLI:   0,
			wantVN:   0,
			wantMode: 0,
		},
		{
			name:     "all bits set",
			byte1:    0xFF,
			wantLI:   3,
			wantVN:   7,
			wantMode: 7,
		},
		{
			name:     "LI=1 VN=2 Mode=5 (broadcast v2, last minute 61s)",
			byte1:    0x55,
			wantLI:   1,
			wantVN:   2,
			wantMode: 5,
		},
		{
			name:     "LI=2 VN=3 Mode=4 (server v3, last minute 59s)",
			byte1:    0x9C,
			wantLI:   2,
			wantVN:   3,
			wantMode: 4,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			li, vn, mode := decodeFirstByte(tc.byte1)
			if li != tc.wantLI {
				t.Errorf("LI = %d, want %d", li, tc.wantLI)
			}
			if vn != tc.wantVN {
				t.Errorf("VN = %d, want %d", vn, tc.wantVN)
			}
			if mode != tc.wantMode {
				t.Errorf("Mode = %d, want %d", mode, tc.wantMode)
			}
		})
	}
}

// ─── normalizeStratum ───────────────────────────────────────────────────────

func TestNormalizeStratum(t *testing.T) {
	tests := []struct {
		name    string
		stratum uint8
		want    uint8
	}{
		{"stratum 0 (unspecified)", 0, 0},
		{"stratum 1 (primary)", 1, 1},
		{"stratum 2 (secondary low)", 2, 2},
		{"stratum 8 (secondary mid)", 8, 2},
		{"stratum 15 (secondary high)", 15, 2},
		{"stratum 16 (reserved)", 16, 16},
		{"stratum 100 (reserved high)", 100, 16},
		{"stratum 255 (reserved max)", 255, 16},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeStratum(tc.stratum)
			if got != tc.want {
				t.Errorf("normalizeStratum(%d) = %d, want %d", tc.stratum, got, tc.want)
			}
		})
	}
}

// ─── interpretReferenceID ───────────────────────────────────────────────────

func TestInterpretReferenceID(t *testing.T) {
	tests := []struct {
		name    string
		refID   uint32
		stratum uint8
		vn      uint8
		want    string
	}{
		{
			name:    "stratum 1 GOOG",
			refID:   0x474F4F47, // "GOOG"
			stratum: 1,
			vn:      4,
			want:    "GOOG",
		},
		{
			name:    "stratum 1 GPS with null padding",
			refID:   0x47505300, // "GPS\x00"
			stratum: 1,
			vn:      4,
			want:    "GPS",
		},
		{
			name:    "stratum 1 PPS",
			refID:   0x50505300, // "PPS\x00"
			stratum: 1,
			vn:      3,
			want:    "PPS",
		},
		{
			name:    "stratum 2 vn 3 returns IPv4",
			refID:   0xC0A80001, // 192.168.0.1
			stratum: 2,
			vn:      3,
			want:    "IPv4 address: 192.168.0.1",
		},
		{
			name:    "stratum 2 vn 3 loopback",
			refID:   0x7F000001, // 127.0.0.1
			stratum: 2,
			vn:      3,
			want:    "IPv4 address: 127.0.0.1",
		},
		{
			name:    "stratum 2 vn 4 returns hex ref ID",
			refID:   0xABCD1234,
			stratum: 2,
			vn:      4,
			want:    "Ref ID: ABCD1234",
		},
		{
			name:    "stratum 0 fallback to hex",
			refID:   0xDEADBEEF,
			stratum: 0,
			vn:      4,
			want:    "DEADBEEF",
		},
		{
			name:    "stratum 16 fallback to hex",
			refID:   0x12345678,
			stratum: 16,
			vn:      4,
			want:    "12345678",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := interpretReferenceID(tc.refID, tc.stratum, tc.vn)
			if got != tc.want {
				t.Errorf("interpretReferenceID(0x%08X, %d, %d) = %q, want %q",
					tc.refID, tc.stratum, tc.vn, got, tc.want)
			}
		})
	}
}

// ─── convertNTPTime ─────────────────────────────────────────────────────────

func TestConvertNTPTime(t *testing.T) {
	const epsilon = 1e-6

	tests := []struct {
		name string
		sec  uint32
		frac uint32
		want float64
	}{
		{
			name: "Unix epoch exactly (NTP sec = time1970)",
			sec:  2208988800,
			frac: 0,
			want: 0.0,
		},
		{
			name: "one second after Unix epoch",
			sec:  2208988801,
			frac: 0,
			want: 1.0,
		},
		{
			name: "NTP time zero maps to negative Unix time",
			sec:  0,
			frac: 0,
			want: -2208988800.0,
		},
		{
			name: "half-second fraction",
			sec:  2208988800,
			frac: 1 << 31, // 0.5 in NTP fractional
			want: 0.5,
		},
		{
			name: "quarter-second fraction",
			sec:  2208988800,
			frac: 1 << 30, // 0.25 in NTP fractional
			want: 0.25,
		},
		{
			name: "known Unix timestamp 1700000000",
			sec:  2208988800 + 1700000000,
			frac: 0,
			want: 1700000000.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := convertNTPTime(tc.sec, tc.frac)
			if math.Abs(got-tc.want) > epsilon {
				t.Errorf("convertNTPTime(%d, %d) = %f, want %f (diff %e)",
					tc.sec, tc.frac, got, tc.want, math.Abs(got-tc.want))
			}
		})
	}
}

// ─── unixFloat64 ────────────────────────────────────────────────────────────

func TestUnixFloat64(t *testing.T) {
	const epsilon = 1e-6

	tests := []struct {
		name string
		time time.Time
		want float64
	}{
		{
			name: "Unix epoch",
			time: time.Unix(0, 0),
			want: 0.0,
		},
		{
			name: "one second after epoch",
			time: time.Unix(1, 0),
			want: 1.0,
		},
		{
			name: "with nanoseconds",
			time: time.Unix(1000, 500000000), // 1000.5 seconds
			want: 1000.5,
		},
		{
			name: "known timestamp",
			time: time.Unix(1700000000, 0),
			want: 1700000000.0,
		},
		{
			name: "fractional nanoseconds",
			time: time.Unix(100, 250000000), // 100.25 seconds
			want: 100.25,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := unixFloat64(tc.time)
			if math.Abs(got-tc.want) > epsilon {
				t.Errorf("unixFloat64(%v) = %f, want %f", tc.time, got, tc.want)
			}
		})
	}
}

// ─── createNTPRequest ───────────────────────────────────────────────────────

func TestCreateNTPRequest(t *testing.T) {
	tests := []struct {
		name        string
		version     uint8
		wantLen     int
		wantLiVnMod uint8
	}{
		{
			name:        "version 3 request",
			version:     3,
			wantLen:     48,
			wantLiVnMod: 0x1B, // LI=0, VN=3, Mode=3
		},
		{
			name:        "version 4 request",
			version:     4,
			wantLen:     48,
			wantLiVnMod: 0x23, // LI=0, VN=4, Mode=3
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := NewNTPClient("localhost", tc.version, 5*time.Second)
			packet := client.createNTPRequest()

			if len(packet) != tc.wantLen {
				t.Errorf("packet length = %d, want %d", len(packet), tc.wantLen)
			}

			if packet[0] != tc.wantLiVnMod {
				t.Errorf("LiVnMode byte = 0x%02X, want 0x%02X", packet[0], tc.wantLiVnMod)
			}

			// Verify remaining bytes are zero
			for i := 1; i < len(packet); i++ {
				if packet[i] != 0 {
					t.Errorf("byte[%d] = 0x%02X, want 0x00", i, packet[i])
				}
			}
		})
	}
}

func TestCreateNTPRequest_DecodesCorrectly(t *testing.T) {
	client := NewNTPClient("time.google.com", 4, 10*time.Second)
	packet := client.createNTPRequest()

	li, vn, mode := decodeFirstByte(packet[0])
	if li != 0 {
		t.Errorf("LI = %d, want 0", li)
	}
	if vn != 4 {
		t.Errorf("VN = %d, want 4", vn)
	}
	if mode != 3 {
		t.Errorf("Mode = %d, want 3 (client)", mode)
	}
}

// ─── parseResponse ──────────────────────────────────────────────────────────

// buildNTPResponsePacket constructs a synthetic 48-byte NTP response packet.
func buildNTPResponsePacket(
	liVnMode uint8,
	stratum uint8,
	poll int8,
	precision int8,
	rootDelay uint32,
	rootDisp uint32,
	referenceID uint32,
	refTimeSec, refTimeFrac uint32,
	origTimeSec, origTimeFrac uint32,
	recvTimeSec, recvTimeFrac uint32,
	xmitTimeSec, xmitTimeFrac uint32,
) []byte {
	pkt := &ntpPacket{
		LiVnMode:     liVnMode,
		Stratum:      stratum,
		Poll:         poll,
		Precision:    precision,
		RootDelay:    rootDelay,
		RootDisp:     rootDisp,
		ReferenceID:  referenceID,
		RefTimeSec:   refTimeSec,
		RefTimeFrac:  refTimeFrac,
		OrigTimeSec:  origTimeSec,
		OrigTimeFrac: origTimeFrac,
		RecvTimeSec:  recvTimeSec,
		RecvTimeFrac: recvTimeFrac,
		XmitTimeSec:  xmitTimeSec,
		XmitTimeFrac: xmitTimeFrac,
	}
	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, pkt)
	return buf.Bytes()
}

func TestParseResponse_BasicServerReply(t *testing.T) {
	const epsilon = 1e-6

	// Simulate a stratum-1 NTPv4 server response
	// LI=0, VN=4, Mode=4 => 0x24
	liVnMode := uint8(0x24)
	stratum := uint8(1)
	poll := int8(4)
	precision := int8(-18)
	rootDelay := uint32(0)
	rootDisp := uint32(0x00000100) // small dispersion
	referenceID := uint32(0x474F4F47) // "GOOG"

	// Use a known NTP time: Unix 1700000000 => NTP sec = 1700000000 + 2208988800
	ntpSec := uint32(1700000000 + 2208988800)
	ntpFrac := uint32(0)

	data := buildNTPResponsePacket(
		liVnMode, stratum, poll, precision,
		rootDelay, rootDisp, referenceID,
		ntpSec, ntpFrac, // refTime
		ntpSec, ntpFrac, // origTime (echoed from client — server sets this)
		ntpSec, ntpFrac, // recvTime
		ntpSec, ntpFrac, // xmitTime
	)

	// Verify packet is 48 bytes
	if len(data) != 48 {
		t.Fatalf("packet length = %d, want 48", len(data))
	}

	client := NewNTPClient("time.google.com", 4, 10*time.Second)

	addr := &net.UDPAddr{
		IP:   net.ParseIP("216.239.35.0"),
		Port: 123,
	}

	originateTime := time.Unix(1700000000, 0)
	destinationTime := time.Unix(1700000000, 100000000) // 0.1s later

	result, err := client.parseResponse(data, addr, originateTime, destinationTime)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}

	// Verify header fields
	if result.Server != "time.google.com" {
		t.Errorf("Server = %q, want %q", result.Server, "time.google.com")
	}
	if result.Address.IP != "216.239.35.0" {
		t.Errorf("Address.IP = %q, want %q", result.Address.IP, "216.239.35.0")
	}
	if result.Address.Port != 123 {
		t.Errorf("Address.Port = %d, want %d", result.Address.Port, 123)
	}
	if result.LeapIndicator != 0 {
		t.Errorf("LeapIndicator = %d, want 0", result.LeapIndicator)
	}
	if result.Version != 4 {
		t.Errorf("Version = %d, want 4", result.Version)
	}
	if result.Mode != 4 {
		t.Errorf("Mode = %d, want 4", result.Mode)
	}
	if result.Stratum != 1 {
		t.Errorf("Stratum = %d, want 1", result.Stratum)
	}
	if result.Poll != 4 {
		t.Errorf("Poll = %d, want 4", result.Poll)
	}
	if result.Precision != -18 {
		t.Errorf("Precision = %d, want -18", result.Precision)
	}
	if result.RootDelay != 0 {
		t.Errorf("RootDelay = %d, want 0", result.RootDelay)
	}
	if result.RootDispersion != 0x00000100 {
		t.Errorf("RootDispersion = 0x%08X, want 0x00000100", result.RootDispersion)
	}
	if result.ReferenceID != "GOOG" {
		t.Errorf("ReferenceID = %q, want %q", result.ReferenceID, "GOOG")
	}

	// Verify times
	wantRefTime := 1700000000.0
	if math.Abs(result.ReferenceTime-wantRefTime) > epsilon {
		t.Errorf("ReferenceTime = %f, want %f", result.ReferenceTime, wantRefTime)
	}
	if math.Abs(result.ReceiveTime-wantRefTime) > epsilon {
		t.Errorf("ReceiveTime = %f, want %f", result.ReceiveTime, wantRefTime)
	}
	if math.Abs(result.TransmitTime-wantRefTime) > epsilon {
		t.Errorf("TransmitTime = %f, want %f", result.TransmitTime, wantRefTime)
	}

	// OriginateTime comes from the passed-in time.Time, not the packet
	wantOriginateTime := 1700000000.0
	if math.Abs(result.OriginateTime-wantOriginateTime) > epsilon {
		t.Errorf("OriginateTime = %f, want %f", result.OriginateTime, wantOriginateTime)
	}

	wantDestTime := 1700000000.1
	if math.Abs(result.DestinationTime-wantDestTime) > epsilon {
		t.Errorf("DestinationTime = %f, want %f", result.DestinationTime, wantDestTime)
	}
}

func TestParseResponse_ClockOffsetAndRoundtrip(t *testing.T) {
	const epsilon = 1e-6

	// Use fractional values that are exact in binary (powers of 2) to avoid
	// truncation errors when encoding NTP fractional seconds.
	//
	// T1 (originateTime)  = 1000.0    [client sends request]
	// T2 (receiveTime)    = 1000.25   [server receives request]
	// T3 (transmitTime)   = 1000.5    [server sends reply]
	// T4 (destinationTime)= 1000.75   [client receives reply]
	//
	// Code formula:
	//   offset = ((T2-T1) + (T3-T4)) / 2 = ((0.25) + (-0.25)) / 2 = 0.0
	//   delay  = (T4-T1) - (T2-T3) = 0.75 - (0.25-0.5) = 0.75 - (-0.25) = 1.0

	baseSec := uint32(1000 + time1970)

	ntpT2Sec := baseSec
	ntpT2Frac := uint32(1 << 30) // 0.25
	ntpT3Sec := baseSec
	ntpT3Frac := uint32(1 << 31) // 0.5

	data := buildNTPResponsePacket(
		0x24, 1, 4, -20,
		0, 0, 0x474F4F47,
		ntpT2Sec, ntpT2Frac, // refTime (use T2)
		0, 0, // orig time in packet (not used by parseResponse for offset calc)
		ntpT2Sec, ntpT2Frac, // recvTime = T2
		ntpT3Sec, ntpT3Frac, // xmitTime = T3
	)

	client := NewNTPClient("ntp.example.com", 4, 10*time.Second)
	addr := &net.UDPAddr{IP: net.ParseIP("10.0.0.1"), Port: 123}

	originateTime := time.Unix(1000, 0)          // T1 = 1000.0
	destinationTime := time.Unix(1000, 750000000) // T4 = 1000.75

	result, err := client.parseResponse(data, addr, originateTime, destinationTime)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}

	// Expected offset = ((T2-T1) + (T3-T4)) / 2 = 0.0
	if math.Abs(result.ClockOffset-0.0) > epsilon {
		t.Errorf("ClockOffset = %f, want ~0.0", result.ClockOffset)
	}

	// Expected delay = (T4-T1) - (T2-T3) = 0.75 - (-0.25) = 1.0
	if math.Abs(result.RoundtripDelay-1.0) > epsilon {
		t.Errorf("RoundtripDelay = %f, want ~1.0", result.RoundtripDelay)
	}
}

func TestParseResponse_Stratum2Version3_IPv4RefID(t *testing.T) {
	// Stratum 2 with NTPv3 should show IPv4 reference ID
	// LI=0, VN=3, Mode=4 => (0<<6)|(3<<3)|4 = 0x1C
	refID := uint32(0xC0A80101) // 192.168.1.1

	ntpSec := uint32(1700000000 + 2208988800)

	data := buildNTPResponsePacket(
		0x1C, 2, 6, -16,
		0x00010000, 0x00020000, refID,
		ntpSec, 0,
		ntpSec, 0,
		ntpSec, 0,
		ntpSec, 0,
	)

	client := NewNTPClient("pool.ntp.org", 3, 10*time.Second)
	addr := &net.UDPAddr{IP: net.ParseIP("192.168.1.1"), Port: 123}

	now := time.Unix(1700000000, 0)
	result, err := client.parseResponse(data, addr, now, now)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}

	if result.Version != 3 {
		t.Errorf("Version = %d, want 3", result.Version)
	}
	if result.Stratum != 2 {
		t.Errorf("Stratum = %d, want 2", result.Stratum)
	}
	if result.ReferenceID != "IPv4 address: 192.168.1.1" {
		t.Errorf("ReferenceID = %q, want %q", result.ReferenceID, "IPv4 address: 192.168.1.1")
	}
	if result.RootDelay != 0x00010000 {
		t.Errorf("RootDelay = 0x%08X, want 0x00010000", result.RootDelay)
	}
	if result.RootDispersion != 0x00020000 {
		t.Errorf("RootDispersion = 0x%08X, want 0x00020000", result.RootDispersion)
	}
}

func TestParseResponse_Stratum2Version4_HexRefID(t *testing.T) {
	// Stratum 2 with NTPv4 should show hex reference ID
	// LI=0, VN=4, Mode=4 => 0x24
	refID := uint32(0xABCD1234)

	ntpSec := uint32(1700000000 + 2208988800)

	data := buildNTPResponsePacket(
		0x24, 2, 6, -20,
		0, 0, refID,
		ntpSec, 0,
		ntpSec, 0,
		ntpSec, 0,
		ntpSec, 0,
	)

	client := NewNTPClient("time.cloudflare.com", 4, 10*time.Second)
	addr := &net.UDPAddr{IP: net.ParseIP("162.159.200.1"), Port: 123}

	now := time.Unix(1700000000, 0)
	result, err := client.parseResponse(data, addr, now, now)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}

	if result.Version != 4 {
		t.Errorf("Version = %d, want 4", result.Version)
	}
	if result.Stratum != 2 {
		t.Errorf("Stratum = %d, want 2", result.Stratum)
	}
	if result.ReferenceID != "Ref ID: ABCD1234" {
		t.Errorf("ReferenceID = %q, want %q", result.ReferenceID, "Ref ID: ABCD1234")
	}
}

func TestParseResponse_FractionalTimestamps(t *testing.T) {
	const epsilon = 1e-6

	// Test that fractional NTP timestamps are decoded correctly
	ntpSec := uint32(2208988800 + 1700000000)
	halfSecFrac := uint32(1 << 31) // 0.5 seconds

	data := buildNTPResponsePacket(
		0x24, 1, 4, -18,
		0, 0, 0x474F4F47,
		ntpSec, halfSecFrac, // refTime = 1700000000.5
		ntpSec, 0,
		ntpSec, halfSecFrac, // recvTime = 1700000000.5
		ntpSec, halfSecFrac, // xmitTime = 1700000000.5
	)

	client := NewNTPClient("time.google.com", 4, 10*time.Second)
	addr := &net.UDPAddr{IP: net.ParseIP("216.239.35.0"), Port: 123}

	now := time.Unix(1700000000, 0)
	result, err := client.parseResponse(data, addr, now, now)
	if err != nil {
		t.Fatalf("parseResponse returned error: %v", err)
	}

	wantTime := 1700000000.5
	if math.Abs(result.ReferenceTime-wantTime) > epsilon {
		t.Errorf("ReferenceTime = %f, want %f", result.ReferenceTime, wantTime)
	}
	if math.Abs(result.ReceiveTime-wantTime) > epsilon {
		t.Errorf("ReceiveTime = %f, want %f", result.ReceiveTime, wantTime)
	}
	if math.Abs(result.TransmitTime-wantTime) > epsilon {
		t.Errorf("TransmitTime = %f, want %f", result.TransmitTime, wantTime)
	}
}

func TestParseResponse_TooShortPacket(t *testing.T) {
	client := NewNTPClient("localhost", 4, 5*time.Second)
	addr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 123}
	now := time.Now()

	shortData := make([]byte, 10) // too short
	_, err := client.parseResponse(shortData, addr, now, now)
	if err == nil {
		t.Error("expected error for short packet, got nil")
	}
}

// ─── NewNTPClient ───────────────────────────────────────────────────────────

func TestNewNTPClient(t *testing.T) {
	tests := []struct {
		name    string
		server  string
		version uint8
		timeout time.Duration
	}{
		{
			name:    "default settings",
			server:  "time.google.com",
			version: 4,
			timeout: 20 * time.Second,
		},
		{
			name:    "version 3 with custom timeout",
			server:  "pool.ntp.org",
			version: 3,
			timeout: 5 * time.Second,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := NewNTPClient(tc.server, tc.version, tc.timeout)
			if client == nil {
				t.Fatal("NewNTPClient returned nil")
			}
			if client.server != tc.server {
				t.Errorf("server = %q, want %q", client.server, tc.server)
			}
			if client.version != tc.version {
				t.Errorf("version = %d, want %d", client.version, tc.version)
			}
			if client.timeout != tc.timeout {
				t.Errorf("timeout = %v, want %v", client.timeout, tc.timeout)
			}
		})
	}
}
