# IP Failover Daemon

A Go daemon that provides automatic DNS failover functionality. The application monitors the current public IP address and automatically switches DNS records between primary and secondary IPs when the primary connection fails.

## Features

- **Automatic IP Detection**: Monitors current public IP using multiple external services
- **DNS Provider Support**: Cloudflare, cPanel, AWS Route53, and Hetzner DNS
- **State Persistence**: Remembers last applied IP to avoid redundant updates
- **Prometheus Metrics**: Exposes metrics for monitoring and alerting
- **Structured Logging**: Uses uber-go/zap for high-performance structured logging
- **Configuration Management**: YAML configuration with environment variable overrides
- **Command-Line Interface**: Support for health checks, version info, and help
- **Cross-Platform Builds**: Single script builds for Linux, macOS, and Windows
- **Docker Support**: Distroless containers with multi-architecture support
- **Graceful Shutdown**: Proper signal handling for clean shutdowns
- **High Test Coverage**: 60%+ code coverage with comprehensive unit tests

## Architecture

### Core Components

1. **IP Monitor**: Polls external services to determine current public IP
2. **DNS Manager**: Manages DNS records across multiple providers
3. **State Manager**: Persists the last applied IP to avoid redundant updates
4. **Metrics Exporter**: Exposes Prometheus metrics for monitoring
5. **Configuration Manager**: Handles YAML configuration with environment overrides

### Key Interfaces

- `DNSProvider`: Interface for DNS operations (Cloudflare, cPanel, Route53, Hetzner implementations)
- `IPChecker`: Interface for IP detection services
- `StateStore`: Interface for persisting application state
- `MetricsCollector`: Interface for metrics collection

## Installation

### From Source

```bash
git clone https://github.com/devhat/ipfailover.git
cd ipfailover

# Build single binary
make build

# Build all platform binaries
make build-all
```

### Using Docker

```bash
# Build Docker image
make docker-build

# Run container
make docker-run
```

### Using Docker Compose

```bash
docker-compose up -d
```

## Configuration

The application uses YAML configuration with the following key sections:

```yaml
poll_interval: "30s"
check_endpoints:
  - "https://ifconfig.io/ip"
  - "https://api.ipify.org"

primary_ip: "203.0.113.10"
secondary_ip: "198.51.100.77"

state_file: "/var/lib/ipfailover/state.json"
metrics_addr: ":8080"
log_level: "info"

dns:
  - name: "home.example.com"
    type: "A"
    provider: "cloudflare"
    ttl: 300
    cloudflare:
      api_token: "${CLOUDFLARE_API_TOKEN}"
      zone_id: "${CLOUDFLARE_ZONE_ID}"
      proxied: false
```

### Environment Variables

- `CLOUDFLARE_API_TOKEN`: Cloudflare API token with Zone.DNS.Edit permission
- `CLOUDFLARE_ZONE_ID`: Cloudflare zone ID
- `CPANEL_USERNAME`: cPanel username (for cPanel provider)
- `CPANEL_API_TOKEN`: cPanel API token (for cPanel provider)
- `AWS_ACCESS_KEY_ID`: AWS access key (for Route53 provider)
- `AWS_SECRET_ACCESS_KEY`: AWS secret key (for Route53 provider)
- `HETZNER_API_TOKEN`: Hetzner DNS API token (for Hetzner provider)
- `HETZNER_ZONE_ID`: Hetzner DNS zone ID (for Hetzner provider)

## Usage

### Command Line

```bash
# Normal operation
./ipfailover -config /path/to/config.yaml

# Health check
./ipfailover -health-check -config /path/to/config.yaml

# Show version
./ipfailover -version

# Show help
./ipfailover -help
```

### Docker

```bash
docker run -d \
  --name ipfailover \
  -p 8080:8080 \
  -v /path/to/config.yaml:/app/config/config.yaml:ro \
  -e CLOUDFLARE_API_TOKEN=your_token \
  -e CLOUDFLARE_ZONE_ID=your_zone_id \
  ipfailover:latest
```

