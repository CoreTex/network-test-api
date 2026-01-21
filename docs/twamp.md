# TWAMP Client Documentation

## Overview

The TWAMP (Two-Way Active Measurement Protocol) client implements RFC 5357 for measuring network performance metrics including:

- **Round-Trip Time (RTT)** - Total time for a packet to travel to the reflector and back
- **One-Way Delays** - Forward (sender→reflector) and reverse (reflector→sender) delays
- **Jitter** - Packet delay variation (RFC 3393 IPDV and RFC 3550 smoothed jitter)
- **Packet Loss** - Percentage of packets not returned
- **Hop Count** - Number of network hops in each direction

## Endpoint

```
POST /twamp/client/run
```

## Request Parameters

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| `server_host` | string | Yes | - | TWAMP server hostname or IP address |
| `server_port` | integer | No | 862 | TWAMP control port (standard: 862) |
| `count` | integer | No | 10 | Number of test probes to send |
| `padding` | integer | No | 0 | Padding bytes to add to test packets |

## Example Request

```bash
curl -X POST http://localhost:8080/twamp/client/run \
  -H "Content-Type: application/json" \
  -d '{
    "server_host": "twamp.example.com",
    "server_port": 862,
    "count": 50
  }'
```

## Response Fields

### Basic Information

| Field | Type | Description |
|-------|------|-------------|
| `server` | string | Target server hostname |
| `local_endpoint` | string | Local test endpoint (IP:port) |
| `remote_endpoint` | string | Remote test endpoint (IP:port) |
| `probes` | integer | Number of probes sent |
| `loss_percent` | float | Packet loss percentage (0-100) |

### Round-Trip Time (RTT)

The RTT values exclude reflector processing time, making them comparable to ping measurements.

| Field | Type | Description |
|-------|------|-------------|
| `rtt_min_ms` | float | Minimum RTT in milliseconds |
| `rtt_max_ms` | float | Maximum RTT in milliseconds |
| `rtt_avg_ms` | float | Average RTT in milliseconds |
| `rtt_stddev_ms` | float | RTT standard deviation |
| `rtt_raw_ms` | object | Raw RTT including reflector turnaround (min, max, avg, stddev) |

### Reflector Turnaround

| Field | Type | Description |
|-------|------|-------------|
| `reflector_turnaround_ms` | object | Processing time at reflector T3-T2 (min, max, avg) |

### Clock Synchronization

| Field | Type | Description |
|-------|------|-------------|
| `estimated_clock_offset_ms` | float | Estimated clock offset between sender and reflector |
| `sync_status.sender_synced` | boolean | Whether sender clock is NTP synchronized |
| `sync_status.reflector_synced` | boolean | Whether reflector clock is NTP synchronized |
| `sync_status.both_synced` | boolean | Both clocks synchronized |
| `sync_status.sender_error_estimate` | object | Sender's Error Estimate field (RFC 4656) |
| `sync_status.reflector_error_estimate` | object | Reflector's Error Estimate field (RFC 4656) |

### One-Way Delays

**Raw delays** are affected by clock offset between sender and reflector:

| Field | Type | Description |
|-------|------|-------------|
| `forward_delay_raw_ms` | object | Raw forward delay (min, max, avg) |
| `reverse_delay_raw_ms` | object | Raw reverse delay (min, max, avg) |

**Corrected delays** assume symmetric paths (each direction = RTT/2):

| Field | Type | Description |
|-------|------|-------------|
| `forward_delay_corrected_ms` | object | Corrected forward delay (min, max, avg) |
| `reverse_delay_corrected_ms` | object | Corrected reverse delay (min, max, avg) |

### Jitter (Delay Variation)

**RFC 3393 IPDV** (IP Packet Delay Variation) - difference between consecutive packet delays:

| Field | Type | Description |
|-------|------|-------------|
| `forward_ipdv_ms` | object | Forward IPDV (min, max, avg, mean_abs) |
| `reverse_ipdv_ms` | object | Reverse IPDV (min, max, avg, mean_abs) |

**RFC 3550 Jitter** - exponentially smoothed mean absolute IPDV:

| Field | Type | Description |
|-------|------|-------------|
| `forward_jitter_ms` | float | Forward jitter (RFC 3550) |
| `reverse_jitter_ms` | float | Reverse jitter (RFC 3550) |

> **Note:** IPDV calculation cancels out clock offset, providing true one-way delay variation even without synchronized clocks.

### Hop Count

| Field | Type | Description |
|-------|------|-------------|
| `hops.forward` | object | Forward hop count (min, max, avg) |
| `hops.reverse` | object | Reverse hop count (min, max, avg) |

Forward hops are calculated as `255 - SenderTTL` (sender uses TTL=255).
Reverse hops are estimated based on received TTL and assumed initial TTL (64/128/255).

