package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"net"
	"os"
	"time"
)

// NTP Constants
const (
	NTP_PORT    = 123
	NTP_TIMEOUT = 20 * time.Second
	TIME1970    = 2208988800 // Seconds between 1900 and 1970
)

// NTP packet structure - exactly 48 bytes
// First byte is packed: LI (2 bits) + VN (3 bits) + Mode (3 bits)
type ntpPacket struct {
	LiVnMode    uint8  // Leap indicator (2 bits) + Version (3 bits) + Mode (3 bits)
	Stratum     uint8  // Stratum
	Poll        int8   // Poll interval
	Precision   int8   // Precision
	RootDelay   uint32 // Root delay (fixed point)
	RootDisp    uint32 // Root dispersion (fixed point)
	ReferenceID uint32 // Reference identifier
	RefTimeSec  uint32 // Reference timestamp (seconds)
	RefTimeFrac uint32 // Reference timestamp (fraction)
	OrigTimeSec uint32 // Originate timestamp (seconds)
	OrigTimeFrac uint32 // Originate timestamp (fraction)
	RecvTimeSec uint32 // Receive timestamp (seconds)
	RecvTimeFrac uint32 // Receive timestamp (fraction)
	XmitTimeSec uint32 // Transmit timestamp (seconds)
	XmitTimeFrac uint32 // Transmit timestamp (fraction)
}

// NTPClient represents an SNTP client
type NTPClient struct {
	server  string
	version uint8
	timeout time.Duration
}

// LeapIndicator text descriptions
var leapIndicatorText = map[uint8]string{
	0: "no warning",
	1: "last minute of current day has 61 sec",
	2: "last minute of current day has 59 sec",
	3: "alarm condition (clock not synchronized)",
}

// Mode text descriptions
var modeText = map[uint8]string{
	0: "reserved",
	1: "symmetric active",
	2: "symmetric passive",
	3: "client",
	4: "server",
	5: "broadcast",
	6: "reserved for NTP control message",
	7: "reserved for private use",
}

// Stratum text descriptions
var stratumText = map[uint8]string{
	0: "unspecified or unavailable",
	1: "primary reference (e.g. radio clock)",
	2: "2...15: secondary reference (via NTP or SNTP)",
	16: "16...255: reserved",
}

// Default NTP servers
var defaultServers = []string{
	"129.132.2.21",   // swisstime.ethz.ch
	"0.pool.ntp.org",
	"time.google.com",
	"time.cloudflare.com",
}

var verbose bool

// NewNTPClient creates a new NTP client
func NewNTPClient(server string, version uint8, timeout time.Duration) *NTPClient {
	return &NTPClient{
		server:  server,
		version: version,
		timeout: timeout,
	}
}

// createNTPRequest creates an NTP request packet (exactly 48 bytes)
func (c *NTPClient) createNTPRequest() []byte {
	// First byte: LI (2 bits, value 0) | VN (3 bits) | Mode (3 bits, value 3 = client)
	liVnMode := (uint8(0) << 6) | (c.version << 3) | uint8(3)

	packet := &ntpPacket{
		LiVnMode: liVnMode,
	}

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, packet)
	return buf.Bytes()
}

// decodeFirstByte decodes the first byte of the NTP response
func decodeFirstByte(byte1 uint8) (li, vn, mode uint8) {
	li = (byte1 & 0xC0) >> 6
	vn = (byte1 & 0x38) >> 3
	mode = byte1 & 0x07
	return
}

// normalizeStratum normalizes the stratum value for display
func normalizeStratum(stratum uint8) uint8 {
	if stratum == 1 {
		return 1
	} else if stratum >= 2 && stratum <= 15 {
		return 2
	} else if stratum > 15 {
		return 16
	}
	return 0
}

// interpretReferenceID interprets the reference identification field
func interpretReferenceID(refID uint32, stratum uint8, vn uint8) string {
	if stratum == 1 {
		// Primary reference - decode as 4-character string
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, refID)
		for i := range b {
			if b[i] == 0 {
				return string(b[:i])
			}
		}
		return string(b)
	} else if stratum == 2 {
		if vn == 3 {
			return fmt.Sprintf("IPv4 address: %d.%d.%d.%d",
				byte(refID>>24), byte(refID>>16), byte(refID>>8), byte(refID))
		} else if vn == 4 {
			return fmt.Sprintf("Ref ID: %02X%02X%02X%02X",
				byte(refID>>24), byte(refID>>16), byte(refID>>8), byte(refID))
		}
	}
	return fmt.Sprintf("%08X", refID)
}

