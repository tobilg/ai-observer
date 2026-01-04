# Export Command

The `ai-observer export` command allows you to export telemetry data from AI Observer to portable Parquet files with an optional DuckDB views database for easy querying.

## Overview

Export your observability data for:
- **Archiving** — Create backups of historical telemetry data
- **Sharing** — Distribute data to team members or other systems
- **Analysis** — Load into external tools (DuckDB CLI, Python, R, Spark)
- **Portability** — Move data between AI Observer instances

## Output Structure

The export creates the following files in your specified output directory:

```
output-directory/
├── traces.parquet           # All trace/span data
├── logs.parquet             # All log records
├── metrics.parquet          # All metric data points
└── ai-observer-export-{SOURCE}-{RANGE}.duckdb   # Views database
```

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

## Source Mapping

The source argument maps to internal service names:

| Source | Internal ServiceName |
|--------|---------------------|
| `claude-code` | `claude-code` |
| `codex` | `codex_cli_rs` |
| `gemini` | `gemini_cli` |
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

When using `--from-files`, the export reads directly from raw session files without requiring prior import:

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
ai-observer export all --output ./migration

# Copy to new machine and import (manually or via DuckDB)
scp -r ./migration user@newhost:~/
```

## Tips

1. **Large exports**: Use `--verbose` to monitor progress on large datasets
2. **Disk space**: Ensure sufficient space in the output directory before exporting
3. **Date filtering**: Always use date filters for large databases to avoid exporting everything
4. **ZIP for sharing**: Use `--zip` when sharing exports to simplify file transfer
5. **Relative paths**: The views database uses relative paths, so keep all files together
