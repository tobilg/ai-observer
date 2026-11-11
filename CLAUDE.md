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

# API types (internal/apigen)
make api-types          # Regenerate frontend/src/types/{api,mcp}.gen.ts and docs/openapi.json from the Go types (go test fails when stale)

# Release notes
make release-notes TAG=vX.Y.Z

# Cleanup
make clean              # Remove bin/ and built frontend assets
```

**Toolchain:** Go 1.26 (per `go.mod`; CI and Dockerfile also use 1.26), Node 22, pnpm 10. CGO is required for DuckDB. The backend embeds `backend/internal/frontend/dist`, so build the frontend before building or testing the backend from a clean checkout.

**pnpm version:** `frontend/package.json` pins pnpm via `packageManager` (also used in CI, the Dockerfile and `.tool-versions`); a newer global pnpm switches to the pinned version automatically inside `frontend/`. Update all four together. Packages allowed to run build scripts (esbuild) are listed in `pnpm-workspace.yaml` both as `onlyBuiltDependencies` (pnpm 10) and `allowBuilds` (newer pnpm, which ignores the former and fails with `ERR_PNPM_IGNORED_BUILDS`: the cause of the failed v0.6.0 Docker build, which installed an unpinned pnpm 12).

CI additionally runs `go test -race ./... -timeout 120s` on macOS.

**Test isolation:** tests that import session files must point `AI_OBSERVER_CLAUDE_PATH`, `AI_OBSERVER_CODEX_PATH` and `AI_OBSERVER_GEMINI_PATH` at generated fixtures with `t.Setenv` (see `setupFromFilesFixtures` in `internal/exporter/exporter_test.go`); an unset override falls back to the developer's real session directories. The server tests bind ports 18080/14318/14317, so don't run manual instances on those ports; tests that start a server set `AI_OBSERVER_OTLP_GRPC_PORT` to a free port or `off`.

## Architecture

### Backend (Go)

The backend runs **three listeners** simultaneously (all opened before serving, so a busy port fails startup):

1. **OTLP/HTTP Ingestion Server** (port 4318) - Receives telemetry from AI tools
   - `POST /v1/traces` - Trace data (protobuf or JSON)
   - `POST /v1/metrics` - Metrics data
   - `POST /v1/logs` - Log data
   - `GET /health` (liveness, always 200), `GET /ready` (503 when `SELECT 1` does not answer within 2 s, `storage.Ping`); both need no token
   - HTTP/1.1 + h2c (HTTP/2 cleartext; with TLS HTTP/2 via ALPN instead), 10MB payload limit (on the decompressed body)
   - Request bodies are decoded by `compression.DecompressMiddleware`: `Content-Encoding` parsed as a list of codings, `gzip`/`x-gzip`, `deflate` (zlib, or raw deflate), `identity`; others (zstd) `415` with `Accept-Encoding`, corrupt bodies `400`
   - Auto-detects JSON vs Protobuf format regardless of Content-Type header

2. **OTLP/gRPC Server** (port 4317, `internal/otlpgrpc`; `AI_OBSERVER_OTLP_GRPC_PORT`/`--otlp-grpc-port`, `0`/`off` disables it) - the OTLP Trace, Metrics and Logs services with their own method handlers (an undecodable request is `InvalidArgument`, not grpc-go's `Internal`), gzip, the shared ingestion functions; a storage failure is `Unavailable` (retried), a message over 10 MB `ResourceExhausted`, a panic `Internal` (recovered). Auth via `authorization: Bearer` metadata (`TokenAuth.GRPCAuthorizer`, same limiter as HTTP). TLS with the HTTP servers' certificate. Shutdown is `GracefulStop` with `grpc.WaitForHandlers(true)`, so no export writes after storage closes. No Origin check: browsers cannot send plain-text gRPC (HTTP/2 prior knowledge, trailers), and over TLS the CORS preflight that `application/grpc` forces is answered 405 (package doc)

3. **API/WebSocket Server** (port 8080) - Serves dashboard and real-time updates (HTTP/1.1 only, no h2c; with TLS `https`/`wss`)
   - `/api/traces`, `/api/metrics`, `/api/logs`, `/api/sessions` - Query endpoints
   - `/api/services`, `/api/stats`, `/api/status` - Aggregations and server status
   - `/api/dashboards` - Dashboard CRUD
   - `/api/admin/*` - Admin API, only with `AI_OBSERVER_ADMIN_API=true` (otherwise `404` with a hint)
   - `/metrics` (Prometheus text), `/api/internal/metrics` (JSON) - Self-metrics, behind the token
   - `/health`, `/ready` - as on the OTLP port
   - `/ws` - WebSocket for real-time updates to frontend
   - Serves embedded React frontend at root (`frontend.SPAHandler`: hashed files in `assets/` are `immutable`, the page is `no-cache`); responses are gzip-compressed for clients that accept it (`middleware.Compress` on the API router)
   - `/mcp` - MCP endpoint for agents, only with `AI_OBSERVER_MCP=true` or `--mcp` (otherwise `404` with a hint)

**Modes** (`server.ModeServe`, `ModeWatch`, `ModeServeWatch`, reported by `/api/status`): `serve` runs all three listeners; `watch` starts only the API server (`ListenAndServeAPIOnly`), so OTLP ingestion is not available; `serve --watch [tool] [--backfill]` runs the three listeners and the file watcher in one process (`Server.StartWatcher`; mode `serve+watch`, `watchedTools` in the status).

**Network:** all servers listen on `AI_OBSERVER_HOST` (default `127.0.0.1`; the Dockerfile sets `0.0.0.0`). The API router rejects `Host` headers that are not an IP, `localhost`, the frontend URL host or in `AI_OBSERVER_ALLOWED_HOSTS` (`server.HostCheck`, DNS rebinding protection). Both routers and the WebSocket upgrade reject requests whose `Origin` is not loopback, a CORS origin, an allowed host, or same-origin with an IP/`localhost`/allowed host (`server.OriginPolicy`, CSRF protection: OTLP decodes JSON regardless of Content-Type, so a cross-site `text/plain` POST would otherwise be accepted; same-origin alone is not trusted because a rebound domain is same-origin with the OTLP port, which has no Host check). Requests without `Origin` (exporters, CLIs) pass. With `AI_OBSERVER_AUTH_TOKEN` set, `server.TokenAuth` requires `Authorization: Bearer <token>` on OTLP and API requests; the dashboard trades `?token=` for an HttpOnly `SameSite=Strict` cookie holding an HMAC of the token, never the token (`Secure` over TLS; `/health`, `/ready` and static files are exempt). One `TokenAuth` per server (`Server.auth`) serves OTLP/HTTP, gRPC and API. Failed credentials are limited per connection address (`server/auth_limit.go`: 10 distinct wrong ones within a minute block the address for 5 minutes, `429` + `Retry-After`, gRPC `ResourceExhausted`; while blocked credentials are not checked; requests without credentials and repeats of the same wrong one do not count; successes do not reset the count; never forwarded headers, loopback not exempt; at most 10,000 addresses remembered). `AI_OBSERVER_TLS_CERT`/`AI_OBSERVER_TLS_KEY` (both or neither, checked by `loadTLS` in `server.New`, read once at startup) serve HTTPS on the API (HTTP/1.1 only) and OTLP/HTTP (h2 via ALPN, no h2c) ports and gRPC over TLS, TLS ≥ 1.2. Listening beyond loopback without a token logs a warning, with a token but without TLS another. API bodies are limited to 1 MB (`413`, also when exceeded while decoding: `handlers.decodeBody`). With `AI_OBSERVER_RETENTION_DAYS` set, the server deletes older telemetry on startup and hourly, a day per transaction so ingestion is not blocked (`server/retention.go`); with `AI_OBSERVER_MAX_DB_SIZE` the same ticker deletes the oldest day per transaction while the used blocks after a checkpoint exceed the limit, never the last 24 h (`server/database_size.go`: `applySizeLimit`; `sizeGuard` stops when a deleted day of ≥ 10,000 rows shows no freed space, until `WriteGeneration` changes; a row group is 122,880 rows, so it may delete about a day more than needed). Both checkpoint after deleting. At startup `logDatabaseGrowth` logs size, used/free space, oldest record and growth per day, and warns when a database of ≥ 1 GiB grows without bound. `serveUntilDone` in `cmd/server/main.go` handles SIGINT/SIGTERM and returns only after `Server.Shutdown` has closed storage.

**Admin API** (`internal/handlers/admin.go`, `server/admin.go`; `AI_OBSERVER_ADMIN_API=true`): `POST /api/admin/delete` (`from`, `to`, `service`, `scope`, `dryRun`; a day per transaction, then a checkpoint), `/export`, `/backup` (into `AI_OBSERVER_BACKUP_DIR`, default `backups` next to the database: `Config.AdminBackupDir`; names must match `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`, an existing one is `409`, short disk space `507`), `/checkpoint`. One operation at a time (`409` otherwise); behind Host, Origin and token checks and the 1 MB body limit; exempt from the request timeout (`exceptPrefix`) and ended on shutdown (`endOnStop`). Compaction is not offered: it replaces the open file. The CLI's `delete`, `export` and `backup` print the matching `curl` command when a server holds the database (`cmd/server/admin_hint.go`, the token as `$AI_OBSERVER_AUTH_TOKEN`).

**Self-metrics** (`internal/selfmetrics`: counters, gauges, histograms with labels, atomics, no dependency; declared in `metrics.go`, recorded with single calls such as `selfmetrics.RecordsStored.With("logs").Add(n)`; label values are fixed words, never request data): OTLP requests by `transport` (`http` via `server.OTLPMetrics`, `grpc` via the `otlpgrpc` stats handler, which also counts messages grpc-go rejects) and result, records stored/rejected/deduplicated, noise spans dropped, write transaction and lock wait times, API request durations and timeouts by route (`server.APIMetrics`), WebSocket clients and dropped broadcasts, watcher files, retention deletions, auth failures. Served as `GET /metrics` (Prometheus text 0.0.4) and `GET /api/internal/metrics`.

**OTLP ingestion pipeline** (HTTP handlers only decode and call the shared `Handlers.IngestTraces`/`IngestMetrics`/`IngestLogs` in `handlers/otlp_ingest.go`, which gRPC calls too): decode (`otlp.GetDecoderWithDetection`; logs and metrics hash the body while it is read; the JSON decoder turns trace and span IDs that `protojson` read as base64 back into the hex IDs OTLP/JSON sends, recognized by their length: `otlp.hexID`, migration 9 repaired IDs stored before) → skip retried log/metric batches (`handlers.batchDedup`: a batch identical to one stored in the last 15 minutes is acknowledged without storing it, and gets the same partial success; HTTP keys by the body hash, gRPC by `handlers.MessageBatchKey`, SHA-256 of the deterministic protobuf encoding taken before validation; in memory, so a retry after a restart is stored again) → drop invalid records (`otlp.DropInvalidSpans`: trace ID not 16 or span ID not 8 bytes, or all zero; `otlp.DropInvalidDataPoints`: number points without value, histogram points whose `bucket_counts` is not `len(explicit_bounds)+1`; metrics without points count as a warning with 0 rejected; logs have no invalid form), reported as `partial_success` (`rejected_spans`/`rejected_data_points`, `error_message`) and logged at WARN; HTTP JSON responses are written with `protojson` → convert to row structs (`otlp.Convert*`) → drop noise spans (`storage.dropNoiseSpans` in `InsertTraces`: spans named in `tools.Provider.NoiseSpans`, Codex's per-event spans, unless they failed or a stored or same-export span has them as parent; children end and arrive before their parents, so nothing is orphaned; `AI_OBSERVER_STORE_ALL_SPANS` keeps them) → derive metrics → insert into DuckDB, with secrets redacted (`internal/redact`; records and their derived metrics in one transaction, `InsertTelemetry`, which also reconciles usage with session files, see below; a failed insert answers `503` + `Retry-After` (gRPC `Unavailable`), which exporters retry, never `500`, which they drop; oversized bodies `413`) → broadcast a content-free notification over the WebSocket hub (`websocket.SpansStored`/`LogsStored`/`MetricsStored`: count, services, timestamps; one JSON message per frame; the hub drops messages when its broadcast channel is full and disconnects clients whose buffer is full) → respond in the request's encoding (`writeExportResponse`). Derived metrics:
- Gemini cost from token usage (`otlp.DeriveGeminiCostMetrics`, over all points of a request). Gemini's `input` tokens include the cached ones, so the input cost uses input minus the `cache` point of the same export (same series attributes apart from `type`, same timestamp); imports use `pricing.CalculateGeminiCost` per response. Gemini sends cumulative counters; the cost point keeps the token point's temporality and attributes (`type` renamed to `token_type`), so it is a cumulative series too and queries compute its per-interval increases. Never derive a per-interval value from a cumulative point at ingestion. Cumulative counters are stored as they are; the `.delta` names of earlier versions are query-time aliases (`storage.deltaAliases`), not listed by `/api/metrics/names`
- Claude Code user-facing token/cost metrics (`otlp/claude_user_facing_metrics.go`)
- Codex token/cost from `response.completed` log events (`ExtractCodexMetrics`, called from `otlp/logs.go`), with `session.id` from the event's session keys (`conversation.id`), as imports set it from `session_meta.id`
- GitHub Copilot token/cost from chat spans (`DeriveCopilotMetricsFromSpans`, passed to `DuckDBStore.InsertTraces`, which derives them only from spans not stored yet, so a resent batch adds nothing)

**Usage recorded over OTLP and in session files** (`storage/usage_overlap.go`): each model request is counted once, from OTLP when OTLP recorded it, from the file otherwise; the file's token/cost points of such a request are deleted, everything else of both sides is kept. `reconcileUsage` runs in the write transaction of every insert storing usage of either side (`insertImportedFileTx`, `InsertWatcherBatch`, `InsertTelemetry`, `InsertMetrics`; on the OTLP side only when `import_state` has rows of the tool), so the result does not depend on arrival order. Claude Code: by `request_id` of OTLP's `api_request` event and the file's `claude_code.api_request` log (whose usage points share its `SourceFile` and timestamp; a timestamp is taken only when every file request at it is recorded), and only when the session sent `claude_code.token.usage` over OTLP within an hour (logs-only exporters keep the file usage). Codex: inferred, by equal per-type token counts within ±30 s and equal `session.id` when both carry one. Gemini CLI (`reconcileGeminiUsage`): inferred, a file response's points are removed when an OTLP `gemini_cli.api_response` of the same `session.id` has equal input/output/cached/thoughts/tool counts within ±1 h (`geminiMatchWindow`), the session sent `gemini_cli.token.usage` points within 1 h after the event (`geminiMeteredWindow`; a cumulative point includes every earlier response of its process, and logs-only exporters keep the file usage), and the file response has input > 0; imported Gemini usage points carry `session.id`. Migration 11 applied the Claude rule to stored data (literal SQL); Codex and Gemini are repaired by `import codex --force` / `import gemini --force`, never by `watch`, which resumes where it was.

**Local file ingestion** (Claude Code, Codex CLI, Gemini CLI only):
- `import` - full import via one `SessionParser` per tool; each file's records and its `import_state` row are written in one transaction (`InsertImportedFile`), and failed files are always listed; any failure makes `Import` return `importer.ErrIncomplete` (exit status 1). Records from files carry the file path in `SourceFile` (logs, metrics, spans); re-importing a file that was imported before deletes its records in the import's range in the same transaction, so changed, forced and after-`watch` imports never duplicate. For files whose state has `source_tracked = false` (records stored before migration 3) the replacement also deletes the imported records without `SourceFile` that equal the new ones (`FileRecords.Legacy`, `deleteLegacyRecords`: matched by time, service and identity, only rows with `import_source`, never OTLP data). A date range that leaves out records of a file makes its state `partial`: the next import reads it again, and when records after the range were left out the watch offsets are 0 so `watch` reads the file again and replaces its records. Unchanged files are recognized by `file_size`/`file_mtime_ns` without hashing, and a changed file is hashed once per run (`StateManager.fileHash`). `--purge` deletes only the imported tools' services in the range and re-imports all of their files in it (`purgeRanges`), per tool in one transaction (`DuckDBStore.InImportTx`; a failed file rolls the tool back); with `--from`/`--to` unchanged files are rebuilt too. Date flags are local days (`importer.ParseDateArg`, also used by `export` and `delete`). The import state records `ParsedBytes`/`MessageCount` (end of the last complete line, a final line without newline counting when it is valid JSON like in watch; Gemini message count) as the watch offset, so `watch` resumes where the import stopped. The preview pass (parse all files, show counts, confirm) only runs for the interactive prompt, `--dry-run` and `--purge`; with `--yes` each file is parsed once (`importOnce`). Parsers must set `ImportResult.FileHash` to the hash of the bytes they parsed (hash while reading, e.g. `io.TeeReader`), never by re-reading the file afterwards. Read JSONL with `jsonlLines` (no line-length limit), never `bufio.Scanner`: session files contain lines of several MB. Claude Code entries' `message.content` may be a string (typed prompts) or a list (`ClaudeMessage.UnmarshalJSON`; before, typed prompts failed to decode and were dropped, since v0.3.0); imported Claude messages carry `message.uuid`, `request_id` and `tool.use_id`, which transcripts use to merge them with OTLP events. Gemini imports record the watch state (`importer.GeminiWatchState`: message count and what was stored of the last message), since Gemini CLI adds tool calls and tokens to the last message later; Gemini `content` and tool results are read as PartListUnion (string, Part or list of them, `GeminiMessage.UnmarshalJSON`); imported replies carry `thoughts_token_count`/`tool_token_count`, tool messages `tool.call_id`
- Codex rollouts: Codex compresses rollouts older than 7 days to `.jsonl.zst` (one zstd frame with the content size, mtime kept; resuming writes the `.jsonl` again and appends). A session file is identified by its plain `.jsonl` path in import state, `SourceFile` and watch (`importer/codex_rollout.go`: `CompressedSessionFile`, `StatSessionFile` with the content size from the frame header, `OpenSessionFile` streaming through `klauspost/compress/zstd`, a direct dependency); when both forms exist the `.jsonl` is read, temp files are ignored. The unchanged check compares content size and mtime, else the decompressed hash. Watch's startup scan and `--backfill` read `.zst` files; live `.zst` events are ignored (a compressed file cannot seek: what was read is skipped). Codex files over 64 MiB imported without a date range are parsed and stored in batches of 10,000 records in one transaction (`codex_batches.go`: `ParseFileInBatches`, a first pass for repository attribution; the preview counts them batch by batch). Measured on 714 files (~4.4 GB of JSONL): 285 s, 2.87 GB database, peak RSS ~3 GB (DuckDB's cache) or 0.9 GB with `AI_OBSERVER_DB_MEMORY_LIMIT=512MB`
- Codex tool calls (`importer/codex_tools.go`): a `tool_use` and a `tool_result` per call, from the model's call (`function_call`, `custom_tool_call`, `local_shell_call`, `tool_search_call`, hosted `web_search_call`/`image_generation_call`) and its output, else from its execution (`item_completed` CommandExecution, FileChange, McpToolCall, WebSearch, ImageView, Extension; legacy `patch_apply_end`, `mcp_tool_call_end`, `web_search_end`), which is all that code mode's `exec` cell's tools have, with `tool.call_id` = OTLP's `call_id`; an execution of a model call adds exit code, duration, success to its result. Code mode's `write_stdin`/`curr_time` are in no file (OTLP only). Tool text is capped at 16 KiB per field (`codexToolText`, start and end kept); results store the output only in `tool.output` with `tool_result_size_bytes` (full size) and a summary line as body (`toolResultBody`). `CodexState` keeps calls waiting for their output and the IDs of answered calls (≤ 1,000 each, `codexCallLimit`) so watch pairs them across runs; watch after an import rebuilds them from the file prefix (`ObserveCodexToolCalls`)
- `watch` - fsnotify-based incremental ingestion with per-file debounce; the startup scan of existing files (and `--backfill`) runs in the background, so `Watcher.Start` returns right away and `Stop` interrupts the scan (tests wait with `waitInitialScan`); the event loop runs during the scan (so the kernel's event queue cannot overflow on a long backfill), but files changed meanwhile are only recorded (`scanPending`) and processed when the scan ends (`scanExisting`), so an event never stores a file's existing records before the scan has recorded where watching starts; persists byte offsets, message counts, and parser state in `import_state`. A file is never processed twice at once: events during a run trigger one re-run afterwards (`Watcher.processExclusive`). Gemini resumes by message count, the JSONL tools by byte offset (parser context is rebuilt from the file prefix when the parser state is empty). A file shorter than what was read, or left partial by a ranged import, is read again and its records replaced (`InsertWatcherBatch(..., replace)`, legacy records included). Claude watch state keeps only the last 1,000 usage dedup keys. Watch converts records with the importers' code (`importer.ConvertGeminiMessages`, `importer.GeminiMessageRecords`, `importer.ConvertCodexEntry`, `ClaudeParser.CreateTranscriptLogs`/`UsageRecords`), so import and watch store the same records; only line reading, resuming and Claude activity reconstruction differ
- Claude Code activity metrics (lines of code, commits, PR links, sessions, active time) are reconstructed in full imports only, not in watch mode
- Repository/branch attribution is added to Claude Code and Codex logs and token/cost metrics in both modes (`importer/repos.go`, `watcher/repository.go`)

**Database space, backup and restore:** DuckDB reuses freed blocks but never shrinks the file; blocks freed by a checkpoint are released only at the next checkpoint, which DuckDB skips on an empty WAL, so `DuckDBStore.Checkpoint` (`storage/maintenance.go`) runs `CHECKPOINT; COMMENT ON TABLE metric_names IS NULL; CHECKPOINT` under the write lock; blocks are freed by whole row groups (122,880 rows). Deletions (`delete`, retention, size limit, admin API) and `restore` checkpoint afterwards (measured 605 instead of 806 MB after deleting 1.6M and storing 600k spans). `DatabaseSize` reports file, WAL, used and free bytes (`CompactRecommended`: free ≥ 20% and ≥ 64 MB). `CopyDatabase` writes `COPY FROM DATABASE` in one transaction to `.partial` and renames it after comparing table definitions, row counts, index definitions and schema version, after checking free disk space (`CopySpaceNeeded`: used + WAL + 25% + 16 MB; `internal/diskspace`). `internal/maintenance`: `Backup` (`backup` command) and `Compact` (`compact`: copy to `<db>.compact`, refuse a remaining WAL, rename the database to `<db>.pre-compact`, reopen through `NewDuckDBStore` so the base schema recreates missing indexes, check again, put the original back on failure, keep it with `--keep-original`; refuses leftover `.compact`/`.pre-compact` files; 751 → 575 MB in 4.6 s). `internal/restore` + `storage/restore.go` (`restore` command): loads an export directory or zip (only known top-level names extracted) in one transaction; refuses (`ErrConflict`) when the database holds records of the export's services in its [min, max] range unless `--replace` deletes them first; spans are inserted with an anti-join on (ServiceName, TraceId, SpanId); columns are matched by name (a missing nullable column is NULL), `SessionId`/`SessionIds` computed like `sessionIDColumns` when absent, `SourceFile` kept (rewritten by `--map-path OLD=NEW`: `ParsePathMapping`, `mapPathExpr`, whole path elements, first match wins; also applied to `import_state.file_path`), `metric_names` refilled, baselines merged, dashboards restored by ID (existing IDs kept; a restored default stays default only if there is none). A column the export lacks takes its column default, else NULL. `import_state` is merged by (source, file_path) (`restoreImportState`): a file without stored state gets the export's, an equal state is kept, another state is marked partial with offsets 0 and no parser state, so the next import or watch replaces all of the file's records. Format-1 exports (no `import_state.parquet`, no `SourceFile`) restore with `SourceFile` NULL and a warning recommending `import --purge`. Derived columns are recomputed in the restore transaction (`restoreDerived`: `otel_logs.MessageKey` NULL, then `fillMessageKeysTx`). Text is redacted with the store's redactor (set from `AI_OBSERVER_REDACT` by `cmd_restore`) through scalar UDFs registered on the write transaction's connection (`storage/restore_redact.go`: `COALESCE(f(x), x)`, f returns NULL when nothing changes, `{}`/`[]` skipped in SQL, resource/scope maps cached, values scanned in place and decoded only when changed; session IDs read before redaction; baselines redacted like their series): 10 s instead of 5.5 s for 3.5M records. A derived column added by a migration must be in `restoreDerived` with its recomputation, or in exports, or computed in `storage.restoreSelect`. **Before migrations** `NewDuckDBStoreWithOptions` copies a database that holds data and is below `LatestSchemaVersion()` to `<db>.pre-migration-v<version>` (`storage/migration_backup.go`: checkpoint, disk-space check that fails before copying and migrating, byte copy via `.partial` + fsync + rename so an older version can open it, `COPY FROM DATABASE` fallback when the open file cannot be read; older backups removed; `AI_OBSERVER_SKIP_MIGRATION_BACKUP` skips it; 1.4 s for 717 MiB)

**Key packages:**
- `internal/otlp/` - Format detection (`format_detector.go`), decoder interface with proto/JSON implementations, OTLP → row conversion, derived metrics
- `internal/storage/` - DuckDB storage layer with separate stores for traces, logs, metrics, dashboards, import state. `session_store.go` reads the logs and spans of a session (`SessionLogs`, `SessionSpans`: of the session's traces, whole traces only when they have GenAI spans, `gen_ai.operation.name`, the only spans that become transcript messages; of other traces, such as Codex turns, only the spans carrying the session ID; a time range would cut off sessions that span more than the listed range, and spans that outlast a session's logs). Logs and spans store `SessionId` (first ID in `tools.SessionIDKeys` order) and `SessionIds` (all aliases, `VARCHAR[]`) at insert time (`sessionIDColumns`); query sessions through these columns, never by parsing attribute JSON. `SourceFile` is the session file a record was read from (empty for OTLP; a compressed Codex rollout's plain `.jsonl` path); the `insert*Tx` helpers take it, exports keep it, and restore writes it back. `otel_logs.MessageKey` (migration 13 only adds the column) is the log's encoded `msgmerge.Key`, or `msgmerge.KeyVersion` alone for logs that are no message, set in `insertLogsTx` after redaction (`storage/message_keys.go`); keys that are NULL or of another `KeyVersion` are computed by readers (`computeMessageKeys`), refilled by servers in the background (`DuckDBStore.StartMessageKeyRefill` in `serveAPI`, for `serve`, `watch` and `serve --watch`: `RefillMessageKeys`, 5,000 rowids per write transaction, `messageKeyRefillChunk`; 42k logs in 0.8 s, ≤ 125 ms per transaction; stopped by `Close`) and by restore in its transaction (`fillMessageKeysTx`); CLI commands do not refill. Exports leave the column out (`exporter.exportColumns`). Metric series (`metrics_series.go`, `QuerySeries`/`QuerySeriesBatch`; `QueryMetricSeries`/`QueryBatchMetricSeries`/`QueryMetricTotals` wrap them) turn CUMULATIVE points into per-series increases (LAG over ServiceName, Attributes + ResourceAttributes, 24h lookback) before grouping, so cumulative and delta counters aggregate alike. Series are grouped by service and by type (aggregates: type and model), or by up to two attributes (`MetricQuery.GroupBy`, keys validated by `metricAttributeJSONPath`, `service` rejected), filtered by exact attribute values (`Filters`, at most 10, applied before the LAG since they keep or drop whole series; baselines get the same filter), or merged into one series (`Merge`). Bucketed queries return at most `MaxSeries` (100) series (`too_many_series`); intervals are 1 s to 31 days. Histograms (`metrics_histogram.go`) take a `Stat`: avg (default; sum/count, exact), sum, count, min, max, p50/p90/p95/p99; a stat on another type is accepted only as its natural aggregation (sum for counters, avg for gauges). Cumulative histograms' count, sum and bucket counts become increases (a series restarts when its count drops or its bounds change; min/max are since the series start); percentiles sum the bucket counts per cell in SQL (`SUM(bc[i])` columns: 29 ms vs 45 ms unnest, 92 ms `list_reduce` on 53k points) and `estimateQuantile` interpolates in Go (bounds merged through piecewise-linear CDFs); exponential histograms have no percentiles. Empty buckets of averages, extremes, percentiles and gauges are NaN, JSON `null` (`api.TimeSeries.MarshalJSON`). Buckets follow `SeriesRange.TimeZone` with ICU (built into the embedded DuckDB): `time_bucket` on the zone's wall clock (day buckets 23/25 h on DST days), names validated by Go (`time/tzdata` embedded) and ICU (`DuckDBStore.ValidateTimeZone`); UTC keeps the plain SQL; `LocalTimeZone` (ICU's detected zone) is the MCP default. Failed queries are `MetricQueryError` with an `api.MetricError*` code. Deleting metric rows (`deleteInRange`: delete, retention, purge) first keeps the last point of each cumulative series in `metric_baselines` (`keepCumulativeBaselines`, `arg_max_null` so all values come from one row; histograms also `Count`, `BucketCounts`, `ExplicitBounds`, migration 12; baselines without them make the next point count for the sum only), which series use as the previous point, and prunes `metric_names`. `metric_names` (base schema, filled by `insertMetricsTx`) answers metric name lists without scanning points. List queries (logs, traces, sessions, metric points) take a `Page`: the first page has no cursor, the next passes the previous page's `NextCursor` (keyset pagination on the sort key plus a tiebreaker: `rowid` for logs and metric points, `TraceId` for traces, `SessionId, ServiceName` for sessions; `newestFirstAfter`, `encodeCursor`/`decodeCursor`), and `Offset` remains for API clients, up to `maxOffset` (10,000; deeper offsets return `ErrOffsetTooLarge`, a 400, since an offset page sorts every row before it). They fetch one row more than the page to set `HasMore`. Logs, metric points and traces count `Total` with a separate `COUNT(*)` (a `COUNT(*) OVER ()` in the page query materializes every matching row and was 10x slower for logs; for traces it gained nothing, since the page query aggregates every trace anyway). Sessions page through the sessions active in the range by their last activity there, count the grouped sessions with a window count in the same query (2.5x faster), and then read each listed session's whole extent, so start, count and model are not clipped to the range (`fillSessionExtents`; the message count is the logs whose event becomes a transcript message, `tools.TranscriptEvents`, and for sessions without such logs their GenAI spans, `countSpanMessages`; title, repository, branch and usage of the listed sessions come from `sessionDetailsQuery`, which reads all attribute keys with one `json_extract_string(LogAttributes, [paths])` per log: one extraction per key was 4x slower, since Codex tool results make the attributes MBs large. The usage attribute keys are shared with the transcript: `tools.InputTokenKeys`, `OutputTokenKeys`, `CacheReadTokenKeys`, `CacheWriteTokenKeys`, `CostKeys`, `PromptEvents`). Message counts and usage follow the transcript's merge rules (`session_list.go`, `session_details.go`): `sessionDetailsQuery` is exact for sessions whose records cannot merge; sessions that can (both paths, and Claude Code and Gemini CLI OTLP sessions) are merged again from their stored message keys by the transcript's own `msgmerge.Merge` (`mergeSessionMessages`; a dual-path 7-day page 295 → 120 ms, single-path ~60 ms; `TestSessionListCountsMatchTranscripts` checks counts, usage and titles of lists and transcripts). `SessionLogs` and the key query order by `Timestamp, rowid`. Prompts whose text the tool did not record give no title (`msgmerge.UnrecordedPrompt`: `<REDACTED>`, Gemini's `User prompt. Length: N.`); SQL tests only prompts ≤ 64 bytes, with a literal regexp (a parameter is compiled per row). Context a tool injects as a user message gives no title either (`msgmerge.InjectedPrompt`: text starting with a tag such as Codex's `<environment_context>` or Claude Code's `<command-name>`, or Codex's `# AGENTS.md instructions for ` / `# Files mentioned by the user`), so a Codex session's title is its first typed prompt. Lists of code constants in SQL use `sqlIn` (`list_contains` of literals): DuckDB turned `IN` lists into joins that copied the attributes. Trace lists select and order a page of traces by their first span in the range (the keyset key) and describe each by all of its spans (`traceOverviews`: one `TraceId IN (...)` query per page, also the detail view's overview), so a trace that began before `from` shows its real start (3.2M spans: 24h 68 ms, 7d 86 ms). `NewDuckDBStore` sets `late_materialization_max_rows` to `maxPageSize + 1`: above it (DuckDB default 50) an `ORDER BY ... LIMIT` reads wide columns such as log bodies for every row in range (15x slower). Search conditions come from `containsCondition`, which adds `ESCAPE` only for terms with `%`, `_` or `\` (it halves `ILIKE` speed). Log and trace searches over JSON attribute columns use `searchCondition`: a term that reads the same in JSON (`jsonUnchanged`) matches the JSON text (`CAST(col AS VARCHAR) ILIKE`, keys included); other terms (`"`, `\`, control characters, `<`, `>`, `&`, which Go's encoder escapes) match the unescaped values, `jsonValuesText` (`array_to_string(json_extract_string(col, '$.*'), chr(10))`, shared with session search's `termCondition`), 1.4–2.4x slower; plain words cost the same. Searches read every body in range, twice (count and page); measured alternatives were not better: `contains(lower(...))` 50% slower, case-insensitive `regexp_matches` 12% faster but needs regex escaping, and one query with a window count joined back to the page 2.6x slower. Build `WHERE`/`HAVING` clauses with `conditions` (`query_helpers.go`), which keeps each condition with its arguments. A `service` argument is a service filter (`serviceCondition`, `conditions.addService`): a tool name (`codex`) matches all of the tool's services (`tools.FilterServices`), a service name only itself; callers pass `tools.ResolveFilter` output or the raw API parameter. Telemetry writes go through `inWriteTx` + DuckDB appenders (`appender.go`); `writeTx.redact` (the store's redactor, `SetRedactor`, set by `server.NewRedactor` in `serve`/`watch`/`import`) redacts text in `insertLogsTx`, `spanRow` and `insertMetricsTx`, after session IDs are read, so every ingestion path is covered (resource and scope attribute maps, shared by the records of an export, once per transaction: `writeTx.redactShared`); values for `JSON` columns must be wrapped in `rawJSON(...)`, otherwise the appender stores them as JSON strings and `json_extract` returns nothing. `s.mu` serializes writes only; reads take no lock (DuckDB MVCC). A single writer goroutine with group commit was measured and not adopted: running 8 queued OTLP-sized inserts in one transaction gained at most ~25% (2.8 → 2.1 ms per request, dominated by appender work rather than commits), while serialized writes already sustain ~350 requests/s. Files: `telemetry_insert.go` (`InsertTelemetry`, `InsertTraces`), `imported_file.go` (`InsertImportedFile`, `ImportTx`, legacy replacement), `usage_overlap.go`, `session_list.go` (`QuerySessions`), `stats.go`, `values.go` (array/map formatting), `maintenance.go`, `migration_backup.go`, `restore.go`, `restore_redact.go`, `export.go`, `message_keys.go`, `ping.go`. List reads return `[]` for empty results, since the API sends lists as arrays (`api.List` guards handlers with several sources; `TestAPIResponsesMatchTypes` catches `null`)
- `internal/handlers/` - HTTP handlers for OTLP ingestion (`otlp_*.go`, shared `otlp_ingest.go`), query API (`query.go`), dashboards (`dashboard.go`), admin API (`admin.go`), health (`health.go`), status (`status.go`); `handlers.go` holds `Handlers`, `New`, `decodeBody`. Read query parameters with `newParams` (`params.go`): typed getters (`Int`, `Int64`, `Limit`, `Bool`, `Enum`, `TimeRange`, `OptionalTimeRange`, `Page`, `SearchTerms`, …) collect errors, and `p.Valid(w)` answers one 400 naming all of them. Lists: `limit` a positive integer, clamped to the endpoint maximum; `offset` 0..10,000; booleans only `true`/`false`; never default an invalid value silently. `statsCache` (`stats_cache.go`, TTL 5 s) shares one load among callers, detached from their contexts (timeout 1 min), and serves stats expired less than a minute ago while refreshing
- `internal/importer/` - Session file parsers (`claude.go`, `codex.go`, `gemini.go`), Claude activity reconstruction (`claude_activity.go`), repository attribution
- `internal/watcher/` - File watcher and incremental per-tool processors
- `internal/pricing/` - Embedded pricing JSON (`data/*.json`), model alias resolution, cost calculation; `LoadOverrides` merges files from `AI_OBSERVER_PRICING_DIR` at startup, such as the files written by `ai-observer pricing update`. All four price files come from models.dev (`models_dev.go`: `ModelsDevFetcher` reads `https://models.dev/api.json`, MIT; `UpdatePricingFile` merges a provider into its file per `ModelsDevTargets`, keeping models models.dev lacks, never undoing deprecation, refusing changes over 10x; `pricing_file.go`: `FormatPricingFile` keeps the built-in layout so refreshes diff cleanly, enforced by a test). The providers' own APIs return no prices. `make pricing-update` sets `lastUpdated`, which the daily `Pricing freshness` workflow (`scripts/check-pricing-age.sh`) requires to be at most 7 days old. `github_models_catalog.json` is the final snapshot of the retired GitHub Models catalog (Copilot alias matching only) and is not checked. Claude imports support pricing modes `auto` (default: use recorded `costUSD`, else calculate), `calculate`, `display`
- `internal/traceview/` - Assembles the trace detail views from storage span reads (`traceview.Load`, used by the trace handler and `get_trace`), a page of spans at a time with the overview of the whole trace (`api.SpansResponse`; `storage.TraceSpansPage`, keyset on `Timestamp, SpanId, ServiceName`; never read a trace whole: Codex turns have over 100,000 spans; parents start before their children, so the pages read so far form a tree; the detail page loads further pages on request): OTel traces, and Codex operations (paged in memory, `storage.PageOfSpans`) (a turn's spans and their descendants within a Codex session trace)
- `internal/transcript/` - Assembles session transcripts from the logs and spans storage reads (`transcript.Load`, used by the transcript handler, which returns a page of it, `transcript.Page` in `page.go`: the transcript is assembled whole, pages bound what is sent and rendered; the transcript page loads further pages on request and, when all are loaded, asks for the messages after them on live refresh). `transcript.Cache` (`cache.go`; one per server, shared by REST and MCP: `Handlers.SetTranscriptCache`, `mcpserver.WithTranscripts`) keeps up to 8 transcripts / ~128 MB (LRU; the last used is always kept), each for a minute after its last use and only while `DuckDBStore.WriteGeneration` (a counter of committed write transactions) is unchanged; concurrent loads of a session share one build on a detached context, cancelled when no caller waits; failed builds are not cached (largest measured session: 7,665 messages, ~17 MB, 0.6 s to build). Imported transcript messages, OTLP events and GenAI span content become one ordered message list (logs by `msgmerge.Merge`, which returns a `Plan{Into, Steps}` that transcripts replay; spans afterwards in `merge.go`): the records of one message (session file message, OTLP event, GenAI span, in that order of precedence) are shown once, filled in with what the others add (tool name, usage, duration, success), matched by tool call ID (`tools.ToolCallIDKeys`: results with results, calls with calls), entry UUID (`tools.MessageUUIDKey`), request ID (Claude Code's usage event `api_request` goes into the file's reply or first tool call of its request, else its `assistant_response` event, `tools.ReplyEvent`, whose `response` holds the reply text), prompt time and text start (`tools.PromptMatchWindow` 2 s, `PromptMatchPrefix` 32 characters; Claude Code and Codex; a redacted prompt by time alone), equal reply text within `ReplyMatchWindow` (Claude Code replies without UUID), and a span and a log event by span and content. Gemini CLI (no shared ID): prompts by text start, or by `prompt_length` (UTF-16 length) when the prompt was not logged, within 5 min (`msgmerge.GeminiPromptMatchWindow`); `api_response` into the file reply of equal token counts within 1 h (`GeminiMatchWindow`); its `gen_ai.client.inference.operation.details` record into the same message (over OTLP only, the `api_response` into the details record, which has the text); `tool_call` into the file `tool_use` of the same tool and canonical arguments within 1 h. Pairings by time and value are one to one, nearest first. A transcript mixing file and event messages is numbered by position; span selection follows Copilot session aliases
- `internal/msgmerge/` - Log → transcript message (`LogMessage`, content extraction) and the rules that merge the records of one message (`Merge` → `Plan{Into, Steps}`), shared by transcripts and session lists, which run it on stored keys. `Key` (`KeyOf`) holds what the rules compare (tool, event, role, call ID, UUID, request ID, prompt prefix and length, reply text hash, usage, Gemini tool name and argument hash), stored per log in `otel_logs.MessageKey`. `KeyVersion` = `keyRevision` + a hash of the `tools` constants keys depend on, so changed lists change it by themselves; bump `keyRevision` when `KeyOf` or `LogMessage` change
- `internal/tools/` - Provider table (`providers`): tool names ↔ OTLP `service.name` (a tool can send several: `OtherServices`; `ToolForService`, `ServiceNames`), display names, whether session files can be imported, and each tool's transcript event roles (`TranscriptRole`). Also the attribute key lists the tools share, in priority order (`SessionIDKeys`, `ModelKeys`, `ToolNameKeys`, `ToolInputKeys`, `ToolOutputKeys`, `ToolCallIDKeys`), and the transcript merge constants (`MessageUUIDKey`, `RequestIDKey`, `ResponseKey`, `UsageEvent`, `ReplyEvent`, `PromptMatch*`, `MatchesPromptsByTime`). `Tool.HasService` tells whether a service is one of a tool's. Importer sources, export sources, the CLI's tool arguments and `GET /api/providers` (dashboard service names) derive from it
- `internal/attrval/` - Typed reads (`Int`, `Float`, `Bool`) from string attribute maps, taking candidate keys in priority order; use these instead of ad-hoc parsing
- `internal/exporter/`, `internal/deleter/` - Parquet export and data deletion for the `export`/`delete` commands (and the admin API). An export writes, in one read transaction (`Exporter.inSnapshot`, so the Parquet files are one snapshot; `dashboards.json` is read outside it), `traces.parquet`, `logs.parquet`, `metrics.parquet` (all columns incl. `SourceFile`; logs `* EXCLUDE (MessageKey)`, `exportColumns`), `import_state.parquet` (`storage.ExportImportStateQuery`: states of the files whose records are exported; a file with records the export leaves out is marked partial with offsets 0; an unranged tool or `all` export adds its files' states without records), `metric_baselines.parquet` (stored baselines up to `to`, plus for a ranged export the last cumulative point before `from` within the 24 h lookback), `dashboards.json` (all dashboards with widgets), `manifest.json` (`format` 2 (`ManifestFormat`; restore accepts ≤ 2, older versions refuse 2), version, schema version, source, services, range, rows per file) and the views database (created with `SET file_search_path`, never `os.Chdir`); a tool export covers all of the tool's services. `deleter.ExecuteChunked` deletes a day per transaction and reports the file's free space afterwards. They and the importer write to an injectable writer and read confirmations from an injectable reader (`Options.Out`/`In`, `Importer.SetIO`; `prompt.Confirmed` reads a yes), never `fmt.Print*`/`os.Stdin` directly
- `internal/maintenance/`, `internal/restore/`, `internal/diskspace/` - `backup`/`compact`, `restore`, and free disk space (`Check`, `FormatBytes`, `FileSize`; `GetDiskFreeSpaceExW` on Windows), see "Database space, backup and restore"
- `internal/otlpgrpc/` - OTLP/gRPC server (`NewServer(ingester, Options)`; `metrics.go`: a `stats.Handler` counting requests)
- `internal/selfmetrics/` - AI Observer's own metrics (see Self-metrics)
- `internal/prompt/` - `Confirmed`, the shared reader of confirmation answers
- `internal/apigen/` - Generates the API's TypeScript types and OpenAPI document from Go types, standard library only: `reflect` over a registry of root types, so the result follows `encoding/json` (a field `encoding/json` can leave out, `omitempty` on a type it applies to or `omitzero`, is optional; nil-able pointers, slices and maps without `omitempty` are `| null` unless declared `NonNull`; types only clients send have no nulls), and `go/ast` for doc comments (→ TSDoc, OpenAPI descriptions) and the string constants of enums (a name prefix or a named type). The registry is `internal/apigen/spec` (`spec.go`: REST/WebSocket and MCP configs; `routes.go`: the route table with query parameters, hand-maintained). `go run ./cmd/apigen` (`make api-types`) writes `frontend/src/types/api.gen.ts`, `frontend/src/types/mcp.gen.ts` and `docs/openapi.json`; `-check` only reports. Drift tests (`go test`): `spec.TestGeneratedFilesAreUpToDate` (stale files), `server.TestRouteTableMatchesRouter` (walks both chi routers with MCP and admin API on; fails on routes missing from the table and table routes not registered), `server.TestAPIResponsesMatchTypes` and `TestMCPOutputsMatchTypes` (real responses on an empty and a seeded database validated against the generated types: missing required field, unexpected `null`, undeclared field, wrong enum value, non-RFC 3339 time), `spec.TestEnumsCoverWrittenValues`. When adding an API type or field: new root types go into `spec.REST()`; bind closed string sets to enums (`FieldSpec.Enum`); declare a list `NonNull` only when every handler writes `[]`; types with their own `MarshalJSON` must be described in `Config.Custom` (only `api.TimeSeries`). When adding a route: add it to `spec/routes.go`
- **Consumer-side store interfaces:** `handlers`, `mcpserver`, `deleter` and `restore` take a `Store` interface listing exactly the store methods they call (`*/store.go`, with `var _ Store = (*storage.DuckDBStore)(nil)`); tests fail single calls with a fake that embeds it (`store_fake_test.go`). `importer` (its `InImportTx` callback takes `*storage.ImportTx`), `exporter`, `handlers.Admin`, `maintenance` and `server` keep `*storage.DuckDBStore`; `traceview`, `transcript` and `watcher` already had interfaces
- `internal/mcpserver/` - MCP server (official Go SDK `modelcontextprotocol/go-sdk`, Streamable HTTP, stateless with JSON responses) with read-only tools over storage, `transcript.Load` and `traceview.Load`: `search_conversations` (`DuckDBStore.SearchSessions`: records of session logs and spans containing all terms as substrings, in any order, in the body or attribute values (not keys; `termCondition` checks the JSON text first as a cheap filter), longest term checked first since DuckDB evaluates the conditions in order; the query is split into words and "quoted phrases" by `storage.SearchTerms`; `GET /api/sessions?search=` runs the same query with a cursor, `SearchSessionsPage`; snippets are cut from redacted text; whole-session extents, from spans for sessions without logs), `list_sessions` (rows of both carry `title`, redacted again, `repository`, `branch`, `cost_usd`, `input_tokens`, `output_tokens` when known; the message count is the transcript's), `get_transcript` (paged in memory, text cut to `max_chars`; `title` and `totals` over the whole session; `search` selects matching messages like `transcript.Search`, combinable with `roles`, each with its `position` in the whole transcript, and `cursor` takes a position when no filter is given; messages recorded over OTLP and in the session file appear once), `usage_summary` (cost/token metrics per tool via `QueryMetricTotals`, which groups aggregates by an attribute such as `repository`, or daily series in `timezone`, default the server's, `DuckDBStore.LocalTimeZone`; a Claude Code, Codex or Gemini CLI request recorded both over OTLP and in its session file counts once), `search_logs`, `list_traces`, `get_trace` (depth-first span tree), `list_metric_names`, `query_metrics` (`group_by` ≤ 2 attributes, `filters`, `stat`, `timezone`; empty histogram and gauge buckets are left out; `interval` in the output is the bucket size used), plus the resource `ai-observer://sessions/{id}/transcript`. Times accept RFC 3339, dates or ages (`7d`). Results are redacted again with the store's redactor (covers data stored before redaction). `usage_summary`, `query_metrics`, `get_trace` and `list_sessions` name a view for MCP Apps hosts in `_meta.ui.resourceUri` (`app.go`: `withApp`, also the legacy `ui/resourceUri` key): `ui://ai-observer/view.html`, served by `resources/read` from `frontend.MCPAppHTML()` (with `openai/widgetDescription`, `appModelDescription`, telling OpenAI hosts' models not to restate what the view shows; MCP Apps has no such field) and left out of `resources/list` (`hideAppResources`, so agents that browse resources do not read the ~1 MB page); clients without MCP Apps ignore it. The view draws the tools' structured content, whose types are generated from `mcpserver.AppOutputs()` (`app_types.go`) into `frontend/src/types/mcp.gen.ts` (`make api-types`). Tool errors (store failure, invalid cursor, timeout) are tool results with `isError`, not protocol errors. `New`/`NewHandler(store, version, opts...)` take `mcpserver.Store`. `usage_summary` leaves token types contained in another (`usageMetrics.partOf`: cached input, Codex cache writes and reasoning) out of `total_tokens`, and lists models with tokens but no cost as `unpriced_models`. `list_traces` leaves out background traces unless `include_background`. MCP requests have no timeout but are cancelled when shutdown starts (`endOnStop`), since `http.Server.Shutdown` would wait for them. Mounted on the API router behind HostCheck/OriginPolicy/TokenAuth (`/mcp` is in `TokenAuth`'s protected paths); the SDK's own localhost check is off since HostCheck covers it, and `/mcp` is exempt from the 5-second request timeout (`exceptPath`)
- `internal/redact/` - Secret and personal-data redaction (`AI_OBSERVER_REDACT`: `secrets` default, `all`, `off`): issuer formats (`sk-ant-`, `ghp_`, `AKIA`, JWTs, PEM keys, `Authorization` headers, URL credentials) and credential-looking assignment values; mode `all` adds emails (pseudonym with hash), Luhn-checked cards, IBANs, international phone numbers, US SSNs, public IPv4. Deterministic replacements (`[REDACTED:kind]`) keep metric series stable. Rules skip strings without their literal hints or enough digits (keep it that way: about 130 MB/s). Build credential-shaped test data at runtime (`fake(...)`), never as literals, so secret scanners do not flag the repository
- `internal/websocket/` - Hub/client pattern for real-time broadcasting; the hub holds the origin check (`Hub.SetOriginCheck`) and is stopped on shutdown (`Hub.Stop`)
- `internal/server/` - Server setup, routing configuration (`routes.go`), TLS (`tls.go`), auth and its limiter (`auth.go`, `auth_limit.go`, `grpc_auth.go`), admin routes (`admin.go`), retention and size limit (`retention.go`, `database_size.go`), self-metric middleware
- `internal/frontend/` - Embedded SPA handler, and the MCP tools' view (`MCPAppHTML`, embedded from `mcpapp/mcp-app.html`, not served by the SPA handler)
- `internal/middleware/` - OTLP payload limit, API request timeout
- `pkg/compression/` - Request decompression middleware for incoming OTLP/HTTP data (`DecompressMiddleware`: gzip, deflate, identity)

