# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AI Observer is an OpenTelemetry-compatible observability backend designed for monitoring AI coding tools (Claude Code, OpenAI Codex CLI, Gemini CLI). It provides real-time ingestion of OTLP traces, metrics, and logs with a DuckDB-based storage layer and a React dashboard.

## Build & Development Commands

```bash
# Setup
make setup              # Install all dependencies (go mod download + pnpm install)

# Development
make dev                # Run both backend and frontend in dev mode (parallel)
make backend-dev        # Run only backend (go run ./cmd/server)
make frontend-dev       # Run only frontend (pnpm dev at localhost:5173)

# Build
make all                # Build both backend and frontend
make backend            # Build backend binary to bin/ai-observer
make frontend           # Build frontend for production

# Testing
make test               # Run all tests
make backend-test       # Run Go tests: cd backend && go test -v ./...
make frontend-test      # Run frontend tests: cd frontend && pnpm test

# Run a single Go test
cd backend && go test -v -run TestFunctionName ./path/to/package

# Run a single frontend test
cd frontend && pnpm vitest run src/path/to/file.test.ts

# Linting
make lint               # Run all linters
make backend-lint       # Run golangci-lint
make frontend-lint      # Run ESLint

# Cleanup
make clean              # Remove bin/ and frontend/dist/
```

## Architecture

### Backend (Go)

The backend runs **two HTTP servers** simultaneously:

1. **OTLP Ingestion Server** (port 4318) - Receives telemetry from AI tools
   - `POST /v1/traces` - Trace data (protobuf or JSON)
   - `POST /v1/metrics` - Metrics data
   - `POST /v1/logs` - Log data
   - `POST /` - Auto-detects signal type (Gemini CLI sends to root path instead of `/v1/*`)
   - Supports HTTP/1.1 + h2c (HTTP/2 cleartext), gzip-compressed payloads
   - Auto-detects JSON vs Protobuf format regardless of Content-Type header

2. **API/WebSocket Server** (port 8080) - Serves dashboard and real-time updates
   - `/api/traces`, `/api/metrics`, `/api/logs` - Query endpoints
   - `/api/services`, `/api/stats` - Aggregations
   - `/api/dashboards` - Dashboard CRUD
   - `/ws` - WebSocket for real-time updates to frontend
   - Serves embedded React frontend at root

**Key packages:**
- `internal/otlp/` - Format detection (`format_detector.go`) and decoder interface with proto/JSON implementations
- `internal/storage/` - DuckDB storage layer with separate stores for traces, logs, metrics
- `internal/handlers/` - HTTP handlers for OTLP ingestion (`otlp_*.go`) and query API (`query.go`)
- `internal/websocket/` - Hub/client pattern for real-time broadcasting
- `internal/server/` - Server setup, routing configuration
- `pkg/compression/` - GZIP decompression middleware for incoming OTLP data

