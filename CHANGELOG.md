# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.8.0] - 2026-11-21

### Upgrade notes

- **The database is backed up and migrated on the first start.** Before a command opens a database that holds data and needs migrations, AI Observer copies it to `<database>.pre-migration-v<N>` (`N` is its schema version; only the latest such copy is kept; 1.4 seconds for 717 MiB). This needs free disk space of about the database's size: without it, the command stops with an explanation and the database is not changed. `AI_OBSERVER_SKIP_MIGRATION_BACKUP=true` migrates without the copy. Version 0.8.0 upgrades the database from schema 8 to 9 in one transaction. It repairs trace and span IDs, corrects Codex and Claude Code usage, and adds columns for histogram baselines, message keys and Codex usage matches. Databases created by development builds complete any pending changes and record the same schema version. To go back, stop AI Observer and copy the backup over the database file.
- **OTLP/gRPC listens on port 4317.** `serve` now also receives OTLP over gRPC and, as for port 4318, fails to start when the port is taken: set `AI_OBSERVER_OTLP_GRPC_PORT` (or `--otlp-grpc-port`) to another port, or to `off`. Docker users publish it with `-p 127.0.0.1:4317:4317`.
- **Token usage and cost recorded both over OTLP and from session files are counted once.** A Claude Code, Codex or Gemini CLI session that was sent over OTLP and also imported or watched counted each request twice; now each request counts once, from OTLP when OTLP recorded it, from the file otherwise. Claude Code requests are matched by their request ID; Codex and Gemini CLI send no shared ID, so a file response counts as recorded when an OTLP response of the same session has the same token counts (Codex within 30 seconds, Gemini CLI within an hour). Migration 9 removes the duplicated Claude Code file usage from stored data; for Codex and Gemini CLI run `ai-observer import codex --force` and `ai-observer import gemini --force` once. Totals of affected days drop.
- **Re-import session files once with `--force`** (stop `watch` or `serve --watch` first, since only one process can open the database; `watch` does not repair records it stored before, it only adds what is written after the upgrade):
  - `ai-observer import claude-code --force` adds the typed prompts that Claude Code imports and `watch` have dropped since v0.3.0 (see Fixed).
  - `ai-observer import codex --force` adds the tool calls missing from imported Codex sessions, stores tool outputs once, and removes Codex usage counted twice. A plain `import codex` imports the compressed sessions that were never imported, but keeps the old records of the others.
  - `ai-observer import gemini --force` removes Gemini CLI usage counted twice and adds the error text of failed tool calls.
  - Importing all Codex sessions can take minutes and grow the database by gigabytes: 714 files (about 4.4 GB of session data) took 285 seconds and gave a 2.87 GB database. DuckDB caches up to 80% of memory meanwhile (about 3 GB measured); `AI_OBSERVER_DB_MEMORY_LIMIT=512MB` kept the import below 0.9 GB.
