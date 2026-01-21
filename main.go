package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	mathrand "math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/tcaine/twamp"
)

// TWAMP test port range (perfSONAR default)
const (
	twampPortMin = 18762 // Leave 18760-18761 for receiver
	twampPortMax = 19960
)

// ErrorEstimateInfo contains parsed Error Estimate field information (RFC 4656/5357)
type ErrorEstimateInfo struct {
	Synced       bool    // S-bit: clock is synchronized to UTC
	Unavailable  bool    // Z-bit: timestamp not available (error is infinite)
	Scale        uint8   // 6-bit scale factor
	Multiplier   uint8   // 8-bit multiplier
	ErrorSeconds float64 // Calculated error: Multiplier × 2^(-Scale)
}

// parseErrorEstimate extracts S, Z, Scale, and Multiplier from the 16-bit Error Estimate field
// Format (RFC 4656 Section 4.1.2):
//   Bit 15: S (Synchronized) - 1 if clock is synced to UTC via external source
//   Bit 14: Z (Zero) - 1 if timestamp is not available
//   Bits 8-13: Scale (6-bit unsigned)
//   Bits 0-7: Multiplier (8-bit unsigned)
// Error in seconds = Multiplier × 2^(-Scale)
func parseErrorEstimate(ee uint16) ErrorEstimateInfo {
	info := ErrorEstimateInfo{
		Synced:      (ee>>15)&1 == 1,
		Unavailable: (ee>>14)&1 == 1,
		Scale:       uint8((ee >> 8) & 0x3F), // bits 8-13 (6 bits)
		Multiplier:  uint8(ee & 0xFF),        // bits 0-7 (8 bits)
	}

	if info.Unavailable || info.Multiplier == 0 {
		info.ErrorSeconds = -1 // Infinite/unavailable
	} else {
		// Error = Multiplier × 2^(-Scale)
		info.ErrorSeconds = float64(info.Multiplier) * math.Pow(2, -float64(info.Scale))
	}

	return info
}

// API Version
const API_VERSION = "2.2.0"

// iperf3 Protocol Constants
const (
	// Protocol States
	TEST_START       = 1
	TEST_RUNNING     = 2
	TEST_END         = 4
	PARAM_EXCHANGE   = 9
	CREATE_STREAMS   = 10
	EXCHANGE_RESULTS = 13
	DISPLAY_RESULTS  = 14
	IPERF_START      = 15
	IPERF_DONE       = 16
	ACCESS_DENIED    = -1
	SERVER_ERROR     = -2

	// Protocol Constants
	COOKIE_SIZE       = 37
	DEFAULT_TCP_BLKSIZE = 128 * 1024 // 128KB
	DEFAULT_UDP_BLKSIZE = 1460
)

// iperf3 Client
type Iperf3Client struct {
	Host       string
	Port       int
	Duration   int
	Parallel   int
	Protocol   string
	Reverse    bool
	BlockSize  int
	Bandwidth  int64 // Bandwidth limit in bits per second

	controlConn net.Conn
	cookie      []byte
	streams     []net.Conn

	mu sync.Mutex
}

const DEFAULT_BANDWIDTH = 100 * 1000 * 1000 // 100 Mbit/s default

// iperf3 Test Parameters (sent to server)
type Iperf3Params struct {
	TCP          bool   `json:"tcp"`
	UDP          bool   `json:"udp,omitempty"`
	Omit         int    `json:"omit"`
	Time         int    `json:"time"`
	Num          int    `json:"num"`
	BlockCount   int    `json:"blockcount"`
	Parallel     int    `json:"parallel"`
	Len          int    `json:"len"`
	PacingTimer  int    `json:"pacing_timer"`
	ClientVer    string `json:"client_version"`
	Reverse      int    `json:"reverse,omitempty"`
}

// iperf3 Test Results
type Iperf3Result struct {
	Server        string  `json:"server"`
	Port          int     `json:"port"`
	Protocol      string  `json:"protocol"`
	Duration      float64 `json:"duration_sec"`
	SentBytes     int64   `json:"sent_bytes"`
	ReceivedBytes int64   `json:"received_bytes,omitempty"`
	BandwidthMbps float64 `json:"bandwidth_mbps"`
	Retransmits   int     `json:"retransmits,omitempty"`
}

// Generate random cookie (iperf3 format: 36 chars from base32 + null terminator)
func generateCookie() []byte {
	const chars = "abcdefghijklmnopqrstuvwxyz234567"
	cookie := make([]byte, COOKIE_SIZE)
	for i := 0; i < COOKIE_SIZE-1; i++ {
		b := make([]byte, 1)
		rand.Read(b)
		cookie[i] = chars[int(b[0])%len(chars)]
	}
	cookie[COOKIE_SIZE-1] = 0 // null terminator
	return cookie
}

// Create new iperf3 client
func NewIperf3Client(host string, port, duration, parallel int, protocol string, reverse bool, bandwidthMbps int) *Iperf3Client {
	if parallel < 1 {
		parallel = 1
	}
	blkSize := DEFAULT_TCP_BLKSIZE
	if strings.ToUpper(protocol) == "UDP" {
		blkSize = DEFAULT_UDP_BLKSIZE
	}
	// Convert Mbps to bps, use default if not specified
	bandwidth := int64(bandwidthMbps) * 1000 * 1000
	if bandwidth <= 0 {
		bandwidth = DEFAULT_BANDWIDTH
	}
	return &Iperf3Client{
		Host:      host,
		Port:      port,
		Duration:  duration,
		Parallel:  parallel,
		Protocol:  strings.ToUpper(protocol),
		Reverse:   reverse,
		BlockSize: blkSize,
		Bandwidth: bandwidth,
		cookie:    generateCookie(),
		streams:   make([]net.Conn, 0),
	}
}

// Read state byte from control connection
func (c *Iperf3Client) readState() (int8, error) {
	buf := make([]byte, 1)
	_, err := io.ReadFull(c.controlConn, buf)
	if err != nil {
		return 0, err
	}
	return int8(buf[0]), nil
}

// Write state byte to control connection
func (c *Iperf3Client) writeState(state int8) error {
	_, err := c.controlConn.Write([]byte{byte(state)})
	return err
}

// Read JSON message from control connection
func (c *Iperf3Client) readJSON(v interface{}) error {
	// Read 4-byte length (big endian)
	lenBuf := make([]byte, 4)
	_, err := io.ReadFull(c.controlConn, lenBuf)
	if err != nil {
		return fmt.Errorf("read length: %w", err)
	}
	length := binary.BigEndian.Uint32(lenBuf)

	if length == 0 || length > 1024*1024 {
		return fmt.Errorf("invalid JSON length: %d", length)
	}

	// Read JSON data
	jsonBuf := make([]byte, length)
	_, err = io.ReadFull(c.controlConn, jsonBuf)
	if err != nil {
		return fmt.Errorf("read JSON: %w", err)
	}

	return json.Unmarshal(jsonBuf, v)
}

// Write JSON message to control connection
func (c *Iperf3Client) writeJSON(v interface{}) error {
	jsonData, err := json.Marshal(v)
	if err != nil {
		return err
	}

	// Write 4-byte length (big endian)
	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(jsonData)))

	_, err = c.controlConn.Write(lenBuf)
	if err != nil {
		return err
	}

	_, err = c.controlConn.Write(jsonData)
	return err
}

// Connect to iperf3 server
func (c *Iperf3Client) Connect() error {
	target := fmt.Sprintf("%s:%d", c.Host, c.Port)

	conn, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		return fmt.Errorf("connect to %s failed: %w", target, err)
	}
	c.controlConn = conn

	// Set TCP_NODELAY for control connection
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetNoDelay(true)
	}

	// Send cookie (37 bytes including null terminator)
	_, err = c.controlConn.Write(c.cookie)
	if err != nil {
		c.controlConn.Close()
		return fmt.Errorf("send cookie failed: %w", err)
	}

	log.Printf("iperf3: Connected to %s, cookie sent (%d bytes)", target, len(c.cookie))
	return nil
}

