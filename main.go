package main

import (
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/tcaine/twamp"
)

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

	controlConn net.Conn
	cookie      []byte
	streams     []net.Conn

	mu sync.Mutex
}

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
func NewIperf3Client(host string, port, duration, parallel int, protocol string, reverse bool) *Iperf3Client {
	if parallel < 1 {
		parallel = 1
	}
	blkSize := DEFAULT_TCP_BLKSIZE
	if strings.ToUpper(protocol) == "UDP" {
		blkSize = DEFAULT_UDP_BLKSIZE
	}
	return &Iperf3Client{
		Host:      host,
		Port:      port,
		Duration:  duration,
		Parallel:  parallel,
		Protocol:  strings.ToUpper(protocol),
		Reverse:   reverse,
		BlockSize: blkSize,
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

	buffer := make([]byte, c.BlockSize)
	rand.Read(buffer)

	for _, stream := range c.streams {
		wg.Add(1)
		go func(conn net.Conn) {
			defer wg.Done()

			var streamBytes int64
			conn.SetWriteDeadline(deadline)

			for time.Now().Before(deadline) {
				n, err := conn.Write(buffer)
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
func iperf3Test(host string, port, duration, parallel int, protocol string, reverse bool) (*Iperf3Result, error) {
	client := NewIperf3Client(host, port, duration, parallel, protocol, reverse)
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

	log.Printf("iperf3 test: %s:%d (%s, %ds, %d streams, reverse=%v)",
		req.ServerHost, req.ServerPort, req.Protocol, req.Duration, req.Parallel, req.Reverse)

	// Run native iperf3 test
	result, err := iperf3Test(req.ServerHost, req.ServerPort, req.Duration, req.Parallel, req.Protocol, req.Reverse)

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
	if req.Padding == 0 {
		req.Padding = 42
	}

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

	sessionConfig := twamp.TwampSessionConfig{
		ReceiverPort: 6666,
		SenderPort:   6666,
		Timeout:      5,
		Padding:      req.Padding,
		TOS:          twamp.EF,
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

	results, err := test.RunMultiple(uint64(req.Count), nil, time.Second, nil)
	if err != nil {
		jsonResponse(w, ApiResponse{
			Status: "error",
			Error:  fmt.Sprintf("Test run failed: %v", err),
		}, http.StatusInternalServerError)
		return
	}

	stat := results.Stat
	jsonResponse(w, ApiResponse{
		Status: "ok",
		Data: map[string]interface{}{
			"server":        req.ServerHost,
			"port":          req.ServerPort,
			"probes":        req.Count,
			"loss_percent":  stat.Loss,
			"rtt_min_ms":    float64(stat.Min.Nanoseconds()) / 1e6,
			"rtt_max_ms":    float64(stat.Max.Nanoseconds()) / 1e6,
			"rtt_avg_ms":    float64(stat.Avg.Nanoseconds()) / 1e6,
			"rtt_stddev_ms": float64(stat.StdDev.Nanoseconds()) / 1e6,
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
		"version":     "2.0.0",
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
				"description": "Run a TWAMP latency test to measure RTT and packet loss",
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
							"description": "TWAMP server port",
						},
						"count": map[string]string{
							"type":        "integer",
							"required":    "false",
							"default":     "10",
							"description": "Number of test probes to send",
						},
						"padding": map[string]string{
							"type":        "integer",
							"required":    "false",
							"default":     "42",
							"description": "Padding bytes in test packets",
						},
					},
				},
				"response": map[string]interface{}{
					"content_type": "application/json",
					"body": map[string]string{
						"server":        "Target server hostname",
						"port":          "Target server port",
						"probes":        "Number of probes sent",
						"loss_percent":  "Packet loss percentage",
						"rtt_min_ms":    "Minimum RTT in milliseconds",
						"rtt_max_ms":    "Maximum RTT in milliseconds",
						"rtt_avg_ms":    "Average RTT in milliseconds",
						"rtt_stddev_ms": "RTT standard deviation in milliseconds",
					},
				},
				"example": map[string]interface{}{
					"request":  `{"server_host": "twamp.example.com", "server_port": 862, "count": 20}`,
					"response": `{"status": "ok", "data": {"server": "twamp.example.com", "port": 862, "probes": 20, "loss_percent": 0.0, "rtt_min_ms": 1.2, "rtt_max_ms": 5.8, "rtt_avg_ms": 2.5, "rtt_stddev_ms": 0.9}}`,
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
	html := `<!DOCTYPE html>
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
        <span class="version">v2.0.0</span>
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
    "protocol": "TCP"
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
                <p class="description">Run a TWAMP (Two-Way Active Measurement Protocol) latency test to measure round-trip time and packet loss with high precision.</p>

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
                            <td>TWAMP server port</td>
                        </tr>
                        <tr>
                            <td><span class="param-name">count</span></td>
                            <td><span class="param-type">integer</span></td>
                            <td><span class="param-optional">optional</span></td>
                            <td><span class="param-default">10</span></td>
                            <td>Number of test probes to send</td>
                        </tr>
                        <tr>
                            <td><span class="param-name">padding</span></td>
                            <td><span class="param-type">integer</span></td>
                            <td><span class="param-optional">optional</span></td>
                            <td><span class="param-default">42</span></td>
                            <td>Padding bytes in test packets</td>
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
                            <tr><td><span class="param-name">port</span></td><td>Target server port</td></tr>
                            <tr><td><span class="param-name">probes</span></td><td>Number of probes sent</td></tr>
                            <tr><td><span class="param-name">loss_percent</span></td><td>Packet loss percentage</td></tr>
                            <tr><td><span class="param-name">rtt_min_ms</span></td><td>Minimum RTT in milliseconds</td></tr>
                            <tr><td><span class="param-name">rtt_max_ms</span></td><td>Maximum RTT in milliseconds</td></tr>
                            <tr><td><span class="param-name">rtt_avg_ms</span></td><td>Average RTT in milliseconds</td></tr>
                            <tr><td><span class="param-name">rtt_stddev_ms</span></td><td>RTT standard deviation in milliseconds</td></tr>
                        </tbody>
                    </table>

                    <h3 class="section-title">Example Response</h3>
                    <div class="code-block">
                        <pre>{
  "status": "ok",
  "data": {
    "server": "twamp.example.com",
    "port": 862,
    "probes": 20,
    "loss_percent": 0.0,
    "rtt_min_ms": 1.2,
    "rtt_max_ms": 5.8,
    "rtt_avg_ms": 2.5,
    "rtt_stddev_ms": 0.9
  }
}</pre>
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
        <p>Network Test API v2.0.0 - Native iperf3 Protocol Implementation</p>
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

