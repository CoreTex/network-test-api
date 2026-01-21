# API Reference

## Base URL

```
http://localhost:8080
```

## Content Types

All endpoints accept and return `application/json`.

The root endpoint (`/`) returns HTML documentation by default, or JSON schema when requested with `Content-Type: application/json` header.

## Authentication

No authentication required. The API is designed for internal/trusted network use.

## Response Format

All responses follow this structure:

### Success Response

```json
{
  "status": "ok",
  "data": { ... }
}
```

### Error Response

```json
{
  "status": "error",
  "error": "Error message describing what went wrong"
}
```

## HTTP Status Codes

| Code | Description |
|------|-------------|
| 200 | Success |
| 400 | Bad Request - Invalid JSON or missing required parameters |
| 500 | Internal Server Error - Test execution failed |

---

## Endpoints

### GET /

Returns API documentation.

**Headers:**
- Without `Content-Type: application/json`: Returns HTML documentation
- With `Content-Type: application/json`: Returns JSON API schema

**Example:**

```bash
# Get HTML documentation
curl http://localhost:8080/

# Get JSON schema
curl -H "Content-Type: application/json" http://localhost:8080/
```

---

### GET /health

Health check endpoint to verify the API is running.

**Response:**

```json
{
  "status": "healthy"
}
```

**Example:**

```bash
curl http://localhost:8080/health
```

---

### POST /iperf/client/run

Run an iperf3 bandwidth test.

**Request Body:**

```json
{
  "server_host": "string (required)",
  "server_port": "integer (default: 5201)",
  "duration": "integer (default: 5)",
  "parallel": "integer (default: 1)",
  "protocol": "string (default: 'TCP')",
  "reverse": "boolean (default: false)",
  "bandwidth": "integer (default: 100)"
}
```

**Response:**

```json
{
  "status": "ok",
  "data": {
    "server": "string",
    "port": "integer",
    "protocol": "string",
    "duration_sec": "float",
    "sent_bytes": "integer",
    "received_bytes": "integer",
    "bandwidth_mbps": "float",
    "retransmits": "integer"
  }
}
```

**Example:**

```bash
curl -X POST http://localhost:8080/iperf/client/run \
  -H "Content-Type: application/json" \
  -d '{"server_host": "iperf.he.net", "duration": 10}'
```

See [iperf3 Documentation](iperf3.md) for detailed information.

---

### POST /twamp/client/run

Run a TWAMP latency test.

**Request Body:**

```json
{
  "server_host": "string (required)",
  "server_port": "integer (default: 862)",
  "count": "integer (default: 10)",
  "padding": "integer (default: 0)"
}
```

**Response:**

```json
{
  "status": "ok",
  "data": {
    "server": "string",
    "local_endpoint": "string",
    "remote_endpoint": "string",
    "probes": "integer",
    "loss_percent": "float",
    "rtt_min_ms": "float",
    "rtt_max_ms": "float",
    "rtt_avg_ms": "float",
    "rtt_stddev_ms": "float",
    "rtt_raw_ms": {
      "min": "float",
      "max": "float",
      "avg": "float",
      "stddev": "float"
    },
    "reflector_turnaround_ms": {
      "min": "float",
      "max": "float",
      "avg": "float"
    },
    "estimated_clock_offset_ms": "float",
    "sync_status": {
      "sender_synced": "boolean",
      "reflector_synced": "boolean",
      "both_synced": "boolean",
      "sender_error_estimate": { ... },
      "reflector_error_estimate": { ... }
    },
    "forward_delay_raw_ms": { "min", "max", "avg" },
    "forward_delay_corrected_ms": { "min", "max", "avg" },
    "forward_ipdv_ms": { "min", "max", "avg", "mean_abs" },
    "forward_jitter_ms": "float",
    "reverse_delay_raw_ms": { "min", "max", "avg" },
    "reverse_delay_corrected_ms": { "min", "max", "avg" },
    "reverse_ipdv_ms": { "min", "max", "avg", "mean_abs" },
    "reverse_jitter_ms": "float",
    "hops": {
      "forward": { "min", "max", "avg" },
      "reverse": { "min", "max", "avg" }
    }
  }
}
```

**Example:**

```bash
curl -X POST http://localhost:8080/twamp/client/run \
  -H "Content-Type: application/json" \
  -d '{"server_host": "twamp.example.com", "count": 50}'
```

See [TWAMP Documentation](twamp.md) for detailed information.

---

## Error Responses

### Invalid JSON

```json
{
  "status": "error",
  "error": "invalid character 'x' looking for beginning of value"
}
```

### Missing Required Parameter

```json
{
  "status": "error",
  "error": "server_host is required"
}
```

### Connection Failed

```json
{
  "status": "error",
  "error": "Connect failed: dial tcp: lookup unknown.host: no such host"
}
```

### Test Execution Failed

```json
{
  "status": "error",
  "error": "Test run failed: timeout waiting for response"
}
```

---

## Rate Limiting

The API does not implement rate limiting. Each request initiates a network test that consumes bandwidth and server resources.

**Recommendations:**
- Avoid concurrent tests to the same server
- Allow adequate time between tests (at least test duration + 5 seconds)
- Use reasonable test durations (5-60 seconds)

---

## Timeouts

| Operation | Timeout |
|-----------|---------|
| iperf3 TCP connection | 10 seconds |
| iperf3 stream creation | 5 seconds |
| TWAMP control connection | 5 seconds |
| TWAMP test packet | 5 seconds |

---

## Docker Usage

### Build and Run

```bash
# Build
docker build -t network-test-api .

# Run with host networking (recommended for accurate measurements)
docker run -d --name network-test-api --network host network-test-api

# Run with port mapping
docker run -d --name network-test-api -p 8080:8080 network-test-api
```

### Environment Variables

Currently, no environment variables are supported. Configuration is done via API parameters.

---

## Versioning

The API version is available in the response headers and documentation.

Current version: **2.2.0**

Version history:
- 2.2.0: RFC-compliant jitter, corrected RTT, hop counts
- 2.1.0: Bandwidth limiting with pacing
- 2.0.0: Native iperf3 protocol support
- 1.0.0: Initial release
