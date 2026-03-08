package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"time"
)

const (
	ntpPort    = 123
	ntpTimeout = 20 * time.Second
	time1970   = 2208988800 // Seconds between 1900 and 1970
)

// ntpPacket is the NTP wire format - exactly 48 bytes.
// First byte is packed: LI (2 bits) + VN (3 bits) + Mode (3 bits)
type ntpPacket struct {
	LiVnMode     uint8 // LI (2 bits) + VN (3 bits) + Mode (3 bits)
	Stratum      uint8
	Poll         int8
	Precision    int8
	RootDelay    uint32
	RootDisp     uint32
	ReferenceID  uint32
	RefTimeSec   uint32
	RefTimeFrac  uint32
	OrigTimeSec  uint32
	OrigTimeFrac uint32
	RecvTimeSec  uint32
	RecvTimeFrac uint32
	XmitTimeSec  uint32
	XmitTimeFrac uint32
}

// NTPAddress holds the resolved server address.
type NTPAddress struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

func (a NTPAddress) String() string {
	return fmt.Sprintf("%s:%d", a.IP, a.Port)
}

// NTPResult holds the parsed and computed results of an NTP query.
type NTPResult struct {
	Server          string     `json:"server"`
	Address         NTPAddress `json:"address"`
	LeapIndicator   uint8      `json:"leap_indicator"`
	Version         uint8      `json:"version"`
	Mode            uint8      `json:"mode"`
	Stratum         uint8      `json:"stratum"`
	Poll            int8       `json:"poll"`
	Precision       int8       `json:"precision"`
	RootDelay       uint32     `json:"root_delay"`
	RootDispersion  uint32     `json:"root_dispersion"`
	ReferenceID     string     `json:"reference_id"`
	ReferenceTime   float64    `json:"reference_time"`
	OriginateTime   float64    `json:"originate_time"`
	ReceiveTime     float64    `json:"receive_time"`
	TransmitTime    float64    `json:"transmit_time"`
	DestinationTime float64    `json:"destination_time"`
	ClockOffset     float64    `json:"clock_offset"`
	RoundtripDelay  float64    `json:"roundtrip_delay"`
}

// NTPClient represents an SNTP client.
type NTPClient struct {
	server  string
	version uint8
	timeout time.Duration
}

var leapIndicatorText = map[uint8]string{
	0: "no warning",
	1: "last minute of current day has 61 sec",
	2: "last minute of current day has 59 sec",
	3: "alarm condition (clock not synchronized)",
}

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

var stratumText = map[uint8]string{
	0: "unspecified or unavailable",
	1: "primary reference (e.g. radio clock)",
	2: "2...15: secondary reference (via NTP or SNTP)",
	16: "16...255: reserved",
}

var defaultServers = []string{
	"129.132.2.21", // swisstime.ethz.ch
	"0.pool.ntp.org",
	"time.google.com",
	"time.cloudflare.com",
}

var verbose bool

// NewNTPClient creates a new NTP client.
func NewNTPClient(server string, version uint8, timeout time.Duration) *NTPClient {
	return &NTPClient{
		server:  server,
		version: version,
		timeout: timeout,
	}
}

func (c *NTPClient) createNTPRequest() []byte {
	liVnMode := (uint8(0) << 6) | (c.version << 3) | uint8(3)

	packet := &ntpPacket{
		LiVnMode: liVnMode,
	}

	buf := new(bytes.Buffer)
	binary.Write(buf, binary.BigEndian, packet)
	return buf.Bytes()
}

func decodeFirstByte(byte1 uint8) (li, vn, mode uint8) {
	li = (byte1 & 0xC0) >> 6
	vn = (byte1 & 0x38) >> 3
	mode = byte1 & 0x07
	return
}

func normalizeStratum(stratum uint8) uint8 {
	switch {
	case stratum == 1:
		return 1
	case stratum >= 2 && stratum <= 15:
		return 2
	case stratum > 15:
		return 16
	default:
		return 0
	}
}

