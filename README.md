# Network Test API

![Version](https://img.shields.io/badge/version-2.2.0-blue.svg)
![Go](https://img.shields.io/badge/Go-1.21+-00ADD8.svg)
![License](https://img.shields.io/badge/license-Apache%202.0-green.svg)
![Tests](https://github.com/CoreTex/network-test-api/actions/workflows/test.yml/badge.svg)

A pure Go REST API for network performance testing with **native iperf3 protocol support**, TCP/UDP bandwidth measurements, and TWAMP latency tests.

## Features

- **Native iperf3 Protocol** - Compatible with standard iperf3 servers (e.g., iperf.he.net)
- **TWAMP Testing** - RFC 5357 compliant Two-Way Active Measurement Protocol
- **RFC-Compliant Jitter** - IPDV (RFC 3393) and smoothed jitter (RFC 3550)
- **Bandwidth Testing** - TCP and UDP throughput with accurate pacing
- **Parallel Streams** - Multiple concurrent test streams
- **Reverse Mode** - Download tests (server sends, client receives)
- **Hop Count** - Network hop tracking via TTL analysis
- **NTP Sync Detection** - Automatic clock synchronization status
- **Pure Go** - No external binaries required
- **Docker Ready** - Easy containerized deployment

## Documentation

| Document | Description |
|----------|-------------|
| [API Reference](docs/api-reference.md) | Complete API endpoint documentation |
| [TWAMP Guide](docs/twamp.md) | TWAMP client usage and response fields |
| [iperf3 Guide](docs/iperf3.md) | iperf3 client usage and examples |

## Quick Start

### Installation

```bash
# Clone the repository
git clone https://github.com/CoreTex/network-test-api.git
cd network-test-api

# Build
go build -o main .

# Run
./main
```

### Docker

```bash
# Build and run with host networking (recommended)
docker build -t network-test-api .
docker run -d --name network-test-api --network host network-test-api

# Or with port mapping
docker run -d -p 8080:8080 network-test-api
```

### Basic Usage

```bash
# Health check
curl http://localhost:8080/health

# iperf3 bandwidth test
curl -X POST http://localhost:8080/iperf/client/run \
  -H "Content-Type: application/json" \
  -d '{"server_host": "iperf.he.net", "duration": 10}'

# TWAMP latency test
curl -X POST http://localhost:8080/twamp/client/run \
  -H "Content-Type: application/json" \
  -d '{"server_host": "twamp.example.com", "count": 50}'
```

## API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/` | GET | API documentation (HTML/JSON) |
| `/health` | GET | Health check |
| `/iperf/client/run` | POST | Run iperf3 bandwidth test |
| `/twamp/client/run` | POST | Run TWAMP latency test |

## Example Responses

### iperf3 Test

```json
{
  "status": "ok",
  "data": {
    "server": "iperf.he.net",
    "port": 5201,
    "protocol": "TCP",
    "duration_sec": 10.0,
    "sent_bytes": 125000000,
    "bandwidth_mbps": 100.0
  }
}
```

### TWAMP Test

```json
{
  "status": "ok",
  "data": {
    "server": "twamp.example.com",
    "probes": 50,
    "loss_percent": 0.0,
    "rtt_avg_ms": 31.8,
    "rtt_stddev_ms": 1.2,
    "forward_jitter_ms": 0.52,
    "reverse_jitter_ms": 0.48,
    "hops": {
      "forward": {"avg": 10},
      "reverse": {"avg": 10}
    }
  }
}
```

## Development

### Prerequisites

- Go 1.21 or later
- Docker (optional)

### Build & Test

```bash
# Build
make build

# Run tests
make test

# Run with coverage
make test-coverage

# Lint
make lint
```

### Project Structure

```
.
├── main.go              # Main application
├── ntp_linux.go         # Linux NTP detection
├── ntp_other.go         # Non-Linux NTP fallback
├── vendor/              # Vendored dependencies
├── docs/                # Documentation
│   ├── api-reference.md
│   ├── twamp.md
│   └── iperf3.md
├── tests/               # Test suites
│   ├── unit/
│   ├── integration/
│   ├── functional/
│   ├── e2e/
│   └── acceptance/
├── Dockerfile
├── Makefile
└── README.md
```

## Testing

The project includes comprehensive test coverage:

| Test Type | Description | Location |
|-----------|-------------|----------|
| Unit Tests | Test individual functions | `tests/unit/` |
| Integration Tests | Test component interactions | `tests/integration/` |
| Functional Tests | Test API endpoints | `tests/functional/` |
| E2E Tests | Test complete workflows | `tests/e2e/` |
| Acceptance Tests | Test user scenarios | `tests/acceptance/` |

Run all tests:

```bash
make test-all
```

## CI/CD

GitHub Actions automatically runs:
- Unit tests on every push
- Integration tests on pull requests
- Full test suite on releases

## Compatibility

### iperf3 Servers
- iperf3 v3.x servers
- Public servers: iperf.he.net, speedtest.wtnet.de
- perfSONAR pscheduler

### TWAMP Servers
- perfSONAR twampd (v5.x)
- Juniper TWAMP reflector
- Cisco IP SLA TWAMP responder
- RFC 5357 compliant servers

## License

Apache License 2.0 - See [LICENSE](LICENSE) for details.

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests: `make test-all`
5. Submit a pull request

## Changelog

### v2.2.0
- Add RFC 3393 IPDV (IP Packet Delay Variation)
- Add RFC 3550 exponentially smoothed jitter
- Fix RTT calculation to exclude reflector turnaround
- Add hop count tracking from TTL values
- Add NTP sync detection via adjtimex (Linux)
- Add comprehensive test suite
- Add detailed documentation

### v2.1.0
- Add bandwidth limiting parameter (default: 100 Mbit/s)
- Implement token bucket pacing for accurate bandwidth control

### v2.0.0
- Implement native iperf3 protocol support
- Add parallel streams and reverse mode
- Cookie-based authentication

### v1.0.0
- Initial release
- Basic TCP/UDP bandwidth testing
- TWAMP latency testing

## Credits

Created by [CoreTex](https://github.com/CoreTex)

## Support

If you find this project useful, consider buying me a coffee:

[!["Buy Me A Coffee"](https://www.buymeacoffee.com/assets/img/custom_images/orange_img.png)](https://www.buymeacoffee.com/networkcoder)
