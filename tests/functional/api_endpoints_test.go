package functional

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/mux"
)

// API Response structures
type ApiResponse struct {
	Status string                 `json:"status"`
	Data   map[string]interface{} `json:"data,omitempty"`
	Error  string                 `json:"error,omitempty"`
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
	Bandwidth  int    `json:"bandwidth"`
}

// Mock handlers for functional testing
func mockIperfHandlerFunctional(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ApiResponse{Status: "error", Error: err.Error()})
		return
	}

	if req.ServerHost == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ApiResponse{Status: "error", Error: "server_host is required"})
		return
	}

	// Apply defaults
	port := req.ServerPort
	if port == 0 {
		port = 5201
	}
	duration := req.Duration
	if duration == 0 {
		duration = 5
	}
	// parallel is ignored in mock response
	protocol := req.Protocol
	if protocol == "" {
		protocol = "TCP"
	}
	bandwidth := req.Bandwidth
	if bandwidth == 0 {
		bandwidth = 100
	}

	data := map[string]interface{}{
		"server":         req.ServerHost,
		"port":           port,
		"protocol":       strings.ToUpper(protocol),
		"duration_sec":   float64(duration),
		"bandwidth_mbps": float64(bandwidth),
	}

	if req.Reverse {
		data["received_bytes"] = int64(duration * bandwidth * 125000)
	} else {
		data["sent_bytes"] = int64(duration * bandwidth * 125000)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ApiResponse{Status: "ok", Data: data})
}

func mockTwampHandlerFunctional(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ApiResponse{Status: "error", Error: err.Error()})
		return
	}

	if req.ServerHost == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ApiResponse{Status: "error", Error: "server_host is required"})
		return
	}

	// Apply defaults
	count := req.Count
	if count == 0 {
		count = 10
	}

	data := map[string]interface{}{
		"server":                    req.ServerHost,
		"local_endpoint":            "192.168.1.100:19234",
		"remote_endpoint":           "203.0.113.50:18760",
		"probes":                    count,
		"loss_percent":              0.0,
		"rtt_min_ms":                28.5,
		"rtt_max_ms":                35.2,
		"rtt_avg_ms":                31.8,
		"rtt_stddev_ms":             1.2,
		"forward_jitter_ms":         0.52,
		"reverse_jitter_ms":         0.48,
		"estimated_clock_offset_ms": 0.15,
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
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ApiResponse{Status: "ok", Data: data})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(ApiResponse{Status: "healthy"})
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")

	if contentType == "application/json" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(ApiResponse{
			Status: "ok",
			Data: map[string]interface{}{
				"name":    "Network Test API",
				"version": "2.2.0",
			},
		})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("<html><body><h1>Network Test API</h1></body></html>"))
}

func setupFunctionalRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/", rootHandler).Methods("GET")
	r.HandleFunc("/health", healthHandler).Methods("GET")
	r.HandleFunc("/iperf/client/run", mockIperfHandlerFunctional).Methods("POST")
	r.HandleFunc("/twamp/client/run", mockTwampHandlerFunctional).Methods("POST")
	return r
}

