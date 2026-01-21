# iperf3 Client Documentation

## Overview

The iperf3 client implements the native iperf3 protocol for measuring network bandwidth. It is compatible with any standard iperf3 server, including public servers like `iperf.he.net`.

Key features:
- **Native Protocol** - Pure Go implementation of iperf3 protocol
- **TCP/UDP Support** - Both protocols supported
- **Bandwidth Limiting** - Token bucket pacing for accurate bandwidth control
- **Parallel Streams** - Multiple concurrent test streams
- **Reverse Mode** - Download tests (server sends to client)
- **No Dependencies** - No external iperf3 binary required

## Endpoint

```
POST /iperf/client/run
```

## Request Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `server_host` | string | Yes | - | iperf3 server hostname or IP address |
| `server_port` | integer | No | 5201 | iperf3 server port |
| `duration` | integer | No | 5 | Test duration in seconds |
| `parallel` | integer | No | 1 | Number of parallel streams |
| `protocol` | string | No | "TCP" | Protocol: "TCP" or "UDP" |
| `reverse` | boolean | No | false | Reverse mode (download instead of upload) |
| `bandwidth` | integer | No | 100 | Bandwidth limit in Mbit/s |

## Example Requests

### Basic TCP Upload Test

```bash
curl -X POST http://localhost:8080/iperf/client/run \
  -H "Content-Type: application/json" \
  -d '{
    "server_host": "iperf.he.net",
    "duration": 10
  }'
```

### TCP Download Test with Multiple Streams

```bash
curl -X POST http://localhost:8080/iperf/client/run \
  -H "Content-Type: application/json" \
  -d '{
    "server_host": "iperf.he.net",
    "duration": 10,
    "parallel": 4,
    "reverse": true
  }'
```

### UDP Test with Bandwidth Limit

```bash
curl -X POST http://localhost:8080/iperf/client/run \
  -H "Content-Type: application/json" \
  -d '{
    "server_host": "iperf.he.net",
    "protocol": "UDP",
    "duration": 10,
    "bandwidth": 50
  }'
```

### High-Bandwidth Test

```bash
curl -X POST http://localhost:8080/iperf/client/run \
  -H "Content-Type: application/json" \
  -d '{
    "server_host": "local-iperf-server.example.com",
    "duration": 30,
    "parallel": 8,
    "bandwidth": 1000
  }'
```

## Response Fields

| Field | Type | Description |
|-------|------|-------------|
| `server` | string | Target server hostname |
| `port` | integer | Server port used |
| `protocol` | string | Protocol used (TCP/UDP) |
| `duration_sec` | float | Actual test duration in seconds |
| `sent_bytes` | integer | Total bytes sent (upload mode) |
| `received_bytes` | integer | Total bytes received (reverse mode) |
| `bandwidth_mbps` | float | Measured bandwidth in Megabits per second |
| `retransmits` | integer | TCP retransmit count (if available) |

## Example Responses

### Successful Upload Test

```json
{
  "status": "ok",
  "data": {
    "server": "iperf.he.net",
    "port": 5201,
    "protocol": "TCP",
    "duration_sec": 10.05,
    "sent_bytes": 125829120,
    "bandwidth_mbps": 100.12
  }
}
```

### Successful Download Test (Reverse Mode)

```json
{
  "status": "ok",
  "data": {
    "server": "iperf.he.net",
    "port": 5201,
    "protocol": "TCP",
    "duration_sec": 10.02,
    "received_bytes": 524288000,
    "bandwidth_mbps": 418.56
  }
}
```

### Error Response

```json
{
  "status": "error",
  "error": "connect to iperf.example.com:5201 failed: connection refused"
}
```

## Technical Details

### Protocol Implementation

The client implements the iperf3 protocol as follows:

1. **Connection** - Establish TCP control connection to server
2. **Cookie Exchange** - Send 37-byte authentication cookie (Base32 format)
3. **Parameter Exchange** - JSON parameter negotiation with 4-byte length prefix
4. **Stream Creation** - Create data streams (TCP or UDP)
5. **Test Execution** - Send/receive data with pacing
6. **Results Exchange** - Exchange JSON results with server
7. **Cleanup** - Close connections

### State Machine

```
PARAM_EXCHANGE (9) → CREATE_STREAMS (10) → TEST_START (1) →
TEST_RUNNING (2) → TEST_END (4) → EXCHANGE_RESULTS (13) →
DISPLAY_RESULTS (14) → IPERF_DONE (16)
```

### Bandwidth Pacing

The client uses token bucket pacing to achieve accurate bandwidth limiting:

```
target_bytes_per_second = bandwidth_mbps × 1,000,000 / 8 / parallel_streams
```

During the test, the client calculates expected bytes vs actual bytes and sleeps to maintain the target rate.

### Block Sizes

| Protocol | Default Block Size |
|----------|-------------------|
| TCP | 128 KB |
| UDP | 1460 bytes |

### Compatibility

Tested with:
- iperf3 servers (v3.x)
- Public servers: iperf.he.net, speedtest.wtnet.de
- perfSONAR pscheduler iperf3

## Error Handling

| Error | Description |
|-------|-------------|
| `connect failed` | Cannot establish TCP connection to server |
| `send cookie failed` | Failed to send authentication cookie |
| `server denied access` | Server rejected connection (busy or auth failed) |
| `server error` | Server reported an internal error |
| `unexpected state` | Protocol state machine error |
| `create stream failed` | Cannot create data stream |

## Best Practices

### Choosing Duration

- **Quick tests**: 5-10 seconds for basic connectivity checks
- **Accurate measurements**: 30-60 seconds for stable bandwidth readings
- **Long-term monitoring**: 300+ seconds for capacity planning

### Choosing Parallel Streams

- **Single stream**: Best for measuring TCP efficiency and latency
- **4-8 streams**: Better for saturating high-bandwidth links
- **Many streams**: Can help overcome TCP window limitations

### Bandwidth Limiting

- Always set bandwidth limit to avoid overwhelming the network
- Start with lower values and increase gradually
- Consider the server's available bandwidth

### Protocol Selection

- **TCP**: Most common, measures achievable throughput
- **UDP**: Tests for packet loss and jitter at specific bit rates

## Public iperf3 Servers

| Server | Location | Notes |
|--------|----------|-------|
| iperf.he.net | California, USA | Hurricane Electric |
| speedtest.wtnet.de | Germany | wtnet |
| iperf.biznetnetworks.com | Indonesia | Biznet |
| iperf.scottlinux.com | USA | Community |

> **Note**: Public servers may have usage limits and varying availability.

## Use Cases

1. **Bandwidth Testing** - Measure available bandwidth between two points
2. **Network Provisioning** - Verify new circuit capacity
3. **SLA Validation** - Confirm contracted bandwidth
4. **Troubleshooting** - Identify bandwidth bottlenecks
5. **Capacity Planning** - Assess upgrade requirements
6. **Performance Baselines** - Establish normal performance metrics