- **Exports are format 2** (session file paths and `import_state.parquet`, see Changed). Earlier AI Observer versions refuse to restore them. Exports of earlier versions still restore; their records carry no file paths, so rebuild those sessions with `ai-observer import <tool> --purge` instead of importing them again.
- **Histogram series show the average.** `/api/metrics/series`, batch series and dashboard widgets of histogram metrics (latencies, durations, sizes) return the average per bucket instead of the sum of the values; `stat=sum` (Statistic "Sum" in a widget) returns the old values. Buckets without points are `null` instead of `0` for gauges and for histogram averages, minimums, maximums and percentiles; sums and counts stay `0`.
- **Stricter API parameters.** `interval` of `/api/metrics/series` and of batch series must be whole seconds from 1 to 2,678,400 (31 days): other values, such as `1 hour`, answer `400` instead of falling back to 60 seconds. Invalid `limit` (not a positive number), negative `offset`, booleans (anything but `true` or `false`), `kind` and `type` values answer `400` with a message naming them instead of falling back to defaults; limits above an endpoint's maximum are still reduced to it.
- **Empty lists are `[]` instead of `null`:** `sessions` of `/api/sessions`, `metrics` of `/api/metrics`, `names` of `/api/metrics/names`, `values` of `/api/metrics/breakdown-values`, `logs` of `/api/logs`, `services` of `/api/services` and `/api/stats`, `spans` of a trace page after the last span, and the `datapoints` of a series without points.
- **OTLP/HTTP requests with an unsupported `Content-Encoding`** (such as `zstd`) are answered `415` with `Accept-Encoding: gzip, deflate, identity`.
- **Trace lists describe each trace by all of its spans** (see Changed), so rows of traces that began before the selected range change.
- **Transcripts and message counts change** for sessions received over OTLP and read from session files, which show each message once, for Claude Code sessions over OTLP, whose replies (`assistant_response`) now appear, and for Gemini CLI sessions over OTLP, which showed each response twice (see Fixed and Added).
- **Dashboard links carry their filters.** The Logs, Traces, Sessions and Metrics pages and the dashboard keep their filters and time range in the page address. A fixed date range is no longer remembered between visits (it is part of the link instead); relative ranges still are.
- **`GET /api/dashboards/default` creates the default dashboard** when there is none and answers `200`; it answered `404` before, and the browser created the dashboard.
- **Codex token types change.** On its first start AI Observer rewrites the stored `codex_cli_rs.token.usage` points once: it deletes the `tool` points (Codex's total, see Fixed) and renames the imported `cache_read` and `cache_creation` types to `cacheRead` and `cacheWrite`, as OTLP names them. Dashboard widgets and queries that select these types need the new names.
- **Codex 0.167 and later:** after changing `[otel]` in `~/.codex/config.toml`, quit Codex and stop its background app server (`pkill -f app-server-daemon`): it keeps the settings it started with, so the CLI's conversations are otherwise not exported. See README "AI Tool Setup".
- **The trace list leaves out Codex's background traces** (see Changed); `/api/traces` lists them with `background=true`.
- **`/api/traces/{id}/spans` is paged** (see Changed): it returns at most 1,000 spans; API clients that need more follow `nextCursor`.
- **Codex's per-event spans are no longer stored** (see Changed); `AI_OBSERVER_STORE_ALL_SPANS=true` keeps them.
- **`/api/sessions/{id}/transcript` is paged** (see Changed): it returns at most 1,000 messages, 500 by default; API clients that need more follow `nextCursor`.

### Added

- **OTLP/gRPC** on port 4317 (`AI_OBSERVER_OTLP_GRPC_PORT`, `0` or `off` turns it off): traces, metrics and logs with gzip, token authentication (`authorization: Bearer <token>` metadata), TLS and the same processing as OTLP/HTTP. `ai-observer setup` shows the gRPC settings of each tool, and the OpenCode plugin's default (gRPC on 4317) now works.
- **`ai-observer serve --watch [tool] [--backfill]`** receives OTLP and reads session files in one process, with one database. The Status page shows the mode `serve+watch` and the watched tools. Usage and messages of sessions seen both ways are counted once (Claude Code, Codex and Gemini CLI).
- **Metric breakdowns and filters.** Series can be split by up to two attributes (`groupBy`, such as `model` or `model,repository`), filtered by exact attribute values (`attr.<key>=<value>`, batch `filters`) and merged into one series (`merge`). The Metrics page has a Breakdown selector offering the catalog's breakdowns and the attributes the metric's points have (`GET /api/metrics/attribute-keys`), and chart widgets break down on the server. Bucketed queries return at most 100 series.
- **Histogram statistics.** Histogram metrics are charted as their average by default, or as sum, count, minimum, maximum, p50, p90, p95 or p99 (`stat`); percentiles are estimated from the histogram buckets by linear interpolation (32 ms for 53,000 points). Statistic selectors on the Metrics page and in the widget panel; averages and percentiles are never stacked.
- **Buckets follow a time zone** (`tz`, batch `timeZone`): the dashboard sends the browser's, so daily bars are local days, also across daylight saving changes. The MCP tools `query_metrics` and `usage_summary` (by day) take `timezone`, by default the server's.
- A failed query of a batch series request reports a specific message and a `code` (`invalid_group_by`, `invalid_stat`, `too_many_series`, `timeout`, …), plus the `metricType` and `stat` the server computed; the other queries are unaffected.
- **`ai-observer compact`** rewrites the database into a new file without the space of deleted data, which DuckDB reuses but never returns to the disk. It checks the copy (tables, row counts, indexes, schema version) before it replaces the database: a 751 MB database became 575 MB in 4.6 seconds, and 398 MB after two days had been deleted.
- **`ai-observer backup --output <file>`** writes a consistent, compacted and checked copy of the database.
- **`ai-observer restore <export directory or .zip>`** loads an export back, in one transaction. It refuses to duplicate records of the export's services in its time range unless `--replace` deletes them first, and computes session IDs for exports of earlier versions. Records keep the session file they were imported from, and the import state of those files is restored, so importing or watching the same files afterwards stores nothing twice: unchanged files are skipped, grown files replace their records, and `watch` continues where the exported database stopped. `--map-path OLD=NEW` (repeatable) rewrites the paths when the files moved, such as to another machine or home directory. Text is redacted again with `AI_OBSERVER_REDACT` (restoring 3.5 million records took 10 instead of 5.5 seconds). Exports now also contain `metric_baselines.parquet`, `import_state.parquet`, `dashboards.json` and `manifest.json`, and keep the local paths of session files.
- **Compressed Codex sessions are imported and watched.** Codex compresses session files older than 7 days to `.jsonl.zst`, which no import had read (556 of 713 files on a developer's machine). A compressed file counts as the same session file as the `.jsonl` it replaced: compressing an imported session is no change, and a resumed session continues without duplicates.
- **`AI_OBSERVER_MAX_DB_SIZE`** (off by default, a size such as `20GB`): `serve` and `watch` delete the oldest days while the stored data exceeds it (measured as used space, not file size), never the last 24 hours.
- **Admin API** (`AI_OBSERVER_ADMIN_API=true`, off by default): `POST /api/admin/delete` (with a dry run), `/api/admin/export`, `/api/admin/backup` and `/api/admin/checkpoint` while the server runs, one at a time. Backups and exports go to `AI_OBSERVER_BACKUP_DIR` (default: `backups` next to the database). The Status page gets a Maintenance section with these operations.
- **Readiness and self-monitoring.** `GET /ready` on the API and OTLP ports answers `503` when the database does not answer within 2 seconds (`/health` stays a liveness check). `GET /metrics` (Prometheus text format) and `GET /api/internal/metrics` (JSON) on the API port expose AI Observer's own metrics: OTLP requests by transport and result, records stored, rejected and deduplicated, noise spans dropped, write transaction times and lock waits, API request durations and timeouts, WebSocket clients, watcher files, retention deletions and failed authentication.
- **`ai-observer healthcheck`** exits 0 when the local server is ready. The Docker image runs it as its `HEALTHCHECK` and exposes port 4317.
- **TLS:** `AI_OBSERVER_TLS_CERT` and `AI_OBSERVER_TLS_KEY` serve HTTPS on the API port (HTTP/1.1, `wss://`), the OTLP/HTTP port (HTTP/2 via ALPN) and gRPC over TLS.
- OTLP responses report rejected records as `partial_success`: spans with missing or all-zero trace or span IDs (stored before, where they collided), number data points without a value (stored as 0 before) and histogram points whose bucket counts do not match their bounds; metrics without data points are reported as a warning.
- OTLP/HTTP accepts `deflate` and `identity` request encodings besides gzip; the 10 MB limit applies to the decompressed body.
- Flags `--host`, `--api-port`, `--otlp-port`, `--otlp-grpc-port` and `--db` for `serve` (`watch`: `--host`, `--api-port`, `--db`; `import`, `export`, `delete`, `backup`, `restore`, `compact`: `--db`); they override the environment variables.
- `AI_OBSERVER_LOG_FORMAT=json` writes JSON logs (all commands).
- The server logs the database size, its free space and growth per day at startup, with a warning when a database of 1 GiB or more grows without bound.
- Claude Code's `assistant_response` events (reply text, with content logging) appear in transcripts, together with the usage of their response.
- **MCP:** `list_sessions` and `search_conversations` return each session's title, repository, branch, cost and tokens; `get_transcript` returns the title and usage totals of the whole session and takes `search` (matching messages carry their `position`, usable as `cursor`); `query_metrics` takes `group_by`, `filters`, `stat` and `timezone`. The MCP Apps sessions view shows titles and costs.
- `docs/openapi.json`: an OpenAPI 3.1 description of the REST API, the OTLP/HTTP endpoints and the WebSocket message, generated from the Go types and the route table.

- **Search sessions.** The Sessions page searches conversations by words and "quoted phrases" (`GET /api/sessions?search=`, the search the MCP tool `search_conversations` uses): results show the text around the match with the terms highlighted, the number of matching records and the time of the newest one.
- **Session rows and transcripts show what a session was about and what it cost.** Rows show the first prompt as title, the repository and branch of imported sessions, and cost and tokens where the session's events carry them (Claude Code over OTLP, Gemini CLI tokens; not Codex, imported Claude Code or GitHub Copilot, whose usage is not recorded per session). The transcript header shows the first prompt and totals for the whole session: cost, tokens, prompts, responses, tool calls and tool errors.
- **Search inside a transcript.** It covers the whole session, also messages that are not loaded yet (`search` on `/api/sessions/{id}/transcript` returns the positions of all matches); Previous and Next load further messages as needed, and the terms are highlighted.
- **Span details in full.** The trace page shows attribute values completely (wrapped, JSON formatted, very long values behind "Show all") instead of one cut-off line, plus the span's resource, scope, kind, start offset, event attributes and links.
- A time axis above the trace waterfall, and a span filter (names, IDs, attributes) with match count, "Errors only" and expand/collapse all.
- **Links between logs, traces and sessions.** Logs, spans and traces carry the `sessionId` of their session; log and trace rows link to the transcript, a log's trace ID to the trace, and a trace to its logs ("View logs") and its session. Every trace row has an "Open" link. Back buttons of trace and session pages return to the list with its filters.
- **Edit dashboard widgets.** In edit mode a widget's pencil button changes its title, metric, service, breakdown, statistic (histograms), chart mode and size.
- **Status page** (System → Status, `GET /api/status`): version, mode (`serve`, `watch` or `serve+watch`, with the watched tools) and uptime, endpoints (OTLP/HTTP, OTLP/gRPC, API), database size, its free space, growth per day and the days until the size limit, with hints to compact or set a retention, stored telemetry with its oldest and newest record, retention, size limit, redaction, authentication, TLS, MCP, admin API, log and dashboard origin settings, the age of the pricing data and the imported session files per tool. With the admin API on, its Maintenance section deletes a range, writes a backup and checkpoints the database. The sidebar shows the server version.
- A "Data points" tab on the Metrics page lists the stored points of the selected metric.
- Copy buttons for IDs, attribute values, log bodies, messages, tool input and output, and code blocks.
- The log level filter offers the levels that occur in the selected range, with their counts.
- The Sessions list refreshes its first page as sessions gain messages.
- A page for unknown addresses, a page title per page ("Logs · AI Observer") and an icon.
- **Charts and tables for MCP Apps hosts.** In hosts that render [MCP Apps](https://modelcontextprotocol.io/docs/extensions/apps), such as the Codex app, VS Code with GitHub Copilot or Cursor (not Claude Desktop, which shows no views of a local AI Observer), the results of `usage_summary`, `query_metrics`, `get_trace` and `list_sessions` appear as a view in the conversation: cost and token bars by tool, model, repository or day, metric charts, the trace waterfall and the session table, with switches for grouping and paging. The view is one page built into the binary and gets its data through the host; its `openai/widgetDescription` asks OpenAI hosts' models not to repeat the values shown in it. Clients without MCP Apps support, such as Claude Code, Codex CLI and Gemini CLI, get the same results as before. See `docs/mcp.md`.

- **Codex app and Codex 0.167 and later.** These send conversations as `codex-app-server` and commands as `codex-exec-server`, besides `codex_cli_rs`. All three now count as Codex: token usage and cost are derived from their `response.completed` events, and the tool name `codex` covers all three in the API's `service` filter, the MCP tools, `usage_summary`, `export` and `delete --service` (a service name still selects only that service). The derived metrics carry `originator`, which tells the Codex app (`Codex_Desktop`) from the CLI (`codex-tui`). On its first start AI Observer derives the usage of the `codex.sse_event` logs it stored for `codex-app-server` before and deletes those logs.
- `AI_OBSERVER_DB_MEMORY_LIMIT` and `AI_OBSERVER_DB_THREADS` set DuckDB's memory limit (a size such as `2GB`) and number of query threads; without them DuckDB uses up to 80% of the machine's memory and all CPU cores. On a 746 MB database, the server used 2.4 GB after ten list, search and transcript requests without a limit, 1.4 GB with `1GB` and 1.05 GB with `512MB`; trace search took twice as long with a limit (1.4 instead of 0.7 seconds). The limit covers DuckDB only, so the process uses more than the value set.
- The dashboard's scripts, styles and API responses are compressed for clients that accept gzip (the main script 336 → 104 KB, a page of log search results 387 → 71 KB), and the hashed asset files are cached by the browser instead of being downloaded on every load.
- Startup logs the dashboard's URL.
- Open session transcripts show new messages as they arrive.
- Codex's cache writes (`cache_write_token_count`, `cache_write_input_tokens` in session files) as token type `cacheWrite`, priced at the model's cache write price.
- `usage_summary` lists `unpriced_models`: models with tokens but no price, whose tokens add no cost, such as `codex-auto-review` (Codex's automatic approval reviewer, which starts its own conversations). The MCP view shows them below the chart.
- Codex's own turn metrics (`codex.turn.token_usage`, `codex.conversation.turn.count`, `codex.turn.e2e_duration_ms`, `codex.turn.ttft.duration_ms`) in the metric catalog, display names for Codex's new services, and the exec server's `exec_server_*` metrics grouped with Codex. The service filters of the dashboard pages and the add-widget panel offer **OpenAI Codex (all services)**, which shows the Codex app and the CLI together.
- `ai-observer setup codex` also configures Codex's metrics and explains how to restart its app server.

### Changed

- **Usage of a request is counted once** when it arrives both over OTLP and from its session file (Claude Code, Codex and Gemini CLI; see Upgrade notes). The rule is applied whenever either side is stored, in any order, and only where the tool's session files were read; Codex token and cost metrics and imported Gemini CLI usage now carry `session.id` for it. Imported Gemini CLI replies also carry thought and tool token counts, and tool calls their call IDs.
- **Codex tool output is stored once**, in the tool result's `tool.output` attribute (at most 16 KiB, start and end kept; the full size in `tool_result_size_bytes`), with a one-line summary as the log body (`exec_command: exit 1, 12.3 KB output`). Transcripts show the output as before. With the fix below, a full import of 157 plain Codex session files gave a 246 instead of 376 MB database.
- Large Codex session files (over 64 MiB) are imported in batches of 10,000 records, still in one transaction per file: the import's Go memory for the largest sessions dropped from 1.1 GB to 150 MB.
- **Exports keep the session file of each record** (`SourceFile`) and include the import state of those files (`import_state.parquet`, export format 2), so a restore can be followed by imports without duplicates (see Added). Exports therefore contain local file paths. The derived message key of logs is not exported; restore computes it.
- **An export writes all of its files from one snapshot of the database** (CLI and `POST /api/admin/export`), so files written while the server stores telemetry agree with each other.
- **Trace lists describe each trace by all of its spans** (`/api/traces`, `/api/traces/recent`, `list_traces`), as the trace page does: a trace that began before the range shows its real start, duration, span count and status (3 of 69 listed traces of a day were shown cut off). They are still ordered by the trace's first span in the range. A week's list takes 86 instead of 104 ms, with background traces 10 instead of 52 ms.
- **Histogram series default to the average**, gauges have no value in buckets without points, and `interval` is integer seconds up to 31 days (see Upgrade notes). Intervals over a day are honoured: ranges of 60 days to a year are charted in 2-day to 2-week buckets instead of daily ones.
- **Deletions are followed by a checkpoint** (`delete`, retention, the size limit, the admin API), so new data reuses the freed space: after deleting 1.6 million spans and storing 600,000, the file was 605 instead of 806 MB. `delete` reports the file size and its free space afterwards and suggests `compact` when worthwhile.
- `delete`, `export` and `backup` print the admin API's `curl` command when a server has the database open; `restore` and `compact` say to stop it.
- Transcripts are cached for the REST API and MCP together, up to 8 sessions or about 128 MB, and concurrent requests for one session share one build. `/api/stats` no longer makes callers wait behind one slow count and serves counts up to a minute old while it refreshes.
- `ai-observer --help` lists all settings, grouped, and the commands' options use the same wording everywhere.
- Invalid `AI_OBSERVER_HOST`, `AI_OBSERVER_FRONTEND_URL`, `AI_OBSERVER_ALLOWED_HOSTS` entries and `AI_OBSERVER_LOG_LEVEL` values are logged and replaced by their defaults, like the other settings.
- `import`, `export` and `delete` log as text (or JSON with `AI_OBSERVER_LOG_FORMAT`), like the servers; they logged JSON.
- Metric value widgets filtered to a breakdown value are filtered by the server.
- The usage note of the MCP tools says what is counted once.
- Logs stored before this version get the message key that session lists use in the background after `serve` or `watch` starts, 5,000 logs per transaction (42,000 logs took 0.8 seconds, at most 125 ms per transaction); session lists are correct meanwhile.
- The dashboard's and the MCP view's API types are generated from the backend's Go types (`make api-types`); tests fail when the generated files, the route table or real responses differ from them.
- **The dashboard loads its data with TanStack Query.** Requests for the same data are shared and cached (the service list was fetched by every page), a list keeps showing its rows while the next page or a changed filter loads, and failed requests are retried with backoff (not `4xx`, `503`, `504` or timeouts).
- **Long transcripts render only the messages near the view**, so sessions with thousands of messages scroll smoothly; expanded tool calls stay expanded.
- **The default dashboard is created by the server**, so two browser tabs opened at once no longer create two.
- The Metrics page shows "Live" or "Paused (fixed range)" instead of a counter of updates since the page was opened.
- "Hide metadata-only" in transcripts is now the switch "Show responses without content".
- Transcripts show Claude Code's cache tokens and Gemini CLI's token counts.
- Dashboard count widgets say that they cover the selected range and use thousands separators.
- **The trace list leaves out Codex's background traces.** Codex's app and exec servers record internal requests (file, settings and account reads) as hundreds of traces an hour. Traces of these services in which no span belongs to a conversation are left out of the trace list, the dashboard's recent traces and `list_traces`, unless **Background requests** (`background=true`, `include_background`) is on. Turns (`turn/start`) are listed.
- **Codex's per-event spans are no longer stored.** Codex's app server records a span for every streamed response event, every write of the session file and every file status read (`receiving`, `handle_responses`, `append_items`, `persist_rollout_items`, `realtime_conversation.running_state`, `fs.get_metadata`). In two days of use these were 86% of all stored spans (2.7 of 3.2 million), and a single long turn had 145,000 spans. They are now left out when received over OTLP, except those that failed and those that are the parent of a stored span (a `handle_responses` span above a tool call), so no span loses its parent. `AI_OBSERVER_STORE_ALL_SPANS=true` stores them as before. Spans stored earlier are kept.
- **Trace details load a page of spans at a time.** `/api/traces/{id}/spans` returns at most 1,000 spans, oldest first, with `trace` (start, duration, span count and status of the whole trace), `total`, `hasMore` and `nextCursor` (pass it as `cursor`); it returned every span before, 116 MB for a Codex turn of 145,000 spans. The trace page shows the first 1,000 spans and loads more on request, the expanded rows of the trace list link to it, and the browser skips laying out and painting waterfall rows outside the view. `get_trace` reads the first 1,000 spans of a trace, by start time.
- **Codex's approval reviews and CLI startup spans are background traces.** Codex's automatic approval review records a trace per reviewed command (root `decide_request`, about 550 spans, in a conversation of its own), and the CLI records single spans at startup (`auth`, `fs.get_metadata`, `fs.read_file`, `serve_inner`, `transport_worker`). They were nine of ten listed traces: 1,107 of 1,252 in three days of use. The trace list, the dashboard's recent traces and `list_traces` leave them out unless **Background requests** is on.
- **Session transcripts load a page of messages at a time.** `/api/sessions/{id}/transcript` returns 500 messages by default (`limit`, at most 1,000), oldest first, with `total`, `hasMore` and `nextCursor` (pass it as `cursor`); it returned every message before, 19 MB for a session of 7,665 messages. Paging through a session reuses the assembled transcript until new telemetry is stored. The transcript page shows the first 500 and loads more on request; the role filters and their counts apply to the loaded messages.
- `get_trace` returns span start times with milliseconds, so spans in the same second keep their order and offsets.
- The MCP endpoint's `resources/list` leaves out the view's `ui://` resource, so agents that browse resources do not read it into their context.
- Refreshed the bundled Claude, OpenAI, Gemini and GitHub Copilot prices on November 21. New entries include Claude Fable 5.2, GPT-6.2 Astra and Sol, Gemini 4 Carbon, Hydrogen and Neon, and MAI-Code-2. The refresh preserves the published Gemini image-output rates and the long-context tier for the retired Computer Use Preview.

### Fixed

- **Claude Code imports and `watch` dropped every typed prompt** since transcripts were first imported (v0.3.0): a prompt stored as plain text could not be read, so imported transcripts lacked the user's prompts (1 of 28 in a measured session) and sessions their title. Run `ai-observer import claude-code --force` once to add them. Imported messages now also carry the entry's UUID, request ID and tool call ID.
- **A session received over OTLP and read from its session file showed its messages twice**, and its message count was doubled. Each message is now shown once, filled in with what the other records add, and counted once in session lists: Claude Code's records are matched by entry UUID, request ID and tool call ID, Codex's by tool call ID and by prompt time and text, Gemini CLI's by prompt text (or length) and time, by the token counts of a response and by tool name and arguments. Session lists use the transcripts' own matching, so their message counts, usage and titles equal the transcripts'; a list page holding such sessions takes about 120 ms (60 ms without them).
- **Gemini CLI sessions over OTLP showed each response twice** (its `api_response` event and the operation details record) and counted its tokens twice, in transcripts and session lists. Prompts whose text Gemini CLI did not record no longer become session titles.
- **Codex session titles showed the context Codex sends before the first prompt** (`# AGENTS.md instructions for …`, `<environment_context>`) instead of what the user typed. Context a tool injects as a user message (text starting with a tag, such as Codex's context blocks and image markers or Claude Code's `<command-name>`) no longer becomes a title; the first typed prompt does.
- **Codex imports and `watch` stored only some tool calls.** Since Codex 0.144, commands, patches, MCP tools, image views and web searches run from code mode's `exec` cell are recorded only as completed items, and freeform tool calls (`exec`, `apply_patch`), web searches and tool searches were skipped as well. Every tool call is now stored with its input, output, exit code, duration and success, and carries the call ID Codex sends over OTLP, so a session received both ways shows each call once. Run `ai-observer import codex --force` once (see Upgrade notes). The `write_stdin` and `curr_time` calls of code mode are not in the session files and appear only over OTLP.
- After a full `import gemini`, `watch` missed the tool calls and token counts Gemini CLI adds to the last reply afterwards.
- Gemini CLI session files whose message content or tool results are a part or a list of parts are imported; tool outputs that are not text no longer fail the file, and failed or cancelled tool calls show their error text (`import gemini --force` updates stored sessions).
- **Log and trace searches found text with quotes, backslashes, line breaks, `<`, `>` or `&` only in log bodies, not in attribute values** such as tool output (`/api/logs?search=`, `/api/traces?search=`, MCP `search_logs` and `list_traces`). Such searches take 1.4 to 2.4 times as long as before; searches for plain words are unchanged.
- The "database is in use" message names all commands that need the database to themselves (`import`, `export`, `delete`, `backup`, `restore`, `compact`) and the admin API.
- Histogram counts and percentiles no longer leave out a point per series after retention, the size limit or `delete` removed the points before it: migration 9 keeps the counts and buckets in the metric baselines.
- Metric value widgets broken down by an attribute other than type or model (such as `gen_ai.token.type` or `repository`) always showed "—".
- Exports of a tool include all of its services (Codex exports missed `codex-app-server` and `codex-exec-server`).
- `export` no longer changes the working directory of the process.
- `Content-Encoding` values that merely contained "gzip" were decoded as gzip.
- `ai-observer import`, `export`, `delete`, `watch` and `setup` with `--help` printed "flag: help requested" and exited with status 1.
- **"N new logs" and "N new traces" did nothing on the first page.** The button now loads the new records, and its count respects the filters (the log count included all logs).
- **Relative time ranges stood still.** "Last 7 days" on the Logs, Traces and Sessions pages ended when the page was opened; every request now ends at the current time.
- **A page error stayed on every page.** After one page failed to render, all other pages showed "Something went wrong" until "Try again" was clicked.
- **Failed requests looked like empty lists.** Pages showed a brief notification and then "No traces found"; they now show the error with a Retry button. With `AI_OBSERVER_AUTH_TOKEN` set and no token given, the dashboard says how to open it with the token instead of "Failed to fetch".
- **Updating a dashboard widget without a `config` cleared its configuration** (`PUT /api/dashboards/{id}/widgets/{widgetId}`); widgets wider than the 4-column grid are rejected.
- The Metrics page lost its metric and service on reload; they are part of the address now.
- "Load more spans" could add spans of another trace when the trace was changed while it loaded.
- An expanded log row stayed expanded on the next page.
- The current page was not highlighted in the sidebar, and on phones the sidebar stayed open after choosing a page.
- Dashboard widgets showed "—" or "Waiting for incoming telemetry data…" when their request failed; they show the error with Retry.
- A link to a deleted dashboard silently opened the default one; it now says that the dashboard was not found.
- Filter bars overflowed on narrow screens; trace rows could not be opened with the keyboard; filter controls had no labels for screen readers.
- **Transcripts of long Codex sessions could not be opened** (`504` after 5 seconds, also in `get_transcript`): building one read every span of every trace of the session, 2.4 million for a two-day session. Only traces with GenAI spans (`gen_ai.operation.name`), whose spans become messages, are read whole now; of other traces only the spans that carry the session ID. A session's end time no longer takes spans without a session ID into account.
- **The session list counted every log record of a session as a message**, 10,005 for a Codex session whose transcript has 7,665. It now counts the events that become transcript messages, and for sessions whose conversation is on spans (GitHub Copilot) their GenAI spans. Sessions without any conversation event, such as Codex's approval reviews, show 0 messages instead of their number of log records.
- **Claude Code transcripts over OTLP showed every tool call twice.** Claude Code describes a tool call as a `tool_decision` event, a `tool_result` event and a `claude_code.tool` span; the span was added as a second tool call (252 extra messages in a session of 771). Span and event are now recognized as one call by their tool call ID (`tool_use_id`, `gen_ai.tool.call.id`), and the call shows the span's duration.
- `ai-observer --help` listed GitHub Copilot's and OpenCode's variables among AI Observer's own; they are now a separate list of variables to set for those tools.
- **Live pages did not refresh while telemetry kept arriving.** Each notification restarted the half-second wait before a refresh, so pages stayed unchanged until ingestion paused for that long. They now refresh at the latest 2 seconds after the first notification.
- **The transcript page could show another session's transcript** when sessions were switched while one was loading; the earlier request is now cancelled. A session that cannot be loaded in time says so instead of "Failed to load session transcript".
- **Codex token totals over OTLP were about twice too high.** Codex sends its total tokens (input plus output) as `tool_token_count`, which AI Observer stored as a token type `tool` and added to `usage_summary`'s `total_tokens`. Costs were not affected.
- **Imported Codex cached input counted twice in `total_tokens`.** Imports named the type `cache_read`, which `usage_summary` did not recognize as part of the input.
- **Trace and span IDs sent as OTLP/JSON were stored wrong.** OTLP/JSON encodes them as hex, which AI Observer read as base64, so it stored other, longer IDs than the exporter sent (48 instead of 32 hex characters for trace IDs, 24 instead of 16 for span IDs). This affected the HTTP exporters of Gemini CLI and other tools built on the OpenTelemetry JavaScript exporters; protobuf exports were not affected. They are now stored as sent, and on its first start AI Observer repairs the stored IDs of spans, span links and logs once. Links and bookmarks to such traces by their old IDs stop working.
- Evicting or replacing a cached transcript releases its response even while the runtime still holds its stopped expiration timer.
- Included the log row component in the repository so a clean checkout can build the dashboard.

### Security

- **Failed token attempts are limited.** 10 different wrong tokens from one address within a minute block it for 5 minutes on all ports (`429` with `Retry-After`; gRPC: `RESOURCE_EXHAUSTED`), even with the right token. Requests without a token and repeats of the same wrong one (an exporter with an old token) do not count.
- The server warns at startup when it listens beyond localhost with a token but without TLS, since the token, prompts and code then travel in clear text.
- **`export --from-files` wrote session text unredacted**, so secrets in prompts and tool output ended up in the Parquet files. It now redacts with `AI_OBSERVER_REDACT` like every other path; re-create exports made with `--from-files` before sharing them.

## [0.7.0] - 2026-11-06

### Upgrade notes

- **Back up the database before upgrading.** On its first start, 0.7.0 updates the database once: it adds and fills session ID and source file columns, removes duplicate spans, drops unused indexes, rebuilds Gemini costs from the stored token counters, renames imported Gemini token types and removes stored Gemini `.delta` points. This can take a while on large databases. Opening the updated database with 0.6.x is not tested; restore the backup to go back.
- **The binary listens on `127.0.0.1`** instead of all interfaces. If other machines or containers send telemetry or open the dashboard, set `AI_OBSERVER_HOST=0.0.0.0`, and preferably `AI_OBSERVER_AUTH_TOKEN`. The Docker image still listens on all interfaces.
- **Unknown host names are rejected.** If you open the dashboard through a host name other than `localhost` or an IP address, for example behind a reverse proxy, add it to `AI_OBSERVER_ALLOWED_HOSTS`.
- **Secrets are redacted before they are stored**, by default. Set `AI_OBSERVER_REDACT=off` to store telemetry as received.
- **Startup fails when the OTLP port is in use**, instead of running without OTLP ingestion, and **`import` exits with status 1** when tools or files could not be imported.
- **For API clients:** list endpoints page with `cursor`/`nextCursor`; `offset` still works up to 10,000 rows but is deprecated. `/api/stats` no longer returns `codexOperationCount` and `rawTraceCount`. WebSocket messages announce stored records instead of carrying them. Service display names follow `/api/providers` ("OpenAI Codex CLI", "GitHub Copilot (VS Code)"). The API port no longer accepts HTTP/2 cleartext (h2c); the OTLP port still does.

### Added

- Added prices for new models: Claude Fable 5.1, Mythos 5.1, Opus 5.5, Opus 5, Sonnet 5.5, Sonnet 5 and Haiku 5.5; OpenAI gpt-6-astra, gpt-6.1-sol/terra, gpt-6-sol/luna, `gpt-6-cyber`, `gpt-6-rosalind`, `gpt-5.6`, gpt-5.6-sol/terra/luna, the other cyber models, chat-latest and gpt-realtime-2.1; Gemini 4 Argon, 3.8 Flash, `gemini-3.7-flash`, 3.6 Flash, `gemini-3.5-flash`, 3.5 Flash-Lite, Omni Flash and `gemini-omni-1.2-flash`; and the corresponding GitHub Copilot models.
- Long-context price tiers (Claude Haiku 5.5 above 100K prompt tokens, OpenAI models above 272K, Gemini Pro models above 200K) are stored in the pricing tables and now applied when calculating Claude and Codex costs, as they already were for Copilot. Gemini costs still use base rates.
- Added `AI_OBSERVER_HOST` to choose the network interface the servers listen on, and `AI_OBSERVER_ALLOWED_HOSTS` to accept additional host names for the dashboard and API.
- Added `AI_OBSERVER_PRICING_DIR`: pricing files there (`claude.json`, `codex.json`, `gemini.json`, `github_copilot.json`, same format as the built-in ones) add models and replace built-in prices without a new release. Costs already stored keep their prices; re-import session files to recalculate them.
- **Added an MCP endpoint for agents** (`/mcp` on the API port, off by default; `AI_OBSERVER_MCP=true` or `serve --mcp` / `watch --mcp`). Claude Code, Codex CLI, Gemini CLI, GitHub Copilot and OpenCode can search earlier conversations (`search_conversations`: all words of a query in any order, or "quoted phrases"), list sessions, read transcripts, summarize token usage and cost by tool, model, day or repository, and read logs, traces and metrics, with read-only tools. It uses the API's Host, Origin and token checks and has no request timeout. See `docs/mcp.md`.
- **Added redaction of secrets before telemetry is stored**, on by default (`AI_OBSERVER_REDACT`): API keys and tokens (Anthropic, OpenAI, GitHub, GitLab, AWS, Google, Slack, Stripe, npm), JWTs, private keys, `Authorization` header values, passwords in URLs and credential values of assignments such as `DB_PASSWORD=…` are replaced with markers such as `[REDACTED:github_token]` in logs, spans and metric attributes from OTLP, `import` and `watch`. `AI_OBSERVER_REDACT=all` also replaces personal data (emails as stable pseudonyms, card numbers, IBANs, phone numbers, US social security numbers, public IP addresses); `off` stores telemetry as received. Telemetry stored earlier is not changed. See `docs/redaction.md`.
- Added `ai-observer pricing update`: fetches current model prices from [models.dev](https://models.dev) and writes `claude.json`, `codex.json`, `gemini.json` and `github_copilot.json` to `AI_OBSERVER_PRICING_DIR` (or `--output`), where AI Observer reads them at startup, so prices stay current without a release. It merges into the current files: changed prices are updated, new models added, models models.dev does not list kept, and changes of more than 10x refused unless `--allow-large-changes` is given. `make pricing-update` refreshes the built-in prices.
- Added a pricing freshness check: `scripts/check-pricing-age.sh` (`make check-pricing`) fails when a built-in price file was last updated more than 7 days ago, and the new `Pricing freshness` workflow runs it daily and on pull requests that change pricing data.
- Added `AI_OBSERVER_RETENTION_DAYS`: when set, `serve` and `watch` delete telemetry older than that many days on startup and then hourly.
- Added the `--pricing-mode` flag to `import`, which was documented but missing (`auto`, `calculate`, `display`).
- Added optional token authentication: with `AI_OBSERVER_AUTH_TOKEN` set, OTLP and API requests need `Authorization: Bearer <token>`, and the dashboard is opened once with `?token=<token>`, which sets an HttpOnly, `SameSite=Strict` cookie. The cookie holds a value derived from the token, so it cannot be used to send telemetry. Without the variable nothing changes, and the server warns at startup when it listens beyond localhost.
- CI runs golangci-lint (`backend/.golangci.yml`: `govet`, `ineffassign`, staticcheck bug checks, `unused`).

### Changed

- **Built-in prices come from models.dev** and were refreshed and checked against the providers' pricing pages on November 5, 2026. Gemini 3.1 Flash-Lite is now priced at $0.25 / $1.50 per million tokens. Models that were shut down or deprecated are marked deprecated and keep their prices, so older sessions are still priced. From December 1, 2026 Google bills requests to `gemini-2.5-pro` and `gemini-2.5-flash` at Gemini 3.8 Flash and 3.5 Flash-Lite rates, which the price tables will need then.
- **GitHub Copilot costs follow GitHub's published Copilot prices.** Grok 4.5, 4.6 and 4.7, Kimi K3, MAI-Code-1.1-Flash and Claude Opus 4.8 (fast mode), which had no price, now get costs, and GPT-5.6 Luna's long-context prices apply above 200K instead of 272K input tokens. Models GitHub has retired are marked deprecated. Costs already stored keep their prices.
- **The binary now listens on `127.0.0.1` by default** instead of all interfaces. Set `AI_OBSERVER_HOST=0.0.0.0` to accept telemetry or dashboard requests from other machines or containers. The Docker image keeps listening on all interfaces.
- Updated DuckDB to v2.0.0 through `duckdb-go` v2.20000.0.
- Telemetry is now written with DuckDB appenders instead of one `INSERT` per row (about 300x faster in a 50k-row comparison), and duplicate spans are detected with one query per batch instead of one per span.
- Reduced telemetry table indexes from 23 to the 3 used for trace and span ID lookups. On a 950k-row benchmark this cut insert time by about 22% and database size from 260 MB to 20 MB with unchanged query times. Existing databases drop the unused indexes on startup; the freed space is reused for new data rather than shrinking the file.
- Dashboard and API reads no longer wait for running writes such as large imports; only writes are serialized.
- **Gemini `.delta` series are computed at query time.** Charts compute per-interval increases from the cumulative counters themselves, so ingestion no longer derives and stores `.delta` points. The `.delta` names keep working in widgets, series and `/api/metrics` and now also include imported data, but metric name lists show only the counters, which return the same series (the "(Cumulative)" entries are now plain "Sessions", "Token Usage" and so on); stored `.delta` points are removed once on startup.
- **Imported Gemini token types are named like Gemini CLI's telemetry** (`cache`, `thought` instead of `cached`, `thoughts`), so type breakdowns combine imported and live data; stored points are renamed once on startup.
- The frontend loads pages on demand, reducing the initial JavaScript bundle from 1.39 MB to 345 kB, and no longer buffers real-time metric objects in memory.
- Startup now fails if the OTLP port is unavailable instead of running without OTLP ingestion.
- `make backend` no longer clears the global Go build cache on every build; cached builds remain reproducible and pick up version and embedded frontend changes.
- Pinned pnpm 10.34.7 through `packageManager` in `frontend/package.json`, CI, the Dockerfile and `.tool-versions` (which now also lists Go 1.26.0); newer global pnpm versions switch to it automatically. The frontend package version now matches the release version.
- Split the frontend metric catalog into one file per provider under `src/lib/metricCatalog/`, with shared types in `src/lib/metricTypes.ts`.
- Consolidated the attribute parsing helpers of the transcript and Codex metric code into `internal/attrval`; Codex token counts written as floats (for example `"12.0"`) are now read instead of being treated as 0. Removed unused storage helpers.
- **`import --purge` only replaces the imported tools' data.** It used to delete the data of every tool in the time range, including OTLP-only tools such as GitHub Copilot and OpenCode; it now deletes only the imported tools' services and re-imports all of their files in the range, unchanged ones included.
- **Cumulative counters and histograms are aggregated per series.** Increases are computed between consecutive points of each series (looking back up to 24 hours before the range), so totals of tools that export cumulative values (GitHub Copilot, OpenCode) are no longer undercounted when several sessions run at once, and time-bucketed charts of cumulative counters show the increase per bucket like delta counters instead of a running total. Raw Gemini cumulative series change the same way and now equal their `.delta` series.
- `import` exits with status 1 when tools or files could not be imported, so scripts and scheduled jobs notice. `export --from-files` still exports the readable files and prints a warning.
- Real-time WebSocket messages now announce stored records (count, services and timestamps) instead of carrying their content, so prompts and tool output are no longer pushed to every open browser and large batches no longer produce large frames.
- Sessions and transcripts are looked up through stored `SessionId`/`SessionIds` columns instead of parsing the attributes JSON of every log and span on each request. Existing databases fill the columns once on startup (recorded in the new `schema_migrations` table), and duplicate spans stored by earlier versions are removed once, so trace lists no longer deduplicate on every query.
- `/api/stats` is cached for 5 seconds, and the dashboard and Metrics page stop polling while their tab is hidden and refresh when it is shown again.
- Queries stopped by the 5-second API timeout return `504 Gateway Timeout`, which the frontend no longer retries five times.
- Batch metric queries run at most four at a time, and time-bucketed series are limited to about 2,000 buckets by widening the interval for long ranges.
- Invalid `from`/`to` parameters and ranges with `from` after `to` are rejected with 400 instead of silently showing the last 24 hours. With only `to`, the range now covers the 24 hours before it.
- Successful HTTP requests are logged at DEBUG instead of INFO.
- Code blocks in session transcripts are highlighted once and cached; very long blocks without a language are no longer auto-detected.
- Series in metric responses are sorted, so chart legends and colors stay stable across refreshes.
- Removed the always-zero `codexOperationCount` and the duplicate `rawTraceCount` from `/api/stats`.
- `import --yes` (without `--dry-run` or `--purge`) and `export --from-files` parse each session file once instead of twice, and print the imported totals at the end. File hashes are computed while parsing instead of re-reading each file up to three times, and files without import state are no longer hashed before parsing.
- `import` no longer reads every previously imported file to hash it: files whose size and modification time match the import are skipped without reading them, and a changed file is hashed once per run instead of once for the preview and again for the import.
- `export --from-files` imports into a temporary database file instead of memory.
- Duplicate span checks look spans up by ID instead of reading every stored span of their traces, so each export of a long Codex session no longer reads the whole session again.
- Metric name lists come from a small catalog instead of scanning all metric points, and breakdown values are read from the distinct attribute sets.
- Retention deletes expired telemetry a day per transaction, so a large backlog no longer blocks ingestion until it is gone.
- The import and watch code of each tool share their record conversion, so watched and imported Claude Code, Codex and Gemini sessions produce the same records.
- The dashboard's WebSocket reconnects with a growing delay (1 to 30 seconds) instead of every 3 seconds, network errors are retried in every browser, and requests that time out show an error instead of nothing.
- API bodies over the limit get `413` also when the size is only noticed while decoding.
- **`import`, `export` and `delete` dates are days in the local time zone** instead of UTC.
- `import --purge` with `--from`/`--to` rebuilds unchanged files in the range too; without a range and with nothing changed it explains how to rebuild instead of silently doing nothing.
- After `delete`, the CLI explains that the import state is kept and `import --force` restores deleted records.
- OTLP requests that cannot be stored are answered with `503` and `Retry-After` instead of `500`, so exporters retry them instead of dropping the batch, and payloads over the 10 MB limit get `413` instead of `400`.
- API request bodies are limited to 1 MB.
- `watch` keeps the last 1,000 Claude Code request IDs for usage deduplication instead of every ID of the session, so its saved state no longer grows with the session.
- A locked database (another `ai-observer` process has it open) now fails with an explanation instead of DuckDB's lock error.
- Parquet exports leave out the new internal `SourceFile` column.
- The frontend no longer logs WebSocket and drag-and-drop details to the browser console, and dashboard changes report the server's error message.
- `watch` scans and backfills existing session files in the background: the dashboard is available right away, and Ctrl-C during a long `--backfill` stops it and shuts down cleanly. Files that change during the scan are imported when it ends.
- Invalid numeric settings (`AI_OBSERVER_API_PORT`, `AI_OBSERVER_OTLP_PORT`, `AI_OBSERVER_RETENTION_DAYS`) are logged as warnings instead of being silently replaced by the default, for example `AI_OBSERVER_RETENTION_DAYS=30d`, which kept all data. Ports must be between 1 and 65535.
- Directories that `watch` cannot watch, for example because of the open-file or inotify limit, are logged as warnings instead of at debug level.
- Invalid gzip bodies on the OTLP port are answered with the same JSON error as other OTLP errors.
- `make backend-lint` runs the golangci-lint version that CI uses through `go run`, so it no longer needs a separate installation.
- **The session list shows each session as a whole.** Start time, duration, message count and model cover all of a session's logs, also those before the selected time range; the range selects which sessions are listed (those active in it, most recently active first). The list also loads about 2.5x faster on large databases (64 instead of 162 ms per page for 2M logs), since the total is counted in the same scan as the page.
- `/api/stats` and `/api/logs/levels` accept optional `from`/`to`. The dashboard's stats widgets now count the records in the selected time range instead of all stored telemetry; without a range the API still counts everything.
- Claude Code session files are read once per import instead of twice.
- The session transcript page loads 28% less JavaScript: code blocks are highlighted with a set of 17 common languages instead of highlight.js's full "common" bundle. Other languages are shown unhighlighted.
- Docker builds cache the Go and pnpm dependencies in their own layers, so a source change no longer downloads all dependencies again.
- Dashboard components re-render only when the store values they use change.
- Pages of more than 50 logs or metric points (the Logs page offers 100 rows per page) are up to 15x faster: DuckDB read every log body in the time range for them instead of only those of the page.
- The dashboard's icon-only buttons (move and remove widget, add row) have accessible names for screen readers.
- **The Logs, Traces and Sessions pages page with cursors.** Pages no longer shift while new data arrives, so no rows are skipped or shown twice when paging during ingestion. The list endpoints (`/api/logs`, `/api/traces`, `/api/sessions`, `/api/metrics`) return a `nextCursor` to pass as `cursor` for the next page; `offset` keeps working up to 10,000 rows (deeper offsets return `400`, since every row before them is sorted) but is deprecated. Paging goes forward and back one page at a time; the "last page" button was removed.
- Added `GET /api/providers`, which lists the supported tools. The dashboard takes service names from it, so they now match the CLI: "OpenAI Codex CLI" and "GitHub Copilot (VS Code)" instead of "Codex CLI" and "GitHub Copilot VS Code Extension".
- The dashboard and the Metrics page refresh when new data is announced over the live connection and poll only while it is disconnected (and once a minute to move relative time ranges), instead of polling and refreshing on every notification at the same time. A newer refresh cancels an older one, so a slow response can no longer overwrite newer data.
- The metrics page and the add-widget panel share their metric picker and chart code: metrics of unknown tools are listed with their full names in both, and metrics page charts with buckets under a minute label them with seconds, like dashboard charts.
- Session transcript assembly moved from the storage layer into its own package (`internal/transcript`), the Codex operation view into `internal/traceview`, and the tools' transcript event roles and shared attribute names (session ID, model, tool name, input and output) into the provider table (`internal/tools`).

### Deprecated

- The `offset` parameter of the list endpoints (`/api/logs`, `/api/traces`, `/api/sessions`, `/api/metrics`): pass the previous page's `nextCursor` as `cursor` instead. `offset` works up to 10,000 rows.

### Removed

- Removed the unused, non-functional Claude pricing page scraper (`internal/pricing/claude_fetcher.go`) and the unused GitHub Models catalog fetcher: GitHub retired GitHub Models on July 30, 2026, so the built-in catalog snapshot, which maps catalog names to Copilot models, is final.

### Fixed

- Fixed `import` and `export --from-files` silently skipping whole Claude Code and Codex session files that contain a line longer than 1 MB (large tool outputs or inline images). Files that cannot be imported are now always listed with the reason, not only with `--verbose`.
- Fixed imports leaving partial data behind when storing a file failed midway; each file's records and import state are now written in one transaction.
- Fixed lines appended to a session file during an import being skipped by later imports: the recorded hash now covers exactly the parsed content, so the appended lines mark the file as modified.
- Fixed Ctrl-C and `SIGTERM` exiting with status 1 and logging "Server error" before the database was closed; shutdown now completes before the process exits, in both `serve` and `watch` mode.
- **Fixed Gemini CLI cost charging cached tokens twice.** Gemini's input tokens include the cached ones, which were charged at the input rate and again at the cache rate (with 80% cached input, the prompt cost was about four times too high). Only uncached input is now charged at the input rate, for live telemetry, imports and watch; stored costs, imported ones included, are recomputed once on startup.
- **Fixed date-limited imports marking whole files as imported.** `import --from/--to` stored only the records in the range but recorded the file as fully imported, so later imports skipped the rest and `watch` resumed after it. Such files are now marked partial: the next import reads them again, and `watch` reads a file again when records after the range were left out.
- **Fixed upgrading requiring `--purge`, which also deleted live telemetry.** Files imported by earlier versions are now replaced like others: re-importing one deletes the matching imported records of the earlier version and keeps OTLP data.
- **Fixed `import --purge` losing data when a file failed.** Each tool's deletion and rebuild now run in one transaction, rolled back when any of its files cannot be imported.
- Fixed GitHub Copilot token and cost metrics being stored again when an exporter resent a trace batch; they are derived only from spans that were not stored yet.
- Fixed charts of cumulative counters counting a series' whole running total after its earlier points were deleted by retention or `delete`: the last deleted point is kept as a baseline.
- Fixed `watch` storing the last line of a file again when an import had read it without a trailing newline.
- **Fixed Gemini CLI cost being overcounted many times over.** Gemini sends running token totals every 10 seconds, and the cost of each total was stored as a per-interval amount, so a session's cost was added again with every export. Cost is now a cumulative series per token series like the counters it comes from; costs stored by earlier versions are rebuilt from the stored token counters once on startup. Gemini per-interval series also count overlapping and restarted sessions separately and include each session's first export.
- **Fixed re-imports duplicating data.** Importing a session file again after it grew, with `--force`, or after `watch` had stored part of it added all of its records a second time. Records now carry the file they came from (`SourceFile`), and a re-import replaces them in the same transaction; `watch` also replaces a file's records when the file is truncated or rewritten.
- Fixed Codex and Copilot token and cost metrics being lost when they could not be stored while their logs or spans were; both are now stored in one transaction.
- Fixed `delete all` and retention leaving a partial delete when one table failed; the three tables are now deleted in one transaction.
- Fixed a dashboard import that failed halfway leaving a partial dashboard behind.
- Fixed a dashboard request coalesced with an aborted one failing too; the shared request now runs until every caller has aborted.
- Fixed request timers and abort listeners of the API client not being released.
- Fixed WebSocket connections and the hub goroutine staying open after server shutdown.
- Fixed session transcript requests returning 404 for database errors; only unknown sessions return 404.
- Fixed OTLP endpoints answering protobuf requests with a JSON body; responses now use the request's encoding as the OTLP/HTTP specification requires.
- Fixed the Metrics page no longer refreshing on real-time updates after 10,000 metric points had been received.
- Fixed the from-files exporter test importing the developer's real Codex and Gemini sessions, which could exceed the test timeout; it now uses generated Claude Code, Codex and Gemini fixtures and verifies that each provider is exported.
- Fixed real-time updates being lost when several WebSocket messages were queued: they were joined into one frame that the dashboard could not parse.
- Fixed live updates not connecting when the dashboard is opened via `127.0.0.1`, a LAN address or a host name from `AI_OBSERVER_ALLOWED_HOSTS`: same-origin WebSocket connections were rejected.
- Fixed `watch` storing logs and metrics twice when a session file changed again while it was being processed.
- Fixed `watch` skipping the lines written between a full import and the start of watching, and re-importing every message of Gemini sessions on their next change.
- Fixed flags after a boolean flag and the tool argument being ignored, for example `--verbose` in `import --dry-run claude-code --verbose` or `--from`/`--to` in `delete --yes all --from … --to …`.
- Fixed creating, deleting or setting the default dashboard leaving a partial change (for example no default dashboard) when the second of its statements failed.
- Fixed slow WebSocket clients never being disconnected after their buffer filled up.
- Fixed histogram bounds and quantiles losing precision below 0.000001, and NaN or infinite values making the whole metrics batch fail to store.
- Fixed `export` failing for output paths that contain a single quote.
- Fixed editable dashboard names saving twice on Enter and saving the discarded text on Escape.
- **Fixed retried OTLP log and metric exports being stored twice.** When an exporter resent a batch whose response was lost, delta counters such as Claude Code's cost counted it twice. Batches identical to one stored in the last 15 minutes are now acknowledged without storing them again (spans were already deduplicated by their IDs).
- Fixed the session list showing the alphabetically last model of a session instead of the latest one.
- Fixed `watch` on Linux missing changes made during a long startup scan: file events were not read until the scan ended, so the kernel's event queue could overflow. Events are now read during the scan and the files they name are imported after it.
- Fixed rendering glitches in the session list when the same session ID was listed for two services.
- Error messages of failed dashboard requests include the server's explanation (for example "invalid cursor"), not only the status code.
- Fixed the release workflow writing the checksum of an error page into the Homebrew formula when the release asset could not be downloaded yet; it now retries and fails instead.
- Fixed `%` and `_` in log and trace searches matching any characters.
- Fixed the server keeping the database open when its setup failed.

### Security

- The dashboard and API reject requests whose `Host` header names an unknown domain, preventing DNS rebinding attacks from reading local telemetry.
- API errors no longer include internal details such as SQL errors or file paths; they are logged server-side instead.
- CORS no longer allows credentialed requests, which the API does not use.
- Both servers reject requests from foreign browser origins, so web pages can no longer write telemetry to the OTLP port with "simple" cross-site requests (for example a `text/plain` POST) or change dashboards. Requests without an `Origin` header (exporters, CLI tools) are unaffected; `AI_OBSERVER_ALLOWED_HOSTS` also lists accepted origin host names.
- The README's Docker examples publish the ports on `127.0.0.1` only.
- Same-origin browser requests are only trusted for IP addresses, `localhost` and allowed host names, so a site that rebinds its own domain to `127.0.0.1` can no longer write telemetry to the OTLP port, which has no `Host` check.
- The README explains that the dashboard's authentication cookie lasts 30 days and is revoked by changing `AI_OBSERVER_AUTH_TOKEN`.
- The API port no longer accepts HTTP/2 cleartext (h2c) upgrades, which browsers never use and which could tunnel requests past a reverse proxy's rules. The OTLP port keeps h2c.

## [0.6.0] - 2026-09-15

### Added

- Added reconstructed Claude Code activity metrics to full imports: lines added/deleted from successful edits and writes, and commits from successful Git commit results. Incomplete or failed tool activity is omitted.
- Added Claude Code pull request counts from distinct repository/PR links within each imported file, session counts per non-empty file, and active-time estimates from recorded turn durations. PR links indicate references rather than confirmed PR creation; these activity metrics are available in full imports, not incremental watch mode.
- Added repository and Git branch attribution to Claude Code and Codex logs and token/cost metrics in both import and watch mode, including Codex session, transcript, tool, and reasoning logs.
- Added repository detection from Codex session metadata, local Git remotes and worktrees, structured Claude PR links, GitHub URLs, and literal `gh pr --repo`/`-R` arguments in supported message and tool content. Recorded Git URLs and local remotes take precedence over references to other repositories.
- Added persistent repository context across watcher restarts and context recovery when resuming older watch state, starting after a full import, or starting without backfill. Later evidence enriches subsequent batches; existing database records are not automatically updated.

### Changed

- Updated DuckDB to v1.5.5 through `duckdb-go` v2.10505.0 and Go bindings v0.10505.0.
- Expanded import documentation and metric availability tables with activity reconstruction rules, repository attribution precedence, supported reference sources, and import/watch limitations.
- Added full backend race detection to macOS CI builds and regression coverage for activity reconstruction, repository attribution, transcripts, DuckDB string/blob boundaries, and concurrent logger use.

### Fixed

- Fixed GitHub Copilot local sessions returning "Failed to load session transcript" by reconstructing user, assistant, and tool messages from GenAI spans, including related child spans with missing or alternate session IDs.
- Fixed known sessions without captured conversation content returning 404; they now return an empty transcript while unknown sessions still return 404.
- Preserved imported transcript messages and indices when combining logs with spans, avoided duplicate messages and token usage from overlapping telemetry, and ordered tool results by completion time.
- Preserved OTLP protobuf log event names as `event.name` so transcript events remain identifiable.
- Fixed Claude Code imports dropping token and cost usage when usage arrives in a later record for an already-seen request.
- Fixed a DuckDB pointer-alignment crash when reading non-inlined strings with Go pointer checks enabled through the dependency upgrade.
- Fixed a data race during default logger initialization, access, and reconfiguration.

## [0.5.0] - 2026-06-18

### Added

- Added OpenCode as an OTLP-only provider via the `@devtheops/opencode-plugin-otel` plugin.
- Added `ai-observer setup opencode` instructions with AI Observer-compatible OTLP HTTP/protobuf configuration.
- Added export and delete filtering support for the `opencode` service.
- Added OpenCode frontend provider metadata, service labels, icons, metric grouping, and dashboard widget catalog support.
- Added OpenCode metric metadata for sessions, token usage, cost, lines of code, commits, tool duration, cache activity, session duration, messages, model usage, and retries.
- Added OpenCode session and transcript support for plugin log events and OpenInference-style model, token, cost, tool input, and tool output attributes.
- Added OpenCode setup, usage, architecture, export, import, pricing, and metric availability documentation in README, docs, and the in-app documentation page.
- Added backend and frontend tests for OpenCode provider registration, export behavior, setup output, transcript helpers, and metric metadata.

### Changed

- Extended frontend metric filtering so OpenCode services show provider-specific metrics and shared `gen_ai.*` metrics when selected.
- Updated top-level CLI help to include OpenCode OTLP plugin environment variables.
- Kept `import all` and `watch all` scoped to file-backed providers while allowing OpenCode as a recognized OTLP service.

### Fixed

- Fixed file-import source discovery so adding OpenCode to the shared tool registry does not make `import all` expect an OpenCode file parser.

## [0.4.0] - 2026-06-14

### Added

- Added GitHub Copilot as an OTLP provider, including setup instructions for the VS Code extension and environment-variable based exporters.
- Added GitHub Copilot metric support for token usage, cost, tool calls, agent turns, edit outcomes, lines of code, cloud sessions, pull request events, and generic GenAI metrics.
- Added derived GitHub Copilot token and cost metrics from Copilot chat spans.
- Added GitHub Copilot pricing data, GitHub Models catalog alias matching, and support for dated/versioned model aliases emitted by Copilot telemetry.
- Added `watch` mode for real-time incremental ingestion from local Claude Code, Codex CLI, and Gemini CLI session files, with optional initial backfill.
- Added import-state tracking for file watcher offsets, message counts, and parser state.
- Added export filtering for GitHub Copilot VS Code Extension (`copilot-chat`) and GitHub Copilot CLI (`github-copilot`) services.
- Added frontend metric metadata, service labels, provider icons, dashboard widget catalog entries, and documentation for GitHub Copilot.

### Changed

- Updated DuckDB to v1.5.3.
- Reworked trace list and detail handling around explicit trace row `id` and `kind` values so normal OTLP traces and Codex operation rows can be opened consistently.
- Simplified recent trace queries by using a unified OTLP trace overview path instead of separate Codex and non-Codex query paths.
- Updated Codex trace handling to use raw session trace rows in the list and grouped Codex operation spans in trace detail.
- Expanded pricing data and aliases for Claude, Codex/OpenAI, and Gemini models, including long-context pricing support.
- Improved metric series aggregation by preserving both token type and model labels.
- Updated README and in-app documentation for watch mode, GitHub Copilot telemetry, trace row semantics, and metric availability by ingestion mode.

### Fixed

- Fixed GitHub Copilot trace detail loading returning 404 for traces that appeared in the trace list.
- Rebuilt stale DuckDB trace indexes under versioned names and added an index for span-id lookups.
- Fixed dashboard widget persistence and error handling for missing dashboards, missing widgets, widget position updates, and empty widget lists.
- Fixed Copilot sessions and transcripts by recognizing GenAI conversation IDs, models, tool names, tool arguments, and tool results in logs.
- Fixed frontend trace expansion to load one selected trace row at a time and to route trace detail pages with the correct trace kind.
- Fixed frontend metric selection so GitHub Copilot services show their token and cost metrics even before derived rows exist for every metric.
- Fixed repeated abort-error handling with shared frontend helpers.
- Fixed Codex per-event token and cost metrics so they are not treated as cumulative series.

### Security

- Hardened storage queries by replacing manual `LIMIT`/`OFFSET` string formatting with parameterized query helpers.
- Validated metric JSON attribute keys before building DuckDB JSONPath expressions for breakdown and latest-value queries.

## [0.3.2] - 2026-03-29

### Changed

- Updated the Docker build Go version to 1.26.

## [0.3.1] - 2026-03-29

### Added

- Added `.tool-versions` for local Go, Node.js, and pnpm toolchain pinning.

### Changed

- Updated the backend Go target and dependencies, including DuckDB Go bindings, OpenTelemetry protobufs, protobuf, gRPC, and related transitive packages.
- Updated CI to use Go 1.26, pnpm 10, newer pnpm setup, and newer artifact upload actions.
- Updated frontend package metadata for version 0.3.1 and pnpm 10 engine requirements.
- Updated Claude, Codex/OpenAI, and Gemini pricing datasets with newer model entries.

### Fixed

- Removed the old Gemini CLI root-endpoint OTLP workaround and documented the minimum Gemini CLI version expected for standard OTLP paths.

## [0.3.0] - 2026-01-27

### Added

- Added Sessions and Session Transcript pages for browsing imported AI assistant conversations.
- Added backend query APIs for listing sessions and retrieving session transcripts.
- Added transcript rendering components for timeline, chat bubble, markdown, code block, and tool call views.
- Added transcript log extraction for Claude Code, Codex CLI, and Gemini CLI imports.
- Added shared time-selection state handling for telemetry pages.
- Added helper scripts for generating release notes and social posts.

### Changed

- Updated frontend version to 0.3.0.
- Improved charts and telemetry pages to use the shared time-selection behavior.
- Improved importer parsing for Claude, Codex, and Gemini session files to preserve conversation content for transcripts.

### Fixed

- Fixed several frontend and import-related issues around logs, metrics, traces, and session transcript display.

## [0.2.0] - 2026-01-04

### Added

- Added `import` command for historical Claude Code, Codex CLI, and Gemini CLI local session files.
- Added `export` command for writing telemetry data to Parquet files, optional DuckDB views, and ZIP archives.
- Added `delete` command for removing traces, logs, and metrics by date range.
- Added `setup` command for printing AI tool telemetry configuration instructions.
- Added dashboard export and import support in the frontend.
- Added embedded pricing data and cost calculation for Claude, Codex/OpenAI, and Gemini models.
- Added Claude user-facing token and cost metrics derived from OTLP data.
- Added import state tracking to avoid re-importing unchanged local files.
- Added frontend date range picker, calendar, and time utility helpers.
- Added documentation for import, export, pricing, and token analysis workflows.

### Changed

- Refactored CLI command parsing around subcommands and shared flag handling.
- Improved metric charts, dashboard widgets, dashboard dialogs, and telemetry page filtering.
- Expanded README coverage for CLI commands, pricing, import/export, and dashboard workflows.

### Fixed

- Fixed command parsing behavior for CLI subcommands and options.
- Fixed import/export/delete storage behavior with expanded test coverage.

## [0.1.0] - 2025-01-01

### Added

- Initial release of AI Observer
- OpenTelemetry-compatible OTLP ingestion (HTTP/JSON and HTTP/Protobuf)
- Support for traces, metrics, and logs
- DuckDB-powered storage for fast analytics
- Real-time dashboard with WebSocket updates
- Customizable drag-and-drop dashboard widgets
- Multi-tool support:
  - Claude Code
  - Gemini CLI
  - OpenAI Codex CLI
- Query API for traces, metrics, logs, and dashboards
- Single binary with embedded React frontend
- Multi-arch Docker images (linux/amd64, linux/arm64)
- Homebrew formula for macOS (Apple Silicon)

### Technical Details

- Backend: Go 1.24, chi router, DuckDB, gorilla/websocket
- Frontend: React 19, TypeScript, Vite, Tailwind CSS v4, Zustand, Recharts
- OTLP ingestion on port 4318
- API/Dashboard on port 8080

[Unreleased]: https://github.com/tobilg/ai-observer/compare/v0.8.0...HEAD
[0.8.0]: https://github.com/tobilg/ai-observer/compare/v0.7.0...v0.8.0
[0.7.0]: https://github.com/tobilg/ai-observer/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/tobilg/ai-observer/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/tobilg/ai-observer/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/tobilg/ai-observer/compare/v0.3.2...v0.4.0
[0.3.2]: https://github.com/tobilg/ai-observer/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/tobilg/ai-observer/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/tobilg/ai-observer/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/tobilg/ai-observer/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/tobilg/ai-observer/releases/tag/v0.1.0