// Test iperf3 functionality
func TestIperf_TCPUpload(t *testing.T) {
	router := setupFunctionalRouter()

	body := `{"server_host": "iperf.he.net", "duration": 10, "protocol": "TCP"}`
	req, _ := http.NewRequest("POST", "/iperf/client/run", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	var resp ApiResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	if resp.Data["protocol"] != "TCP" {
		t.Errorf("Expected protocol=TCP, got %v", resp.Data["protocol"])
	}
	if resp.Data["sent_bytes"] == nil {
		t.Error("Expected sent_bytes in upload mode")
	}
	if resp.Data["received_bytes"] != nil {
		t.Error("Unexpected received_bytes in upload mode")
	}
}

func TestIperf_TCPDownload(t *testing.T) {
	router := setupFunctionalRouter()

	body := `{"server_host": "iperf.he.net", "duration": 10, "reverse": true}`
	req, _ := http.NewRequest("POST", "/iperf/client/run", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp ApiResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	if resp.Data["received_bytes"] == nil {
		t.Error("Expected received_bytes in download mode")
	}
	if resp.Data["sent_bytes"] != nil {
		t.Error("Unexpected sent_bytes in download mode")
	}
}

func TestIperf_UDPMode(t *testing.T) {
	router := setupFunctionalRouter()

	body := `{"server_host": "iperf.he.net", "protocol": "UDP", "bandwidth": 50}`
	req, _ := http.NewRequest("POST", "/iperf/client/run", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp ApiResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	if resp.Data["protocol"] != "UDP" {
		t.Errorf("Expected protocol=UDP, got %v", resp.Data["protocol"])
	}
	if resp.Data["bandwidth_mbps"].(float64) != 50 {
		t.Errorf("Expected bandwidth_mbps=50, got %v", resp.Data["bandwidth_mbps"])
	}
}

func TestIperf_ParallelStreams(t *testing.T) {
	router := setupFunctionalRouter()

	body := `{"server_host": "iperf.he.net", "parallel": 4}`
	req, _ := http.NewRequest("POST", "/iperf/client/run", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}
}

func TestIperf_CustomPort(t *testing.T) {
	router := setupFunctionalRouter()

	body := `{"server_host": "iperf.he.net", "server_port": 5202}`
	req, _ := http.NewRequest("POST", "/iperf/client/run", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp ApiResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	if resp.Data["port"].(float64) != 5202 {
		t.Errorf("Expected port=5202, got %v", resp.Data["port"])
	}
}

// Test TWAMP functionality
func TestTwamp_BasicTest(t *testing.T) {
	router := setupFunctionalRouter()

	body := `{"server_host": "twamp.example.com", "count": 50}`
	req, _ := http.NewRequest("POST", "/twamp/client/run", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	var resp ApiResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	// Check RTT fields
	if resp.Data["rtt_avg_ms"] == nil {
		t.Error("Expected rtt_avg_ms field")
	}
	if resp.Data["rtt_min_ms"] == nil {
		t.Error("Expected rtt_min_ms field")
	}
	if resp.Data["rtt_max_ms"] == nil {
		t.Error("Expected rtt_max_ms field")
	}
	if resp.Data["rtt_stddev_ms"] == nil {
		t.Error("Expected rtt_stddev_ms field")
	}
}

func TestTwamp_JitterFields(t *testing.T) {
	router := setupFunctionalRouter()

	body := `{"server_host": "twamp.example.com"}`
	req, _ := http.NewRequest("POST", "/twamp/client/run", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp ApiResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	// Check jitter fields (RFC 3550)
	if resp.Data["forward_jitter_ms"] == nil {
		t.Error("Expected forward_jitter_ms field")
	}
	if resp.Data["reverse_jitter_ms"] == nil {
		t.Error("Expected reverse_jitter_ms field")
	}
}

func TestTwamp_HopFields(t *testing.T) {
	router := setupFunctionalRouter()

	body := `{"server_host": "twamp.example.com"}`
	req, _ := http.NewRequest("POST", "/twamp/client/run", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp ApiResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	// Check hops field
	hops, ok := resp.Data["hops"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected hops field to be a map")
	}

	if hops["forward"] == nil {
		t.Error("Expected hops.forward field")
	}
	if hops["reverse"] == nil {
		t.Error("Expected hops.reverse field")
	}
}

func TestTwamp_SyncStatus(t *testing.T) {
	router := setupFunctionalRouter()

	body := `{"server_host": "twamp.example.com"}`
	req, _ := http.NewRequest("POST", "/twamp/client/run", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp ApiResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	// Check sync_status field
	syncStatus, ok := resp.Data["sync_status"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected sync_status field to be a map")
	}

	if syncStatus["sender_synced"] == nil {
		t.Error("Expected sync_status.sender_synced field")
	}
	if syncStatus["reflector_synced"] == nil {
		t.Error("Expected sync_status.reflector_synced field")
	}
	if syncStatus["both_synced"] == nil {
		t.Error("Expected sync_status.both_synced field")
	}
}

func TestTwamp_Endpoints(t *testing.T) {
	router := setupFunctionalRouter()

	body := `{"server_host": "twamp.example.com"}`
	req, _ := http.NewRequest("POST", "/twamp/client/run", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp ApiResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	if resp.Data["local_endpoint"] == nil {
		t.Error("Expected local_endpoint field")
	}
	if resp.Data["remote_endpoint"] == nil {
		t.Error("Expected remote_endpoint field")
	}
}

// Test root endpoint
func TestRoot_HTMLResponse(t *testing.T) {
	router := setupFunctionalRouter()

	req, _ := http.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(contentType, "text/html") {
		t.Errorf("Expected Content-Type text/html, got %s", contentType)
	}

	if !strings.Contains(rr.Body.String(), "<html>") {
		t.Error("Expected HTML content")
	}
}

func TestRoot_JSONResponse(t *testing.T) {
	router := setupFunctionalRouter()

	req, _ := http.NewRequest("GET", "/", nil)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", rr.Code)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	var resp ApiResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode JSON: %v", err)
	}

	if resp.Data["version"] == nil {
		t.Error("Expected version in JSON response")
	}
}

// Test error handling
func TestErrorHandling_EmptyBody(t *testing.T) {
	router := setupFunctionalRouter()

	req, _ := http.NewRequest("POST", "/iperf/client/run", bytes.NewBufferString(""))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}
}

func TestErrorHandling_MalformedJSON(t *testing.T) {
	router := setupFunctionalRouter()

	testCases := []string{
		`{`,
		`{"server_host":}`,
		`{"server_host": "test", duration: 10}`,
		`not json at all`,
	}

	for _, body := range testCases {
		req, _ := http.NewRequest("POST", "/iperf/client/run", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")

		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("Body '%s': expected status 400, got %d", body, rr.Code)
		}
	}
}

func TestErrorHandling_MissingRequiredField(t *testing.T) {
	router := setupFunctionalRouter()

	// Missing server_host
	body := `{"duration": 10}`
	req, _ := http.NewRequest("POST", "/iperf/client/run", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}

	var resp ApiResponse
	_ = json.NewDecoder(rr.Body).Decode(&resp)

	if resp.Status != "error" {
		t.Errorf("Expected status=error, got %s", resp.Status)
	}
}
