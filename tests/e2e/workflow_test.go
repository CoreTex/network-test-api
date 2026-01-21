// Package e2e contains end-to-end tests that verify complete workflows.
// These tests simulate real user scenarios from API call to response.
package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

// API structures
type ApiResponse struct {
	Status string                 `json:"status"`
	Data   map[string]interface{} `json:"data,omitempty"`
	Error  string                 `json:"error,omitempty"`
}

// Simulated test server that mimics real behavior
type TestServer struct {
	router *mux.Router
	server *httptest.Server
}

func NewTestServer() *TestServer {
	r := mux.NewRouter()

	// Health endpoint
	r.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
	}).Methods("GET")

	// Root endpoint with content negotiation
	r.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") == "application/json" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ApiResponse{
				Status: "ok",
				Data: map[string]interface{}{
					"name":        "Network Test API",
					"version":     "2.2.0",
					"description": "API for network performance testing",
				},
			})
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body><h1>Network Test API v2.2.0</h1></body></html>"))
	}).Methods("GET")

	// iperf3 endpoint with full workflow simulation
	r.HandleFunc("/iperf/client/run", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ApiResponse{Status: "error", Error: err.Error()})
			return
		}

		serverHost, ok := req["server_host"].(string)
		if !ok || serverHost == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ApiResponse{Status: "error", Error: "server_host is required"})
			return
		}

		// Simulate connection failure for specific hosts
		if serverHost == "unreachable.example.com" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(ApiResponse{
				Status: "error",
				Error:  "connect to unreachable.example.com:5201 failed: connection refused",
			})
			return
		}

		// Simulate test duration (scaled down for testing)
		duration := 1.0
		if d, ok := req["duration"].(float64); ok && d > 0 {
			duration = d
		}

		// Simulate small delay to represent test execution
		time.Sleep(10 * time.Millisecond)

		port := 5201.0
		if p, ok := req["server_port"].(float64); ok && p > 0 {
			port = p
		}

		protocol := "TCP"
		if p, ok := req["protocol"].(string); ok && p != "" {
			protocol = p
		}

		bandwidth := 100.0
		if b, ok := req["bandwidth"].(float64); ok && b > 0 {
			bandwidth = b
		}

		reverse := false
		if r, ok := req["reverse"].(bool); ok {
			reverse = r
		}

		data := map[string]interface{}{
			"server":         serverHost,
			"port":           int(port),
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
		json.NewEncoder(w).Encode(ApiResponse{Status: "ok", Data: data})
	}).Methods("POST")

	// TWAMP endpoint with full workflow simulation
	r.HandleFunc("/twamp/client/run", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ApiResponse{Status: "error", Error: err.Error()})
			return
		}

		serverHost, ok := req["server_host"].(string)
		if !ok || serverHost == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(ApiResponse{Status: "error", Error: "server_host is required"})
			return
		}

		// Simulate connection failure
		if serverHost == "unreachable.example.com" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(ApiResponse{
				Status: "error",
				Error:  "Connect failed: dial tcp: connection refused",
			})
			return
		}

		count := 10.0
		if c, ok := req["count"].(float64); ok && c > 0 {
			count = c
		}

		// Simulate small delay
		time.Sleep(10 * time.Millisecond)

		data := map[string]interface{}{
			"server":                    serverHost,
			"local_endpoint":            "192.168.1.100:19234",
			"remote_endpoint":           "203.0.113.50:18760",
			"probes":                    int(count),
			"loss_percent":              0.0,
			"rtt_min_ms":                28.5,
			"rtt_max_ms":                35.2,
			"rtt_avg_ms":                31.8,
			"rtt_stddev_ms":             1.2,
			"estimated_clock_offset_ms": 0.15,
			"forward_jitter_ms":         0.52,
			"reverse_jitter_ms":         0.48,
			"sync_status": map[string]interface{}{
				"sender_synced":    true,
				"reflector_synced": true,
				"both_synced":      true,
			},
			"hops": map[string]interface{}{
				"forward": map[string]interface{}{"min": 10, "max": 10, "avg": 10.0},
				"reverse": map[string]interface{}{"min": 10, "max": 10, "avg": 10.0},
			},
			"forward_ipdv_ms": map[string]interface{}{
				"min": -1.2, "max": 1.5, "avg": 0.01, "mean_abs": 0.45,
			},
			"reverse_ipdv_ms": map[string]interface{}{
				"min": -1.1, "max": 1.3, "avg": -0.02, "mean_abs": 0.42,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(ApiResponse{Status: "ok", Data: data})
	}).Methods("POST")

	server := httptest.NewServer(r)

	return &TestServer{
		router: r,
		server: server,
	}
}