**Configuration (environment variables):** all `AI_OBSERVER_*` variables are listed once, grouped, in `cmd/server/settings.go`, the table `ai-observer --help` prints; `TestHelpListsEverySetting` fails when code reads a variable the table lacks, or the table lists one no code reads. Flags of the same name override them (`cmd/server/server_flags.go`: `serverFlags.register`/`apply`, `registerDBFlag`, `optionalPortFlag`).
- `AI_OBSERVER_HOST` - Interface the servers listen on, an IP or host name (default: 127.0.0.1; Docker: 0.0.0.0; `--host`)
- `AI_OBSERVER_API_PORT` - API and dashboard port (default: 8080; `--api-port`)
- `AI_OBSERVER_OTLP_PORT` - OTLP/HTTP port (default: 4318; `--otlp-port`)
- `AI_OBSERVER_OTLP_GRPC_PORT` - OTLP/gRPC port (default: 4317; `0`, `off`, `false`, `no`, `disabled` turn it off, `config.getOptionalPort`; `--otlp-grpc-port`, `serve` only)
- `AI_OBSERVER_FRONTEND_URL` - CORS allowed origin, an http(s) origin, trailing `/` trimmed (default: http://localhost:5173)
- `AI_OBSERVER_ALLOWED_HOSTS` - Extra host names accepted by the API and as browser origins, comma-separated; entries such as URLs or host:port are dropped (`*` disables the Host and Origin checks)
- `AI_OBSERVER_MCP` - Serve the MCP endpoint `/mcp` on the API port (default: false; also `--mcp` on `serve` and `watch`)
- `AI_OBSERVER_ADMIN_API` - Serve the admin API `/api/admin/*` (default: false)
- `AI_OBSERVER_AUTH_TOKEN` - Shared token required on OTLP and API requests (default: no authentication)
- `AI_OBSERVER_TLS_CERT`, `AI_OBSERVER_TLS_KEY` - PEM certificate and private key: HTTPS on both HTTP ports and gRPC over TLS; both or neither (default: no TLS)
- `AI_OBSERVER_REDACT` - Redaction before storage: `secrets` (default), `all` (secrets and personal data), `off`
- `AI_OBSERVER_DATABASE_PATH` - DuckDB file path (default: ./data/ai-observer.duckdb; `--db` on every command that opens it)
- `AI_OBSERVER_DB_MEMORY_LIMIT` - DuckDB `memory_limit`, a size such as `2GB` (default: DuckDB's, 80% of memory; `storage.Options`, passed to `NewDuckDBStoreWithOptions` by every command via `Config.StorageOptions`). Measured on a 746 MB database after ten list, search and transcript requests: 2.4 GB resident without a limit, 1.4 GB with `1GB`, 1.05 GB with `512MB` (trace search 0.7 → 1.4 s); the limit does not cover Go's memory
- `AI_OBSERVER_DB_THREADS` - DuckDB `threads` (default: one per CPU core)
- `AI_OBSERVER_RETENTION_DAYS` - Delete telemetry older than this many days in `serve`/`watch` (default: keep all)
- `AI_OBSERVER_MAX_DB_SIZE` - Delete the oldest days in `serve`/`watch` while the used blocks exceed this size, such as `20GB` or `512MiB` (KB, MB, GB, TB are 1024-based like KiB, GiB; `storage.ParseByteSize`; default: no limit)
- `AI_OBSERVER_STORE_ALL_SPANS` - Also store the tools' noise spans (default: false; `tools.Provider.NoiseSpans`)
- `AI_OBSERVER_BACKUP_DIR` - Where the admin API writes backups and exports (default: `backups` next to the database)
- `AI_OBSERVER_SKIP_MIGRATION_BACKUP` - Migrate without the pre-migration copy (default: false)
- `AI_OBSERVER_LOG_LEVEL` - Log level: DEBUG, INFO, WARN, ERROR (default: INFO; `config.LogLevel`). Successful HTTP requests are logged at DEBUG
- `AI_OBSERVER_LOG_FORMAT` - `text` (default) or `json`, for all commands (`config.LogFormat`, `logger.Init`, `initLogging` in `cmd/server`)
- `AI_OBSERVER_CLAUDE_PATH` - Claude Code session directories, comma-separated (import/watch; default: `~/.config/claude/projects` and `~/.claude/projects`)
- `AI_OBSERVER_CODEX_PATH` - Codex session directory (import/watch; default: `$CODEX_HOME/sessions` or `~/.codex/sessions`)
- `AI_OBSERVER_GEMINI_PATH` - Gemini CLI directory (import/watch; default: `$GEMINI_HOME/tmp` or `~/.gemini/tmp`)
- `AI_OBSERVER_PRICING_DIR` - Directory with pricing files overriding/adding to the embedded ones (`docs/pricing.md`)

Invalid values (ports, retention days, sizes, booleans, host, origin, log level and format) are logged as warnings and replaced by the default (`config.getEnvInt`, `getEnvBool`, …). `/api/status` reports the settings (`settings`, never the token or key paths).

### Supported Tools

| Tool | `service.name` | Ingestion |
|------|----------------|-----------|
| Claude Code | `claude-code` | OTLP, import, watch |
| OpenAI Codex (CLI and app) | `codex_cli_rs`; since 0.167 also `codex-app-server`, `codex-exec-server` | OTLP, import, watch |
| Gemini CLI | `gemini_cli` | OTLP, import, watch |
| GitHub Copilot (VS Code) | `copilot-chat` | OTLP only |
| GitHub Copilot CLI | `github-copilot` | OTLP only |
| OpenCode | `opencode` | OTLP only (via plugin) |

Adding a provider touches: the provider table in `internal/tools/tools.go` (service name and any further ones in `OtherServices`, display name, transcript event roles; new session ID or model attribute keys go into the key lists there, and a new session ID key needs a migration to backfill `SessionIds`), export/delete service filters, `cmd/server/cmd_setup.go`, content extraction in `internal/transcript/` if its events carry content in new attributes, pricing data (if costs are derived), frontend `lib/metricCatalog/<provider>.ts` (metric definitions, added to `METRIC_CATALOG` in `lib/metricMetadata.ts`) and `lib/metricMetadata.ts` (icons, grouping, and the fallback service names, which the dashboard uses until `/api/providers` has loaded; for a tool with several services also `FALLBACK_TOOL_SERVICES` and `TOOL_FILTER_NAMES`, from which `serviceFilterOptions` adds a tool entry such as "OpenAI Codex (all services)" to the service filters), the MCP view's tool display names (`src/mcp-app/data.ts`, `TOOL_NAMES`), dashboard widget catalog, and documentation (README, `docs/`, `frontend/src/data/documentation.ts`).

### Frontend (React + TypeScript)

Vite-based React app with Tailwind CSS v4:
- `react-router-dom` for routing (Dashboard, Traces, Trace Detail, Metrics, Logs, Sessions, Session Transcript, Status, Docs pages, and a not-found page; each route has its own error boundary, `components/RouteErrorBoundary.tsx`, which resets on navigation)
- `@tanstack/react-query` for all server state (`lib/queryClient.ts`, `lib/queries.ts`); `@tanstack/react-virtual` for the transcript lists
- `zustand` for client state only (`stores/telemetryStore.ts` keeps timestamps and counters from real-time notifications; `stores/dashboardStore.ts` the dashboard's edit mode and panel state; `stores/authStore.ts` whether a request was answered `401`, shown by `layout/AuthBanner.tsx`)
- `recharts` for visualizations
- `@dnd-kit` for drag-and-drop dashboard widgets
- WebSocket singleton (`hooks/useWebSocket.ts`) for live updates

**Key files:**
- `lib/api.ts` - API client: plain requests with a 30-second timeout and an `AbortSignal`; it does not retry or deduplicate, the query layer does. Errors are `HTTPError`/`TimeoutError` from `lib/errors.ts`; show them with `errorText(err, fallback)` (server message, `401` token hint, timeout text)
- `lib/queries.ts` - Query key factory (`queryKeys`) and one hook per read endpoint (`useLogs`, `useTraces`, `useSessions`, `useTraceSpans` and `useSessionTranscript` as infinite queries, `useMetricSeries`, `useBatchMetricSeries`, `useStatus`, ...). Never fetch in an effect. Retries (`lib/queryClient.ts`): not for `4xx`, `503`, `504`, timeouts and aborts. List hooks keep the previous page while loading (`placeholderData`); don't take `nextCursor` from placeholder data. Dashboard and widget changes are mutations in `hooks/useDashboardMutations.ts` (optimistic with rollback, keeping the default and by-ID cache entries in step)
- `hooks/useTimeSelection.ts`, `lib/timeUtils.ts` - A page's time range. Query keys contain the selection (`range.key`), never timestamps, and the query function calls `range.resolve()`, so every request of a relative range ends now. The URL wins, relative selections are stored per page, absolute ones live in the URL only
- `hooks/useUrlState.ts` - A page's filters and time selection in the URL (one writer, defaults omitted, history entry replaced); every page with filters uses it
- `hooks/useLiveInvalidate.ts`, `hooks/useNewItems.ts` - Live data on top of queries: refetch a query key on real-time notifications (timing from `useLiveRefresh`), and the "N new" button of Logs and Traces (count from the API with the list's filters; clicking refetches the first page). Sessions, Metrics and the dashboard refresh by themselves; Logs and Traces only on request, so rows do not move
- `lib/format.ts`, `lib/traces.ts`, `lib/listLinks.ts` - Shared formatters (durations, tokens, cost, bytes, counts), trace row helpers (`traceDetailPath`) and links between pages; links to detail pages pass `ListLinkState` so their Back button returns to the filtered list (`hooks/useBackLink.ts`)
- `lib/metricMetadata.ts` - Metric lookup and formatting helpers, provider/service display names and icons; assembles `METRIC_CATALOG` from `lib/metricCatalog/*.ts` (one file per provider). Shared metric types and `UNIT_FORMATS` live in `lib/metricTypes.ts`. Histogram statistics: `HISTOGRAM_STATS`, `isHistogramMetric` (catalog `metricType`), `defaultHistogramStat` (`sum` for token histograms, else `avg`), `seriesStat`/`statParam`; the stat the server reports decides units (`statUnit`) and stacking (`isAdditiveStat`: avg, min, max and percentiles are never stacked); `breakdownOptions` lists catalog breakdowns and the attribute keys of the metric's points
- `lib/timeZone.ts` - `browserTimeZone`: series requests send the browser's zone (`tz`, batch `timeZone`); not in `lib/api.ts`, which tests mock whole
- `types/api.gen.ts`, `types/mcp.gen.ts` - Generated from the Go types (`make api-types`; do not edit). `types/*.ts`, `lib/api.ts` and `mcp-app/types.ts` re-export them under their usual names; frontend-only types (`TimeSelection`, `WIDGET_TYPES`, `EmptyCell`) stay hand-written
- `components/status/` - Status page cards: `DatabaseCard` (size, free space, growth, size limit hints) and `MaintenanceCard` with `useMaintenance.ts` (admin API mutations, never retried, with `ADMIN_TIMEOUT_MS` = 30 min; a deletion invalidates all queries)
- `data/documentation.ts` - Content for the in-app Docs page
- `stores/dashboardStore.ts` - Dashboard UI state (edit mode, add/edit panel); dashboards and widgets are server state (see `lib/queries.ts`). Grid placement (empty cells, next free position, fit checks, reordering, row compaction) is pure code in `lib/dashboardGrid.ts`. The server creates the default dashboard
- `lib/metricGroups.ts`, `components/common/metric-options.tsx` - Metric picker grouping by tool, shared by the metrics page and the add-widget panel; `components/charts/chartData.ts` turns time series into bar chart rows and X-axis labels for both
- `pages/` - Dashboard, TracesPage, TraceDetailPage (`/traces/:kind/:id`), MetricsPage, LogsPage, SessionsPage, SessionTranscriptPage, StatusPage (`/status`), DocsPage, NotFoundPage. List rows are `components/{logs,traces,sessions}/*Row.tsx`
- `components/traces/WaterfallView.tsx` - Also used by the MCP view, so it must import neither the router nor TanStack Query: links are passed in (`renderSessionLink`), the span filter is opt-in (`filterable`); its logic is pure code in `components/traces/waterfall.ts`
- `components/sessions/TranscriptList.tsx` - Window-virtualized transcript (Chat and Timeline); rows are keyed by position in the transcript and their expansion state lives outside them (`hooks/useExpansion.ts`). The transcript page appends new messages of an open session as one page (`lib/transcript.ts`), never by refetching all pages
- `src/test/queryWrapper.tsx` - `renderWithProviders` (fresh QueryClient without retries, MemoryRouter) for tests; page tests mock `@/lib/api` only and use the real stores

**Path alias:** `@` maps to `./src`

**Dev proxy:** Vite proxies `/api` and `/ws` to backend at localhost:8080

**Build output:** `pnpm build` writes to `../backend/internal/frontend/dist`, which the Go binary embeds, and builds the MCP tools' view with `vite build --mode mcp-app` into `../backend/internal/frontend/mcpapp/mcp-app.html` (one file with scripts and styles inlined by `vite-plugin-singlefile`, since MCP Apps hosts render it in a sandbox without network access).

**MCP Apps view** (`mcp-app.html`, `src/mcp-app/`): connects to the host with `@modelcontextprotocol/ext-apps` (`useHost.ts`), picks a view by the host's `toolInfo` or the result's shape (`data.ts`, pure and tested), and reuses the dashboard's charts and trace waterfall. It calls tools through the host only (`callServerTool`), never the API.

**UI Components:** Always use [shadcn/ui](https://ui.shadcn.com) components from the registry, see [overview](https://ui.shadcn.com/llms.txt) also. Use the shadcn CLI to add new components:

```bash
cd frontend && pnpm dlx shadcn@latest add [component]  # e.g., button, dialog, table
```

Existing components are in `src/components/ui/`. Never manually create UI primitives - add them via CLI instead. After `shadcn add`, check `package.json` for a new `cn` dependency: the registry imports `cn` from it; remove it and import `cn` from `@/lib/utils`. Hand-written composites (markdown, editable text, date range picker, data pagination, `QueryError`, `EmptyState`, `ListSkeleton`, `CopyButton`, `PageHeader`, `HighlightedText`, list filters) live in `src/components/common/`, which is linted without the `components/ui` exemptions. A page shows a skeleton on its first load, `QueryError` with Retry when a request fails (never a toast followed by an empty state), and `EmptyState` when there is no data; `PageHeader` sets the document title. Live pages refresh through `hooks/useLiveRefresh.ts`: on real-time notifications (debounced, but at the latest `maxWaitMs` after the first one, so a steady stream of them still refreshes), polling only while the WebSocket is disconnected (and once a minute to move relative time windows), paused while the tab is hidden (`hooks/usePageVisible.ts`). Pages use it through `useLiveInvalidate`. List pages use cursor pagination (`hooks/usePagination.ts` keeps the visited cursors and returns to the first page when `resetOn`, the filters and time selection, changes).

### Database Schema

DuckDB tables defined in `internal/storage/schema.go`:
- `otel_traces` - Span data with nested events and links arrays
- `otel_logs` - Log records with severity and trace context
- `otel_metrics` - All metric types (gauge, sum, histogram, summary, exponential histogram) unified in one table
- `dashboards` / `dashboard_widgets` - User dashboard persistence
- `import_state` - Per-file import/watch state (hash, record count, byte offset, message count, parser state, file size/mtime, `source_tracked`, `partial`)
- `metric_names`, `metric_baselines` - Stored metric names; last deleted point of each cumulative series (histograms also `Count`, `BucketCounts`, `ExplicitBounds`, migration 12)
- `schema_migrations` - Applied migrations; migration 11 removed Claude Code file usage that OTLP also recorded, 12 added the histogram baseline columns, 13 adds `otel_logs.MessageKey` (column only; servers fill it in the background)

Telemetry tables are only indexed for point lookups (`otel_traces.TraceId`, `otel_traces.SpanId`, `otel_logs.TraceId`). Time-range and service filters rely on DuckDB's automatic min-max zonemaps; a benchmark showed ART indexes on those columns gave no query speedup but made the database ~13x larger and inserts slower, so don't add them back without measuring.

The base schema runs on every startup, so keep it idempotent (`CREATE ... IF NOT EXISTS`). Put new columns, data rewrites and other one-time changes into the ordered `migrations` list in `schema.go`: each runs once per database in its own transaction and is recorded in `schema_migrations` (`run` holds Go code for rewrites SQL cannot do, e.g. migration 5 recomputing Gemini cost; keep literal values such as name lists in the migration rather than referencing code that may change). Never edit or reorder a released migration. A database that holds data is copied to `<db>.pre-migration-v<version>` before pending migrations run (see "Database space, backup and restore"). When an index definition changes, drop the old index and create the new one under a versioned name (e.g. `idx_traces_trace_id_v2`).

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

Further project documentation lives in `docs/` (`import.md`, `export.md`, `mcp.md`, `pricing.md`, `redaction.md`, token analysis notes, and the generated `openapi.json`) and `README.md`.

## AI Coding Tool Integration

Point AI tools to OTLP endpoint `http://localhost:4318` (OTLP/HTTP) or `http://localhost:4317` (OTLP/gRPC). `ai-observer setup <tool>` prints full instructions, with the gRPC variant (`claude-code`, `gemini`, `codex`, `opencode`, `github-copilot`; Copilot's gRPC support is unverified, so its setup shows HTTP only).

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
export OPENCODE_OTLP_PROTOCOL=http/protobuf  # or leave it unset (plugin default: gRPC) with OPENCODE_OTLP_ENDPOINT=http://localhost:4317
```

## CLI Options

The `ai-observer` binary uses subcommands:

```bash
ai-observer [command] [options]
```

**Commands** (help texts and shared flag helpers in `cmd/server/settings.go`: `registerDateFlags`, `registerYesFlag`, `registerDryRunFlag`, `valueFlag`, `toolArguments` from `tools.Providers()`; every command's `--help` exits 0, `exitOnError`):
| Command | Description |
|---------|-------------|
| (none) | Same as `serve` |
| `serve` | Receive OTLP (HTTP and gRPC) and serve the dashboard; `--watch [tool]` also reads session files (mode `serve+watch`) |
| `watch` | Read local session files as they change and serve the dashboard, without OTLP (`all`, `claude-code`, `codex`, `gemini`) |
| `import` | Import local session files once (`all`, `claude-code`, `codex`, `gemini`) |
| `export` | Export telemetry to Parquet files (a tool name or `all`) |
| `delete` | Delete telemetry in a time range (`logs`, `metrics`, `traces`, `all`) |
| `backup` | Write a consistent, compacted, checked copy of the database to a new file |
| `restore` | Load an export directory or `.zip` back into the database |
| `compact` | Rewrite the database into a new file without its free space |
| `setup` | Show how to send a tool's telemetry to AI Observer |
| `pricing` | `pricing update`: fetch current prices from models.dev into the four price files in `AI_OBSERVER_PRICING_DIR` or `--output` |
| `healthcheck` | Exit 0 when `GET /ready` of the local server answers 200 (Docker `HEALTHCHECK`; https when TLS is set, certificate not verified on loopback) |

**Global Options:**
| Option | Description |
|--------|-------------|
| `-h`, `--help` | Show help message and exit |
| `-v`, `--version` | Show version information and exit |

**Command options:**
- `serve`: `--watch` (with an optional tool argument; a tool argument without `--watch` is an error), `--backfill` (only with `--watch`), `--mcp`, `--host`, `--api-port`, `--otlp-port`, `--otlp-grpc-port` (a port, or `0`/`off`), `--db`
- `watch`: `--backfill` (import existing data on first start), `--mcp` (serve the MCP endpoint), `--host`, `--api-port`, `--db`
- `import`: `--from`, `--to` (YYYY-MM-DD), `--dry-run`, `--force`, `--purge`, `--verbose`, `--yes`, `--pricing-mode` (`auto`, `calculate`, `display`), `--db`
- `export`: `--output` (required), `--from`, `--to`, `--from-files` (redacted with `AI_OBSERVER_REDACT`: `exporter.Options.Redactor`), `--zip`, `--dry-run`, `--verbose`, `--yes`, `--db`
- `delete`: `--from`, `--to` (required), `--service`, `--yes`, `--db`
- `backup`: `--output` (required, must not exist), `--db`
- `restore`: `--replace`, `--map-path OLD=NEW` (repeatable), `--dry-run`, `--yes`, `--db`
- `compact`: `--dry-run`, `--yes`, `--keep-original`, `--db`
- `healthcheck`: `--url` (default `/ready` on 127.0.0.1 and the API port), `--timeout` (default 5s)
- `pricing update`: `--output` (default `AI_OBSERVER_PRICING_DIR`), `--allow-large-changes`. It runs before the pricing overrides are loaded and merges into the files already in the output directory (or the built-in ones)

Flags override the environment variables they correspond to. The database can only be opened by one process: `import`, `export`, `delete`, `backup`, `restore` and `compact` fail with an explanation while a server or `watch` runs (`delete`, `export` and `backup` print the admin API's `curl` command). Flags may appear before or after positional arguments (`reorderArgs(fs, args)` in `cmd/server/main.go`; it asks the FlagSet which flags are boolean, so their next argument is not taken as a value).

**Examples:**
```bash
ai-observer                              # Start server (default)
ai-observer import claude-code           # Import Claude Code sessions
ai-observer watch all --backfill         # Watch all file-backed tools, importing existing data first
ai-observer serve --watch --backfill     # OTLP and session files in one process
ai-observer export all --output ./data   # Export to Parquet files
ai-observer delete all --from 2025-01-01 --to 2025-01-31
ai-observer backup --output ./ai-observer-backup.duckdb
ai-observer restore ./data --replace     # Load an export back, replacing its range
ai-observer compact                      # Return the free space of deleted data to the disk
ai-observer setup claude-code            # Show Claude Code setup instructions
```

## API Endpoints Reference

`docs/openapi.json` (OpenAPI 3.1, generated by `make api-types`) is the complete reference of endpoints, parameters and response types; the tables below are a summary. Query parameters are read with `handlers.newParams` (see `internal/handlers/`): invalid values answer `400` naming them, limits above an endpoint's maximum are clamped.

### Query API (Port 8080)

**Traces:**
| Endpoint | Method | Query Parameters |
|----------|--------|------------------|
| `/api/traces` | GET | `service` (or a tool name), `search`, `background` (`true`: include Codex's background traces), `from`, `to`, `limit`, `cursor` (or deprecated `offset`) |
| `/api/traces/recent` | GET | `limit` (default 50, max 100), `from`, `to` |
| `/api/traces/{traceId}` | GET | `kind` (required: `otel_trace` or `codex_operation`), `limit` (default and at most 1,000 spans), `cursor` (or deprecated `offset`) |
| `/api/traces/{traceId}/spans` | GET | same as above |

Trace list rows describe whole traces (all spans; ordered and paged by the first span in the range) and carry an `id` and `kind`; the list returns `otel_trace` rows only (Codex sessions as raw traces). `kind=codex_operation` with an operation root span ID as `id` is a detail-only view that groups the spans of one Codex operation (README "AI Observer Trace Handling").

**Metrics:**
| Endpoint | Method | Query Parameters |
|----------|--------|------------------|
| `/api/metrics` | GET | `service`, `name`, `type` (`gauge`, `sum`, `histogram`, `exponential_histogram`, `summary`), `from`, `to`, `limit`, `cursor` (or deprecated `offset`) |
| `/api/metrics/names` | GET | `service` |
| `/api/metrics/breakdown-values` | GET | `name` (required), `attribute` (required), `service` |
| `/api/metrics/attribute-keys` | GET | `name` (required), `service`: keys of the metric's distinct attribute sets (not time-filtered) that `groupBy`/filters accept |
| `/api/metrics/series` | GET | `name` (required), `service`, `from`, `to`, `interval` (integer seconds 1 to 2,678,400, default 60), `aggregate`, `groupBy` (≤ 2 attribute keys, comma-separated or repeated), `attr.<key>=<value>` (≤ 10 exact filters), `stat` (histograms: `avg` default, `sum`, `count`, `min`, `max`, `p50`, `p90`, `p95`, `p99`), `merge` (one series), `tz` (IANA zone, default UTC). Response: `series` (`datapoints` `[ms, number \| null]`), `metricType`, `stat`, `interval` used, `timeZone` |
| `/api/metrics/batch-series` | POST | Body `{from, to, interval?, timeZone?, queries: [{id, name, service?, aggregate?, groupBy?, filters?, stat?, merge?}]}` (at most 50 queries); results `{id, success, error?, code?, series?, metricType?, stat?}`, a failed query does not affect the others |

**Logs:**
| Endpoint | Method | Query Parameters |
|----------|--------|------------------|
| `/api/logs` | GET | `service`, `severity`, `traceId`, `search`, `from`, `to`, `limit`, `cursor` (or deprecated `offset`) |
| `/api/logs/levels` | GET | optional `from`, `to` (without them: all logs) |

**Sessions:**
| Endpoint | Method | Query Parameters |
|----------|--------|------------------|
| `/api/sessions` | GET | `service`, `from`, `to` (which sessions: those with logs in the range; each is shown whole), `search` (words and "quoted phrases", at most 10 terms: sessions with a matching record in the range, newest match first, with `snippet`, `matches`, `matchedAt`), `limit`, `cursor` (or deprecated `offset`). Rows carry, where known, `title` (first prompt), `repository`, `branch`, `costUsd`, `inputTokens`, `outputTokens` |
| `/api/sessions/{sessionId}/transcript` | GET | `limit` (default 500, at most 1,000 messages), `cursor`, `search`; a page of the messages of the whole session, oldest first, with `total`, `hasMore`, `nextCursor`, and `title` and `totals` over all messages. With `search`, `search.matches` lists the positions (0-based, in the whole transcript, not `message.index`) of all matching messages. Known sessions without content return an empty transcript; unknown sessions return 404 |

**Dashboards:**
| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/dashboards` | GET/POST | List all / Create new |
| `/api/dashboards/default` | GET | Get default dashboard with widgets; created with its default widgets in one transaction when there is none |
| `/api/dashboards/{id}` | GET/PUT/DELETE | CRUD by ID |
| `/api/dashboards/{id}/default` | PUT | Set as default |
| `/api/dashboards/{id}/widgets` | POST | Add widget |
| `/api/dashboards/{id}/widgets/positions` | PUT | Update widget positions |
| `/api/dashboards/{id}/widgets/{widgetId}` | PUT/DELETE | Update/delete widget. An update keeps what it omits (also `config`; a sent `config` replaces the stored one) and rejects `colSpan` over 4; overlaps are checked by the client (`widgetFitError`) |

**Other:**
- `GET /api/services` - List all services sending telemetry
- `GET /api/providers` - Supported tools (`tool`, `serviceName`, `displayName`, `sessionFiles`) from the provider table
- `GET /api/status` - Version, mode (`serve`, `watch`, `serve+watch`; `watchedTools`), endpoints (`otlpGrpcPort`, `otlpGrpcEnabled`), database size, used/free bytes, `compactRecommended`, `growthBytesPerDay`, stored telemetry and its extent, settings (size limit, admin API and backup dir, TLS, log level and format, CORS origin, allowed hosts, migration backup; never the token or key paths), pricing files, import state per tool (`handlers/status.go`, `storage/status.go`); shown by the Status page
- `GET /api/stats` - Aggregate statistics; optional `from`/`to` count only that range (the dashboard passes its time range), `services` always lists all services
- `GET /ws` - WebSocket for real-time updates
- `GET /health` - Liveness, always 200 (on both HTTP ports, no token)
- `GET /ready` - Readiness: 200 `{"status":"ok","database":"ok"}`, or 503 when the database does not answer within 2 s (on both HTTP ports, no token)
- `GET /metrics`, `GET /api/internal/metrics` - Self-metrics, Prometheus text and JSON (behind the token)
- `POST /api/admin/delete` (`{from, to, service?, scope?: all|logs|metrics|traces, dryRun?}`), `/api/admin/export` (`{name?, source?, from?, to?, zip?}`), `/api/admin/backup` (`{name?}`), `/api/admin/checkpoint` - Admin API, only with `AI_OBSERVER_ADMIN_API=true`
- `POST /mcp` - MCP endpoint for agents (opt-in, `docs/mcp.md`); stateless, so `GET`/`DELETE` answer `405`

Log, span and trace rows carry `sessionId` (a trace when all its spans with a session share one), which the dashboard links to `/sessions/{sessionId}`.

**Note:** List responses carry `total`, `hasMore` and, when there are more, `nextCursor` (pass it as `cursor`; invalid cursors and offsets over 10,000 return 400). Lists are always arrays (`[]` when empty). With `AI_OBSERVER_AUTH_TOKEN` set, requests without the token get 401, and 10 different wrong tokens from one address within a minute get 429 for 5 minutes. `from`/`to` are RFC 3339; `to` defaults to now and `from` to 24 hours before `to`. Invalid values or `from` after `to` return 400. Queries stopped by the 5-second API timeout return 504 (the frontend does not retry 503/504). `/api/stats` without a range is cached for 5 seconds (stale-while-revalidate up to a minute). Endpoints with optional `from`/`to` (`/api/stats`, `/api/logs/levels`) leave an unset side open instead of applying the 24-hour default. Time-bucketed series are capped at ~2,000 buckets, and batch series run at most 4 queries at once.

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

**Events** (logs): `user_prompt`, `api_request`, `api_error`, `tool_result`, `tool_decision`, `assistant_response` (Claude Code 2.1 with content logging: reply text in `response`; merged with its request's `api_request` usage in transcripts). `user_prompt` and `assistant_response` carry `message.uuid`, the session file entry's `uuid`; the request and tool events carry `request_id`, tool events `tool_use_id`.

### Gemini CLI

**Metrics** (mix of counters and histograms):
- Cumulative counters (series queries compute per-interval increases): `session.count`, `token.usage`, `api.request.count`, `file.operation.count`
- Histograms: `api.request.latency`, `tool.call.latency`, `agent.duration`, `startup.duration`
- Also emits `gen_ai.client.token.usage` and `gen_ai.client.operation.duration` (OTel semantic conventions)

**Derived Metrics:** `gemini_cli.cost.usage` from token usage (cumulative like the tokens; cached tokens at the cache rate only, since `input` includes them). Token types are `input`, `output`, `cache`, `thought`, `tool` for OTLP and imports alike. `<counter>.delta` names are query-time aliases of the cumulative counters above.

**Logs**: `config`, `user_prompt`, `api_request`, `api_response`, `api_error`, `tool_call`, `file_operation`, `agent.start/finish`, `conversation_finished`

### OpenAI Codex

**Services:** `codex_cli_rs`; Codex 0.167 and later run conversations in an app server, which the Codex app and the CLI share (`codex-app-server`, `originator` `Codex_Desktop` or `codex-tui`; the CLI's is a daemon that keeps the `[otel]` settings it started with), and commands in `codex-exec-server`. `codex_cli_rs` then carries only the CLI's startup telemetry. The provider table lists the two as `OtherServices`.

**Derived Metrics** (computed from `response.completed` logs of any Codex service, `otlp.ExtractCodexMetrics`):
- `codex_cli_rs.token.usage` - Tokens by type: `input` (includes `cacheRead` and `cacheWrite`), `output` (includes `reasoning`), `cacheRead`, `cacheWrite`, `reasoning` (`pricing.CodexTokenType*`, imports use the same); `tool_token_count` is Codex's total and is not read
- `codex_cli_rs.cost.usage` - Cost in USD

These are per-event values, not cumulative series, with `model`, `originator` and `session.id` (OTLP: from the event's `conversation.id`; imports: `session_meta.id`), which usage deduplication compares. Codex also sends its own metrics (`codex.turn.token_usage` per turn, with `token_type` `total` = input + output; don't add it to the derived tokens). Migration 10 dropped stored `tool` points, renamed imported `cache_read`/`cache_creation` and derived the usage of stored app server `codex.sse_event` logs.

**Events** (logs): `conversation_starts`, `api_request`, `user_prompt`, `tool_decision`, `tool_result` (the tool events carry `call_id`, the session file's `tool.call_id`; prompts carry no ID)

**Traces**: Codex 0.167 and later send a trace per turn (root `turn/start`); most of the app and exec servers' traces are internal requests outside a conversation (`fs.*`, `getAuthStatus`, `thread/read`), which trace lists leave out unless asked for (`storage.TraceFilter.Background`: traces of `tools.BackgroundTraceServices()` in which no span has a session ID, and traces with a parentless span named in `Provider.BackgroundRoots`: the approval reviews, root `decide_request`, which run in sessions of their own, and the single spans `codex_cli_rs` still records at startup; `codex_cli_rs` is not a background service since earlier versions send their sessions as it). The app server's spans per streamed response event, session file write and file status read (86% of its spans) are not stored (`NoiseSpans` in the provider table). Earlier versions use a single trace per session with all spans nested; AI Observer treats first-level child spans as virtual traces (`codex_operation` kind, `codex_cli_rs` only) for usability.

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

Override with `AI_OBSERVER_DATABASE_PATH` environment variable. The image exposes 8080, 4318 and 4317 (publish them on `127.0.0.1`) and its `HEALTHCHECK` runs `ai-observer healthcheck` (`GET /ready`), since the distroless image has no shell, curl or wget.

## CI/CD

GitHub Actions (`.github/workflows/ci.yml`) triggers:

| Trigger | Actions |
|---------|---------|
| Push to main / PR to main / tag | `gofmt` check, `go vet`, golangci-lint (built with the job's Go), ESLint, then tests (Go + frontend; Go tests include the API type drift checks, so commit `make api-types` output) |
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
