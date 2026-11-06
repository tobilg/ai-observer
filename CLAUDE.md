# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

AI Observer is an OpenTelemetry-compatible observability backend designed for monitoring AI coding tools (Claude Code, OpenAI Codex CLI, Gemini CLI, GitHub Copilot, OpenCode). It provides real-time ingestion of OTLP traces, metrics, and logs, can import or watch local session files (Claude Code, Codex CLI, Gemini CLI), stores everything in DuckDB, and serves a React dashboard embedded in the Go binary.

Record user-facing changes in `CHANGELOG.md` under `[Unreleased]` (Keep a Changelog format).

## Build & Development Commands

```bash
# Setup
make setup              # Install all dependencies (go mod download + pnpm install)

# Development
make dev                # Run both backend and frontend in dev mode (parallel)
make backend-dev        # Run only backend (go run ./cmd/server)
make backend-dev-watch  # Run backend in watch mode (go run ./cmd/server watch)
make frontend-dev       # Run only frontend (pnpm dev at localhost:5173)

# Build
make all                # Build both backend and frontend
make backend            # Build frontend, then backend binary to bin/ai-observer
make frontend           # Build frontend into backend/internal/frontend/dist (embedded)

# Testing
make test               # Run all tests
make backend-test       # Run Go tests: cd backend && go test -v ./...
make frontend-test      # Run frontend tests: cd frontend && pnpm test
make coverage           # Backend + frontend coverage reports

# Run a single Go test
cd backend && go test -v -run TestFunctionName ./path/to/package

# Run a single frontend test
cd frontend && pnpm vitest run src/path/to/file.test.ts

# Linting
make lint               # Run all linters
make backend-lint       # Run golangci-lint (backend/.golangci.yml: govet, ineffassign, staticcheck SA*, unused)
make frontend-lint      # Run ESLint

# Pricing data (docs/pricing.md)
make pricing-update     # Refresh the built-in prices from models.dev (review the diff)
make check-pricing      # Fail when a pricing file's lastUpdated is more than 7 days old

# Release notes
make release-notes TAG=vX.Y.Z

# Cleanup
make clean              # Remove bin/ and built frontend assets
```

**Toolchain:** Go 1.26 (per `go.mod`; CI and Dockerfile also use 1.26), Node 22, pnpm 10. CGO is required for DuckDB. The backend embeds `backend/internal/frontend/dist`, so build the frontend before building or testing the backend from a clean checkout.

**pnpm version:** `frontend/package.json` pins pnpm via `packageManager` (also used in CI, the Dockerfile and `.tool-versions`); a newer global pnpm switches to the pinned version automatically inside `frontend/`. Update all four together. Packages allowed to run build scripts (esbuild) are listed in `pnpm-workspace.yaml` both as `onlyBuiltDependencies` (pnpm 10) and `allowBuilds` (newer pnpm, which ignores the former and fails with `ERR_PNPM_IGNORED_BUILDS`: the cause of the failed v0.6.0 Docker build, which installed an unpinned pnpm 12).

CI additionally runs `go test -race ./... -timeout 120s` on macOS.

**Test isolation:** tests that import session files must point `AI_OBSERVER_CLAUDE_PATH`, `AI_OBSERVER_CODEX_PATH` and `AI_OBSERVER_GEMINI_PATH` at generated fixtures with `t.Setenv` (see `setupFromFilesFixtures` in `internal/exporter/exporter_test.go`); an unset override falls back to the developer's real session directories. The server tests bind ports 18080/14318, so don't run manual instances on those ports.

## Architecture

### Backend (Go)

The backend runs **two HTTP servers** simultaneously:

1. **OTLP Ingestion Server** (port 4318) - Receives telemetry from AI tools
   - `POST /v1/traces` - Trace data (protobuf or JSON)
   - `POST /v1/metrics` - Metrics data
   - `POST /v1/logs` - Log data
   - Supports HTTP/1.1 + h2c (HTTP/2 cleartext), gzip-compressed payloads, 10MB payload limit
   - Auto-detects JSON vs Protobuf format regardless of Content-Type header

2. **API/WebSocket Server** (port 8080) - Serves dashboard and real-time updates (HTTP/1.1 only, no h2c)
   - `/api/traces`, `/api/metrics`, `/api/logs`, `/api/sessions` - Query endpoints
   - `/api/services`, `/api/stats` - Aggregations
   - `/api/dashboards` - Dashboard CRUD
   - `/ws` - WebSocket for real-time updates to frontend
   - Serves embedded React frontend at root
   - `/mcp` - MCP endpoint for agents, only with `AI_OBSERVER_MCP=true` or `--mcp` (otherwise `404` with a hint)

**`watch` mode starts only the API server** (`ListenAndServeAPIOnly`), so OTLP ingestion is not available while watching local files.

