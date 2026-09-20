# CopilotBurn

[![CI](https://github.com/its-the-vibe/CopilotBurn/actions/workflows/ci.yaml/badge.svg)](https://github.com/its-the-vibe/CopilotBurn/actions/workflows/ci.yaml)

A service designed to record and measure GitHub Copilot AI usage, store usage data in Redis, and visualize monthly consumption against quota via an interactive web dashboard and REST API.

## Features

- Prints **Gday World** on startup
- Pings a Redis server at a configurable interval
- Automatically identifies missing daily Copilot usage data for the current month up to today
- Submits asynchronous command batches to Poppit via Redis list
- Listens for command execution results on Redis Pub/Sub and stores verbatim JSON responses with configurable key prefix and TTL
- **Web Dashboard**: Interactive frontend visualizing daily progression towards monthly quota
  - **Cumulative Daily Chart**: Cumulative AI credit burn curve with quota limit line and burn rate pace
  - **Monthly Total**: Total credits consumed compared against configured monthly quota (e.g. `1,234.50 / 1,500 credits`)
  - **Today's Usage Highlight**: Prominently highlights today's consumption and status
  - **Daily Breakdown Table**: Accessible breakdown per day of the month
  - **Responsive & Accessible**: Mobile-friendly layout with WCAG AA compliance and ARIA attributes
- **REST API (`/api/usage`)**: Programmatic endpoint returning monthly summary and daily breakdown
- Minimal distroless runtime image with embedded static assets
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

# 5. Open the dashboard
open http://localhost:8080/
```

### Docker

```bash
cp config.example.yaml config.yaml
cp .env.example .env
# Edit config.yaml and .env

make docker-up
```

Access the dashboard at `http://localhost:8080/`.

## Configuration

| File | Purpose |
|------|---------|
| `config.yaml` | Redis, Poppit, Quota, and Server settings (git-ignored) |
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

# How often (in seconds) to ping the Redis server
ping_interval_seconds: 5

# Maximum monthly Copilot AI credits quota (default: 1500)
ai_credit_quota: 1500

# Web server port for the dashboard and API (default: 8080)
server:
  port: 8080
```

### Environment Variable Overrides

| Variable | Description | Default |
|----------|-------------|---------|
| `REDIS_PASSWORD` | Redis password | `""` |
| `REDIS_KEY_PREFIX` | Redis key prefix for daily usage | `copilot-burn:` |
| `REDIS_TTL_DAYS` | TTL in days for stored usage records | `90` |
| `AI_CREDIT_QUOTA` | Configurable maximum monthly AI credits quota | `1500` |
| `PORT` / `SERVER_PORT` | HTTP server listening port | `8080` |
| `POPPIT_LIST_NAME` | Redis list name monitored by Poppit | `poppit:notifications` |
| `POPPIT_OUTPUT_CHANNEL` | Redis Pub/Sub channel for Poppit outputs | `poppit:command-output` |

## API Endpoints

### `GET /api/usage`

Retrieves monthly credit usage statistics and daily breakdown.

**Query Parameters:**
- `year` (optional): Filter by year (e.g. `2026`, defaults to current year)
- `month` (optional): Filter by month (`1-12`, defaults to current month)

**Example Response:**
```json
{
  "year": 2026,
  "month": 9,
  "month_name": "September",
  "current_date": "2026-09-20",
  "today_day": 20,
  "days_in_month": 30,
  "quota": 1500,
  "monthly_total": 450.5,
  "today_usage": 12.3,
  "remaining_quota": 1049.5,
  "percentage_used": 30.03,
  "daily_usage": [
    {
      "day": 1,
      "date": "2026-09-01",
      "credits": 25.0,
      "cumulative_credits": 25.0,
      "is_today": false,
      "is_future": false,
      "has_data": true
    }
  ]
}
```

### `GET /api/health`

Returns service health status: `{"status": "ok"}`.

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
├── cmd/copilotburn/   # Application entry point & configuration loading
├── pkg/
│   ├── copilotburn/   # Data fetching, Redis storage, and usage aggregation logic
│   ├── poppit/        # Poppit command submission and result structures
│   └── web/           # HTTP server, REST API handlers, and embedded frontend
├── .github/workflows/ci.yaml
├── config.example.yaml
├── .env.example
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod / go.sum
└── README.md
```