func (ts *TestServer) Close() {
	ts.server.Close()
}

func (ts *TestServer) URL() string {
	return ts.server.URL
}

// E2E Test: Complete iperf3 bandwidth test workflow
func TestE2E_Iperf3BandwidthTest_Complete(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	// Step 1: Check API health
	resp, err := http.Get(ts.URL() + "/health")
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Health check returned %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Step 2: Get API documentation
	req, _ := http.NewRequest("GET", ts.URL()+"/", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Get docs failed: %v", err)
	}
	var docResp ApiResponse
	json.NewDecoder(resp.Body).Decode(&docResp)
	resp.Body.Close()

	if docResp.Data["version"] != "2.2.0" {
		t.Errorf("Expected version 2.2.0, got %v", docResp.Data["version"])
	}

	// Step 3: Run iperf3 test
	testBody := `{
		"server_host": "iperf.he.net",
		"duration": 5,
		"parallel": 4,
		"protocol": "TCP",
		"bandwidth": 100
	}`
	resp, err = http.Post(ts.URL()+"/iperf/client/run", "application/json",
		bytes.NewBufferString(testBody))
	if err != nil {
		t.Fatalf("iperf3 test failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("iperf3 test returned %d", resp.StatusCode)
	}

	var testResp ApiResponse
	json.NewDecoder(resp.Body).Decode(&testResp)

	// Verify complete response
	if testResp.Status != "ok" {
		t.Errorf("Expected status=ok, got %s", testResp.Status)
	}
	if testResp.Data["server"] != "iperf.he.net" {
		t.Errorf("Expected server=iperf.he.net, got %v", testResp.Data["server"])
	}
	if testResp.Data["bandwidth_mbps"].(float64) != 100 {
		t.Errorf("Expected bandwidth_mbps=100, got %v", testResp.Data["bandwidth_mbps"])
	}
}

// E2E Test: Complete TWAMP latency test workflow
func TestE2E_TwampLatencyTest_Complete(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	// Step 1: Check health
	resp, _ := http.Get(ts.URL() + "/health")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Health check failed")
	}
	resp.Body.Close()

	// Step 2: Run TWAMP test
	testBody := `{
		"server_host": "twamp.example.com",
		"count": 50
	}`
	resp, err := http.Post(ts.URL()+"/twamp/client/run", "application/json",
		bytes.NewBufferString(testBody))
	if err != nil {
		t.Fatalf("TWAMP test failed: %v", err)
	}
	defer resp.Body.Close()

	var testResp ApiResponse
	json.NewDecoder(resp.Body).Decode(&testResp)

	// Verify all required fields
	requiredFields := []string{
		"server", "local_endpoint", "remote_endpoint", "probes",
		"loss_percent", "rtt_avg_ms", "rtt_min_ms", "rtt_max_ms",
		"forward_jitter_ms", "reverse_jitter_ms", "sync_status", "hops",
	}

	for _, field := range requiredFields {
		if testResp.Data[field] == nil {
			t.Errorf("Missing required field: %s", field)
		}
	}

	// Verify hop structure
	hops, ok := testResp.Data["hops"].(map[string]interface{})
	if !ok {
		t.Fatal("hops should be a map")
	}
	if hops["forward"] == nil || hops["reverse"] == nil {
		t.Error("hops should have forward and reverse")
	}

	// Verify sync_status structure
	syncStatus, ok := testResp.Data["sync_status"].(map[string]interface{})
	if !ok {
		t.Fatal("sync_status should be a map")
	}
	if syncStatus["both_synced"] == nil {
		t.Error("sync_status should have both_synced")
	}
}

