package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/tcaine/twamp"
)

type RunRequest struct {
	ServerHost string `json:"server_host"`
	ServerPort int    `json:"server_port"`
	Duration   int    `json:"duration"`
	Parallel   int    `json:"parallel"`
	Count      int    `json:"count"`
	Padding    int    `json:"padding"`
	Protocol   string `json:"protocol"`
}

type ApiResponse struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// Pure Go TCP Bandwidth Test
func tcpBandwidthTest(target string, duration int) (map[string]interface{}, error) {
	conn, err := net.DialTimeout("tcp", target, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect failed: %w", err)
	}
	defer conn.Close()

	// Send random data
	buffer := make([]byte, 128*1024) // 128KB chunks
	rand.Read(buffer)
	
	start := time.Now()
	deadline := start.Add(time.Duration(duration) * time.Second)
	conn.SetDeadline(deadline)
	
	var totalBytes int64
	for time.Now().Before(deadline) {
		n, err := conn.Write(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				break
			}
			return nil, fmt.Errorf("write error: %w", err)
		}
		totalBytes += int64(n)
	}
	
	elapsed := time.Since(start).Seconds()
	mbps := (float64(totalBytes) * 8) / (elapsed * 1e6)
	
	return map[string]interface{}{
		"sent_bytes":     totalBytes,
		"duration_sec":   elapsed,
		"bandwidth_mbps": mbps,
	}, nil
}

// Pure Go UDP Bandwidth Test
func udpBandwidthTest(target string, duration int) (map[string]interface{}, error) {
	conn, err := net.DialTimeout("udp", target, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect failed: %w", err)
	}
	defer conn.Close()

	buffer := make([]byte, 1470) // Standard UDP packet
	rand.Read(buffer)
	
	start := time.Now()
	deadline := start.Add(time.Duration(duration) * time.Second)
	
	var totalBytes int64
	var packetsSent int
	
	for time.Now().Before(deadline) {
		n, err := conn.Write(buffer)
		if err != nil {
			break
		}
		totalBytes += int64(n)
		packetsSent++
		time.Sleep(10 * time.Microsecond)
	}
	
	elapsed := time.Since(start).Seconds()
	mbps := (float64(totalBytes) * 8) / (elapsed * 1e6)
	
	return map[string]interface{}{
		"sent_bytes":     totalBytes,
		"packets_sent":   packetsSent,
		"duration_sec":   elapsed,
		"bandwidth_mbps": mbps,
	}, nil
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
	if req.Protocol == "" {
		req.Protocol = "TCP"
	}

	target := fmt.Sprintf("%s:%d", req.ServerHost, req.ServerPort)
	log.Printf("Bandwidth test: %s (%s, %ds)", target, req.Protocol, req.Duration)

	var results map[string]interface{}
	var err error

	if req.Protocol == "UDP" {
		results, err = udpBandwidthTest(target, req.Duration)
	} else {
		results, err = tcpBandwidthTest(target, req.Duration)
	}

	if err != nil {
		jsonResponse(w, ApiResponse{
			Status: "error",
			Error:  err.Error(),
		}, http.StatusInternalServerError)
		return
	}

	// ✅ NUR die wichtigen Daten zurückgeben
	jsonResponse(w, ApiResponse{
		Status: "ok",
		Data: map[string]interface{}{
			"server":        req.ServerHost,
			"port":          req.ServerPort,
			"protocol":      req.Protocol,
			"duration_sec":  results["duration_sec"],
			"sent_bytes":    results["sent_bytes"],
			"bandwidth_mbps": results["bandwidth_mbps"],
			"packets_sent":  results["packets_sent"], // nur bei UDP
		},
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
		"version":     "1.0.0",
		"description": "API for network performance testing (bandwidth and latency)",
		"endpoints": []map[string]interface{}{
			{
				"path":        "/iperf/client/run",
				"method":      "POST",
				"description": "Run a bandwidth test (TCP or UDP) to a target server",
				"request": map[string]interface{}{
					"content_type": "application/json",
					"body": map[string]interface{}{
						"server_host": map[string]string{
							"type":        "string",
							"required":    "true",
							"description": "Target server hostname or IP",
						},
						"server_port": map[string]string{
							"type":        "integer",
							"required":    "false",
							"default":     "5201",
							"description": "Target server port",
						},
						"duration": map[string]string{
							"type":        "integer",
							"required":    "false",
							"default":     "5",
							"description": "Test duration in seconds",
						},
						"protocol": map[string]string{
							"type":        "string",
							"required":    "false",
							"default":     "TCP",
							"description": "Protocol to use: TCP or UDP",
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
						"sent_bytes":     "Total bytes sent",
						"bandwidth_mbps": "Measured bandwidth in Mbps",
						"packets_sent":   "Number of packets sent (UDP only)",
					},
				},
				"example": map[string]interface{}{
					"request":  `{"server_host": "iperf.example.com", "server_port": 5201, "duration": 10, "protocol": "TCP"}`,
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
        <span class="version">v1.0.0</span>
    </header>

    <nav>
        <ul>
            <li><a href="#iperf">Bandwidth Test</a></li>
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
                <p class="description">Run a bandwidth test (TCP or UDP) to a target server. Measures throughput by sending data and calculating the achieved bandwidth.</p>

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
                            <td>Target server hostname or IP</td>
                        </tr>
                        <tr>
                            <td><span class="param-name">server_port</span></td>
                            <td><span class="param-type">integer</span></td>
                            <td><span class="param-optional">optional</span></td>
                            <td><span class="param-default">5201</span></td>
                            <td>Target server port</td>
                        </tr>
                        <tr>
                            <td><span class="param-name">duration</span></td>
                            <td><span class="param-type">integer</span></td>
                            <td><span class="param-optional">optional</span></td>
                            <td><span class="param-default">5</span></td>
                            <td>Test duration in seconds</td>
                        </tr>
                        <tr>
                            <td><span class="param-name">protocol</span></td>
                            <td><span class="param-type">string</span></td>
                            <td><span class="param-optional">optional</span></td>
                            <td><span class="param-default">TCP</span></td>
                            <td>Protocol to use: TCP or UDP</td>
                        </tr>
                    </tbody>
                </table>

                <h3 class="section-title">Example Request</h3>
                <div class="code-block">
                    <pre>curl -X POST https://your-api.com/iperf/client/run \
  -H "Content-Type: application/json" \
  -d '{
    "server_host": "iperf.example.com",
    "server_port": 5201,
    "duration": 10,
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
                            <tr><td><span class="param-name">sent_bytes</span></td><td>Total bytes sent</td></tr>
                            <tr><td><span class="param-name">bandwidth_mbps</span></td><td>Measured bandwidth in Mbps</td></tr>
                            <tr><td><span class="param-name">packets_sent</span></td><td>Number of packets sent (UDP only)</td></tr>
                        </tbody>
                    </table>

                    <h3 class="section-title">Example Response</h3>
                    <div class="code-block">
                        <pre>{
  "status": "ok",
  "data": {
    "server": "iperf.example.com",
    "port": 5201,
    "protocol": "TCP",
    "duration_sec": 10.0,
    "sent_bytes": 125000000,
    "bandwidth_mbps": 100.0
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
        <p>Network Test API v1.0.0 - Pure Go Implementation</p>
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

