package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
)

// ApiResponse represents the standard API response structure
type ApiResponse struct {
	Status string      `json:"status"`
	Data   interface{} `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// RunRequest represents the request body for test endpoints
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

// jsonResponse helper function (mirrors main.go)
func jsonResponse(w http.ResponseWriter, resp ApiResponse, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(resp)
}

// Mock handler that simulates iperf validation without actual network test
func mockIperfHandler(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, ApiResponse{
			Status: "error",
			Error:  err.Error(),
		}, http.StatusBadRequest)
		return
	}

	// Validate required field
	if req.ServerHost == "" {
		jsonResponse(w, ApiResponse{
			Status: "error",
			Error:  "server_host is required",
		}, http.StatusBadRequest)
		return
	}

	// Apply defaults
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
		req.Bandwidth = 100
	}

	// Return mock response
	jsonResponse(w, ApiResponse{
		Status: "ok",
		Data: map[string]interface{}{
			"server":         req.ServerHost,
			"port":           req.ServerPort,
			"protocol":       req.Protocol,
			"duration_sec":   float64(req.Duration),
			"sent_bytes":     int64(req.Duration * req.Bandwidth * 125000), // approximate
			"bandwidth_mbps": float64(req.Bandwidth),
		},
	}, http.StatusOK)
}

// Mock handler that simulates TWAMP validation without actual network test
func mockTwampHandler(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonResponse(w, ApiResponse{
			Status: "error",
			Error:  err.Error(),
		}, http.StatusBadRequest)
		return
	}

	// Validate required field
	if req.ServerHost == "" {
		jsonResponse(w, ApiResponse{
			Status: "error",
			Error:  "server_host is required",
		}, http.StatusBadRequest)
		return
	}

	// Apply defaults
	if req.ServerPort == 0 {
		req.ServerPort = 862
	}
	if req.Count == 0 {
		req.Count = 10
	}

	// Return mock response
	jsonResponse(w, ApiResponse{
		Status: "ok",
		Data: map[string]interface{}{
			"server":       req.ServerHost,
			"probes":       req.Count,
			"loss_percent": 0.0,
			"rtt_avg_ms":   25.5,
		},
	}, http.StatusOK)
}

// healthHandler returns health status
func healthHandler(w http.ResponseWriter, r *http.Request) {
	jsonResponse(w, ApiResponse{Status: "healthy"}, http.StatusOK)
}

// setupTestRouter creates a router with mock handlers for testing
func setupTestRouter() *mux.Router {
	r := mux.NewRouter()
	r.HandleFunc("/iperf/client/run", mockIperfHandler).Methods("POST")
	r.HandleFunc("/twamp/client/run", mockTwampHandler).Methods("POST")
	r.HandleFunc("/health", healthHandler).Methods("GET")
	return r
}

func TestHealthEndpoint(t *testing.T) {
	router := setupTestRouter()

	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, status)
	}

	var resp ApiResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", resp.Status)
	}
}

func TestIperfEndpoint_ValidRequest(t *testing.T) {
	router := setupTestRouter()

	body := `{"server_host": "test.example.com", "duration": 10}`
	req, err := http.NewRequest("POST", "/iperf/client/run", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, status)
	}

	var resp ApiResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", resp.Status)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("Expected data to be a map")
	}

	if data["server"] != "test.example.com" {
		t.Errorf("Expected server='test.example.com', got '%s'", data["server"])
	}
}

func TestIperfEndpoint_MissingServerHost(t *testing.T) {
	router := setupTestRouter()

	body := `{"duration": 10}`
	req, err := http.NewRequest("POST", "/iperf/client/run", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, status)
	}

	var resp ApiResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Status != "error" {
		t.Errorf("Expected status 'error', got '%s'", resp.Status)
	}

	if resp.Error != "server_host is required" {
		t.Errorf("Expected error message about server_host, got '%s'", resp.Error)
	}
}

func TestIperfEndpoint_InvalidJSON(t *testing.T) {
	router := setupTestRouter()

	body := `{invalid json}`
	req, err := http.NewRequest("POST", "/iperf/client/run", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, status)
	}
}

func TestIperfEndpoint_Defaults(t *testing.T) {
	router := setupTestRouter()

	body := `{"server_host": "test.example.com"}`
	req, err := http.NewRequest("POST", "/iperf/client/run", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp ApiResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("Expected data to be a map")
	}

	// Check defaults were applied
	if data["port"].(float64) != 5201 {
		t.Errorf("Expected default port=5201, got %v", data["port"])
	}
	if data["protocol"] != "TCP" {
		t.Errorf("Expected default protocol=TCP, got %v", data["protocol"])
	}
}

func TestTwampEndpoint_ValidRequest(t *testing.T) {
	router := setupTestRouter()

	body := `{"server_host": "twamp.example.com", "count": 50}`
	req, err := http.NewRequest("POST", "/twamp/client/run", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, status)
	}

	var resp ApiResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Status != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", resp.Status)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("Expected data to be a map")
	}

	if data["server"] != "twamp.example.com" {
		t.Errorf("Expected server='twamp.example.com', got '%s'", data["server"])
	}
	if data["probes"].(float64) != 50 {
		t.Errorf("Expected probes=50, got %v", data["probes"])
	}
}

func TestTwampEndpoint_MissingServerHost(t *testing.T) {
	router := setupTestRouter()

	body := `{"count": 50}`
	req, err := http.NewRequest("POST", "/twamp/client/run", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, status)
	}
}

func TestTwampEndpoint_Defaults(t *testing.T) {
	router := setupTestRouter()

	body := `{"server_host": "twamp.example.com"}`
	req, err := http.NewRequest("POST", "/twamp/client/run", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	var resp ApiResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	data, ok := resp.Data.(map[string]interface{})
	if !ok {
		t.Fatal("Expected data to be a map")
	}

	// Check defaults
	if data["probes"].(float64) != 10 {
		t.Errorf("Expected default probes=10, got %v", data["probes"])
	}
}

func TestContentTypeJSON(t *testing.T) {
	router := setupTestRouter()

	req, err := http.NewRequest("GET", "/health", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	router := setupTestRouter()

	// Try GET on POST-only endpoint
	req, err := http.NewRequest("GET", "/iperf/client/run", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	// Gorilla mux returns 405 for method not allowed
	if status := rr.Code; status != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, status)
	}
}

func TestNotFound(t *testing.T) {
	router := setupTestRouter()

	req, err := http.NewRequest("GET", "/nonexistent", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusNotFound {
		t.Errorf("Expected status %d, got %d", http.StatusNotFound, status)
	}
}