// Exchange parameters with server
func (c *Iperf3Client) ExchangeParams() error {
	// Wait for PARAM_EXCHANGE state
	state, err := c.readState()
	if err != nil {
		return fmt.Errorf("read initial state: %w", err)
	}

	if state == ACCESS_DENIED {
		return fmt.Errorf("server denied access")
	}
	if state == SERVER_ERROR {
		return fmt.Errorf("server error")
	}
	if state != PARAM_EXCHANGE {
		return fmt.Errorf("unexpected state %d, expected PARAM_EXCHANGE(%d)", state, PARAM_EXCHANGE)
	}

	// Send test parameters
	params := Iperf3Params{
		TCP:         c.Protocol == "TCP",
		UDP:         c.Protocol == "UDP",
		Omit:        0,
		Time:        c.Duration,
		Num:         0,
		BlockCount:  0,
		Parallel:    c.Parallel,
		Len:         c.BlockSize,
		PacingTimer: 1000,
		ClientVer:   "3.16",
	}
	if c.Reverse {
		params.Reverse = 1
	}

	err = c.writeJSON(params)
	if err != nil {
		return fmt.Errorf("send params: %w", err)
	}

	log.Printf("iperf3: Parameters exchanged (duration=%ds, parallel=%d, blksize=%d)",
		c.Duration, c.Parallel, c.BlockSize)
	return nil
}

// Create data streams
func (c *Iperf3Client) CreateStreams() error {
	// Wait for CREATE_STREAMS state
	state, err := c.readState()
	if err != nil {
		return fmt.Errorf("read state: %w", err)
	}
	if state != CREATE_STREAMS {
		return fmt.Errorf("unexpected state %d, expected CREATE_STREAMS(%d)", state, CREATE_STREAMS)
	}

	target := fmt.Sprintf("%s:%d", c.Host, c.Port)

	for i := 0; i < c.Parallel; i++ {
		var conn net.Conn
		var err error

		if c.Protocol == "UDP" {
			conn, err = net.DialTimeout("udp", target, 5*time.Second)
		} else {
			conn, err = net.DialTimeout("tcp", target, 5*time.Second)
		}
		if err != nil {
			return fmt.Errorf("create stream %d: %w", i, err)
		}

		// Send cookie to identify this stream
		_, err = conn.Write(c.cookie)
		if err != nil {
			conn.Close()
			return fmt.Errorf("send cookie on stream %d: %w", i, err)
		}

		c.streams = append(c.streams, conn)
	}

	log.Printf("iperf3: Created %d data streams", len(c.streams))
	return nil
}

// Run the bandwidth test
func (c *Iperf3Client) RunTest() (*Iperf3Result, error) {
	// Wait for TEST_START
	state, err := c.readState()
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	if state != TEST_START {
		return nil, fmt.Errorf("unexpected state %d, expected TEST_START(%d)", state, TEST_START)
	}

	// Wait for TEST_RUNNING
	state, err = c.readState()
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	if state != TEST_RUNNING {
		return nil, fmt.Errorf("unexpected state %d, expected TEST_RUNNING(%d)", state, TEST_RUNNING)
	}

	log.Printf("iperf3: Test running for %d seconds...", c.Duration)

	result := &Iperf3Result{
		Server:   c.Host,
		Port:     c.Port,
		Protocol: c.Protocol,
	}

	start := time.Now()
	deadline := start.Add(time.Duration(c.Duration) * time.Second)

	if c.Reverse {
		// Receive mode: read data from streams
		result.ReceivedBytes = c.receiveData(deadline)
	} else {
		// Send mode: write data to streams
		result.SentBytes = c.sendData(deadline)
	}

	result.Duration = time.Since(start).Seconds()

	// Calculate bandwidth
	totalBytes := result.SentBytes
	if c.Reverse {
		totalBytes = result.ReceivedBytes
	}
	result.BandwidthMbps = (float64(totalBytes) * 8) / (result.Duration * 1e6)

	// Signal TEST_END
	c.writeState(TEST_END)

	// Wait for EXCHANGE_RESULTS
	state, err = c.readState()
	if err != nil {
		log.Printf("iperf3: Warning - could not read EXCHANGE_RESULTS state: %v", err)
	}

	// Exchange results (simplified - just acknowledge)
	if state == EXCHANGE_RESULTS {
		// Send empty results
		c.writeJSON(map[string]interface{}{})

		// Read server results (ignore for now)
		var serverResults map[string]interface{}
		c.readJSON(&serverResults)
	}

	// Wait for DISPLAY_RESULTS
	state, _ = c.readState()
	if state == DISPLAY_RESULTS {
		c.writeState(IPERF_DONE)
	}

	log.Printf("iperf3: Test completed - %.2f Mbps", result.BandwidthMbps)
	return result, nil
}

// Send data on all streams
func (c *Iperf3Client) sendData(deadline time.Time) int64 {
	var totalBytes int64
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Calculate target bytes per second per stream
	targetBytesPerSec := float64(c.Bandwidth) / float64(c.Parallel) / 8.0

	// Use reasonable chunk size (64KB for good throughput)
	chunkSize := 64 * 1024
	if chunkSize > c.BlockSize {
		chunkSize = c.BlockSize
	}

	buffer := make([]byte, chunkSize)
	rand.Read(buffer)

	for _, stream := range c.streams {
		wg.Add(1)
		go func(conn net.Conn) {
			defer wg.Done()

			var streamBytes int64
			startTime := time.Now()
			conn.SetWriteDeadline(deadline)

			for time.Now().Before(deadline) {
				n, err := conn.Write(buffer)
				if err != nil {
					break
				}
				streamBytes += int64(n)

				// Token bucket pacing: calculate expected bytes vs actual
				elapsed := time.Since(startTime).Seconds()
				expectedBytes := targetBytesPerSec * elapsed
				actualBytes := float64(streamBytes)

				// If we're ahead of schedule, sleep to catch up
				if actualBytes > expectedBytes {
					excessBytes := actualBytes - expectedBytes
					sleepTime := time.Duration(excessBytes / targetBytesPerSec * float64(time.Second))
					if sleepTime > 0 && sleepTime < 100*time.Millisecond {
						time.Sleep(sleepTime)
					}
				}
			}

			mu.Lock()
			totalBytes += streamBytes
			mu.Unlock()
		}(stream)
	}

	wg.Wait()
	return totalBytes
}

// Receive data from all streams
func (c *Iperf3Client) receiveData(deadline time.Time) int64 {
	var totalBytes int64
	var wg sync.WaitGroup
	var mu sync.Mutex

	buffer := make([]byte, c.BlockSize)

	for _, stream := range c.streams {
		wg.Add(1)
		go func(conn net.Conn) {
			defer wg.Done()

			var streamBytes int64
			conn.SetReadDeadline(deadline)

			for time.Now().Before(deadline) {
				n, err := conn.Read(buffer)
				if err != nil {
					break
				}
				streamBytes += int64(n)
			}

			mu.Lock()
			totalBytes += streamBytes
			mu.Unlock()
		}(stream)
	}

	wg.Wait()
	return totalBytes
}

// Close all connections
func (c *Iperf3Client) Close() {
	for _, stream := range c.streams {
		stream.Close()
	}
	if c.controlConn != nil {
		c.controlConn.Close()
	}
}

// Run complete iperf3 test
func iperf3Test(host string, port, duration, parallel int, protocol string, reverse bool, bandwidthMbps int) (*Iperf3Result, error) {
	client := NewIperf3Client(host, port, duration, parallel, protocol, reverse, bandwidthMbps)
	defer client.Close()

	if err := client.Connect(); err != nil {
		return nil, err
	}

	if err := client.ExchangeParams(); err != nil {
		return nil, err
	}

	if err := client.CreateStreams(); err != nil {
		return nil, err
	}

	return client.RunTest()
}