// convertNTPTime converts NTP timestamp to Unix epoch
func convertNTPTime(sec, frac uint32) float64 {
	return float64(sec) + float64(frac)/4294967296.0 - float64(TIME1970)
}

// unixFloat64 returns time as float64 (seconds since Unix epoch)
func unixFloat64(t time.Time) float64 {
	return float64(t.Unix()) + float64(t.Nanosecond())/1e9
}

// FetchTime fetches time from the NTP server
func (c *NTPClient) FetchTime() (*map[string]interface{}, error) {
	message := c.createNTPRequest()

	if verbose {
		fmt.Printf("[DEBUG] NTP request packet size: %d bytes\n", len(message))
		fmt.Printf("[DEBUG] Connecting to NTP server: %s:%d\n", c.server, NTP_PORT)
	}

	// Resolve the server address
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", c.server, NTP_PORT))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", c.server, err)
	}

	if verbose {
		fmt.Printf("[DEBUG] Resolved address: %s\n", addr.String())
	}

	// Create UDP connection
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", c.server, err)
	}
	defer conn.Close()

	// Set timeout
	conn.SetDeadline(time.Now().Add(c.timeout))

	if verbose {
		fmt.Printf("[DEBUG] Sending NTP request (version %d)...\n", c.version)
	}

	// Send request
	written, err := conn.Write(message)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if verbose {
		fmt.Printf("[DEBUG] Sent %d bytes\n", written)
	}

	// Receive response
	response := make([]byte, 48)
	n, err := conn.Read(response)
	if err != nil {
		return nil, fmt.Errorf("failed to receive response: %w", err)
	}

	if verbose {
		fmt.Printf("[DEBUG] Received %d bytes\n", n)
	}

	destinationTime := time.Now()
	originateTime := time.Now()

	// Parse response
	return c.parseResponse(response, addr, originateTime, destinationTime)
}

// parseResponse parses the NTP response packet
func (c *NTPClient) parseResponse(
	data []byte,
	addr *net.UDPAddr,
	originateTime time.Time,
	destinationTime time.Time,
) (*map[string]interface{}, error) {
	buf := bytes.NewReader(data)
	var packet ntpPacket
	err := binary.Read(buf, binary.BigEndian, &packet)
	if err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Decode first byte
	li, vn, mode := decodeFirstByte(packet.LiVnMode)

	// Normalize stratum
	stratum := normalizeStratum(packet.Stratum)

	// Interpret reference ID
	refID := interpretReferenceID(packet.ReferenceID, stratum, vn)

	// Convert times to Unix epoch
	receiveTime := convertNTPTime(packet.RecvTimeSec, packet.RecvTimeFrac)
	referenceTime := convertNTPTime(packet.RefTimeSec, packet.RefTimeFrac)
	transmitTime := convertNTPTime(packet.XmitTimeSec, packet.XmitTimeFrac)

	// Get float64 times
	originateTimeFloat := unixFloat64(originateTime)
	destTimeFloat := unixFloat64(destinationTime)

	// Calculate clock offset and roundtrip delay (RFC 2030)
	clockOffset := ((receiveTime - originateTimeFloat) +
		(transmitTime - destTimeFloat)) / 2.0
	roundtripDelay := (destTimeFloat - originateTimeFloat) -
		(receiveTime - transmitTime)

	result := map[string]interface{}{
		"server":           c.server,
		"address":          addr.String(),
		"leap_indicator":  li,
		"version":          vn,
		"mode":             mode,
		"stratum":          stratum,
		"poll":             packet.Poll,
		"precision":        packet.Precision,
		"root_delay":       packet.RootDelay,
		"root_dispersion":  packet.RootDisp,
		"reference_id":     refID,
		"reference_time":   referenceTime,
		"originate_time":   originateTimeFloat,
		"receive_time":     receiveTime,
		"transmit_time":    transmitTime,
		"destination_time": destTimeFloat,
		"clock_offset":     clockOffset,
		"roundtrip_delay":  roundtripDelay,
	}

	return &result, nil
}