### Kubernetes

```bash
kubectl apply -f k8s-deployment.yaml
```

## DNS Provider Support

### Cloudflare

- Uses Cloudflare API v4
- Requires API token with Zone.DNS.Edit permission
- Supports A/AAAA records with TTL and proxied settings
- Implements find-or-create pattern for records

### cPanel

- Uses cPanel UAPI ZoneEdit endpoints
- Requires base URL, username, API token, and zone
- Supports A/AAAA records with TTL
- Implements find-or-create pattern for records

### AWS Route53

- Uses AWS SDK v2 for Go
- Requires AWS access key, secret key, region, and hosted zone ID
- Supports A/AAAA records with TTL
- Implements find-or-create pattern for records

### Hetzner DNS

- Uses Hetzner DNS API v1
- Requires API token and zone ID
- Supports A/AAAA records with TTL
- Implements find-or-create pattern for records
- Based on [Hetzner DNS API documentation](https://dns.hetzner.com/api-docs#tag/Records)

## Metrics

The application exposes Prometheus metrics on the `/metrics` endpoint:

- `ipfailover_checks_total`: Total IP checks performed
- `ipfailover_check_errors_total`: Failed IP checks
- `ipfailover_updates_total{provider,record}`: DNS updates by provider/record
- `ipfailover_update_errors_total{provider,record}`: Failed DNS updates
- `ipfailover_current_ip_info{ip="x.x.x.x"}`: Current detected IP
- `ipfailover_last_change_timestamp_seconds`: Timestamp of last IP change

## Health Checks

The application provides built-in health check functionality:

- **Command-line health check**: `./ipfailover -health-check -config /path/to/config.yaml`
- **Docker health check**: Uses built-in health check command
- **Kubernetes health check**: Uses built-in health check command
- **Metrics endpoint**: `/metrics` for Prometheus metrics

## Development

### Prerequisites

- Go 1.24+
- Docker (optional)
- Make

### Building

```bash
# Build single binary
make build

# Build all platform binaries
make build-all
```

### Testing

```bash
make test
make test-coverage
```

### Running Tests with Coverage Threshold

```bash
make test-coverage-check
```

### Linting

```bash
make lint
```

### Docker Development

```bash
# Build Docker image
make docker-build

# Build multi-platform Docker images
make docker-build-all

# Run container
make docker-run
```

## Project Structure

```
├── cmd/ipfailover/          # Main application
├── internal/
│   ├── config/              # Configuration management
│   ├── dns/                 # DNS provider implementations
│   ├── ipchecker/          # IP detection services
│   ├── metrics/             # Prometheus metrics
│   └── state/               # State management
├── pkg/
│   ├── errors/              # Custom error types
│   └── interfaces/          # Core interfaces
├── scripts/                 # Build scripts
├── testdata/                # Test configuration files
├── Dockerfile               # Docker build configuration
├── docker-compose.yml       # Docker Compose configuration
├── k8s-deployment.yaml      # Kubernetes deployment
├── Makefile                 # Build automation
└── README.md               # This file
```

## Security Considerations

- API tokens stored in configuration (support for env var overrides)
- HTTPS-only for external API calls
- Input validation for all external data
- No sensitive data in logs
- Non-root user in Docker containers
- Systemd security settings

## Performance Considerations

- Configurable poll intervals to balance responsiveness vs. API usage
- Efficient DNS record management (find-or-create pattern)
- Minimal memory footprint
- Graceful handling of API rate limits
- Structured logging with appropriate log levels

## Monitoring and Observability

- Prometheus metrics for operational visibility
- Structured logging for debugging
- Health check endpoints for container orchestration
- State persistence for recovery scenarios

## Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Add tests for new functionality
5. Ensure tests pass with 60%+ coverage
6. Submit a pull request

## License

This project is licensed under the Apache License 2.0 - see the LICENSE file for details.

## Support

For issues and questions, please open an issue on GitHub.