**Network:** both servers listen on `AI_OBSERVER_HOST` (default `127.0.0.1`; the Dockerfile sets `0.0.0.0`). The API router rejects `Host` headers that are not an IP, `localhost`, the frontend URL host or in `AI_OBSERVER_ALLOWED_HOSTS` (`server.HostCheck`, DNS rebinding protection). Both routers and the WebSocket upgrade reject requests whose `Origin` is not loopback, a CORS origin, an allowed host, or same-origin with an IP/`localhost`/allowed host (`server.OriginPolicy`, CSRF protection: OTLP decodes JSON regardless of Content-Type, so a cross-site `text/plain` POST would otherwise be accepted; same-origin alone is not trusted because a rebound domain is same-origin with the OTLP port, which has no Host check). Requests without `Origin` (exporters, CLIs) pass. With `AI_OBSERVER_AUTH_TOKEN` set, `server.TokenAuth` requires `Authorization: Bearer <token>` on OTLP and API requests; the dashboard trades `?token=` for an HttpOnly `SameSite=Strict` cookie holding an HMAC of the token, never the token (`/health` and static files are exempt). Listening beyond loopback without a token logs a warning. API bodies are limited to 1 MB (`413`, also when exceeded while decoding: `handlers.decodeBody`). With `AI_OBSERVER_RETENTION_DAYS` set, the server deletes older telemetry on startup and hourly, a day per transaction so ingestion is not blocked (`server/retention.go`). `serveUntilDone` in `cmd/server/main.go` handles SIGINT/SIGTERM and returns only after `Server.Shutdown` has closed storage.

**OTLP ingestion pipeline:** decode (`otlp.GetDecoderWithDetection`; logs and metrics hash the body while it is read) → skip retried log/metric batches (`handlers.batchDedup`: a body identical to one stored in the last 15 minutes is acknowledged without storing it; in memory, so a retry after a restart is stored again) → convert to row structs (`otlp.Convert*`) → derive metrics → insert into DuckDB, with secrets redacted (`internal/redact`; records and their derived metrics in one transaction, `InsertTelemetry`; a failed insert answers `503` + `Retry-After`, which exporters retry, never `500`, which they drop; oversized bodies `413`) → broadcast a content-free notification over the WebSocket hub (`websocket.SpansStored`/`LogsStored`/`MetricsStored`: count, services, timestamps; one JSON message per frame; the hub drops messages when its broadcast channel is full and disconnects clients whose buffer is full) → respond in the request's encoding (`writeExportResponse`). Derived metrics:
- Gemini cost from token usage (`otlp.DeriveGeminiCostMetrics`, over all points of a request). Gemini's `input` tokens include the cached ones, so the input cost uses input minus the `cache` point of the same export (same series attributes apart from `type`, same timestamp); imports use `pricing.CalculateGeminiCost` per response. Gemini sends cumulative counters; the cost point keeps the token point's temporality and attributes (`type` renamed to `token_type`), so it is a cumulative series too and queries compute its per-interval increases. Never derive a per-interval value from a cumulative point at ingestion. Cumulative counters are stored as they are; the `.delta` names of earlier versions are query-time aliases (`storage.deltaAliases`), not listed by `/api/metrics/names`
- Claude Code user-facing token/cost metrics (`otlp/claude_user_facing_metrics.go`)
- Codex token/cost from `response.completed` log events (`ExtractCodexMetrics`, called from `otlp/logs.go`)
- GitHub Copilot token/cost from chat spans (`DeriveCopilotMetricsFromSpans`, passed to `DuckDBStore.InsertTraces`, which derives them only from spans not stored yet, so a resent batch adds nothing)

