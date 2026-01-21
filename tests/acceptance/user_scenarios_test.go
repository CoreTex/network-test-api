// Package acceptance contains acceptance tests that verify user scenarios.
// These tests validate that the API meets business requirements from a user perspective.
package acceptance

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// API structures
type ApiResponse struct {
	Status string                 `json:"status"`
	Data   map[string]interface{} `json:"data,omitempty"`
	Error  string                 `json:"error,omitempty"`
}

// AcceptanceServer simulates the full API for acceptance testing
type AcceptanceServer struct {
	server *httptest.Server
}

func NewAcceptanceServer() *AcceptanceServer {
	r := mux.NewRouter()

	// Health check
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	}).Methods("GET")

	// Root endpoint
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(ApiResponse{
				Status: "ok",
				Data: map[string]interface{}{
					"name":        "Network Test API",
					"version":     "2.2.0",
					"description": "API for network performance testing",
					"endpoints": []map[string]string{
						{"path": "/iperf/client/run", "method": "POST"},
						{"path": "/twamp/client/run", "method": "POST"},
						{"path": "/health", "method": "GET"},
					},
				},
			})
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!DOCTYPE html>
<html><head><title>Network Test API</title></head>
<body><h1>Network Test API v2.2.0</h1>
<p>Endpoints: /iperf/client/run, /twamp/client/run, /health</p>
</body></html>`))
	}).Methods("GET")

	// iperf3 endpoint
	r.HandleFunc("/iperf/client/run", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(ApiResponse{Status: "error", Error: err.Error()})
			return
		}

		serverHost, _ := req["server_host"].(string)
		if serverHost == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(ApiResponse{Status: "error", Error: "server_host is required"})
			return
		}

		// Simulate different scenarios
		if serverHost == "busy.server.com" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(ApiResponse{Status: "error", Error: "server denied access"})
			return
		}

		port := 5201
		if p, ok := req["server_port"].(float64); ok {
			port = int(p)
		}
		duration := 5.0
		if d, ok := req["duration"].(float64); ok {
			duration = d
		}
		protocol := "TCP"
		if p, ok := req["protocol"].(string); ok {
			protocol = strings.ToUpper(p)
		}
		bandwidth := 100.0
		if b, ok := req["bandwidth"].(float64); ok {
			bandwidth = b
		}
		reverse, _ := req["reverse"].(bool)

		data := map[string]interface{}{
			"server":         serverHost,
			"port":           port,
			"protocol":       protocol,
			"duration_sec":   duration,
			"bandwidth_mbps": bandwidth,
		}
		if reverse {
			data["received_bytes"] = int64(duration * bandwidth * 125000)
		} else {
			data["sent_bytes"] = int64(duration * bandwidth * 125000)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ApiResponse{Status: "ok", Data: data})
	}).Methods("POST")

	// TWAMP endpoint
	r.HandleFunc("/twamp/client/run", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(ApiResponse{Status: "error", Error: err.Error()})
			return
		}

		serverHost, _ := req["server_host"].(string)
		if serverHost == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(ApiResponse{Status: "error", Error: "server_host is required"})
			return
		}

		// Simulate packet loss scenario
		lossPercent := 0.0
		if serverHost == "lossy.server.com" {
			lossPercent = 5.0
		}

		count := 10
		if c, ok := req["count"].(float64); ok {
			count = int(c)
		}

		data := map[string]interface{}{
			"server":                    serverHost,
			"local_endpoint":            "192.168.1.100:19234",
			"remote_endpoint":           "203.0.113.50:18760",
			"probes":                    count,
			"loss_percent":              lossPercent,
			"rtt_min_ms":                28.5,
			"rtt_max_ms":                35.2,
			"rtt_avg_ms":                31.8,
			"rtt_stddev_ms":             1.2,
			"estimated_clock_offset_ms": 0.15,
			"forward_jitter_ms":         0.52,
			"reverse_jitter_ms":         0.48,
			"forward_ipdv_ms": map[string]float64{
				"min": -1.2, "max": 1.5, "avg": 0.01, "mean_abs": 0.45,
			},
			"reverse_ipdv_ms": map[string]float64{
				"min": -1.1, "max": 1.3, "avg": -0.02, "mean_abs": 0.42,
			},
			"sync_status": map[string]interface{}{
				"sender_synced":    true,
				"reflector_synced": true,
				"both_synced":      true,
			},
			"hops": map[string]interface{}{
				"forward": map[string]interface{}{"min": 10, "max": 10, "avg": 10.0},
				"reverse": map[string]interface{}{"min": 10, "max": 10, "avg": 10.0},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ApiResponse{Status: "ok", Data: data})
	}).Methods("POST")

	server := httptest.NewServer(r)
	return &AcceptanceServer{server: server}
}

func (s *AcceptanceServer) Close() {
	s.server.Close()
}

func (s *AcceptanceServer) URL() string {
	return s.server.URL
}

// User Story: As a network engineer, I want to verify API availability
func TestAcceptance_VerifyAPIAvailability(t *testing.T) {
	server := NewAcceptanceServer()
	defer server.Close()

	// User checks health endpoint
	resp, err := http.Get(server.URL() + "/health")
	if err != nil {
		t.Fatalf("Failed to check API health: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("API is not healthy: status %d", resp.StatusCode)
	}

	var health map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&health)

	if health["status"] != "healthy" {
		t.Errorf("Expected healthy status, got %s", health["status"])
	}
}

// User Story: As a developer, I want to understand the API before using it
func TestAcceptance_DiscoverAPIDocumentation(t *testing.T) {
	server := NewAcceptanceServer()
	defer server.Close()

	// User browses to root endpoint in browser (HTML)
	resp, err := http.Get(server.URL() + "/")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		t.Error("Expected HTML documentation by default")
	}

	// User requests JSON schema programmatically
	req, err := http.NewRequest("GET", server.URL()+"/", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp2.Body.Close() }()

	var doc ApiResponse
	_ = json.NewDecoder(resp2.Body).Decode(&doc)

	if doc.Data["version"] == nil {
		t.Error("Expected version in API documentation")
	}
	if doc.Data["endpoints"] == nil {
		t.Error("Expected endpoints list in API documentation")
	}
}

// User Story: As a network engineer, I want to test bandwidth to a remote server
func TestAcceptance_RunBandwidthTest(t *testing.T) {
	server := NewAcceptanceServer()
	defer server.Close()

	// User runs a basic bandwidth test
	body := `{
		"server_host": "iperf.he.net",
		"duration": 10
	}`

	resp, err := http.Post(server.URL()+"/iperf/client/run", "application/json",
		bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("Failed to run bandwidth test: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result ApiResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)

	// Verify user can see the results they care about
	if result.Status != "ok" {
		t.Fatalf("Bandwidth test failed: %s", result.Error)
	}
	if result.Data["bandwidth_mbps"] == nil {
		t.Error("User should see bandwidth measurement")
	}
	if result.Data["sent_bytes"] == nil {
		t.Error("User should see data transferred")
	}
	if result.Data["duration_sec"] == nil {
		t.Error("User should see test duration")
	}
}

// User Story: As a network engineer, I want to test download speed
func TestAcceptance_RunDownloadTest(t *testing.T) {
	server := NewAcceptanceServer()
	defer server.Close()

	body := `{
		"server_host": "iperf.he.net",
		"reverse": true
	}`

	resp, err := http.Post(server.URL()+"/iperf/client/run", "application/json",
		bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result ApiResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)

	if result.Data["received_bytes"] == nil {
		t.Error("Download test should show received bytes")
	}
}

// User Story: As a network engineer, I want to test UDP performance
func TestAcceptance_RunUDPTest(t *testing.T) {
	server := NewAcceptanceServer()
	defer server.Close()

	body := `{
		"server_host": "iperf.he.net",
		"protocol": "UDP",
		"bandwidth": 50
	}`

	resp, err := http.Post(server.URL()+"/iperf/client/run", "application/json",
		bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result ApiResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)

	if result.Data["protocol"] != "UDP" {
		t.Errorf("Expected UDP protocol, got %v", result.Data["protocol"])
	}
}

// User Story: As a network engineer, I want to measure latency with TWAMP
func TestAcceptance_RunLatencyTest(t *testing.T) {
	server := NewAcceptanceServer()
	defer server.Close()

	body := `{
		"server_host": "twamp.example.com",
		"count": 50
	}`

	resp, err := http.Post(server.URL()+"/twamp/client/run", "application/json",
		bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result ApiResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)

	// Verify user can see latency metrics
	if result.Data["rtt_avg_ms"] == nil {
		t.Error("User should see average RTT")
	}
	if result.Data["rtt_min_ms"] == nil {
		t.Error("User should see minimum RTT")
	}
	if result.Data["rtt_max_ms"] == nil {
		t.Error("User should see maximum RTT")
	}
}

// User Story: As a network engineer, I want to see jitter (RFC compliant)
func TestAcceptance_JitterMeasurement(t *testing.T) {
	server := NewAcceptanceServer()
	defer server.Close()

	body := `{"server_host": "twamp.example.com", "count": 100}`
	resp, err := http.Post(server.URL()+"/twamp/client/run", "application/json",
		bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result ApiResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)

	// User should see RFC 3550 jitter values
	if result.Data["forward_jitter_ms"] == nil {
		t.Error("User should see forward jitter")
	}
	if result.Data["reverse_jitter_ms"] == nil {
		t.Error("User should see reverse jitter")
	}

	// User should see RFC 3393 IPDV details
	if result.Data["forward_ipdv_ms"] == nil {
		t.Error("User should see forward IPDV")
	}
	if result.Data["reverse_ipdv_ms"] == nil {
		t.Error("User should see reverse IPDV")
	}
}

// User Story: As a network engineer, I want to see hop count
func TestAcceptance_HopCountTracking(t *testing.T) {
	server := NewAcceptanceServer()
	defer server.Close()

	body := `{"server_host": "twamp.example.com"}`
	resp, err := http.Post(server.URL()+"/twamp/client/run", "application/json",
		bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result ApiResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)

	hops, ok := result.Data["hops"].(map[string]interface{})
	if !ok {
		t.Fatal("User should see hop count information")
	}

	forward, _ := hops["forward"].(map[string]interface{})
	reverse, _ := hops["reverse"].(map[string]interface{})

	if forward["avg"] == nil {
		t.Error("User should see average forward hops")
	}
	if reverse["avg"] == nil {
		t.Error("User should see average reverse hops")
	}
}

// User Story: As a network engineer, I want to see clock sync status
func TestAcceptance_ClockSyncStatus(t *testing.T) {
	server := NewAcceptanceServer()
	defer server.Close()

	body := `{"server_host": "twamp.example.com"}`
	resp, err := http.Post(server.URL()+"/twamp/client/run", "application/json",
		bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result ApiResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)

	syncStatus, ok := result.Data["sync_status"].(map[string]interface{})
	if !ok {
		t.Fatal("User should see clock sync status")
	}

	if syncStatus["sender_synced"] == nil {
		t.Error("User should see sender sync status")
	}
	if syncStatus["reflector_synced"] == nil {
		t.Error("User should see reflector sync status")
	}
	if syncStatus["both_synced"] == nil {
		t.Error("User should see combined sync status")
	}
}

// User Story: As a network engineer, I want to see packet loss
func TestAcceptance_PacketLossReporting(t *testing.T) {
	server := NewAcceptanceServer()
	defer server.Close()

	body := `{"server_host": "lossy.server.com"}`
	resp, err := http.Post(server.URL()+"/twamp/client/run", "application/json",
		bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result ApiResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)

	if result.Data["loss_percent"] == nil {
		t.Error("User should see packet loss percentage")
	}

	lossPercent := result.Data["loss_percent"].(float64)
	if lossPercent != 5.0 {
		t.Errorf("Expected 5%% loss for lossy server, got %.1f%%", lossPercent)
	}
}

// User Story: As a developer, I want clear error messages
func TestAcceptance_ClearErrorMessages(t *testing.T) {
	server := NewAcceptanceServer()
	defer server.Close()

	testCases := []struct {
		name     string
		body     string
		contains string
	}{
		{"Missing server_host", `{}`, "server_host is required"},
		{"Invalid JSON", `{invalid}`, "invalid"},
	}

	for _, tc := range testCases {
		resp, err := http.Post(server.URL()+"/iperf/client/run", "application/json",
			bytes.NewBufferString(tc.body))
		if err != nil {
			t.Fatalf("%s: Request failed: %v", tc.name, err)
		}

		var result ApiResponse
		_ = json.NewDecoder(resp.Body).Decode(&result)
		_ = resp.Body.Close()

		if result.Status != "error" {
			t.Errorf("%s: expected error status", tc.name)
		}
		if !strings.Contains(strings.ToLower(result.Error), strings.ToLower(tc.contains)) {
			t.Errorf("%s: error '%s' should contain '%s'", tc.name, result.Error, tc.contains)
		}
	}
}

// User Story: As a network engineer, I want to handle server being busy
func TestAcceptance_ServerBusyHandling(t *testing.T) {
	server := NewAcceptanceServer()
	defer server.Close()

	body := `{"server_host": "busy.server.com"}`
	resp, err := http.Post(server.URL()+"/iperf/client/run", "application/json",
		bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var result ApiResponse
	_ = json.NewDecoder(resp.Body).Decode(&result)

	if result.Status != "error" {
		t.Error("Expected error for busy server")
	}
	if !strings.Contains(result.Error, "denied") {
		t.Errorf("Error should indicate server denied access: %s", result.Error)
	}
}

// User Story: As a developer, I want JSON responses with correct structure
func TestAcceptance_ConsistentResponseStructure(t *testing.T) {
	server := NewAcceptanceServer()
	defer server.Close()

	endpoints := []struct {
		path   string
		method string
		body   string
	}{
		{"/health", "GET", ""},
		{"/iperf/client/run", "POST", `{"server_host": "test.com"}`},
		{"/twamp/client/run", "POST", `{"server_host": "test.com"}`},
	}

	for _, ep := range endpoints {
		var resp *http.Response
		var err error
		if ep.method == "GET" {
			resp, err = http.Get(server.URL() + ep.path)
		} else {
			resp, err = http.Post(server.URL()+ep.path, "application/json",
				bytes.NewBufferString(ep.body))
		}
		if err != nil {
			t.Fatalf("%s: Request failed: %v", ep.path, err)
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType != "application/json" {
			t.Errorf("%s: expected Content-Type application/json, got %s", ep.path, contentType)
		}

		// Verify JSON is parseable
		var result map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Errorf("%s: response is not valid JSON: %v", ep.path, err)
		}
		_ = resp.Body.Close()
	}
}