// PrintResult prints the NTP time result in a formatted way
func (c *NTPClient) PrintResult(result map[string]interface{}) {
	fmt.Println()
	fmt.Printf("Response received from : %s\n", result["server"])
	fmt.Printf("IP address             : %s\n", result["address"])
	fmt.Println()
	fmt.Println("Header")
	fmt.Println("--------------------------------------------------")

	byte1 := result["leap_indicator"].(uint8)<<6 | result["version"].(uint8)<<3 | result["mode"].(uint8)
	fmt.Printf("Byte1                  : 0x%02X\n", byte1)
	fmt.Printf("  Leap Indicator (LI)  : %d [%s]\n", result["leap_indicator"],
		leapIndicatorText[result["leap_indicator"].(uint8)])
	fmt.Printf("  Version number (VN)  : %d [NTP/SNTP version number]\n", result["version"])
	fmt.Printf("  Mode                 : %d [%s]\n", result["mode"], modeText[result["mode"].(uint8)])
	fmt.Printf("Stratum                : %d [%s]\n", result["stratum"],
		stratumText[result["stratum"].(uint8)])
	fmt.Printf("Poll interval          : %d\n", result["poll"])

	precision := int(result["precision"].(int8))
	precValue := float64(1)
	for i := 0; i < -precision; i++ {
		precValue /= 2
	}
	fmt.Printf("Clock Precision        : 2**%d = %1.5e\n", precision, precValue)

	rootDelay := result["root_delay"].(uint32)
	fmt.Printf("Root Delay             : 0x%08X = %10.5f\n", rootDelay, float64(rootDelay)/65536.0)

	rootDisp := result["root_dispersion"].(uint32)
	fmt.Printf("Root Dispersion        : 0x%08X = %10.5f\n", rootDisp, float64(rootDisp)/65536.0)
	fmt.Printf("Reference Identifier   : %s\n", result["reference_id"])

	fmt.Println()
	fmt.Println("Interpreted results (Unix epoch):")
	fmt.Println("--------------------------------------------------")
	fmt.Printf("Reference Timestamp    : %10.5f [last sync of server clock]\n", result["reference_time"])
	fmt.Printf("Originate Timestamp    : %10.5f [request sent by client]\n", result["originate_time"])
	fmt.Printf("Receive   Timestamp    : %10.5f [request received by server]\n", result["receive_time"])
	fmt.Printf("Transmit  Timestamp    : %10.5f [reply sent by server]\n", result["transmit_time"])
	fmt.Printf("Destination Timestamp  : %10.5f [reply received by client]\n", result["destination_time"])
	fmt.Println("--------------------------------------------------")
	fmt.Println()

	receiveTime := result["receive_time"].(float64)
	unixTime := int64(receiveTime)
	fmt.Printf("Net Time UTC           : %s + %0.3f ms\n",
		time.Unix(unixTime, 0).UTC().Format(time.RFC1123), (receiveTime-float64(int64(receiveTime)))*1000)
	fmt.Printf("Clock Offset           : %10.5f seconds\n", result["clock_offset"])
	fmt.Printf("Roundtrip Delay        : %10.5f seconds\n", result["roundtrip_delay"])
}

func main() {
	server := flag.String("server", defaultServers[0], "NTP server hostname or IP")
	version := flag.Uint("version", 4, "NTP version (3 or 4)")
	timeout := flag.Duration("timeout", NTP_TIMEOUT, "Socket timeout")
	listServers := flag.Bool("list-servers", false, "List available default NTP servers")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose debug output")
	flag.Parse()

	if *listServers {
		fmt.Println("Available default NTP servers:")
		for _, s := range defaultServers {
			fmt.Printf("  - %s\n", s)
		}
		os.Exit(0)
	}

	// Create client and fetch time
	client := NewNTPClient(*server, uint8(*version), *timeout)

	result, err := client.FetchTime()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	client.PrintResult(*result)
}
