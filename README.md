# Network Test API

A pure Go REST API for network performance testing, supporting bandwidth measurements (TCP/UDP) and TWAMP latency tests.

## Features

- **Bandwidth Testing** - TCP and UDP throughput measurements
- **TWAMP Testing** - Two-Way Active Measurement Protocol for latency and packet loss
- **Pure Go** - No external dependencies like iperf3 required
- **Fastly Compute Ready** - Can be deployed to Fastly's edge computing platform
- **Interactive Documentation** - HTML documentation served at `/`

## Installation

### Prerequisites

- Go 1.21 or later

### Build

```bash
# Clone the repository
git clone https://github.com/CoreTex/network-test-api.git
cd network-test-api

# Build
make build

# Or manually
go mod tidy
go build -o main .
```

## Usage

### Start the Server

```bash
make run
# or
./main
```

The server starts on port `8080` by default.

### API Endpoints

#### `GET /`
Returns HTML documentation (or JSON schema with `Content-Type: application/json` header).

#### `GET /health`
Health check endpoint.

```bash
curl http://localhost:8080/health
```

Response:
```json
{"status": "healthy"}
```

#### `POST /iperf/client/run`
Run a bandwidth test (TCP or UDP) to a target server.

**Request:**
```json
{
  "server_host": "iperf.example.com",
  "server_port": 5201,
  "duration": 10,
  "protocol": "TCP"
}
```

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| server_host | string | Yes | - | Target server hostname or IP |
| server_port | integer | No | 5201 | Target server port |
| duration | integer | No | 5 | Test duration in seconds |
| protocol | string | No | TCP | Protocol: TCP or UDP |

**Response:**
```json
{
  "status": "ok",
  "data": {
    "server": "iperf.example.com",
    "port": 5201,
    "protocol": "TCP",
    "duration_sec": 10.0,
    "sent_bytes": 125000000,
    "bandwidth_mbps": 100.0
  }
}
```

#### `POST /twamp/client/run`
Run a TWAMP latency test to measure RTT and packet loss.

**Request:**
```json
{
  "server_host": "twamp.example.com",
  "server_port": 862,
  "count": 20,
  "padding": 42
}
```

| Parameter | Type | Required | Default | Description |
|-----------|------|----------|---------|-------------|
| server_host | string | Yes | - | TWAMP server hostname or IP |
| server_port | integer | No | 862 | TWAMP server port |
| count | integer | No | 10 | Number of test probes |
| padding | integer | No | 42 | Padding bytes in packets |

**Response:**
```json
{
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
}
```

## Makefile Commands

```bash
make help          # Show available commands
make setup         # Clean + install deps + build
make build         # Build binary
make run           # Run server
make dev           # Build + run
make test          # Run tests (server must be running)
make clean         # Clean build artifacts
make fastly-build  # Build for Fastly Compute
make fastly-deploy # Deploy to Fastly
```

## Deployment

### Docker

```bash
make docker-build
make docker-run
```

### Fastly Compute

```bash
make fastly-deploy
```

## License

Apache License 2.0 - See [LICENSE](LICENSE) for details.

## Credits

Created by [CoreTex](https://github.com/CoreTex)

## Donate

If you find this project useful, consider buying me a coffee:

[!["Buy Me A Coffee"](https://www.buymeacoffee.com/assets/img/custom_images/orange_img.png)](https://www.buymeacoffee.com/networkcoder)