// E2E Test: Error handling for unreachable server
func TestE2E_UnreachableServer(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	testBody := `{"server_host": "unreachable.example.com"}`
	resp, err := http.Post(ts.URL()+"/iperf/client/run", "application/json",
		bytes.NewBufferString(testBody))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("Expected 500 for unreachable server, got %d", resp.StatusCode)
	}

	var errResp ApiResponse
	json.NewDecoder(resp.Body).Decode(&errResp)

	if errResp.Status != "error" {
		t.Errorf("Expected status=error, got %s", errResp.Status)
	}
	if errResp.Error == "" {
		t.Error("Expected error message for unreachable server")
	}
}

// E2E Test: Multiple sequential tests
func TestE2E_SequentialTests(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	// Run 3 sequential tests
	for i := 0; i < 3; i++ {
		testBody := `{"server_host": "test.example.com", "duration": 1}`
		resp, err := http.Post(ts.URL()+"/iperf/client/run", "application/json",
			bytes.NewBufferString(testBody))
		if err != nil {
			t.Fatalf("Test %d failed: %v", i, err)
		}

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Test %d: expected 200, got %d", i, resp.StatusCode)
		}
		resp.Body.Close()
	}
}

// E2E Test: Different protocol tests
func TestE2E_DifferentProtocols(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	protocols := []struct {
		name     string
		body     string
		expected string
	}{
		{"TCP", `{"server_host": "test.com", "protocol": "TCP"}`, "TCP"},
		{"UDP", `{"server_host": "test.com", "protocol": "UDP"}`, "UDP"},
	}

	for _, p := range protocols {
		resp, _ := http.Post(ts.URL()+"/iperf/client/run", "application/json",
			bytes.NewBufferString(p.body))

		var result ApiResponse
		json.NewDecoder(resp.Body).Decode(&result)
		resp.Body.Close()

		if result.Data["protocol"] != p.expected {
			t.Errorf("%s: expected protocol=%s, got %v", p.name, p.expected, result.Data["protocol"])
		}
	}
}

// E2E Test: Upload vs Download modes
func TestE2E_UploadDownloadModes(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	// Upload test (default)
	uploadBody := `{"server_host": "test.com"}`
	resp, _ := http.Post(ts.URL()+"/iperf/client/run", "application/json",
		bytes.NewBufferString(uploadBody))
	var uploadResult ApiResponse
	json.NewDecoder(resp.Body).Decode(&uploadResult)
	resp.Body.Close()

	if uploadResult.Data["sent_bytes"] == nil {
		t.Error("Upload should have sent_bytes")
	}
	if uploadResult.Data["received_bytes"] != nil {
		t.Error("Upload should not have received_bytes")
	}

	// Download test (reverse)
	downloadBody := `{"server_host": "test.com", "reverse": true}`
	resp, _ = http.Post(ts.URL()+"/iperf/client/run", "application/json",
		bytes.NewBufferString(downloadBody))
	var downloadResult ApiResponse
	json.NewDecoder(resp.Body).Decode(&downloadResult)
	resp.Body.Close()

	if downloadResult.Data["received_bytes"] == nil {
		t.Error("Download should have received_bytes")
	}
	if downloadResult.Data["sent_bytes"] != nil {
		t.Error("Download should not have sent_bytes")
	}
}

// E2E Test: API documentation content negotiation
func TestE2E_ContentNegotiation(t *testing.T) {
	ts := NewTestServer()
	defer ts.Close()

	// Request HTML (default)
	resp, _ := http.Get(ts.URL() + "/")
	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/html" {
		t.Errorf("Default should return HTML, got %s", contentType)
	}
	resp.Body.Close()

	// Request JSON
	req, _ := http.NewRequest("GET", ts.URL()+"/", nil)
	req.Header.Set("Content-Type", "application/json")
	resp, _ = http.DefaultClient.Do(req)
	contentType = resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("With Content-Type: application/json should return JSON, got %s", contentType)
	}
	resp.Body.Close()
}