type RunRequest struct {
	ServerHost string `json:"server_host"`
	ServerPort int    `json:"server_port"`
	Duration   int    `json:"duration"`
	Parallel   int    `json:"parallel"`
	Count      int    `json:"count"`
	Padding    int    `json:"padding"`
	Protocol   string `json:"protocol"`
	Reverse    bool   `json:"reverse"`
	Bandwidth  int    `json:"bandwidth"` // Bandwidth limit in Mbit/s (default: 100)
}

type ApiResponse struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
}

func iperfClientRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Defaults
	if req.ServerPort == 0 {
		req.ServerPort = 5201
	}
	if req.Duration == 0 {
		req.Duration = 5
	}
	if req.Parallel == 0 {
		req.Parallel = 1
	}
	if req.Protocol == "" {
		req.Protocol = "TCP"
	}
	if req.Bandwidth == 0 {
		req.Bandwidth = 100 // Default: 100 Mbit/s
	}

	log.Printf("iperf3 test: %s:%d (%s, %ds, %d streams, reverse=%v, bandwidth=%dM)",
		req.ServerHost, req.ServerPort, req.Protocol, req.Duration, req.Parallel, req.Reverse, req.Bandwidth)

	// Run native iperf3 test
	result, err := iperf3Test(req.ServerHost, req.ServerPort, req.Duration, req.Parallel, req.Protocol, req.Reverse, req.Bandwidth)

	if err != nil {
		jsonResponse(w, ApiResponse{
			Status: "error",
			Error:  err.Error(),
		}, http.StatusInternalServerError)
		return
	}

	// Return results
	data := map[string]interface{}{
		"server":         result.Server,
		"port":           result.Port,
		"protocol":       result.Protocol,
		"duration_sec":   result.Duration,
		"bandwidth_mbps": result.BandwidthMbps,
	}

	if req.Reverse {
		data["received_bytes"] = result.ReceivedBytes
	} else {
		data["sent_bytes"] = result.SentBytes
	}

	if result.Retransmits > 0 {
		data["retransmits"] = result.Retransmits
	}

	jsonResponse(w, ApiResponse{
		Status: "ok",
		Data:   data,
	}, http.StatusOK)
}