## Example Response

```json
{
  "status": "ok",
  "data": {
    "server": "twamp.example.com",
    "local_endpoint": "192.168.1.100:19234",
    "remote_endpoint": "203.0.113.50:18760",
    "probes": 50,
    "loss_percent": 0.0,
    "rtt_min_ms": 28.5,
    "rtt_max_ms": 35.2,
    "rtt_avg_ms": 31.8,
    "rtt_stddev_ms": 1.2,
    "rtt_raw_ms": {
      "min": 28.55,
      "max": 35.35,
      "avg": 31.88,
      "stddev": 1.25
    },
    "reflector_turnaround_ms": {
      "min": 0.05,
      "max": 0.15,
      "avg": 0.08
    },
    "estimated_clock_offset_ms": 0.15,
    "sync_status": {
      "sender_synced": true,
      "reflector_synced": true,
      "both_synced": true,
      "sender_error_estimate": {
        "synced": true,
        "unavailable": false,
        "scale": 10,
        "multiplier": 1,
        "error_seconds": 0.000976,
        "error_ms": 0.976,
        "raw_value_hex": "0x8A01"
      },
      "reflector_error_estimate": {
        "synced": true,
        "unavailable": false,
        "scale": 5,
        "multiplier": 135,
        "error_seconds": 4.21875,
        "error_ms": 4218.75,
        "raw_value_hex": "0x8587"
      }
    },
    "forward_delay_raw_ms": {
      "min": 14.1,
      "max": 17.8,
      "avg": 15.9
    },
    "forward_delay_corrected_ms": {
      "min": 14.25,
      "max": 17.6,
      "avg": 15.9
    },
    "forward_ipdv_ms": {
      "min": -1.2,
      "max": 1.5,
      "avg": 0.01,
      "mean_abs": 0.45
    },
    "forward_jitter_ms": 0.52,
    "reverse_delay_raw_ms": {
      "min": 14.2,
      "max": 17.6,
      "avg": 15.9
    },
    "reverse_delay_corrected_ms": {
      "min": 14.25,
      "max": 17.6,
      "avg": 15.9
    },
    "reverse_ipdv_ms": {
      "min": -1.1,
      "max": 1.3,
      "avg": -0.02,
      "mean_abs": 0.42
    },
    "reverse_jitter_ms": 0.48,
    "hops": {
      "forward": {
        "min": 10,
        "max": 10,
        "avg": 10.0
      },
      "reverse": {
        "min": 10,
        "max": 10,
        "avg": 10.0
      }
    }
  }
}
```

## Technical Details

### TWAMP Timestamps

TWAMP uses four timestamps for each test packet:

- **T1**: Sender sends test packet
- **T2**: Reflector receives test packet
- **T3**: Reflector sends response
- **T4**: Sender receives response

From these timestamps:
- **Forward delay** = T2 - T1
- **Reverse delay** = T4 - T3
- **Reflector turnaround** = T3 - T2
- **Network RTT** = (T4 - T1) - (T3 - T2) = Forward + Reverse

### Error Estimate Field (RFC 4656)

The Error Estimate is a 16-bit field indicating timestamp accuracy:

| Bits | Field | Description |
|------|-------|-------------|
| 15 | S (Sync) | 1 if clock is synchronized to UTC |
| 14 | Z (Zero) | 1 if timestamp is not available |
| 8-13 | Scale | 6-bit scale factor |
| 0-7 | Multiplier | 8-bit multiplier |

Error in seconds = Multiplier × 2^(-Scale)

### NTP Synchronization Detection

On Linux, the API uses the `adjtimex` syscall to detect NTP synchronization status and estimated error. This provides accurate Error Estimate values in TWAMP packets.

### Compatibility

Compatible with:
- perfSONAR twampd (v5.x)
- Juniper TWAMP reflector
- Cisco IP SLA TWAMP responder
- Any RFC 5357 compliant TWAMP server

### Port Allocation

- Control connection: TCP port 862 (configurable)
- Test packets: UDP ports 18760-19960 (perfSONAR default range)

## Error Handling

| Error | Description |
|-------|-------------|
| `Connect failed` | Cannot establish TCP control connection |
| `Session failed` | TWAMP session negotiation failed |
| `Test creation failed` | Cannot create UDP test session |
| `Test run failed` | Error during test execution |

## Use Cases

1. **Network Latency Monitoring** - Measure RTT to detect network issues
2. **Quality of Service (QoS) Validation** - Verify SLA compliance
3. **Asymmetric Path Analysis** - Compare forward vs reverse delays
4. **Jitter Analysis** - Assess network stability for VoIP/video
5. **Route Analysis** - Track hop count changes over time
