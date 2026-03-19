# Local Codex Cost Guide

This guide shows how to run AI Observer locally and import Codex telemetry so cost data appears in the dashboard.

## Prerequisites

- `ai-observer` installed locally
- `Codex` sessions available under `~/.codex`

## Start AI Observer

Run the server against a local DuckDB file:

```bash
mkdir -p ./data
AI_OBSERVER_DATABASE_PATH=./data/ai-observer.duckdb \
AI_OBSERVER_API_PORT=8080 \
AI_OBSERVER_OTLP_PORT=4318 \
ai-observer
```

Open the dashboard at:

```text
http://localhost:8080
```

## Import Codex Cost Data

Import Codex sessions and recalculate cost rows:

```bash
ai-observer import codex --force --yes
```

Notes:

- `--force` is important when you want to recalculate cost after pricing changes.
- The importer deduplicates by file path and content hash unless you use `--force`.
- Codex cost is calculated from token usage and the built-in pricing table.

## Verify Cost

Check that the Codex cost metric exists:

```bash
curl 'http://localhost:8080/api/metrics/names?service=codex_cli_rs'
```

Check the cost series:

```bash
curl 'http://localhost:8080/api/metrics/series?name=codex_cli_rs.cost.usage&service=codex_cli_rs&from=2026-03-19T00:00:00Z&to=2026-03-20T00:00:00Z&interval=3600&aggregate=true'
```

## Supported Models

Supported Codex model pricing lives in:

```text
backend/internal/pricing/data/codex.json
```

If a Codex model slug is not priced yet, the cost metric may stay empty until the table is updated and the Codex import is rerun.