func interpretReferenceID(refID uint32, stratum uint8, vn uint8) string {
	switch {
	case stratum == 1:
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, refID)
		for i := range b {
			if b[i] == 0 {
				return string(b[:i])
			}
		}
		return string(b)
	case stratum == 2 && vn == 3:
		return fmt.Sprintf("IPv4 address: %d.%d.%d.%d",
			byte(refID>>24), byte(refID>>16), byte(refID>>8), byte(refID))
	case stratum == 2 && vn == 4:
		return fmt.Sprintf("Ref ID: %02X%02X%02X%02X",
			byte(refID>>24), byte(refID>>16), byte(refID>>8), byte(refID))
	default:
		return fmt.Sprintf("%08X", refID)
	}
}

func convertNTPTime(sec, frac uint32) float64 {
	return float64(sec) + float64(frac)/4294967296.0 - float64(time1970)
}

func unixFloat64(t time.Time) float64 {
	return float64(t.Unix()) + float64(t.Nanosecond())/1e9
}

// FetchTime fetches time from the NTP server.
func (c *NTPClient) FetchTime() (*NTPResult, error) {
	message := c.createNTPRequest()

	if verbose {
		fmt.Printf("[DEBUG] NTP request packet size: %d bytes\n", len(message))
		fmt.Printf("[DEBUG] Connecting to NTP server: %s:%d\n", c.server, ntpPort)
	}

	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", c.server, ntpPort))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", c.server, err)
	}

	if verbose {
		fmt.Printf("[DEBUG] Resolved address: %s\n", addr.String())
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", c.server, err)
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(c.timeout))

	if verbose {
		fmt.Printf("[DEBUG] Sending NTP request (version %d)...\n", c.version)
	}

	originateTime := time.Now()

	written, err := conn.Write(message)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	if verbose {
		fmt.Printf("[DEBUG] Sent %d bytes\n", written)
	}

	response := make([]byte, 48)
	n, err := conn.Read(response)
	if err != nil {
		return nil, fmt.Errorf("failed to receive response: %w", err)
	}

	destinationTime := time.Now()

	if verbose {
		fmt.Printf("[DEBUG] Received %d bytes\n", n)
	}

	return c.parseResponse(response, addr, originateTime, destinationTime)
}

func (c *NTPClient) parseResponse(
	data []byte,
	addr *net.UDPAddr,
	originateTime time.Time,
	destinationTime time.Time,
) (*NTPResult, error) {
	buf := bytes.NewReader(data)
	var packet ntpPacket
	if err := binary.Read(buf, binary.BigEndian, &packet); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	li, vn, mode := decodeFirstByte(packet.LiVnMode)
	stratum := normalizeStratum(packet.Stratum)
	refID := interpretReferenceID(packet.ReferenceID, stratum, vn)

	receiveTime := convertNTPTime(packet.RecvTimeSec, packet.RecvTimeFrac)
	referenceTime := convertNTPTime(packet.RefTimeSec, packet.RefTimeFrac)
	transmitTime := convertNTPTime(packet.XmitTimeSec, packet.XmitTimeFrac)

	originateTimeFloat := unixFloat64(originateTime)
	destTimeFloat := unixFloat64(destinationTime)

	clockOffset := ((receiveTime - originateTimeFloat) +
		(transmitTime - destTimeFloat)) / 2.0
	roundtripDelay := (destTimeFloat - originateTimeFloat) -
		(receiveTime - transmitTime)

	result := &NTPResult{
		Server:  c.server,
		Address: NTPAddress{IP: addr.IP.String(), Port: addr.Port},
		LeapIndicator:   li,
		Version:         vn,
		Mode:            mode,
		Stratum:         stratum,
		Poll:            packet.Poll,
		Precision:       packet.Precision,
		RootDelay:       packet.RootDelay,
		RootDispersion:  packet.RootDisp,
		ReferenceID:     refID,
		ReferenceTime:   referenceTime,
		OriginateTime:   originateTimeFloat,
		ReceiveTime:     receiveTime,
		TransmitTime:    transmitTime,
		DestinationTime: destTimeFloat,
		ClockOffset:     clockOffset,
		RoundtripDelay:  roundtripDelay,
	}

	return result, nil
}

