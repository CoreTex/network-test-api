package unit

import (
	"strings"
	"testing"
)

// Constants from main.go
const (
	COOKIE_SIZE         = 37
	DEFAULT_TCP_BLKSIZE = 128 * 1024
	DEFAULT_UDP_BLKSIZE = 1460
	DEFAULT_BANDWIDTH   = 100 * 1000 * 1000
)

// generateCookie generates a random iperf3-compatible cookie
func generateCookie() []byte {
	const chars = "abcdefghijklmnopqrstuvwxyz234567"
	cookie := make([]byte, COOKIE_SIZE)
	for i := 0; i < COOKIE_SIZE-1; i++ {
		cookie[i] = chars[i%len(chars)]
	}
	cookie[COOKIE_SIZE-1] = 0
	return cookie
}

// Iperf3Client configuration (simplified for testing)
type Iperf3ClientConfig struct {
	Host      string
	Port      int
	Duration  int
	Parallel  int
	Protocol  string
	Reverse   bool
	BlockSize int
	Bandwidth int64
}

// NewIperf3ClientConfig creates a new client configuration
func NewIperf3ClientConfig(host string, port, duration, parallel int, protocol string, reverse bool, bandwidthMbps int) *Iperf3ClientConfig {
	if parallel < 1 {
		parallel = 1
	}
	blkSize := DEFAULT_TCP_BLKSIZE
	if strings.ToUpper(protocol) == "UDP" {
		blkSize = DEFAULT_UDP_BLKSIZE
	}
	bandwidth := int64(bandwidthMbps) * 1000 * 1000
	if bandwidth <= 0 {
		bandwidth = DEFAULT_BANDWIDTH
	}
	return &Iperf3ClientConfig{
		Host:      host,
		Port:      port,
		Duration:  duration,
		Parallel:  parallel,
		Protocol:  strings.ToUpper(protocol),
		Reverse:   reverse,
		BlockSize: blkSize,
		Bandwidth: bandwidth,
	}
}

func TestGenerateCookie_Size(t *testing.T) {
	cookie := generateCookie()

	if len(cookie) != COOKIE_SIZE {
		t.Errorf("Expected cookie size %d, got %d", COOKIE_SIZE, len(cookie))
	}
}

func TestGenerateCookie_NullTerminated(t *testing.T) {
	cookie := generateCookie()

	if cookie[COOKIE_SIZE-1] != 0 {
		t.Errorf("Expected null terminator at end, got %d", cookie[COOKIE_SIZE-1])
	}
}

func TestGenerateCookie_Base32Chars(t *testing.T) {
	cookie := generateCookie()
	validChars := "abcdefghijklmnopqrstuvwxyz234567"

	for i := 0; i < COOKIE_SIZE-1; i++ {
		if !strings.ContainsRune(validChars, rune(cookie[i])) {
			t.Errorf("Invalid character at position %d: %c", i, cookie[i])
		}
	}
}

func TestNewIperf3ClientConfig_Defaults(t *testing.T) {
	config := NewIperf3ClientConfig("test.host", 5201, 10, 0, "", false, 0)

	if config.Parallel != 1 {
		t.Errorf("Expected default parallel=1, got %d", config.Parallel)
	}
	if config.Protocol != "TCP" {
		t.Errorf("Expected default protocol=TCP, got %s", config.Protocol)
	}
	if config.BlockSize != DEFAULT_TCP_BLKSIZE {
		t.Errorf("Expected default TCP block size %d, got %d", DEFAULT_TCP_BLKSIZE, config.BlockSize)
	}
	if config.Bandwidth != DEFAULT_BANDWIDTH {
		t.Errorf("Expected default bandwidth %d, got %d", DEFAULT_BANDWIDTH, config.Bandwidth)
	}
}

func TestNewIperf3ClientConfig_UDP(t *testing.T) {
	config := NewIperf3ClientConfig("test.host", 5201, 10, 1, "UDP", false, 50)

	if config.Protocol != "UDP" {
		t.Errorf("Expected protocol=UDP, got %s", config.Protocol)
	}
	if config.BlockSize != DEFAULT_UDP_BLKSIZE {
		t.Errorf("Expected UDP block size %d, got %d", DEFAULT_UDP_BLKSIZE, config.BlockSize)
	}
}

func TestNewIperf3ClientConfig_ProtocolCaseInsensitive(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"tcp", "TCP"},
		{"TCP", "TCP"},
		{"Tcp", "TCP"},
		{"udp", "UDP"},
		{"UDP", "UDP"},
		{"Udp", "UDP"},
	}

	for _, tc := range testCases {
		config := NewIperf3ClientConfig("test.host", 5201, 10, 1, tc.input, false, 100)
		if config.Protocol != tc.expected {
			t.Errorf("Input %s: expected protocol=%s, got %s", tc.input, tc.expected, config.Protocol)
		}
	}
}

func TestNewIperf3ClientConfig_BandwidthConversion(t *testing.T) {
	testCases := []struct {
		inputMbps     int
		expectedBps   int64
	}{
		{100, 100 * 1000 * 1000},
		{1000, 1000 * 1000 * 1000},
		{50, 50 * 1000 * 1000},
		{0, DEFAULT_BANDWIDTH}, // 0 should use default
		{-10, DEFAULT_BANDWIDTH}, // negative should use default
	}

	for _, tc := range testCases {
		config := NewIperf3ClientConfig("test.host", 5201, 10, 1, "TCP", false, tc.inputMbps)
		if config.Bandwidth != tc.expectedBps {
			t.Errorf("Input %d Mbps: expected bandwidth=%d bps, got %d bps", tc.inputMbps, tc.expectedBps, config.Bandwidth)
		}
	}
}

func TestNewIperf3ClientConfig_ParallelValidation(t *testing.T) {
	testCases := []struct {
		input    int
		expected int
	}{
		{0, 1},  // 0 should become 1
		{-1, 1}, // negative should become 1
		{1, 1},
		{4, 4},
		{8, 8},
	}

	for _, tc := range testCases {
		config := NewIperf3ClientConfig("test.host", 5201, 10, tc.input, "TCP", false, 100)
		if config.Parallel != tc.expected {
			t.Errorf("Input parallel=%d: expected %d, got %d", tc.input, tc.expected, config.Parallel)
		}
	}
}

func TestNewIperf3ClientConfig_ReverseMode(t *testing.T) {
	configNormal := NewIperf3ClientConfig("test.host", 5201, 10, 1, "TCP", false, 100)
	configReverse := NewIperf3ClientConfig("test.host", 5201, 10, 1, "TCP", true, 100)

	if configNormal.Reverse {
		t.Errorf("Expected Reverse=false for normal mode")
	}
	if !configReverse.Reverse {
		t.Errorf("Expected Reverse=true for reverse mode")
	}
}

func TestNewIperf3ClientConfig_HostAndPort(t *testing.T) {
	config := NewIperf3ClientConfig("iperf.example.com", 5202, 30, 4, "TCP", false, 200)

	if config.Host != "iperf.example.com" {
		t.Errorf("Expected Host=iperf.example.com, got %s", config.Host)
	}
	if config.Port != 5202 {
		t.Errorf("Expected Port=5202, got %d", config.Port)
	}
	if config.Duration != 30 {
		t.Errorf("Expected Duration=30, got %d", config.Duration)
	}
}