func twampClientRun(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Defaults
	if req.ServerPort == 0 {
		req.ServerPort = 862
	}
	if req.Count == 0 {
		req.Count = 10
	}
	// Note: padding defaults to 0, which matches server's 41-byte response

	target := fmt.Sprintf("%s:%d", req.ServerHost, req.ServerPort)
	log.Printf("TWAMP test: %s (%d probes)", target, req.Count)

	client := twamp.NewClient()
	conn, err := client.Connect(target)
	if err != nil {
		jsonResponse(w, ApiResponse{
			Status: "error",
			Error:  fmt.Sprintf("Connect failed: %v", err),
		}, http.StatusInternalServerError)
		return
	}
	defer conn.Close()

	// Use random port in perfSONAR's allowed range to avoid conflicts
	senderPort := twampPortMin + mathrand.Intn(twampPortMax-twampPortMin)
	// Calculate Error Estimate based on actual NTP sync status and clock precision
	errorEstimate := calculateErrorEstimate()
	sessionConfig := twamp.TwampSessionConfig{
		ReceiverPort:  18760,         // Use port in perfSONAR's allowed range
		SenderPort:    senderPort,    // Random port in allowed range
		Timeout:       5,
		Padding:       req.Padding,
		TOS:           0,             // Best Effort (default) - EF not supported by all servers
		ErrorEstimate: errorEstimate, // Calculated from adjtimex (NTP sync + esterror)
	}
	session, err := conn.CreateSession(sessionConfig)
	if err != nil {
		jsonResponse(w, ApiResponse{
			Status: "error",
			Error:  fmt.Sprintf("Session failed: %v", err),
		}, http.StatusInternalServerError)
		return
	}
	defer session.Stop()

	test, err := session.CreateTest()
	if err != nil {
		jsonResponse(w, ApiResponse{
			Status: "error",
			Error:  fmt.Sprintf("Test creation failed: %v", err),
		}, http.StatusInternalServerError)
		return
	}

	// Capture test port information
	localAddr := test.GetConnection().LocalAddr().String()
	remoteAddr := test.GetConnection().RemoteAddr().String()
	log.Printf("TWAMP test created, remote: %s, local: %s", remoteAddr, localAddr)

	results, err := test.RunMultiple(uint64(req.Count), nil, time.Second, nil)
	if err != nil {
		jsonResponse(w, ApiResponse{
			Status: "error",
			Error:  fmt.Sprintf("Test run failed: %v", err),
		}, http.StatusInternalServerError)
		return
	}

	stat := results.Stat

	// Calculate raw forward and reverse delays (affected by clock offset)
	// Raw forward = T2 - T1 = actual_forward + clock_offset
	// Raw reverse = T4 - T3 = actual_reverse - clock_offset
	// Per-packet offset = (raw_forward - raw_reverse) / 2
	// Per-packet corrected: forward = raw_forward - offset, reverse = raw_reverse + offset

	var fwdMin, fwdMax, fwdTotal time.Duration
	var revMin, revMax, revTotal time.Duration
	var fwdCorrMin, fwdCorrMax, fwdCorrTotal time.Duration
	var revCorrMin, revCorrMax, revCorrTotal time.Duration
	var offsetTotal time.Duration
	var turnaroundMin, turnaroundMax, turnaroundTotal time.Duration // Reflector processing time (T3-T2)
	var networkRttMin, networkRttMax, networkRttTotal time.Duration // Corrected RTT without turnaround
	var networkRttSquaredTotal float64                              // For stddev calculation
	validCount := 0

	// RFC 3393 IPDV (IP Packet Delay Variation) tracking
	// IPDV(i) = D(i) - D(i-1) where D is one-way delay
	// Clock offset cancels out: IPDV_fwd = (T2[i]-T1[i]) - (T2[i-1]-T1[i-1])
	var prevRawFwd, prevRawRev time.Duration
	var fwdIPDVMin, fwdIPDVMax, fwdIPDVTotal time.Duration
	var revIPDVMin, revIPDVMax, revIPDVTotal time.Duration
	var fwdIPDVAbsTotal, revIPDVAbsTotal time.Duration // For mean absolute IPDV
	var ipdvCount int

	// RFC 3550 Jitter (exponentially weighted mean absolute deviation)
	// J(i) = J(i-1) + (|D(i-1,i)| - J(i-1)) / 16
	var fwdJitterRFC3550, revJitterRFC3550 float64

	// Hop count tracking (from TTL values)
	// Forward hops: 255 - SenderTTL (sender sends with TTL=255, reflector reports what it received)
	// Reverse hops: InitialTTL - ReceivedTTL (need to estimate InitialTTL from received value)
	var fwdHopsMin, fwdHopsMax, fwdHopsTotal int
	var revHopsMin, revHopsMax, revHopsTotal int
	var hopsCount int

	// Check local clock synchronization via adjtimex syscall
	senderSynced := checkNTPSync()

	// Parse Error Estimate fields from both sender and reflector
	var senderErrorInfo, reflectorErrorInfo ErrorEstimateInfo
	var senderErrorRaw, reflectorErrorRaw uint16
	reflectorSynced := false

	for _, r := range results.Results {
		if r.FinishedTimestamp.IsZero() {
			continue // Skip lost packets
		}
		rawFwd := r.ReceiveTimestamp.Sub(r.SenderTimestamp)
		rawRev := r.FinishedTimestamp.Sub(r.Timestamp)
		// Reflector turnaround time (T3 - T2) - processing time at reflector
		turnaround := r.Timestamp.Sub(r.ReceiveTimestamp)

		// Per-packet offset correction (removes clock drift from jitter)
		offset := (rawFwd - rawRev) / 2
		fwdCorr := rawFwd - offset // = (rawFwd + rawRev) / 2 = RTT / 2
		revCorr := rawRev + offset // = (rawFwd + rawRev) / 2 = RTT / 2

		// Network RTT = rawFwd + rawRev = (T2-T1) + (T4-T3) = (T4-T1) - (T3-T2)
		// This is the true network round-trip time without reflector processing delay
		networkRtt := rawFwd + rawRev

		// Parse full Error Estimate fields (only need to do this once, values should be consistent)
		if validCount == 0 {
			senderErrorRaw = r.SenderErrorEstimate
			reflectorErrorRaw = r.ErrorEstimate
			senderErrorInfo = parseErrorEstimate(senderErrorRaw)
			reflectorErrorInfo = parseErrorEstimate(reflectorErrorRaw)
			reflectorSynced = reflectorErrorInfo.Synced

			log.Printf("TWAMP Error Estimates - Sender: 0x%04X (S=%v, Z=%v, Scale=%d, Mult=%d, Err=%.9fs), Reflector: 0x%04X (S=%v, Z=%v, Scale=%d, Mult=%d, Err=%.9fs)",
				senderErrorRaw, senderErrorInfo.Synced, senderErrorInfo.Unavailable, senderErrorInfo.Scale, senderErrorInfo.Multiplier, senderErrorInfo.ErrorSeconds,
				reflectorErrorRaw, reflectorErrorInfo.Synced, reflectorErrorInfo.Unavailable, reflectorErrorInfo.Scale, reflectorErrorInfo.Multiplier, reflectorErrorInfo.ErrorSeconds)
		}

		// Calculate hop counts from TTL values
		// Forward: Sender sends with TTL=255, SenderTTL is what reflector received
		if r.SenderTTL > 0 && r.SenderTTL <= 255 {
			fwdHops := 255 - int(r.SenderTTL)
			if hopsCount == 0 {
				fwdHopsMin, fwdHopsMax = fwdHops, fwdHops
			} else {
				if fwdHops < fwdHopsMin {
					fwdHopsMin = fwdHops
				}
				if fwdHops > fwdHopsMax {
					fwdHopsMax = fwdHops
				}
			}
			fwdHopsTotal += fwdHops
		}

		// Reverse: Estimate initial TTL from received value
		// Common initial TTLs: 64 (Linux), 128 (Windows), 255 (Cisco/Network devices)
		if r.ReceivedTTL > 0 {
			var initialTTL int
			if r.ReceivedTTL > 128 {
				initialTTL = 255
			} else if r.ReceivedTTL > 64 {
				initialTTL = 128
			} else {
				initialTTL = 64
			}
			revHops := initialTTL - r.ReceivedTTL
			if hopsCount == 0 {
				revHopsMin, revHopsMax = revHops, revHops
			} else {
				if revHops < revHopsMin {
					revHopsMin = revHops
				}
				if revHops > revHopsMax {
					revHopsMax = revHops
				}
			}
			revHopsTotal += revHops
		}
		hopsCount++

		// Calculate IPDV for consecutive packets (RFC 3393)
		// This cancels out clock offset!
		if validCount > 0 {
			fwdIPDV := rawFwd - prevRawFwd // (T2[i]-T1[i]) - (T2[i-1]-T1[i-1])
			revIPDV := rawRev - prevRawRev // (T4[i]-T3[i]) - (T4[i-1]-T3[i-1])

			// Track IPDV statistics
			if ipdvCount == 0 {
				fwdIPDVMin, fwdIPDVMax = fwdIPDV, fwdIPDV
				revIPDVMin, revIPDVMax = revIPDV, revIPDV
			} else {
				if fwdIPDV < fwdIPDVMin {
					fwdIPDVMin = fwdIPDV
				}
				if fwdIPDV > fwdIPDVMax {
					fwdIPDVMax = fwdIPDV
				}
				if revIPDV < revIPDVMin {
					revIPDVMin = revIPDV
				}
				if revIPDV > revIPDVMax {
					revIPDVMax = revIPDV
				}
			}
			fwdIPDVTotal += fwdIPDV
			revIPDVTotal += revIPDV

			// Absolute IPDV for mean absolute deviation
			if fwdIPDV < 0 {
				fwdIPDVAbsTotal += -fwdIPDV
			} else {
				fwdIPDVAbsTotal += fwdIPDV
			}
			if revIPDV < 0 {
				revIPDVAbsTotal += -revIPDV
			} else {
				revIPDVAbsTotal += revIPDV
			}

			// RFC 3550 exponential smoothing: J = J + (|D| - J) / 16
			fwdIPDVAbsNs := math.Abs(float64(fwdIPDV.Nanoseconds()))
			revIPDVAbsNs := math.Abs(float64(revIPDV.Nanoseconds()))
			fwdJitterRFC3550 = fwdJitterRFC3550 + (fwdIPDVAbsNs-fwdJitterRFC3550)/16.0
			revJitterRFC3550 = revJitterRFC3550 + (revIPDVAbsNs-revJitterRFC3550)/16.0

			ipdvCount++
		}

		// Store for next iteration
		prevRawFwd = rawFwd
		prevRawRev = rawRev

		if validCount == 0 {
			fwdMin, fwdMax = rawFwd, rawFwd
			revMin, revMax = rawRev, rawRev
			fwdCorrMin, fwdCorrMax = fwdCorr, fwdCorr
			revCorrMin, revCorrMax = revCorr, revCorr
			turnaroundMin, turnaroundMax = turnaround, turnaround
			networkRttMin, networkRttMax = networkRtt, networkRtt
		} else {
			if rawFwd < fwdMin {
				fwdMin = rawFwd
			}
			if rawFwd > fwdMax {
				fwdMax = rawFwd
			}
			if rawRev < revMin {
				revMin = rawRev
			}
			if rawRev > revMax {
				revMax = rawRev
			}
			if fwdCorr < fwdCorrMin {
				fwdCorrMin = fwdCorr
			}
			if fwdCorr > fwdCorrMax {
				fwdCorrMax = fwdCorr
			}
			if revCorr < revCorrMin {
				revCorrMin = revCorr
			}
			if revCorr > revCorrMax {
				revCorrMax = revCorr
			}
			if turnaround < turnaroundMin {
				turnaroundMin = turnaround
			}
			if turnaround > turnaroundMax {
				turnaroundMax = turnaround
			}
			if networkRtt < networkRttMin {
				networkRttMin = networkRtt
			}
			if networkRtt > networkRttMax {
				networkRttMax = networkRtt
			}
		}
		fwdTotal += rawFwd
		revTotal += rawRev
		turnaroundTotal += turnaround
		fwdCorrTotal += fwdCorr
		revCorrTotal += revCorr
		offsetTotal += offset
		networkRttTotal += networkRtt
		networkRttSquaredTotal += float64(networkRtt.Nanoseconds()) * float64(networkRtt.Nanoseconds())
		validCount++
	}

	var fwdAvg, revAvg, offsetAvg time.Duration
	var fwdCorrAvg, revCorrAvg time.Duration
	var turnaroundAvg time.Duration
	var networkRttAvg time.Duration
	var networkRttStdDev time.Duration
	var fwdIPDVAvg, revIPDVAvg time.Duration             // Mean IPDV (can be negative)
	var fwdIPDVAbsAvg, revIPDVAbsAvg time.Duration       // Mean Absolute IPDV
	if validCount > 0 {
		fwdAvg = fwdTotal / time.Duration(validCount)
		revAvg = revTotal / time.Duration(validCount)
		fwdCorrAvg = fwdCorrTotal / time.Duration(validCount)
		revCorrAvg = revCorrTotal / time.Duration(validCount)
		offsetAvg = offsetTotal / time.Duration(validCount)
		turnaroundAvg = turnaroundTotal / time.Duration(validCount)
		networkRttAvg = networkRttTotal / time.Duration(validCount)

		// Calculate stddev for network RTT: sqrt(E[X²] - E[X]²)
		meanNs := float64(networkRttAvg.Nanoseconds())
		meanSquaredNs := networkRttSquaredTotal / float64(validCount)
		varianceNs := meanSquaredNs - (meanNs * meanNs)
		if varianceNs > 0 {
			networkRttStdDev = time.Duration(math.Sqrt(varianceNs))
		}
	}

	// Calculate IPDV averages
	if ipdvCount > 0 {
		fwdIPDVAvg = fwdIPDVTotal / time.Duration(ipdvCount)
		revIPDVAvg = revIPDVTotal / time.Duration(ipdvCount)
		fwdIPDVAbsAvg = fwdIPDVAbsTotal / time.Duration(ipdvCount)
		revIPDVAbsAvg = revIPDVAbsTotal / time.Duration(ipdvCount)
	}

	// Calculate hop averages
	var fwdHopsAvg, revHopsAvg float64
	if hopsCount > 0 {
		fwdHopsAvg = float64(fwdHopsTotal) / float64(hopsCount)
		revHopsAvg = float64(revHopsTotal) / float64(hopsCount)
	}

	// Determine sync status
	bothSynced := senderSynced && reflectorSynced

	jsonResponse(w, ApiResponse{
		Status: "ok",
		Data: map[string]interface{}{
			"server":                    req.ServerHost,
			"local_endpoint":            localAddr,
			"remote_endpoint":           remoteAddr,
			"probes":                    req.Count,
			"loss_percent":              stat.Loss,
			// Corrected network RTT: (T4-T1) - (T3-T2) = pure network delay without reflector processing
			"rtt_min_ms":                float64(networkRttMin.Nanoseconds()) / 1e6,
			"rtt_max_ms":                float64(networkRttMax.Nanoseconds()) / 1e6,
			"rtt_avg_ms":                float64(networkRttAvg.Nanoseconds()) / 1e6,
			"rtt_stddev_ms":             float64(networkRttStdDev.Nanoseconds()) / 1e6,
			// Raw RTT from library for reference: T4-T1 (includes reflector processing time)
			"rtt_raw_ms": map[string]float64{
				"min":    float64(stat.Min.Nanoseconds()) / 1e6,
				"max":    float64(stat.Max.Nanoseconds()) / 1e6,
				"avg":    float64(stat.Avg.Nanoseconds()) / 1e6,
				"stddev": float64(stat.StdDev.Nanoseconds()) / 1e6,
			},
			"reflector_turnaround_ms": map[string]float64{
				"min": float64(turnaroundMin.Nanoseconds()) / 1e6,
				"max": float64(turnaroundMax.Nanoseconds()) / 1e6,
				"avg": float64(turnaroundAvg.Nanoseconds()) / 1e6,
			},
			"estimated_clock_offset_ms": float64(offsetAvg.Nanoseconds()) / 1e6,
			"sync_status": map[string]interface{}{
				"sender_synced":    senderSynced,
				"reflector_synced": reflectorSynced,
				"both_synced":      bothSynced,
				"sender_error_estimate": map[string]interface{}{
					"synced":           senderErrorInfo.Synced,
					"unavailable":      senderErrorInfo.Unavailable,
					"scale":            senderErrorInfo.Scale,
					"multiplier":       senderErrorInfo.Multiplier,
					"error_seconds":    senderErrorInfo.ErrorSeconds,
					"error_ms":         senderErrorInfo.ErrorSeconds * 1000,
					"raw_value_hex":    fmt.Sprintf("0x%04X", senderErrorRaw),
				},
				"reflector_error_estimate": map[string]interface{}{
					"synced":           reflectorErrorInfo.Synced,
					"unavailable":      reflectorErrorInfo.Unavailable,
					"scale":            reflectorErrorInfo.Scale,
					"multiplier":       reflectorErrorInfo.Multiplier,
					"error_seconds":    reflectorErrorInfo.ErrorSeconds,
					"error_ms":         reflectorErrorInfo.ErrorSeconds * 1000,
					"raw_value_hex":    fmt.Sprintf("0x%04X", reflectorErrorRaw),
				},
			},
			"forward_delay_raw_ms": map[string]float64{
				"min": float64(fwdMin.Nanoseconds()) / 1e6,
				"max": float64(fwdMax.Nanoseconds()) / 1e6,
				"avg": float64(fwdAvg.Nanoseconds()) / 1e6,
			},
			"forward_delay_corrected_ms": map[string]float64{
				"min": float64(fwdCorrMin.Nanoseconds()) / 1e6,
				"max": float64(fwdCorrMax.Nanoseconds()) / 1e6,
				"avg": float64(fwdCorrAvg.Nanoseconds()) / 1e6,
			},
			// RFC 3393 IPDV (IP Packet Delay Variation) - difference between consecutive packet delays
			// Clock offset cancels out, so this is true one-way delay variation
			"forward_ipdv_ms": map[string]float64{
				"min":      float64(fwdIPDVMin.Nanoseconds()) / 1e6,
				"max":      float64(fwdIPDVMax.Nanoseconds()) / 1e6,
				"avg":      float64(fwdIPDVAvg.Nanoseconds()) / 1e6,
				"mean_abs": float64(fwdIPDVAbsAvg.Nanoseconds()) / 1e6, // Mean Absolute Deviation
			},
			// RFC 3550 Jitter - exponentially smoothed mean absolute IPDV
			"forward_jitter_ms": fwdJitterRFC3550 / 1e6,
			"reverse_delay_raw_ms": map[string]float64{
				"min": float64(revMin.Nanoseconds()) / 1e6,
				"max": float64(revMax.Nanoseconds()) / 1e6,
				"avg": float64(revAvg.Nanoseconds()) / 1e6,
			},
			"reverse_delay_corrected_ms": map[string]float64{
				"min": float64(revCorrMin.Nanoseconds()) / 1e6,
				"max": float64(revCorrMax.Nanoseconds()) / 1e6,
				"avg": float64(revCorrAvg.Nanoseconds()) / 1e6,
			},
			// RFC 3393 IPDV for reverse direction
			"reverse_ipdv_ms": map[string]float64{
				"min":      float64(revIPDVMin.Nanoseconds()) / 1e6,
				"max":      float64(revIPDVMax.Nanoseconds()) / 1e6,
				"avg":      float64(revIPDVAvg.Nanoseconds()) / 1e6,
				"mean_abs": float64(revIPDVAbsAvg.Nanoseconds()) / 1e6,
			},
			// RFC 3550 Jitter for reverse direction
			"reverse_jitter_ms": revJitterRFC3550 / 1e6,
			// Hop counts derived from TTL values
			// Forward: 255 - SenderTTL (sender uses TTL=255)
			// Reverse: EstimatedInitialTTL - ReceivedTTL (initial TTL estimated from received value)
			"hops": map[string]interface{}{
				"forward": map[string]interface{}{
					"min": fwdHopsMin,
					"max": fwdHopsMax,
					"avg": fwdHopsAvg,
				},
				"reverse": map[string]interface{}{
					"min": revHopsMin,
					"max": revHopsMax,
					"avg": revHopsAvg,
				},
			},
		},
	}, http.StatusOK)
}