// PrintResult prints the NTP time result in a formatted way.
func (c *NTPClient) PrintResult(result *NTPResult) {
	fmt.Println()
	fmt.Printf("Response received from : %s\n", result.Server)
	fmt.Printf("IP address             : %s\n", result.Address.String())
	fmt.Println()
	fmt.Println("Header")
	fmt.Println("--------------------------------------------------")

	byte1 := result.LeapIndicator<<6 | result.Version<<3 | result.Mode
	fmt.Printf("Byte1                  : 0x%02X\n", byte1)
	fmt.Printf("  Leap Indicator (LI)  : %d [%s]\n", result.LeapIndicator,
		leapIndicatorText[result.LeapIndicator])
	fmt.Printf("  Version number (VN)  : %d [NTP/SNTP version number]\n", result.Version)
	fmt.Printf("  Mode                 : %d [%s]\n", result.Mode, modeText[result.Mode])
	fmt.Printf("Stratum                : %d [%s]\n", result.Stratum,
		stratumText[result.Stratum])
	fmt.Printf("Poll interval          : %d\n", result.Poll)

	precision := int(result.Precision)
	fmt.Printf("Clock Precision        : 2**%d = %1.5e\n", precision, math.Exp2(float64(precision)))

	fmt.Printf("Root Delay             : 0x%08X = %10.5f\n", result.RootDelay, float64(result.RootDelay)/65536.0)
	fmt.Printf("Root Dispersion        : 0x%08X = %10.5f\n", result.RootDispersion, float64(result.RootDispersion)/65536.0)
	fmt.Printf("Reference Identifier   : %s\n", result.ReferenceID)

	fmt.Println()
	fmt.Println("Interpreted results (Unix epoch):")
	fmt.Println("--------------------------------------------------")
	fmt.Printf("Reference Timestamp    : %10.5f [last sync of server clock]\n", result.ReferenceTime)
	fmt.Printf("Originate Timestamp    : %10.5f [request sent by client]\n", result.OriginateTime)
	fmt.Printf("Receive   Timestamp    : %10.5f [request received by server]\n", result.ReceiveTime)
	fmt.Printf("Transmit  Timestamp    : %10.5f [reply sent by server]\n", result.TransmitTime)
	fmt.Printf("Destination Timestamp  : %10.5f [reply received by client]\n", result.DestinationTime)
	fmt.Println("--------------------------------------------------")
	fmt.Println()

	unixTime := int64(result.ReceiveTime)
	fmt.Printf("Net Time UTC           : %s + %0.3f ms\n",
		time.Unix(unixTime, 0).UTC().Format(time.RFC1123), (result.ReceiveTime-float64(unixTime))*1000)
	fmt.Printf("Clock Offset           : %10.5f seconds\n", result.ClockOffset)
	fmt.Printf("Roundtrip Delay        : %10.5f seconds\n", result.RoundtripDelay)
}

func main() {
	server := flag.String("server", defaultServers[0], "NTP server hostname or IP")
	version := flag.Uint("version", 4, "NTP version (3 or 4)")
	timeout := flag.Duration("timeout", ntpTimeout, "Socket timeout")
	listServers := flag.Bool("list-servers", false, "List available default NTP servers")
	jsonOutput := flag.Bool("json", false, "Output result as JSON")
	compactOutput := flag.Bool("compact", false, "Output compact single-line JSON (implies -json)")
	flag.BoolVar(&verbose, "verbose", false, "Enable verbose debug output")
	flag.Parse()

	if *listServers {
		fmt.Println("Available default NTP servers:")
		for _, s := range defaultServers {
			fmt.Printf("  - %s\n", s)
		}
		os.Exit(0)
	}

	client := NewNTPClient(*server, uint8(*version), *timeout)

	result, err := client.FetchTime()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if *jsonOutput || *compactOutput {
		enc := json.NewEncoder(os.Stdout)
		if !*compactOutput {
			enc.SetIndent("", "  ")
		}
		if err := enc.Encode(result); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		client.PrintResult(result)
	}
}