**Local file ingestion** (Claude Code, Codex CLI, Gemini CLI only):
- `import` - full import via one `SessionParser` per tool; each file's records and its `import_state` row are written in one transaction (`InsertImportedFile`), and failed files are always listed; any failure makes `Import` return `importer.ErrIncomplete` (exit status 1). Records from files carry the file path in `SourceFile` (logs, metrics, spans); re-importing a file that was imported before deletes its records in the import's range in the same transaction, so changed, forced and after-`watch` imports never duplicate. For files whose state has `source_tracked = false` (records stored before migration 3) the replacement also deletes the imported records without `SourceFile` that equal the new ones (`FileRecords.Legacy`, `deleteLegacyRecords`: matched by time, service and identity, only rows with `import_source`, never OTLP data). A date range that leaves out records of a file makes its state `partial`: the next import reads it again, and when records after the range were left out the watch offsets are 0 so `watch` reads the file again and replaces its records. Unchanged files are recognized by `file_size`/`file_mtime_ns` without hashing, and a changed file is hashed once per run (`StateManager.fileHash`). `--purge` deletes only the imported tools' services in the range and re-imports all of their files in it (`purgeRanges`), per tool in one transaction (`DuckDBStore.InImportTx`; a failed file rolls the tool back); with `--from`/`--to` unchanged files are rebuilt too. Date flags are local days (`importer.ParseDateArg`, also used by `export` and `delete`). The import state records `ParsedBytes`/`MessageCount` (end of the last complete line, a final line without newline counting when it is valid JSON like in watch; Gemini message count) as the watch offset, so `watch` resumes where the import stopped. The preview pass (parse all files, show counts, confirm) only runs for the interactive prompt, `--dry-run` and `--purge`; with `--yes` each file is parsed once (`importOnce`). Parsers must set `ImportResult.FileHash` to the hash of the bytes they parsed (hash while reading, e.g. `io.TeeReader`), never by re-reading the file afterwards. Read JSONL with `jsonlLines` (no line-length limit), never `bufio.Scanner`: session files contain lines of several MB
- `watch` - fsnotify-based incremental ingestion with per-file debounce; the startup scan of existing files (and `--backfill`) runs in the background, so `Watcher.Start` returns right away and `Stop` interrupts the scan (tests wait with `waitInitialScan`); the event loop runs during the scan (so the kernel's event queue cannot overflow on a long backfill), but files changed meanwhile are only recorded (`scanPending`) and processed when the scan ends (`scanExisting`), so an event never stores a file's existing records before the scan has recorded where watching starts; persists byte offsets, message counts, and parser state in `import_state`. A file is never processed twice at once: events during a run trigger one re-run afterwards (`Watcher.processExclusive`). Gemini resumes by message count, the JSONL tools by byte offset (parser context is rebuilt from the file prefix when the parser state is empty). A file shorter than what was read, or left partial by a ranged import, is read again and its records replaced (`InsertWatcherBatch(..., replace)`, legacy records included). Claude watch state keeps only the last 1,000 usage dedup keys. Watch converts records with the importers' code (`importer.ConvertGeminiMessages`, `importer.ConvertCodexEntry`, `ClaudeParser.CreateTranscriptLogs`/`UsageRecords`), so import and watch store the same records; only line reading, resuming and Claude activity reconstruction differ
- Claude Code activity metrics (lines of code, commits, PR links, sessions, active time) are reconstructed in full imports only, not in watch mode
- Repository/branch attribution is added to Claude Code and Codex logs and token/cost metrics in both modes (`importer/repos.go`, `watcher/repository.go`)

**Key packages:**
- `internal/otlp/` - Format detection (`format_detector.go`), decoder interface with proto/JSON implementations, OTLP → row conversion, derived metrics
- `internal/storage/` - DuckDB storage layer with separate stores for traces, logs, metrics, dashboards, import state. `session_store.go` reads all logs and spans of a session (`SessionLogs`, `SessionSpans`; a time range would cut off sessions that span more than the listed range, and spans that outlast a session's logs). Logs and spans store `SessionId` (first ID in `tools.SessionIDKeys` order) and `SessionIds` (all aliases, `VARCHAR[]`) at insert time (`sessionIDColumns`); query sessions through these columns, never by parsing attribute JSON. `SourceFile` is the session file a record was read from (empty for OTLP); the `insert*Tx` helpers take it, and exports leave it out. Metric series (`queryMetricSeriesInternal`) turn CUMULATIVE points into per-series increases (LAG over Attributes + ResourceAttributes, 24h lookback) so cumulative and delta counters aggregate alike. Deleting metric rows (`deleteInRange`: delete, retention, purge) first keeps the last point of each cumulative series in `metric_baselines`, which series use as the previous point, and prunes `metric_names`. `metric_names` (base schema, filled by `insertMetricsTx`) answers metric name lists without scanning points. List queries (logs, traces, sessions, metric points) take a `Page`: the first page has no cursor, the next passes the previous page's `NextCursor` (keyset pagination on the sort key plus a tiebreaker: `rowid` for logs and metric points, `TraceId` for traces, `SessionId, ServiceName` for sessions; `newestFirstAfter`, `encodeCursor`/`decodeCursor`), and `Offset` remains for API clients, up to `maxOffset` (10,000; deeper offsets return `ErrOffsetTooLarge`, a 400, since an offset page sorts every row before it). They fetch one row more than the page to set `HasMore`. Logs, metric points and traces count `Total` with a separate `COUNT(*)` (a `COUNT(*) OVER ()` in the page query materializes every matching row and was 10x slower for logs; for traces it gained nothing, since the page query aggregates every trace anyway). Sessions page through the sessions active in the range by their last activity there, count the grouped sessions with a window count in the same query (2.5x faster), and then read each listed session's whole extent, so start, count and model are not clipped to the range (`fillSessionExtents`). `NewDuckDBStore` sets `late_materialization_max_rows` to `maxPageSize + 1`: above it (DuckDB default 50) an `ORDER BY ... LIMIT` reads wide columns such as log bodies for every row in range (15x slower). Search conditions come from `containsCondition`, which adds `ESCAPE` only for terms with `%`, `_` or `\` (it halves `ILIKE` speed). Searches read every body in range, twice (count and page); measured alternatives were not better: `contains(lower(...))` 50% slower, case-insensitive `regexp_matches` 12% faster but needs regex escaping, and one query with a window count joined back to the page 2.6x slower. Build `WHERE`/`HAVING` clauses with `conditions` (`query_helpers.go`), which keeps each condition with its arguments. Telemetry writes go through `inWriteTx` + DuckDB appenders (`appender.go`); `writeTx.redact` (the store's redactor, `SetRedactor`, set by `server.NewRedactor` in `serve`/`watch`/`import`) redacts text in `insertLogsTx`, `spanRow` and `insertMetricsTx`, after session IDs are read, so every ingestion path is covered (resource and scope attribute maps, shared by the records of an export, once per transaction: `writeTx.redactShared`); values for `JSON` columns must be wrapped in `rawJSON(...)`, otherwise the appender stores them as JSON strings and `json_extract` returns nothing. `s.mu` serializes writes only; reads take no lock (DuckDB MVCC). A single writer goroutine with group commit was measured and not adopted: running 8 queued OTLP-sized inserts in one transaction gained at most ~25% (2.8 → 2.1 ms per request, dominated by appender work rather than commits), while serialized writes already sustain ~350 requests/s
- `internal/handlers/` - HTTP handlers for OTLP ingestion (`otlp_*.go`), query API (`query.go`), dashboards (`dashboard.go`)
- `internal/importer/` - Session file parsers (`claude.go`, `codex.go`, `gemini.go`), Claude activity reconstruction (`claude_activity.go`), repository attribution
- `internal/watcher/` - File watcher and incremental per-tool processors
- `internal/pricing/` - Embedded pricing JSON (`data/*.json`), model alias resolution, cost calculation; `LoadOverrides` merges files from `AI_OBSERVER_PRICING_DIR` at startup, such as the files written by `ai-observer pricing update`. All four price files come from models.dev (`models_dev.go`: `ModelsDevFetcher` reads `https://models.dev/api.json`, MIT; `UpdatePricingFile` merges a provider into its file per `ModelsDevTargets`, keeping models models.dev lacks, never undoing deprecation, refusing changes over 10x; `pricing_file.go`: `FormatPricingFile` keeps the built-in layout so refreshes diff cleanly, enforced by a test). The providers' own APIs return no prices. `make pricing-update` sets `lastUpdated`, which the daily `Pricing freshness` workflow (`scripts/check-pricing-age.sh`) requires to be at most 7 days old. `github_models_catalog.json` is the final snapshot of the retired GitHub Models catalog (Copilot alias matching only) and is not checked. Claude imports support pricing modes `auto` (default: use recorded `costUSD`, else calculate), `calculate`, `display`
- `internal/traceview/` - Assembles the trace detail views from storage span reads (`traceview.Load`, used by the trace handler): whole OTel traces, and Codex operations (a turn's spans and their descendants within a Codex session trace)
- `internal/transcript/` - Assembles session transcripts from the logs and spans storage reads (`transcript.Load`, used by the transcript handler): imported transcript messages, OTLP events and GenAI span content become one ordered message list; span selection follows Copilot session aliases
- `internal/tools/` - Provider table (`providers`): tool names ↔ OTLP `service.name`, display names, whether session files can be imported, and each tool's transcript event roles (`TranscriptRole`). Also the attribute key lists the tools share, in priority order (`SessionIDKeys`, `ModelKeys`, `ToolNameKeys`, `ToolInputKeys`, `ToolOutputKeys`). Importer sources, export sources and `GET /api/providers` (dashboard service names) derive from it
- `internal/attrval/` - Typed reads (`Int`, `Float`, `Bool`) from string attribute maps, taking candidate keys in priority order; use these instead of ad-hoc parsing
- `internal/exporter/`, `internal/deleter/` - Parquet export and data deletion for the `export`/`delete` commands. They and the importer write to an injectable writer and read confirmations from an injectable reader (`Options.Out`/`In`, `Importer.SetIO`), never `fmt.Print*`/`os.Stdin` directly
- `internal/mcpserver/` - MCP server (official Go SDK `modelcontextprotocol/go-sdk`, Streamable HTTP, stateless with JSON responses) with read-only tools over storage, `transcript.Load` and `traceview.Load`: `search_conversations` (`DuckDBStore.SearchSessions`: records of session logs and spans containing all terms as substrings, in any order, in the body or attribute values (not keys; `termCondition` checks the JSON text first as a cheap filter), longest term checked first since DuckDB evaluates the conditions in order; the query is split into words and "quoted phrases" by `searchTerms`; snippets are cut from redacted text; whole-session extents, from spans for sessions without logs), `list_sessions`, `get_transcript` (paged in memory, text cut to `max_chars`), `usage_summary` (cost/token metrics per tool via `QueryMetricTotals`, which groups aggregates by an attribute such as `repository`, or daily series), `search_logs`, `list_traces`, `get_trace` (depth-first span tree), `list_metric_names`, `query_metrics`, plus the resource `ai-observer://sessions/{id}/transcript`. Times accept RFC 3339, dates or ages (`7d`). Results are redacted again with the store's redactor (covers data stored before redaction). `usage_summary` leaves token types contained in another (`usageMetrics.partOf`: cached input, Codex reasoning) out of `total_tokens`. MCP requests have no timeout but are cancelled when shutdown starts (`endOnStop`), since `http.Server.Shutdown` would wait for them. Mounted on the API router behind HostCheck/OriginPolicy/TokenAuth (`/mcp` is in `TokenAuth`'s protected paths); the SDK's own localhost check is off since HostCheck covers it, and `/mcp` is exempt from the 5-second request timeout (`exceptPath`)
- `internal/redact/` - Secret and personal-data redaction (`AI_OBSERVER_REDACT`: `secrets` default, `all`, `off`): issuer formats (`sk-ant-`, `ghp_`, `AKIA`, JWTs, PEM keys, `Authorization` headers, URL credentials) and credential-looking assignment values; mode `all` adds emails (pseudonym with hash), Luhn-checked cards, IBANs, international phone numbers, US SSNs, public IPv4. Deterministic replacements (`[REDACTED:kind]`) keep metric series stable. Rules skip strings without their literal hints or enough digits (keep it that way: about 130 MB/s). Build credential-shaped test data at runtime (`fake(...)`), never as literals, so secret scanners do not flag the repository
- `internal/websocket/` - Hub/client pattern for real-time broadcasting; the hub holds the origin check (`Hub.SetOriginCheck`) and is stopped on shutdown (`Hub.Stop`)
- `internal/server/` - Server setup, routing configuration
- `internal/frontend/` - Embedded SPA handler
- `internal/middleware/` - OTLP payload limit, API request timeout
- `pkg/compression/` - GZIP decompression middleware for incoming OTLP data

**Configuration (environment variables):**
- `AI_OBSERVER_HOST` - Interface both servers listen on (default: 127.0.0.1; Docker: 0.0.0.0)
- `AI_OBSERVER_ALLOWED_HOSTS` - Extra host names accepted by the API and as browser origins, comma-separated (`*` disables the Host and Origin checks)
- `AI_OBSERVER_API_PORT` - HTTP server port (default: 8080)
- `AI_OBSERVER_OTLP_PORT` - OTLP ingestion port (default: 4318)
- `AI_OBSERVER_DATABASE_PATH` - DuckDB file path (default: ./data/ai-observer.duckdb)
- `AI_OBSERVER_FRONTEND_URL` - CORS allowed origin (default: http://localhost:5173)
- `AI_OBSERVER_LOG_LEVEL` - Log level: DEBUG, INFO, WARN, ERROR (default: INFO). Successful HTTP requests are logged at DEBUG
- `AI_OBSERVER_PRICING_DIR` - Directory with pricing files overriding/adding to the embedded ones (`docs/pricing.md`)
- `AI_OBSERVER_RETENTION_DAYS` - Delete telemetry older than this many days in `serve`/`watch` (default: keep all)
- `AI_OBSERVER_AUTH_TOKEN` - Shared token required on OTLP and API requests (default: no authentication)
- `AI_OBSERVER_REDACT` - Redaction before storage: `secrets` (default), `all` (secrets and personal data), `off`
- `AI_OBSERVER_MCP` - Serve the MCP endpoint `/mcp` on the API port (default: false; also `--mcp` on `serve` and `watch`)
- `AI_OBSERVER_CLAUDE_PATH` - Custom Claude Code config directory (import/watch)
- `AI_OBSERVER_CODEX_PATH` - Custom Codex CLI home directory (import/watch)
- `AI_OBSERVER_GEMINI_PATH` - Custom Gemini CLI home directory (import/watch)

Invalid numbers (ports, retention days) are logged as warnings and replaced by the default (`config.getEnvInt`).

### Supported Tools

| Tool | `service.name` | Ingestion |
|------|----------------|-----------|
| Claude Code | `claude-code` | OTLP, import, watch |
| OpenAI Codex CLI | `codex_cli_rs` | OTLP, import, watch |
| Gemini CLI | `gemini_cli` | OTLP, import, watch |
| GitHub Copilot (VS Code) | `copilot-chat` | OTLP only |
| GitHub Copilot CLI | `github-copilot` | OTLP only |
| OpenCode | `opencode` | OTLP only (via plugin) |

Adding a provider touches: the provider table in `internal/tools/tools.go` (service name, display name, transcript event roles; new session ID or model attribute keys go into the key lists there, and a new session ID key needs a migration to backfill `SessionIds`), export/delete service filters, `cmd/server/cmd_setup.go`, content extraction in `internal/transcript/` if its events carry content in new attributes, pricing data (if costs are derived), frontend `lib/metricCatalog/<provider>.ts` (metric definitions, added to `METRIC_CATALOG` in `lib/metricMetadata.ts`) and `lib/metricMetadata.ts` (icons, grouping, and the fallback service names, which the dashboard uses until `/api/providers` has loaded), dashboard widget catalog, and documentation (README, `docs/`, `frontend/src/data/documentation.ts`).

### Frontend (React + TypeScript)

Vite-based React app with Tailwind CSS v4:
- `react-router-dom` for routing (Dashboard, Traces, Trace Detail, Metrics, Logs, Sessions, Session Transcript, Docs pages)
- `zustand` for state management (`stores/telemetryStore.ts` keeps timestamps and counters from real-time notifications; pages re-query the API)
- `recharts` for visualizations
- `@dnd-kit` for drag-and-drop dashboard widgets
- WebSocket singleton (`hooks/useWebSocket.ts`) for live updates

**Key files:**
- `lib/api.ts` - API client with request deduplication
- `lib/metricMetadata.ts` - Metric lookup and formatting helpers, provider/service display names and icons; assembles `METRIC_CATALOG` from `lib/metricCatalog/*.ts` (one file per provider). Shared metric types and `UNIT_FORMATS` live in `lib/metricTypes.ts`
- `data/documentation.ts` - Content for the in-app Docs page
- `stores/dashboardStore.ts` - Dashboard widget state; grid placement (empty cells, next free position, fit checks, reordering) is pure code in `lib/dashboardGrid.ts`
- `lib/metricGroups.ts`, `components/common/metric-options.tsx` - Metric picker grouping by tool, shared by the metrics page and the add-widget panel; `components/charts/chartData.ts` turns time series into bar chart rows and X-axis labels for both
- `pages/` - Dashboard, TracesPage, TraceDetailPage (`/traces/:kind/:id`), MetricsPage, LogsPage, SessionsPage, SessionTranscriptPage, DocsPage

**Path alias:** `@` maps to `./src`

**Dev proxy:** Vite proxies `/api` and `/ws` to backend at localhost:8080

**Build output:** `pnpm build` writes to `../backend/internal/frontend/dist`, which the Go binary embeds.

**UI Components:** Always use [shadcn/ui](https://ui.shadcn.com) components from the registry, see [overview](https://ui.shadcn.com/llms.txt) also. Use the shadcn CLI to add new components:

```bash
cd frontend && pnpm dlx shadcn@latest add [component]  # e.g., button, dialog, table
```

Existing components are in `src/components/ui/`. Never manually create UI primitives - add them via CLI instead. Hand-written composites (markdown, editable text, date range picker, data pagination) live in `src/components/common/`, which is linted without the `components/ui` exemptions. Live pages refresh through `hooks/useLiveRefresh.ts`: on real-time notifications (debounced), polling only while the WebSocket is disconnected (and once a minute to move relative time windows), paused while the tab is hidden (`hooks/usePageVisible.ts`). Fetch inside the effect with an `AbortController`, so a newer refresh cancels an older one. List pages use cursor pagination (`hooks/usePagination.ts` keeps the visited cursors).

### Database Schema

DuckDB tables defined in `internal/storage/schema.go`:
- `otel_traces` - Span data with nested events and links arrays
- `otel_logs` - Log records with severity and trace context
- `otel_metrics` - All metric types (gauge, sum, histogram, summary, exponential histogram) unified in one table
- `dashboards` / `dashboard_widgets` - User dashboard persistence
- `import_state` - Per-file import/watch state (hash, record count, byte offset, message count, parser state, file size/mtime, `source_tracked`, `partial`)
- `metric_names`, `metric_baselines` - Stored metric names; last deleted point of each cumulative series

Telemetry tables are only indexed for point lookups (`otel_traces.TraceId`, `otel_traces.SpanId`, `otel_logs.TraceId`). Time-range and service filters rely on DuckDB's automatic min-max zonemaps; a benchmark showed ART indexes on those columns gave no query speedup but made the database ~13x larger and inserts slower, so don't add them back without measuring.

The base schema runs on every startup, so keep it idempotent (`CREATE ... IF NOT EXISTS`). Put new columns, data rewrites and other one-time changes into the ordered `migrations` list in `schema.go`: each runs once per database in its own transaction and is recorded in `schema_migrations` (`run` holds Go code for rewrites SQL cannot do, e.g. migration 5 recomputing Gemini cost; keep literal values such as name lists in the migration rather than referencing code that may change). Never edit or reorder a released migration. When an index definition changes, drop the old index and create the new one under a versioned name (e.g. `idx_traces_trace_id_v2`).

## External Reference Projects

The `external-projects/` directory contains local checkouts of the AI coding tools that send telemetry to AI Observer:
- `external-projects/claude-code/` - Claude Code CLI examples
- `external-projects/claude-code-npm/` - Claude Code CLI source (minimized) from the npm package
- `external-projects/gemini-cli/` - Gemini CLI source
- `external-projects/codex/` - OpenAI Codex CLI source
- `external-projects/CodexBar` - SwiftUI application showing cost and usage metrics from different providers
- `external-projects/ccusage` - CLI tool for analyzing Claude Code/Codex CLI usage from local JSONL files
- `external-projects/duckdb-otlp` - DuckDB extension for querying OTLP files with SQL (similar row-based schema)

**Use these local sources** instead of web search when investigating OTLP telemetry formats, metric names, log events, or span structures. These repos show exactly what telemetry data each tool emits.

Further project documentation lives in `docs/` (`import.md`, `export.md`, `pricing.md`, token analysis notes) and `README.md`.

## AI Coding Tool Integration

Point AI tools to OTLP endpoint `http://localhost:4318`. `ai-observer setup <tool>` prints full instructions (`claude-code`, `gemini`, `codex`, `opencode`, `github-copilot`).

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

# GitHub Copilot (CLI / env-based exporters)
export COPILOT_OTEL_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export COPILOT_OTEL_CAPTURE_CONTENT=true   # captures prompts/tool content; trusted environments only
# VS Code: set github.copilot.chat.otel.{enabled,exporterType="otlp-http",otlpEndpoint,captureContent}

# OpenCode - add "plugin": ["@devtheops/opencode-plugin-otel"] to the OpenCode config, then:
export OPENCODE_ENABLE_TELEMETRY=1
export OPENCODE_OTLP_ENDPOINT=http://localhost:4318
export OPENCODE_OTLP_PROTOCOL=http/protobuf  # plugin default is gRPC on 4317, which AI Observer does not accept
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
| `serve` | Explicitly start the OTLP server (`--mcp`: serve the MCP endpoint) |
| `import` | Import local sessions from AI tool files (`all`, `claude-code`, `codex`, `gemini`) |
| `watch` | Watch local session files and import incrementally (API server only, no OTLP) |
| `export` | Export telemetry data to Parquet files |
| `delete` | Delete telemetry data from database |
| `setup` | Show setup instructions for AI tools |
| `pricing` | `pricing update`: fetch current prices from models.dev into the four price files in `AI_OBSERVER_PRICING_DIR` or `--output` |

**Global Options:**
| Option | Description |
|--------|-------------|
| `-h`, `--help` | Show help message and exit |
| `-v`, `--version` | Show version information and exit |

**Command options:**
- `import`: `--from`, `--to` (YYYY-MM-DD), `--dry-run`, `--force`, `--purge`, `--verbose`, `--yes`, `--pricing-mode` (`auto`, `calculate`, `display`)
- `watch`: `--backfill` (import existing data on first start), `--mcp` (serve the MCP endpoint)
- `export`: `--output` (required), `--from`, `--to`, `--from-files`, `--zip`, `--dry-run`, `--verbose`, `--yes`
- `delete`: `--from`, `--to` (required), `--service`, `--yes`
- `pricing update`: `--output` (default `AI_OBSERVER_PRICING_DIR`), `--allow-large-changes`. It runs before the pricing overrides are loaded and merges into the files already in the output directory (or the built-in ones)

The database can only be opened by one process: `import`, `export` and `delete` fail with an explanation while a server or `watch` runs. Flags may appear before or after positional arguments (`reorderArgs(fs, args)` in `cmd/server/main.go`; it asks the FlagSet which flags are boolean, so their next argument is not taken as a value).

**Examples:**
```bash
ai-observer                              # Start server (default)
ai-observer import claude-code           # Import Claude Code sessions
ai-observer watch all --backfill         # Watch all file-backed tools, importing existing data first
ai-observer export all --output ./data   # Export to Parquet files
ai-observer delete all --from 2025-01-01 --to 2025-01-31
ai-observer setup claude-code            # Show Claude Code setup instructions
```

## API Endpoints Reference

### Query API (Port 8080)

**Traces:**
| Endpoint | Method | Query Parameters |
|----------|--------|------------------|
| `/api/traces` | GET | `service`, `search`, `from`, `to`, `limit`, `cursor` (or deprecated `offset`) |
| `/api/traces/recent` | GET | `limit` (max 100), `from`, `to` |
| `/api/traces/{traceId}` | GET | `kind` (required: `otel_trace` or `codex_operation`) |
| `/api/traces/{traceId}/spans` | GET | `kind` (required; same as above) |

Trace list rows carry an `id` and `kind`; the list returns `otel_trace` rows only (Codex sessions as raw traces). `kind=codex_operation` with an operation root span ID as `id` is a detail-only view that groups the spans of one Codex operation (README "AI Observer Trace Handling").

**Metrics:**
| Endpoint | Method | Query Parameters |
|----------|--------|------------------|
| `/api/metrics` | GET | `service`, `name`, `type`, `from`, `to`, `limit`, `cursor` (or deprecated `offset`) |
| `/api/metrics/names` | GET | `service` |
| `/api/metrics/breakdown-values` | GET | `name` (required), `attribute` (required), `service` |
| `/api/metrics/series` | GET | `name` (required), `service`, `from`, `to`, `interval`, `aggregate` |
| `/api/metrics/batch-series` | POST | Body: array of queries with `id`, `name`, optional `service`, `aggregate`, `interval` |

**Logs:**
| Endpoint | Method | Query Parameters |
|----------|--------|------------------|
| `/api/logs` | GET | `service`, `severity`, `traceId`, `search`, `from`, `to`, `limit`, `cursor` (or deprecated `offset`) |
| `/api/logs/levels` | GET | optional `from`, `to` (without them: all logs) |

**Sessions:**
| Endpoint | Method | Query Parameters |
|----------|--------|------------------|
| `/api/sessions` | GET | `service`, `from`, `to` (which sessions: those with logs in the range; each is shown whole), `limit`, `cursor` (or deprecated `offset`) |
| `/api/sessions/{sessionId}/transcript` | GET | — (all records of the session). Known sessions without content return an empty transcript; unknown sessions return 404 |

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
- `GET /api/providers` - Supported tools (`tool`, `serviceName`, `displayName`, `sessionFiles`) from the provider table
- `GET /api/stats` - Aggregate statistics; optional `from`/`to` count only that range (the dashboard passes its time range), `services` always lists all services
- `GET /ws` - WebSocket for real-time updates
- `GET /health` - Health check (on both ports)
- `POST /mcp` - MCP endpoint for agents (opt-in, `docs/mcp.md`); stateless, so `GET`/`DELETE` answer `405`

**Note:** List responses carry `total`, `hasMore` and, when there are more, `nextCursor` (pass it as `cursor`; invalid cursors and offsets over 10,000 return 400). With `AI_OBSERVER_AUTH_TOKEN` set, requests without the token get 401. `from`/`to` are RFC 3339; `to` defaults to now and `from` to 24 hours before `to`. Invalid values or `from` after `to` return 400. Queries stopped by the 5-second API timeout return 504 (the frontend does not retry 503/504). `/api/stats` without a range is cached for 5 seconds. Endpoints with optional `from`/`to` (`/api/stats`, `/api/logs/levels`) leave an unset side open instead of applying the 24-hour default. Time-bucketed series are capped at ~2,000 buckets, and batch series run at most 4 queries at once.

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

**Import-only reconstruction:** full imports rebuild lines of code, commits, PR counts (distinct PR links, not confirmed PR creation), session counts, and active time from session files.

**Events** (logs): `user_prompt`, `api_request`, `api_error`, `tool_result`, `tool_decision`

### Gemini CLI

**Metrics** (mix of counters and histograms):
- Cumulative counters (series queries compute per-interval increases): `session.count`, `token.usage`, `api.request.count`, `file.operation.count`
- Histograms: `api.request.latency`, `tool.call.latency`, `agent.duration`, `startup.duration`
- Also emits `gen_ai.client.token.usage` and `gen_ai.client.operation.duration` (OTel semantic conventions)

**Derived Metrics:** `gemini_cli.cost.usage` from token usage (cumulative like the tokens; cached tokens at the cache rate only, since `input` includes them). Token types are `input`, `output`, `cache`, `thought`, `tool` for OTLP and imports alike. `<counter>.delta` names are query-time aliases of the cumulative counters above.

**Logs**: `config`, `user_prompt`, `api_request`, `api_response`, `api_error`, `tool_call`, `file_operation`, `agent.start/finish`, `conversation_finished`

### OpenAI Codex CLI

**Derived Metrics** (computed from `response.completed` logs):
- `codex_cli_rs.token.usage` - Tokens by type
- `codex_cli_rs.cost.usage` - Cost in USD

These are per-event values, not cumulative series.

**Events** (logs): `conversation_starts`, `api_request`, `user_prompt`, `tool_decision`, `tool_result`

**Traces**: Uses single trace per session with all spans nested. AI Observer treats first-level child spans as virtual traces (`codex_operation` kind) for usability.

**Note:** `codex.sse_event` logs are filtered out to reduce noise.

### GitHub Copilot

**Services:** `copilot-chat` (VS Code extension) and `github-copilot` (CLI).

**Metrics:** `copilot_chat.*` counters and histograms, e.g. `session.count`, `tool.call.count`, `tool.call.duration`, `agent.turn.count`, `agent.invocation.duration`, `chat_edit.outcome.count`, `edit.acceptance.count`, `lines_of_code.count`, `pull_request.count`, `cloud.session.count`, `time_to_first_token`, plus generic `gen_ai.*` metrics.

**Derived Metrics** (computed from chat spans):
- `github_copilot.token.usage` - Tokens by type
- `github_copilot.cost.usage` - Cost in USD (pricing from `github_copilot.json` with GitHub Models catalog alias matching)

**Sessions/transcripts:** identified via `session.id`, `gen_ai.conversation.id`, or `copilot_chat.session_id`. Transcripts are reconstructed from GenAI spans when logs lack content.

### OpenCode

Sent via the `@devtheops/opencode-plugin-otel` plugin. **Metrics:** `opencode.session.count`, `opencode.token.usage`, `opencode.cost.usage`, `opencode.lines_of_code.count`, `opencode.commit.count`, `opencode.tool.duration`, `opencode.cache.count`, `opencode.session.duration`, `opencode.message.count`, `opencode.model.usage`, `opencode.retry.count`, plus session totals. Sessions are keyed by `session.id`; transcripts use plugin log events and OpenInference-style attributes.

## Docker vs Binary Paths

| Setting | Binary Default | Docker Default |
|---------|---------------|----------------|
| Database path | `./data/ai-observer.duckdb` | `/app/data/ai-observer.duckdb` |
| Data directory | `./data/` | `/app/data/` |

Override with `AI_OBSERVER_DATABASE_PATH` environment variable.

## CI/CD

GitHub Actions (`.github/workflows/ci.yml`) triggers:

| Trigger | Actions |
|---------|---------|
| Push to main / PR to main / tag | `gofmt` check, `go vet`, golangci-lint (built with the job's Go), ESLint, then tests (Go + frontend) |
| Every run, after tests | Build binaries (linux/amd64, darwin/arm64, windows/amd64); macOS build also runs `go test -race`; artifacts uploaded on push only |
| Tag `v*` | Create GitHub Release with archives |
| Tag `v*` (or manual dispatch with `docker_version`) | Push multi-arch Docker images to Docker Hub |
| Tag `v*` | Update Homebrew formula in [homebrew-ai-observer](https://github.com/tobilg/homebrew-ai-observer) tap |
| Daily, manual, PRs changing pricing data | `.github/workflows/pricing-freshness.yml`: fail when a file in `internal/pricing/data/` has `lastUpdated` more than 7 days ago |

The binary version comes from `git describe` via ldflags (`internal/version`).

**Creating a release:** move the `[Unreleased]` changelog entries into a version section (with upgrade notes for breaking changes; update the compare links at the end), set the same version in `frontend/package.json`, make sure the pricing data is less than 7 days old (`make pricing-update`), merge to `main`, then tag; `make release-notes TAG=…` adds generated notes to the created release.
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

Alternatively, ingest existing local sessions with `ai-observer import <tool>` or `make backend-dev-watch` (watch mode, no OTLP).
