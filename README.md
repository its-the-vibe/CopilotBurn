# CopilotBurn

[![CI](https://github.com/its-the-vibe/CopilotBurn/actions/workflows/ci.yaml/badge.svg)](https://github.com/its-the-vibe/CopilotBurn/actions/workflows/ci.yaml)

A service designed to record and measure GitHub Copilot AI usage, store usage data in Redis, and display monthly metrics.

## Features

- Prints **Gday World** on startup
- Pings a Redis server at a configurable interval
- Automatically identifies missing daily Copilot usage data for the current month up to today
- Submits asynchronous command batches to Poppit via Redis list
- Listens for command execution results on Redis Pub/Sub and stores verbatim JSON responses with configurable key prefix and TTL
- Minimal distroless runtime image
- Read-only container filesystem
- Configuration via `config.yaml` + environment variables

## Prerequisites

- [Go 1.24+](https://go.dev/dl/)
- [Docker](https://docs.docker.com/get-docker/) & [Docker Compose](https://docker.com/compose/)
- An external Redis instance

## Quick Start

### Local

```bash
# 1. Copy and customise configuration
cp config.example.yaml config.yaml
cp .env.example .env

# 2. Edit config.yaml to point to your Redis host/port
# 3. Set REDIS_PASSWORD in .env

# 4. Build and run
make run
```

### Docker

```bash
cp config.example.yaml config.yaml
cp .env.example .env
# Edit config.yaml and .env

make docker-up
```

## Configuration

| File | Purpose |
|------|---------|
| `config.yaml` | Redis and Poppit settings, ping interval (git-ignored) |
| `config.example.yaml` | Template – copy to `config.yaml` |
| `.env` | `REDIS_PASSWORD` secret (git-ignored) |
| `.env.example` | Template – copy to `.env` |

### `config.yaml` options

```yaml
redis:
  host: localhost
  port: 6379
  key_prefix: "copilot-burn:"
  ttl_days: 90

poppit:
  list_name: "poppit:notifications"
  output_channel: "poppit:command-output"

ping_interval_seconds: 5
```

## Makefile targets

| Target | Description |
|--------|-------------|
| `make build` | Compile binary to `bin/copilotburn` |
| `make run` | Build and run locally |
| `make test` | Run Go tests |
| `make lint` | Run `go vet` |
| `make docker-build` | Build Docker image |
| `make docker-up` | Start via Docker Compose |
| `make docker-down` | Stop Docker Compose stack |

## Project Layout

```
.
├── cmd/copilotburn/   # Application entry point
├── pkg/
│   ├── copilotburn/   # Copilot usage data fetching and Redis storage logic
│   └── poppit/        # Poppit command submission and result structures
├── .github/workflows/ci.yaml
├── config.example.yaml
├── .env.example
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod / go.sum
└── README.md
```
