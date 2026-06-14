# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.4.0]: https://github.com/tobilg/ai-observer/compare/v0.3.2...HEAD
[0.3.2]: https://github.com/tobilg/ai-observer/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/tobilg/ai-observer/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/tobilg/ai-observer/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/tobilg/ai-observer/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/tobilg/ai-observer/releases/tag/v0.1.0
