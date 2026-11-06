# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.7.0]: https://github.com/tobilg/ai-observer/compare/v0.6.0...v0.7.0
[0.6.0]: https://github.com/tobilg/ai-observer/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/tobilg/ai-observer/compare/v0.4.0...v0.5.0
[0.4.0]: https://github.com/tobilg/ai-observer/compare/v0.3.2...v0.4.0
[0.3.2]: https://github.com/tobilg/ai-observer/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/tobilg/ai-observer/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/tobilg/ai-observer/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/tobilg/ai-observer/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/tobilg/ai-observer/releases/tag/v0.1.0
