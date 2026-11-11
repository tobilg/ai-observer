# Export Command

The `ai-observer export` command allows you to export telemetry data from AI Observer to portable Parquet files with an optional DuckDB views database for easy querying.

## Overview

Export your observability data for:
- **Archiving** — Create backups of historical telemetry data
- **Sharing** — Distribute data to team members or other systems (an export holds the sessions' prompts, code and tool output, and the local paths of their session files)
- **Analysis** — Load into external tools (DuckDB CLI, Python, R, Spark)
- **Portability** — Move data between AI Observer instances

## Output Structure

The export creates the following files in your specified output directory:

```
output-directory/
├── traces.parquet           # All trace/span data
├── logs.parquet             # All log records
├── metrics.parquet          # All metric data points
├── metric_baselines.parquet # Last points of cumulative series before the range
├── import_state.parquet     # How far each exported session file was read
├── dashboards.json          # All dashboards with their widgets
├── manifest.json            # What the export holds
└── ai-observer-export-{SOURCE}-{RANGE}.duckdb   # Views database
```

- `metric_baselines.parquet` holds the stored baselines of cumulative series (the last point kept when older points were deleted) and, for an export with `--from`, the last cumulative point of each series in the 24 hours before the range, so a restored series keeps its per-interval increases. Histogram baselines include their counts and buckets.
- `dashboards.json` holds all dashboards with their widgets, as `GET /api/dashboards/{id}` returns them (dashboards are not filtered by tool or range).
- `import_state.parquet` holds the import state of the session files whose records are exported (how far each was read), so imports and `watch` after a restore continue there instead of storing the records again. A file with records the export leaves out (outside the range, or of a service the export does not cover) is marked to be read again in full by the next import or watch, which then replaces its records.
- `manifest.json` describes the export: `format` (2), the AI Observer `version` and database `schemaVersion` that wrote it, `exportedAt`, `source`, `services`, `from`/`to` and the row count of each file.

Records keep the internal `SourceFile` column, the path of the session file a record was imported from, so **an export contains local file paths**. The derived `MessageKey` column of logs is left out; restore computes it. All files but `dashboards.json` are written from one snapshot of the database, also by the admin API while the server stores telemetry.

Format 2 (`SourceFile`, `import_state.parquet`) is new in this version: earlier AI Observer versions refuse to restore such exports, while this version restores exports of earlier versions.

**Naming examples:**
```
ai-observer-export-all-all.duckdb                     # --export all, no date filter
ai-observer-export-all-2025-01-01-2025-01-15.duckdb   # --export all --from/--to
ai-observer-export-claude-code-all.duckdb                  # --export claude-code, no date filter
ai-observer-export-claude-code-2025-01-01-2025-01-15.duckdb # --export claude-code --from/--to
```

**With `--zip` flag:**
```
output-directory/
└── ai-observer-export-{SOURCE}-{RANGE}.zip   # Single ZIP containing all files
```

## Usage

```bash
# Export all data from database
ai-observer export all --output ./export

# Export only Claude data
ai-observer export claude-code --output ./export

# Export OpenCode telemetry
ai-observer export opencode --output ./export

# Export with date filter
ai-observer export all --output ./export --from 2025-01-01 --to 2025-01-15

# Export to ZIP archive
ai-observer export claude-code --output ./export --zip

# Export directly from raw files (without prior import)
ai-observer export claude-code --output ./export --from-files

# Dry run to preview what would be exported
ai-observer export all --output ./export --dry-run
```

## Options

| Option | Description |
|--------|-------------|
| `--output DIR` | Output directory (required) |
| `--from DATE` | Start date filter (YYYY-MM-DD) |
| `--to DATE` | End date filter (YYYY-MM-DD) |
| `--from-files` | Read from raw JSON/JSONL files instead of database |
| `--zip` | Create single ZIP archive of exported files |
| `--dry-run` | Preview what would be exported without creating files |
| `--verbose` | Show detailed progress |
| `--yes` | Skip confirmation prompt |
| `--db FILE` | Database file (default `AI_OBSERVER_DATABASE_PATH` or `./data/ai-observer.duckdb`) |

While a server runs, which holds the database, `export` prints the `curl` command of the admin API's `POST /api/admin/export` (with `AI_OBSERVER_ADMIN_API=true`), which writes the export to `AI_OBSERVER_BACKUP_DIR` (default: `backups` next to the database).

## Source Mapping

The source argument maps to internal service names; a tool's export covers all of its services:

| Source | Internal ServiceName |
|--------|---------------------|
| `claude-code` | `claude-code` |
| `codex` | `codex_cli_rs`, `codex-app-server`, `codex-exec-server` |
| `gemini` | `gemini_cli` |
| `opencode` | `opencode` |
| `copilot-chat` | `copilot-chat` |
| `github-copilot` | `github-copilot` |
| `all` | No filter (all services) |

## Workflow

### Database Export Mode (Default)

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      DATABASE EXPORT WORKFLOW                               │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   $ ai-observer export [SOURCE] --output [DIR] [options]                    │
│                                    │                                        │
│                                    ▼                                        │
│                         ┌─────────────────────┐                             │
│                         │   Parse Options     │                             │
│                         │ • Source validation │                             │
│                         │ • Date parsing      │                             │
│                         │ • Path validation   │                             │
│                         └──────────┬──────────┘                             │
│                                    │                                        │
│                                    ▼                                        │
│                         ┌─────────────────────┐                             │
│                         │   Open DuckDB       │                             │
│                         │   ai-observer.duckdb│                             │
│                         └──────────┬──────────┘                             │
│                                    │                                        │
│                                    ▼                                        │
│                         ┌─────────────────────┐                             │
│                         │      Preview        │                             │
│                         │ • Count traces      │                             │
│                         │ • Count logs        │                             │
│                         │ • Count metrics     │                             │
│                         └──────────┬──────────┘                             │
│                                    │                                        │
│                         [if --dry-run, stop here]                           │
│                                    │                                        │
│                                    ▼                                        │
│                         ┌─────────────────────┐                             │
│                         │  User Confirmation  │                             │
│                         │  (skip with --yes)  │                             │
│                         └──────────┬──────────┘                             │
│                                    │                                        │
│           ┌────────────────────────┼────────────────────────┐               │
│           ▼                        ▼                        ▼               │
│  ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐        │
│  │  Export Traces  │     │  Export Logs    │     │ Export Metrics  │        │
│  │  to Parquet     │     │  to Parquet     │     │  to Parquet     │        │
│  └────────┬────────┘     └────────┬────────┘     └────────┬────────┘        │
│           │                       │                       │                 │
│           │   COPY (SELECT * FROM otel_traces             │                 │
│           │         WHERE Timestamp >= ? AND Timestamp <= ?                 │
│           │         [AND ServiceName = ?])                │                 │
│           │   TO 'traces.parquet'                         │                 │
│           │   (FORMAT PARQUET, COMPRESSION 'ZSTD')        │                 │
│           │                       │                       │                 │
│           └───────────────────────┼───────────────────────┘                 │
│                                   │                                         │
│                                   ▼                                         │
│                        ┌─────────────────────┐                              │
│                        │ Create Views DB     │                              │
│                        │ • CREATE VIEW traces│                              │
│                        │ • CREATE VIEW logs  │                              │
│                        │ • CREATE VIEW metrics                              │
│                        └──────────┬──────────┘                              │
│                                   │                                         │
│                        [if --zip flag set]                                  │
│                                   │                                         │
│                                   ▼                                         │
│                        ┌─────────────────────┐                              │
│                        │  Create ZIP Archive │                              │
│                        │ • Add all files     │                              │
│                        │ • Remove originals  │                              │
│                        └──────────┬──────────┘                              │
│                                   │                                         │
│                                   ▼                                         │
│                        ┌─────────────────────┐                              │
│                        │    Output Files     │                              │
│                        │ output-directory/   │                              │
│                        │ ├── traces.parquet  │                              │
│                        │ ├── logs.parquet    │                              │
│                        │ ├── metrics.parquet │                              │
│                        │ └── *.duckdb        │                              │
│                        └─────────────────────┘                              │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### From-Files Export Mode

When using `--from-files`, the export reads directly from raw session files without requiring prior import. The text is redacted with `AI_OBSERVER_REDACT` like imported data, and records keep their file paths:

`--from-files` is only available for `claude-code`, `codex`, `gemini`, and `all`. GitHub Copilot and OpenCode are OTLP-only in AI Observer and export from the DuckDB database.

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                      FROM-FILES EXPORT WORKFLOW                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   $ ai-observer export [SOURCE] --output [DIR] --from-files                 │
│                                    │                                        │
│                                    ▼                                        │
│                         ┌─────────────────────┐                             │
│                         │   Parse Options     │                             │
│                         └──────────┬──────────┘                             │
│                                    │                                        │
│                                    ▼                                        │
│                         ┌─────────────────────┐                             │
│                         │ Create Temp DuckDB  │                             │
│                         │   (in-memory)       │                             │
│                         └──────────┬──────────┘                             │
│                                    │                                        │
│           ┌────────────────────────┼────────────────────────┐               │
│           ▼                        ▼                        ▼               │
│  ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐        │
│  │ ~/.claude/      │     │ ~/.codex/       │     │ ~/.gemini/tmp/  │        │
│  │ projects/**     │     │ sessions/*.jsonl│     │ **/session-*    │        │
│  │ *.jsonl         │     │                 │     │ .json           │        │
│  └────────┬────────┘     └────────┬────────┘     └────────┬────────┘        │
│           │                       │                       │                 │
│           │    (reuses import parsers)                    │                 │
│           │                       │                       │                 │
│           └───────────────────────┼───────────────────────┘                 │
│                                   │                                         │
│                                   ▼                                         │
│                        ┌─────────────────────┐                              │
│                        │  Populate Temp DB   │                              │
│                        │ • Parse all files   │                              │
│                        │ • Insert into temp  │                              │
│                        │   otel_* tables     │                              │
│                        └──────────┬──────────┘                              │
│                                   │                                         │
│                                   ▼                                         │
│                        ┌─────────────────────┐                              │
│                        │  Export to Parquet  │                              │
│                        │ (same as DB mode)   │                              │
│                        └──────────┬──────────┘                              │
│                                   │                                         │
│                                   ▼                                         │
│                        ┌─────────────────────┐                              │
│                        │ Create Views DB +   │                              │
│                        │ Optional ZIP        │                              │
│                        └─────────────────────┘                              │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Views Database

The exported DuckDB database contains views that reference the Parquet files using **relative paths**:

```sql
-- Views created in the export database
CREATE VIEW traces AS SELECT * FROM read_parquet('traces.parquet');
CREATE VIEW logs AS SELECT * FROM read_parquet('logs.parquet');
CREATE VIEW metrics AS SELECT * FROM read_parquet('metrics.parquet');
```

This allows you to:
- Move the entire export directory to any location
- Share the directory with others
- Query the data using DuckDB CLI or any DuckDB-compatible tool

**Example usage with DuckDB CLI:**

```bash
cd /path/to/export-directory
duckdb ai-observer-export-all-all.duckdb

# Query the views
SELECT COUNT(*) FROM traces;
SELECT ServiceName, COUNT(*) FROM logs GROUP BY ServiceName;
SELECT MetricName, AVG(Value) FROM metrics GROUP BY MetricName;
```

## Parquet File Schema

All existing DuckDB types map directly to Parquet:

| DuckDB Type | Parquet Type | Notes |
|-------------|--------------|-------|
| TIMESTAMP | TIMESTAMP_MICROS | Microsecond precision |
| VARCHAR | STRING | UTF-8 strings |
| BIGINT/INTEGER | INT64/INT32 | Signed integers |
| DOUBLE | DOUBLE | 64-bit floating point |
| BOOLEAN | BOOLEAN | True/false values |
| JSON | STRING | JSON preserved as string |
| UINTEGER/UBIGINT | UINT32/UINT64 | Unsigned integers |

JSON columns remain as strings in Parquet—DuckDB can still query them with `json_extract()`.

## Compression

- **Parquet files** use ZSTD compression (built into DuckDB)
- **ZIP archives** use Store method (no additional compression since Parquet is already compressed)

This results in highly efficient storage with fast read performance.

## Example Output

```
$ ai-observer export all --output ./export --from 2025-01-01 --to 2025-01-15

Export Preview
==============
Source: all (from database)
Time range: 2025-01-01 to 2025-01-15

Data to export:
  Traces:  1,234 spans
  Logs:    5,678 records
  Metrics: 9,012 data points

Output directory: ./export
Files to create:
  - traces.parquet
  - logs.parquet
  - metrics.parquet
  - ai-observer-export-all-2025-01-01-2025-01-15.duckdb

Continue? [y/N] y

Exporting traces... done (1,234 rows)
Exporting logs... done (5,678 rows)
Exporting metrics... done (9,012 rows)
Creating views database... done

Export complete!
Output: ./export (4 files, 15.2 MB)
```

## Use Cases

### Archiving Historical Data

```bash
# Export last month's data to archive
ai-observer export all --output ./archive/2025-01 \
  --from 2025-01-01 --to 2025-01-31 --zip

# Delete from database after archiving
ai-observer delete all --from 2025-01-01 --to 2025-01-31
```

### Sharing Data with Team

```bash
# Export Claude data for analysis
ai-observer export claude-code --output ./claude-analysis --zip

# Share the ZIP file
# Recipients can extract and query with:
# duckdb ai-observer-export-claude-code-all.duckdb
```

### Quick Analysis Without Import

```bash
# Export directly from raw session files
ai-observer export claude-code --output ./quick-analysis --from-files

# Analyze immediately
duckdb ./quick-analysis/ai-observer-export-claude-code-all.duckdb \
  "SELECT SUM(Value) as total_cost FROM metrics WHERE MetricName = 'claude_code.cost.usage'"
```

### Data Migration

```bash
# Export from one AI Observer instance
ai-observer export all --output ./migration --zip

# Copy to the new machine and restore it there (AI Observer stopped)
scp ./migration/ai-observer-export-all-all.zip user@newhost:~/
ai-observer restore ~/ai-observer-export-all-all.zip
```

## Restoring an Export

`ai-observer restore <export directory or .zip>` loads an export back into the database (`--db` or `AI_OBSERVER_DATABASE_PATH`; AI Observer must not be running on it):

| Option | Description |
|--------|-------------|
| `--replace` | Delete the stored records of the export's services in its time range first |
| `--map-path OLD=NEW` | Replace the session file path prefix `OLD` with `NEW`, such as `/Users/old=/Users/new` (repeatable; the first match applies; whole path elements only) |
| `--dry-run` | Show what the export holds and what it would replace, without changing anything |
| `--yes` | Skip the confirmation prompt |
| `--db FILE` | Database file to restore into; a new file creates a new database |

- It shows the export's range, services, rows per file, dashboards and schema version, asks for confirmation, and restores spans, logs, metric points, metric baselines and dashboards in one transaction.
- Spans already stored are skipped. Logs and metric points have no identity, so a restore into a database that holds records of the export's services in the export's time range is refused (`nothing was restored`, exit status 1), unless `--replace` deletes those records first.
- Exports of earlier versions (Parquet files only, without session ID columns) can be restored: session IDs are computed from the attributes. An export of an older schema version is noted, since data migrations after it are not applied to the restored records.
- Dashboards are restored by ID; existing dashboards are kept, and a restored default dashboard stays the default only when the database has none.
- **Session files:** restored records keep the session file they were imported from, and the import state of those files is restored. Importing or watching the same files afterwards skips unchanged files and replaces the records of grown ones, and `watch` continues where the exported database stopped, so nothing is stored twice. A file the database had already imported with another state is read again by the next import or watch, which replaces all of its records.
- **Moved files:** on another computer, or with another home directory, the paths differ: pass `--map-path OLD=NEW` so the moved session files are recognized (the preview counts the files not found on this machine). Without it, importing them stores them a second time.
- **Exports of earlier versions** (format 1, without `import_state.parquet`) carry no file paths: after restoring one, rebuild its sessions with `ai-observer import <tool> --purge` instead of a plain import, which would store them twice.
- **Redaction:** text is redacted again with `AI_OBSERVER_REDACT` (default `secrets`), as when telemetry is received, so an export of a database that did not redact is stored redacted. Restoring 3.5 million records took 10 seconds, 5.5 without redaction.
- A path to a `.duckdb` file is refused with a hint: to restore a backup made by `ai-observer backup`, stop AI Observer and copy it over the database file.

## Tips

1. **Large exports**: Use `--verbose` to monitor progress on large datasets
2. **Disk space**: Ensure sufficient space in the output directory before exporting
3. **Date filtering**: Always use date filters for large databases to avoid exporting everything
4. **ZIP for sharing**: Use `--zip` when sharing exports to simplify file transfer
5. **Relative paths**: The views database uses relative paths, so keep all files together
6. **Whole-database copies**: `ai-observer backup --output <file>` writes a copy of the database itself, which is restored by copying it over the database file (see the README, Maintenance)