func jsonResponse(w http.ResponseWriter, resp ApiResponse, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

func getAPIDoc() map[string]interface{} {
	return map[string]interface{}{
		"name":        "Network Test API",
		"version":     API_VERSION,
		"description": "API for network performance testing with native iperf3 protocol support",
		"endpoints": []map[string]interface{}{
			{
				"path":        "/iperf/client/run",
				"method":      "POST",
				"description": "Run an iperf3 bandwidth test (TCP or UDP) to a target iperf3 server",
				"request": map[string]interface{}{
					"content_type": "application/json",
					"body": map[string]interface{}{
						"server_host": map[string]string{
							"type":        "string",
							"required":    "true",
							"description": "iperf3 server hostname or IP",
						},
						"server_port": map[string]string{
							"type":        "integer",
							"required":    "false",
							"default":     "5201",
							"description": "iperf3 server port",
						},
						"duration": map[string]string{
							"type":        "integer",
							"required":    "false",
							"default":     "5",
							"description": "Test duration in seconds",
						},
						"parallel": map[string]string{
							"type":        "integer",
							"required":    "false",
							"default":     "1",
							"description": "Number of parallel streams",
						},
						"protocol": map[string]string{
							"type":        "string",
							"required":    "false",
							"default":     "TCP",
							"description": "Protocol to use: TCP or UDP",
						},
						"reverse": map[string]string{
							"type":        "boolean",
							"required":    "false",
							"default":     "false",
							"description": "Reverse mode (download instead of upload)",
						},
						"bandwidth": map[string]string{
							"type":        "integer",
							"required":    "false",
							"default":     "100",
							"description": "Bandwidth limit in Mbit/s",
						},
					},
				},
				"response": map[string]interface{}{
					"content_type": "application/json",
					"body": map[string]string{
						"server":         "Target server hostname",
						"port":           "Target server port",
						"protocol":       "Protocol used (TCP/UDP)",
						"duration_sec":   "Actual test duration in seconds",
						"sent_bytes":     "Total bytes sent (upload mode)",
						"received_bytes": "Total bytes received (reverse/download mode)",
						"bandwidth_mbps": "Measured bandwidth in Mbps",
					},
				},
				"example": map[string]interface{}{
					"request":  `{"server_host": "iperf.example.com", "server_port": 5201, "duration": 10, "parallel": 4, "protocol": "TCP"}`,
					"response": `{"status": "ok", "data": {"server": "iperf.example.com", "port": 5201, "protocol": "TCP", "duration_sec": 10.0, "sent_bytes": 125000000, "bandwidth_mbps": 100.0}}`,
				},
			},
			{
				"path":        "/twamp/client/run",
				"method":      "POST",
				"description": "Run a TWAMP latency test to measure RTT, one-way delays, jitter, and packet loss. Compatible with perfSONAR twampd.",
				"request": map[string]interface{}{
					"content_type": "application/json",
					"body": map[string]interface{}{
						"server_host": map[string]string{
							"type":        "string",
							"required":    "true",
							"description": "TWAMP server hostname or IP",
						},
						"server_port": map[string]string{
							"type":        "integer",
							"required":    "false",
							"default":     "862",
							"description": "TWAMP control port",
						},
						"count": map[string]string{
							"type":        "integer",
							"required":    "false",
							"default":     "10",
							"description": "Number of test probes to send",
						},
					},
				},
				"response": map[string]interface{}{
					"content_type": "application/json",
					"body": map[string]string{
						"server":                      "Target server hostname",
						"local_endpoint":              "Local test endpoint (IP:port)",
						"remote_endpoint":             "Remote test endpoint (IP:port)",
						"probes":                      "Number of probes sent",
						"loss_percent":                "Packet loss percentage",
						"rtt_min_ms":                  "Minimum RTT in milliseconds",
						"rtt_max_ms":                  "Maximum RTT in milliseconds",
						"rtt_avg_ms":                  "Average RTT in milliseconds",
						"rtt_stddev_ms":               "RTT standard deviation in milliseconds",
						"estimated_clock_offset_ms":   "Estimated clock offset between sender and reflector",
						"sync_status":                 "Clock sync status (sender_synced, reflector_synced, both_synced)",
						"forward_delay_raw_ms":        "Raw forward delay (min, max, avg)",
						"forward_delay_corrected_ms":  "Corrected forward delay (min, max, avg)",
						"forward_jitter_ms":           "Forward path jitter (max - min)",
						"reverse_delay_raw_ms":        "Raw reverse delay (min, max, avg)",
						"reverse_delay_corrected_ms":  "Corrected reverse delay (min, max, avg)",
						"reverse_jitter_ms":           "Reverse path jitter (max - min)",
					},
				},
				"example": map[string]interface{}{
					"request":  `{"server_host": "twamp.example.com", "server_port": 862, "count": 20}`,
					"response": `{"status": "ok", "data": {"server": "twamp.example.com", "local_endpoint": "192.168.1.100:19234", "remote_endpoint": "203.0.113.50:18760", "probes": 20, "loss_percent": 0.0, "rtt_avg_ms": 31.8, "sync_status": {"both_synced": true}, "forward_jitter_ms": 3.7, "reverse_jitter_ms": 3.4}}`,
				},
			},
			{
				"path":        "/health",
				"method":      "GET",
				"description": "Health check endpoint",
				"response": map[string]interface{}{
					"content_type": "application/json",
					"example":      `{"status": "healthy"}`,
				},
			},
		},
	}
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")

	if contentType == "application/json" {
		jsonResponse(w, ApiResponse{
			Status: "ok",
			Data:   getAPIDoc(),
		}, http.StatusOK)
		return
	}

	// HTML Documentation
	htmlTemplate := `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Network Test API - Documentation</title>
    <style>
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, Oxygen, Ubuntu, sans-serif;
            background: #f5f7fa;
            color: #333;
            line-height: 1.6;
        }
        .container {
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
        }
        header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 60px 20px;
            text-align: center;
        }
        header h1 {
            font-size: 2.5rem;
            margin-bottom: 10px;
        }
        header p {
            font-size: 1.2rem;
            opacity: 0.9;
        }
        .version {
            display: inline-block;
            background: rgba(255,255,255,0.2);
            padding: 5px 15px;
            border-radius: 20px;
            margin-top: 15px;
            font-size: 0.9rem;
        }
        nav {
            background: white;
            padding: 15px 20px;
            box-shadow: 0 2px 4px rgba(0,0,0,0.1);
            position: sticky;
            top: 0;
            z-index: 100;
        }
        nav ul {
            list-style: none;
            display: flex;
            gap: 30px;
            justify-content: center;
        }
        nav a {
            color: #667eea;
            text-decoration: none;
            font-weight: 500;
            transition: color 0.2s;
        }
        nav a:hover {
            color: #764ba2;
        }
        .endpoint {
            background: white;
            border-radius: 12px;
            margin: 30px 0;
            box-shadow: 0 4px 6px rgba(0,0,0,0.07);
            overflow: hidden;
        }
        .endpoint-header {
            padding: 20px 25px;
            border-bottom: 1px solid #eee;
            display: flex;
            align-items: center;
            gap: 15px;
        }
        .method {
            padding: 6px 14px;
            border-radius: 6px;
            font-weight: 700;
            font-size: 0.85rem;
            text-transform: uppercase;
        }
        .method-post {
            background: #49cc90;
            color: white;
        }
        .method-get {
            background: #61affe;
            color: white;
        }
        .path {
            font-family: 'Monaco', 'Menlo', monospace;
            font-size: 1.1rem;
            color: #333;
        }
        .endpoint-body {
            padding: 25px;
        }
        .description {
            color: #666;
            margin-bottom: 25px;
            font-size: 1.05rem;
        }
        .section-title {
            font-size: 0.85rem;
            text-transform: uppercase;
            letter-spacing: 1px;
            color: #999;
            margin-bottom: 12px;
            font-weight: 600;
        }
        .params-table {
            width: 100%;
            border-collapse: collapse;
            margin-bottom: 25px;
        }
        .params-table th,
        .params-table td {
            padding: 12px 15px;
            text-align: left;
            border-bottom: 1px solid #eee;
        }
        .params-table th {
            background: #f8f9fa;
            font-weight: 600;
            color: #555;
            font-size: 0.9rem;
        }
        .param-name {
            font-family: monospace;
            color: #e83e8c;
            font-weight: 500;
        }
        .param-type {
            color: #6c757d;
            font-size: 0.9rem;
        }
        .param-required {
            color: #dc3545;
            font-size: 0.8rem;
            font-weight: 600;
        }
        .param-optional {
            color: #28a745;
            font-size: 0.8rem;
        }
        .param-default {
            font-family: monospace;
            background: #e9ecef;
            padding: 2px 6px;
            border-radius: 4px;
            font-size: 0.85rem;
        }
        .code-block {
            background: #2d3748;
            color: #e2e8f0;
            padding: 20px;
            border-radius: 8px;
            overflow-x: auto;
            font-family: 'Monaco', 'Menlo', monospace;
            font-size: 0.9rem;
            margin-bottom: 20px;
        }
        .code-block pre {
            margin: 0;
            white-space: pre-wrap;
            word-wrap: break-word;
        }
        .response-section {
            margin-top: 25px;
            padding-top: 25px;
            border-top: 1px solid #eee;
        }
        footer {
            text-align: center;
            padding: 40px 20px;
            color: #999;
            font-size: 0.9rem;
        }
        .tip {
            background: #fff3cd;
            border-left: 4px solid #ffc107;
            padding: 15px 20px;
            margin: 20px 0;
            border-radius: 0 8px 8px 0;
        }
        .tip-title {
            font-weight: 600;
            color: #856404;
            margin-bottom: 5px;
        }
    </style>
</head>
<body>
    <header>
        <h1>Network Test API</h1>
        <p>API for network performance testing (bandwidth and latency)</p>
        <span class="version">v{{VERSION}}</span>
    </header>

    <nav>
        <ul>
            <li><a href="#iperf">iperf3 Bandwidth Test</a></li>
            <li><a href="#twamp">TWAMP Test</a></li>
            <li><a href="#health">Health Check</a></li>
        </ul>
    </nav>

    <div class="container">
        <div class="tip">
            <div class="tip-title">API Tip</div>
            Request this endpoint with <code>Content-Type: application/json</code> header to get the JSON schema instead of this HTML documentation.
        </div>

        <section class="endpoint" id="iperf">
            <div class="endpoint-header">
                <span class="method method-post">POST</span>
                <span class="path">/iperf/client/run</span>
            </div>
            <div class="endpoint-body">
                <p class="description">Run an iperf3 bandwidth test to a target iperf3 server. Uses native iperf3 protocol implementation - compatible with any standard iperf3 server (e.g., iperf.he.net).</p>

                <h3 class="section-title">Request Parameters</h3>
                <table class="params-table">
                    <thead>
                        <tr>
                            <th>Parameter</th>
                            <th>Type</th>
                            <th>Required</th>
                            <th>Default</th>
                            <th>Description</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr>
                            <td><span class="param-name">server_host</span></td>
                            <td><span class="param-type">string</span></td>
                            <td><span class="param-required">required</span></td>
                            <td>-</td>
                            <td>iperf3 server hostname or IP</td>
                        </tr>
                        <tr>
                            <td><span class="param-name">server_port</span></td>
                            <td><span class="param-type">integer</span></td>
                            <td><span class="param-optional">optional</span></td>
                            <td><span class="param-default">5201</span></td>
                            <td>iperf3 server port</td>
                        </tr>
                        <tr>
                            <td><span class="param-name">duration</span></td>
                            <td><span class="param-type">integer</span></td>
                            <td><span class="param-optional">optional</span></td>
                            <td><span class="param-default">5</span></td>
                            <td>Test duration in seconds</td>
                        </tr>
                        <tr>
                            <td><span class="param-name">parallel</span></td>
                            <td><span class="param-type">integer</span></td>
                            <td><span class="param-optional">optional</span></td>
                            <td><span class="param-default">1</span></td>
                            <td>Number of parallel streams</td>
                        </tr>
                        <tr>
                            <td><span class="param-name">protocol</span></td>
                            <td><span class="param-type">string</span></td>
                            <td><span class="param-optional">optional</span></td>
                            <td><span class="param-default">TCP</span></td>
                            <td>Protocol to use: TCP or UDP</td>
                        </tr>
                        <tr>
                            <td><span class="param-name">reverse</span></td>
                            <td><span class="param-type">boolean</span></td>
                            <td><span class="param-optional">optional</span></td>
                            <td><span class="param-default">false</span></td>
                            <td>Reverse mode (download test instead of upload)</td>
                        </tr>
                        <tr>
                            <td><span class="param-name">bandwidth</span></td>
                            <td><span class="param-type">integer</span></td>
                            <td><span class="param-optional">optional</span></td>
                            <td><span class="param-default">100</span></td>
                            <td>Bandwidth limit in Mbit/s</td>
                        </tr>
                    </tbody>
                </table>

                <h3 class="section-title">Example Request</h3>
                <div class="code-block">
                    <pre>curl -X POST https://your-api.com/iperf/client/run \
  -H "Content-Type: application/json" \
  -d '{
    "server_host": "iperf.he.net",
    "server_port": 5201,
    "duration": 10,
    "parallel": 4,
    "protocol": "TCP",
    "bandwidth": 100
  }'</pre>
                </div>

                <div class="response-section">
                    <h3 class="section-title">Response Fields</h3>
                    <table class="params-table">
                        <thead>
                            <tr>
                                <th>Field</th>
                                <th>Description</th>
                            </tr>
                        </thead>
                        <tbody>
                            <tr><td><span class="param-name">server</span></td><td>Target server hostname</td></tr>
                            <tr><td><span class="param-name">port</span></td><td>Target server port</td></tr>
                            <tr><td><span class="param-name">protocol</span></td><td>Protocol used (TCP/UDP)</td></tr>
                            <tr><td><span class="param-name">duration_sec</span></td><td>Actual test duration in seconds</td></tr>
                            <tr><td><span class="param-name">sent_bytes</span></td><td>Total bytes sent (upload mode)</td></tr>
                            <tr><td><span class="param-name">received_bytes</span></td><td>Total bytes received (reverse/download mode)</td></tr>
                            <tr><td><span class="param-name">bandwidth_mbps</span></td><td>Measured bandwidth in Mbps</td></tr>
                        </tbody>
                    </table>

                    <h3 class="section-title">Example Response</h3>
                    <div class="code-block">
                        <pre>{
  "status": "ok",
  "data": {
    "server": "iperf.he.net",
    "port": 5201,
    "protocol": "TCP",
    "duration_sec": 10.0,
    "sent_bytes": 1250000000,
    "bandwidth_mbps": 1000.0
  }
}</pre>
                    </div>
                </div>
            </div>
        </section>

        <section class="endpoint" id="twamp">
            <div class="endpoint-header">
                <span class="method method-post">POST</span>
                <span class="path">/twamp/client/run</span>
            </div>
            <div class="endpoint-body">
                <p class="description">Run a TWAMP (Two-Way Active Measurement Protocol) latency test to measure round-trip time, one-way delays, jitter, and packet loss. Compatible with perfSONAR twampd servers.</p>

                <h3 class="section-title">Request Parameters</h3>
                <table class="params-table">
                    <thead>
                        <tr>
                            <th>Parameter</th>
                            <th>Type</th>
                            <th>Required</th>
                            <th>Default</th>
                            <th>Description</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr>
                            <td><span class="param-name">server_host</span></td>
                            <td><span class="param-type">string</span></td>
                            <td><span class="param-required">required</span></td>
                            <td>-</td>
                            <td>TWAMP server hostname or IP</td>
                        </tr>
                        <tr>
                            <td><span class="param-name">server_port</span></td>
                            <td><span class="param-type">integer</span></td>
                            <td><span class="param-optional">optional</span></td>
                            <td><span class="param-default">862</span></td>
                            <td>TWAMP control port</td>
                        </tr>
                        <tr>
                            <td><span class="param-name">count</span></td>
                            <td><span class="param-type">integer</span></td>
                            <td><span class="param-optional">optional</span></td>
                            <td><span class="param-default">10</span></td>
                            <td>Number of test probes to send</td>
                        </tr>
                    </tbody>
                </table>

                <h3 class="section-title">Example Request</h3>
                <div class="code-block">
                    <pre>curl -X POST https://your-api.com/twamp/client/run \
  -H "Content-Type: application/json" \
  -d '{
    "server_host": "twamp.example.com",
    "server_port": 862,
    "count": 20
  }'</pre>
                </div>

                <div class="response-section">
                    <h3 class="section-title">Response Fields</h3>
                    <table class="params-table">
                        <thead>
                            <tr>
                                <th>Field</th>
                                <th>Description</th>
                            </tr>
                        </thead>
                        <tbody>
                            <tr><td><span class="param-name">server</span></td><td>Target server hostname</td></tr>
                            <tr><td><span class="param-name">local_endpoint</span></td><td>Local test endpoint (IP:port)</td></tr>
                            <tr><td><span class="param-name">remote_endpoint</span></td><td>Remote test endpoint (IP:port)</td></tr>
                            <tr><td><span class="param-name">probes</span></td><td>Number of probes sent</td></tr>
                            <tr><td><span class="param-name">loss_percent</span></td><td>Packet loss percentage</td></tr>
                            <tr><td><span class="param-name">rtt_*_ms</span></td><td>Network RTT without reflector processing (min, max, avg, stddev)</td></tr>
                            <tr><td><span class="param-name">rtt_raw_ms</span></td><td>Raw RTT including reflector turnaround (min, max, avg, stddev)</td></tr>
                            <tr><td><span class="param-name">reflector_turnaround_ms</span></td><td>Reflector processing time T3-T2 (min, max, avg)</td></tr>
                            <tr><td><span class="param-name">estimated_clock_offset_ms</span></td><td>Estimated clock offset between sender and reflector</td></tr>
                            <tr><td><span class="param-name">sync_status</span></td><td>Clock sync status with Error Estimate details (RFC 4656)</td></tr>
                            <tr><td><span class="param-name">forward_delay_raw_ms</span></td><td>Raw forward delay (min, max, avg) - affected by clock offset</td></tr>
                            <tr><td><span class="param-name">forward_delay_corrected_ms</span></td><td>Corrected forward delay assuming symmetric path (min, max, avg)</td></tr>
                            <tr><td><span class="param-name">forward_ipdv_ms</span></td><td>RFC 3393 IP Packet Delay Variation (min, max, avg, mean_abs)</td></tr>
                            <tr><td><span class="param-name">forward_jitter_ms</span></td><td>RFC 3550 Jitter - exponentially smoothed mean absolute IPDV</td></tr>
                            <tr><td><span class="param-name">reverse_delay_raw_ms</span></td><td>Raw reverse delay (min, max, avg) - affected by clock offset</td></tr>
                            <tr><td><span class="param-name">reverse_delay_corrected_ms</span></td><td>Corrected reverse delay assuming symmetric path (min, max, avg)</td></tr>
                            <tr><td><span class="param-name">reverse_ipdv_ms</span></td><td>RFC 3393 IP Packet Delay Variation (min, max, avg, mean_abs)</td></tr>
                            <tr><td><span class="param-name">reverse_jitter_ms</span></td><td>RFC 3550 Jitter - exponentially smoothed mean absolute IPDV</td></tr>
                            <tr><td><span class="param-name">hops</span></td><td>Hop counts derived from TTL (forward/reverse with min, max, avg)</td></tr>
                        </tbody>
                    </table>

                    <h3 class="section-title">Example Response</h3>
                    <div class="code-block">
                        <pre>{
  "status": "ok",
  "data": {
    "server": "twamp.example.com",
    "local_endpoint": "192.168.1.100:19234",
    "remote_endpoint": "203.0.113.50:18760",
    "probes": 20,
    "loss_percent": 0.0,
    "rtt_min_ms": 28.5,
    "rtt_max_ms": 35.2,
    "rtt_avg_ms": 31.8,
    "rtt_stddev_ms": 1.2,
    "reflector_turnaround_ms": {"min": 0.05, "max": 0.15, "avg": 0.08},
    "estimated_clock_offset_ms": 0.15,
    "sync_status": {"sender_synced": true, "reflector_synced": true, "both_synced": true},
    "forward_delay_raw_ms": {"min": 14.1, "max": 17.8, "avg": 15.9},
    "forward_delay_corrected_ms": {"min": 14.25, "max": 17.6, "avg": 15.9},
    "forward_ipdv_ms": {"min": -1.2, "max": 1.5, "avg": 0.01, "mean_abs": 0.45},
    "forward_jitter_ms": 0.52,
    "reverse_delay_raw_ms": {"min": 14.2, "max": 17.6, "avg": 15.9},
    "reverse_delay_corrected_ms": {"min": 14.25, "max": 17.6, "avg": 15.9},
    "reverse_ipdv_ms": {"min": -1.1, "max": 1.3, "avg": -0.02, "mean_abs": 0.42},
    "reverse_jitter_ms": 0.48,
    "hops": {"forward": {"min": 10, "max": 10, "avg": 10}, "reverse": {"min": 10, "max": 10, "avg": 10}}
  }
}</pre>
                    </div>

                    <div class="tip">
                        <div class="tip-title">Jitter Measurement (RFC 3393 &amp; RFC 3550)</div>
                        Jitter is calculated using RFC-compliant methods. IPDV (IP Packet Delay Variation, RFC 3393) measures the difference in delay between consecutive packets - clock offset cancels out, giving true one-way delay variation. The jitter value uses RFC 3550's exponentially smoothed mean absolute IPDV. Hop counts are derived from TTL values.
                    </div>
                </div>
            </div>
        </section>

        <section class="endpoint" id="health">
            <div class="endpoint-header">
                <span class="method method-get">GET</span>
                <span class="path">/health</span>
            </div>
            <div class="endpoint-body">
                <p class="description">Health check endpoint to verify the API is running and responsive.</p>

                <h3 class="section-title">Example Request</h3>
                <div class="code-block">
                    <pre>curl https://your-api.com/health</pre>
                </div>

                <div class="response-section">
                    <h3 class="section-title">Example Response</h3>
                    <div class="code-block">
                        <pre>{"status": "healthy"}</pre>
                    </div>
                </div>
            </div>
        </section>
    </div>

    <footer>
        <p>Network Test API v{{VERSION}} - Native iperf3 Protocol Implementation</p>
        <p style="margin-top: 15px;">
            Created by <a href="https://github.com/CoreTex" style="color: #667eea; text-decoration: none;">CoreTex</a>
        </p>
        <p style="margin-top: 10px;">
            <a href="https://www.buymeacoffee.com/networkcoder" target="_blank">
                <img src="https://www.buymeacoffee.com/assets/img/custom_images/orange_img.png" alt="Buy Me A Coffee" style="height: 41px; width: 174px;">
            </a>
        </p>
    </footer>
</body>
</html>`
	html := strings.Replace(htmlTemplate, "{{VERSION}}", API_VERSION, -1)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}

func main() {
	r := mux.NewRouter()
	
	// Client endpoints
	r.HandleFunc("/iperf/client/run", iperfClientRun).Methods("POST")
	r.HandleFunc("/twamp/client/run", twampClientRun).Methods("POST")

	// Health/Info
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, ApiResponse{Status: "healthy"}, http.StatusOK)
	}).Methods("GET")
	
	r.HandleFunc("/", handleRoot).Methods("GET")

	log.Println("🚀 Network Test API listening on :8080")
	log.Println("📦 Pure Go implementation - Fastly Compute ready")
	log.Fatal(http.ListenAndServe(":8080", r))
}