**Configuration (environment variables):**
- `AI_OBSERVER_API_PORT` - HTTP server port (default: 8080)
- `AI_OBSERVER_OTLP_PORT` - OTLP ingestion port (default: 4318)
- `AI_OBSERVER_DATABASE_PATH` - DuckDB file path (default: ./data/ai-observer.duckdb)
- `AI_OBSERVER_FRONTEND_URL` - CORS allowed origin (default: http://localhost:5173)
- `AI_OBSERVER_LOG_LEVEL` - Log level: DEBUG, INFO, WARN, ERROR (default: INFO)

### Frontend (React + TypeScript)

Vite-based React app with Tailwind CSS v4:
- `react-router-dom` for routing (Dashboard, Traces, Metrics, Logs pages)
- `zustand` for state management (`stores/telemetryStore.ts` buffers real-time data)
- `recharts` for visualizations
- `@dnd-kit` for drag-and-drop dashboard widgets
- WebSocket singleton (`hooks/useWebSocket.ts`) for live updates

**Key files:**
- `lib/api.ts` - API client with request deduplication
- `stores/dashboardStore.ts` - Dashboard widget state
- `pages/` - TracesPage, MetricsPage, LogsPage, Dashboard

**Path alias:** `@` maps to `./src`

**Dev proxy:** Vite proxies `/api` and `/ws` to backend at localhost:8080

**UI Components:** Always use [shadcn/ui](https://ui.shadcn.com) components from the registry, see [overview](https://ui.shadcn.com/llms.txt) also. Use the shadcn CLI to add new components:

```bash
cd frontend && pnpm dlx shadcn@latest add [component]  # e.g., button, dialog, table
```

Existing components are in `src/components/ui/`. Never manually create UI primitives - add them via CLI instead.

### Database Schema

DuckDB tables defined in `internal/storage/schema.go`:
- `otel_traces` - Span data with nested events and links arrays
- `otel_logs` - Log records with severity and trace context
- `otel_metrics` - All metric types (gauge, sum, histogram, summary, exponential histogram) unified in one table
- `dashboards` / `dashboard_widgets` - User dashboard persistence

All tables indexed on `Timestamp`, `ServiceName`, and relevant query fields.

## External Reference Projects

The `external-projects/` directory contains local checkouts of the AI coding tools that send telemetry to AI Observer:
- `external-projects/claude-code/` - Claude Code CLI examples
- `external-projects/claude-code-npm/` - Claude Code CLI source (minimized) from the npm package
- `external-projects/gemini-cli/` - Gemini CLI source
- `external-projects/codex/` - OpenAI Codex CLI source
- `external-projects/CodexBar` - SwiftUI application showing cost and usage metrics from different providers
- `external-projects/ccusage` - CLI tool for analyzing Claude Code/Codex CLI usage from local JSONL files

**Use these local sources** instead of web search when investigating OTLP telemetry formats, metric names, log events, or span structures. These repos show exactly what telemetry data each tool emits.

## AI Coding Tool Integration

Point AI tools to OTLP endpoint `http://localhost:4318`:

```bash
# Claude Code
export CLAUDE_CODE_ENABLE_TELEMETRY=1
export OTEL_METRICS_EXPORTER=otlp
export OTEL_LOGS_EXPORTER=otlp
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318

# Gemini CLI - add to ~/.gemini/settings.json
# { "telemetry": { "enabled": true, "target": "local", "useCollector": true, "otlpEndpoint": "http://localhost:4318" } }

# OpenAI Codex CLI - add to ~/.codex/config.toml
# [otel]
# exporter = { otlp-http = { endpoint = "http://localhost:4318/v1/logs", protocol = "binary" } }
```

## CLI Options

The `ai-observer` binary uses subcommands:

```bash
ai-observer [command] [options]
```

**Commands:**
| Command | Description |
|---------|-------------|
| (none) | Start the OTLP server (default) |
| `serve` | Explicitly start the OTLP server |
| `import` | Import local sessions from AI tool files |
| `export` | Export telemetry data to Parquet files |
| `delete` | Delete telemetry data from database |
| `setup` | Show setup instructions for AI tools |

**Global Options:**
| Option | Description |
|--------|-------------|
| `-h`, `--help` | Show help message and exit |
| `-v`, `--version` | Show version information and exit |

**Examples:**
```bash
ai-observer                              # Start server (default)
ai-observer import claude-code           # Import Claude Code sessions
ai-observer export all --output ./data   # Export to Parquet files
ai-observer delete all --from 2025-01-01 --to 2025-01-31
ai-observer setup claude-code            # Show Claude Code setup instructions
```

## API Endpoints Reference

### Query API (Port 8080)

**Traces:**
| Endpoint | Method | Query Parameters |
|----------|--------|------------------|
| `/api/traces` | GET | `service`, `search`, `from`, `to`, `limit`, `offset` |
| `/api/traces/recent` | GET | - |
| `/api/traces/{traceId}` | GET | - |
| `/api/traces/{traceId}/spans` | GET | - |

**Metrics:**
| Endpoint | Method | Query Parameters |
|----------|--------|------------------|
| `/api/metrics` | GET | `service`, `from`, `to` |
| `/api/metrics/names` | GET | - |
| `/api/metrics/series` | GET | `name` (required), `service`, `from`, `to`, `interval`, `aggregate` |
| `/api/metrics/batch-series` | POST | Body: array of queries with `id`, `name`, optional `service`, `aggregate`, `interval` |

**Logs:**
| Endpoint | Method | Query Parameters |
|----------|--------|------------------|
| `/api/logs` | GET | `service`, `severity`, `traceId`, `search`, `from`, `to`, `limit`, `offset` |
| `/api/logs/levels` | GET | `from`, `to` |

**Dashboards:**
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/dashboards` | GET/POST | List all / Create new |
| `/api/dashboards/default` | GET | Get default dashboard with widgets |
| `/api/dashboards/{id}` | GET/PUT/DELETE | CRUD by ID |
| `/api/dashboards/{id}/default` | PUT | Set as default |
| `/api/dashboards/{id}/widgets` | POST | Add widget |
| `/api/dashboards/{id}/widgets/positions` | PUT | Update widget positions |
| `/api/dashboards/{id}/widgets/{widgetId}` | PUT/DELETE | Update/delete widget |

**Other:**
- `GET /api/services` - List all services sending telemetry
- `GET /api/stats` - Aggregate statistics
- `GET /ws` - WebSocket for real-time updates
- `GET /health` - Health check

**Note:** `from`/`to` default to last 24 hours if omitted.

## Telemetry Reference

### Claude Code

**Metrics** (all counters):
- `claude_code.session.count` - Sessions started
- `claude_code.token.usage` - Tokens by type (input/output/cache)
- `claude_code.cost.usage` - Cost in USD
- `claude_code.lines_of_code.count` - Lines added/removed
- `claude_code.pull_request.count`, `claude_code.commit.count` - Git activity
- `claude_code.code_edit_tool.decision` - Tool permission decisions
- `claude_code.active_time.total` - Active time in seconds

**Derived Metrics** (computed by AI Observer):
- `claude_code.token.usage_user_facing` - Tokens from user-facing API calls only (excludes tool-routing)
- `claude_code.cost.usage_user_facing` - Cost from user-facing API calls only (excludes tool-routing)

**Events** (logs): `user_prompt`, `api_request`, `api_error`, `tool_result`, `tool_decision`

### Gemini CLI

**Metrics** (mix of counters and histograms):
- Cumulative counters (require delta computation): `session.count`, `token.usage`, `api.request.count`, `file.operation.count`
- Histograms: `api.request.latency`, `tool.call.latency`, `agent.duration`, `startup.duration`
- Also emits `gen_ai.client.token.usage` and `gen_ai.client.operation.duration` (OTel semantic conventions)

**Logs**: `config`, `user_prompt`, `api_request`, `api_response`, `api_error`, `tool_call`, `file_operation`, `agent.start/finish`, `conversation_finished`

**Note:** AI Observer computes `.delta` metrics from cumulative counters for per-interval display.

### OpenAI Codex CLI

**Derived Metrics** (computed from logs):
- `codex_cli_rs.token.usage` - Tokens by type
- `codex_cli_rs.cost.usage` - Cost in USD

**Events** (logs): `conversation_starts`, `api_request`, `user_prompt`, `tool_decision`, `tool_result`

**Traces**: Uses single trace per session with all spans nested. AI Observer treats first-level child spans as virtual traces for usability.

**Note:** `codex.sse_event` logs are filtered out to reduce noise.

## Docker vs Binary Paths

| Setting | Binary Default | Docker Default |
|---------|---------------|----------------|
| Database path | `./data/ai-observer.duckdb` | `/app/data/ai-observer.duckdb` |
| Data directory | `./data/` | `/app/data/` |

Override with `AI_OBSERVER_DATABASE_PATH` environment variable.

## CI/CD

GitHub Actions triggers:

| Trigger | Actions |
|---------|---------|
| Push/PR | Run tests (Go + frontend) |
| Push to main | Build binaries (linux/amd64, darwin/arm64, windows/amd64) |
| Tag `v*` | Create GitHub Release with archives |
| Tag `v*` | Push multi-arch Docker images to Docker Hub |
| Release published | Update Homebrew formula in [ai-observer-homebrew](https://github.com/tobilg/ai-observer-homebrew) tap |

**Creating a release:**
```bash
git tag v1.0.0
git push origin v1.0.0
```

## Generating Test Telemetry

To generate real telemetry data for testing:

1. Start AI Observer: `make backend-dev`
2. Configure an AI tool (see AI Coding Tool Integration above)
3. Run the AI tool and interact with it - telemetry flows automatically
4. View data at `http://localhost:5173` (with `make frontend-dev`) or `http://localhost:8080` (production build)
