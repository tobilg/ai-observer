# Import Command

The `ai-observer import` command allows you to import historical session data from local AI coding tool files into the AI Observer database.

## Supported Tools

| Tool | File Location | File Format |
|------|---------------|-------------|
| Claude Code | `~/.claude/projects/**/*.jsonl` | JSONL |
| Codex CLI | `~/.codex/sessions/*.jsonl` | JSONL |
| Gemini CLI | `~/.gemini/tmp/**/session-*.json` | JSON |

## Usage

```bash
# Import from all tools
ai-observer import all

# Import from specific tool
ai-observer import claude-code
ai-observer import codex
ai-observer import gemini

# With options
ai-observer import claude-code --from 2025-01-01 --to 2025-12-31
ai-observer import claude-code --pricing-mode calculate
ai-observer import all --force --dry-run
```

## Options

| Option | Description |
|--------|-------------|
| `--from DATE` | Only import sessions starting from DATE (YYYY-MM-DD) |
| `--to DATE` | Only import sessions up to DATE (YYYY-MM-DD) |
| `--force` | Re-import already imported files |
| `--dry-run` | Show what would be imported without making changes |
| `--skip-confirm` | Skip confirmation prompt |
| `--purge` | Delete existing data in time range before importing |
| `--pricing-mode MODE` | Cost calculation mode for Claude (see [Pricing](pricing.md)) |
| `--verbose` | Show detailed progress |

## Environment Variables

Override default file locations:

| Variable | Description |
|----------|-------------|
| `AI_OBSERVER_CLAUDE_PATH` | Comma-separated list of paths to Claude session directories |
| `AI_OBSERVER_CODEX_PATH` | Path to Codex sessions directory |
| `AI_OBSERVER_GEMINI_PATH` | Path to Gemini data directory |

## Workflow

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         IMPORT COMMAND FLOW                                 │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│   $ ai-observer import [claude|codex|gemini|all] [options]                  │
│                                    │                                        │
│                                    ▼                                        │
│                         ┌─────────────────────┐                             │
│                         │     Importer        │                             │
│                         │ RegisterAllParsers  │                             │
│                         └──────────┬──────────┘                             │
│                                    │                                        │
│           ┌────────────────────────┼────────────────────────┐               │
│           ▼                        ▼                        ▼               │
│  ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐        │
│  │  ClaudeParser   │     │  CodexParser    │     │  GeminiParser   │        │
│  └────────┬────────┘     └────────┬────────┘     └────────┬────────┘        │
│           │                       │                       │                 │
│           ▼                       ▼                       ▼                 │
│  ┌─────────────────┐     ┌─────────────────┐     ┌─────────────────┐        │
│  │ ~/.claude/      │     │ ~/.codex/       │     │ ~/.gemini/tmp/  │        │
│  │ projects/**     │     │ sessions/*.jsonl│     │ **/session-*    │        │
│  │ *.jsonl         │     │                 │     │ .json           │        │
│  └────────┬────────┘     └────────┬────────┘     └────────┬────────┘        │
│           │                       │                       │                 │
│           └───────────────────────┼───────────────────────┘                 │
│                                   ▼                                         │
│                        ┌─────────────────────┐                              │
│                        │  For each file:     │                              │
│                        │  1. Check status    │                              │
│                        │  2. Skip if imported│                              │
│                        │  3. Parse content   │                              │
│                        └──────────┬──────────┘                              │
│                                   │                                         │
│                                   ▼                                         │
│  ┌─────────────────────────────────────────────────────────────────┐        │
│  │                      ParseFile()                                │        │
│  │  • Extract timestamps, session ID, model                        │        │
│  │  • Parse token counts (input, output, cache, reasoning, etc.)   │        │
│  │  • Create LogRecords for events                                 │        │
│  │  • Create MetricDataPoints for tokens and costs                 │        │
│  │  • Calculate costs using pricing package                        │        │
│  └──────────────────────────────┬──────────────────────────────────┘        │
│                                 │                                           │
│                                 ▼                                           │
│                      ┌─────────────────────┐                                │
│                      │    ImportResult     │                                │
│                      │  • Logs[]           │                                │
│                      │  • Metrics[]        │                                │
│                      │  • SessionID        │                                │
│                      │  • FirstTime        │                                │
│                      │  • LastTime         │                                │
│                      │  • RecordCount      │                                │
│                      └──────────┬──────────┘                                │
│                                 │                                           │
│                                 ▼                                           │
│                      ┌─────────────────────┐                                │
│                      │   DuckDB Storage    │                                │
│                      │  • otel_logs        │                                │
│                      │  • otel_metrics     │                                │
│                      │  • import_state     │                                │
│                      └─────────────────────┘                                │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Data Extracted

### Claude Code

**Log events:**
- `claude_code.api_request` - Each API request with model and session info

**Metrics:**
- `claude_code.token.usage` - Token counts by type (input, output, cache_creation, cache_read)
- `claude_code.cost.usage` - Cost in USD per request

### Codex CLI

**Log events:**
- `codex.conversation_starts` - Session start with model and CLI version
- `codex.user_message` - User prompts
- `codex.agent_message` - Agent responses

**Metrics:**
- `codex_cli_rs.token.usage` - Token counts by type (input, output, cache_creation, cache_read, reasoning, tool)
- `codex_cli_rs.cost.usage` - Cost in USD per token count event

### Gemini CLI

**Log events:**
- `gemini_cli.user` - User prompts
- `gemini_cli.gemini` - API responses
- `gemini_cli.error` - Errors
- `gemini_cli.warning` - Warnings
- `gemini_cli.info` - Info messages

**Metrics:**
- `gemini_cli.token.usage` - Token counts by type (input, output, cached, thoughts, tool)
- `gemini_cli.cost.usage` - Cost in USD per response

## Import State Tracking

The importer tracks which files have been imported to avoid duplicates:

- Files are identified by path and content hash
- Already-imported files are skipped unless `--force` is used
- Modified files (same path, different hash) are re-imported
- State is stored in the `import_state` table

## Example Output

```
$ ai-observer import all

Import Summary
==============

Data to IMPORT (from files):

  [claude]
    Files: 45 total (3 new, 1 modified, 41 skipped)
    Logs:    156
    Metrics: 624

  [codex]
    Files: 12 total (2 new, 0 modified, 10 skipped)
    Logs:    89
    Metrics: 234

  [gemini]
    Files: 8 total (1 new, 0 modified, 7 skipped)
    Logs:    42
    Metrics: 168

  Total:
    Files: 6 new, 1 modified
    Logs:    287
    Metrics: 1026

Continue? [y/N] y

Importing data...
[claude] Imported 4 files
[codex] Imported 2 files
[gemini] Imported 1 files

Import complete.
```
