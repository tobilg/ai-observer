# Import Command

The `ai-observer import` command allows you to import historical session data from local AI coding tool files into the AI Observer database.

## Supported Tools

| Tool | File Location | File Format |
|------|---------------|-------------|
| Claude Code | `~/.claude/projects/**/*.jsonl` | JSONL |
| Codex CLI | `~/.codex/sessions/*.jsonl` | JSONL |
| Gemini CLI | `~/.gemini/tmp/**/session-*.json` | JSON |

GitHub Copilot and OpenCode are OTLP-only in AI Observer. They do not have local file import parsers; run `ai-observer serve` and use their OTLP setup instead.

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

## Claude Code activity metrics

Full `import claude-code` reconstructs selected activity metrics from JSONL records. The incremental `watch` command continues to import transcripts and token/cost usage only. Imported activity is historical reconstruction, not a complete copy of Claude Code's live OpenTelemetry counters.

| Metric | Reconstruction |
|--------|----------------|
| `claude_code.lines_of_code.count` | Successful `Edit`, `Write`, and `MultiEdit` calls matched to their tool results by tool ID. Recorded patches provide additions and deletions without unchanged context. Otherwise, single replacements use a line diff; file overwrites require the original contents. A trailing newline does not add an extra line. |
| `claude_code.commit.count` | Successful Bash results containing Git commit completion headers, paired with parsed `git commit` commands, including `git -C` and `git -c`. Each distinct completion hash in a result counts once. Session commands are parsed, never executed. |
| `claude_code.pull_request.count` | One event per distinct repository and PR number found in structured `pr-link` entries within each file, timestamped at the link. Linking an existing PR is not proof of creation; `reconstruction=pr_link` identifies this proxy. |
| `claude_code.session.count` | One event per non-empty parsed session file, at its first activity. This counts imported files, including separate agent files, rather than CLI launches or resumes. |
| `claude_code.active_time.total` | Recorded positive `turn_duration` values converted from milliseconds to seconds, with `type=cli`, unit `s`, and `reconstruction=turn_duration`. User typing/reading time is unavailable; recorded turn duration is an approximation of CLI active time. |

Tool activity without a matching successful result is omitted. LOC is also omitted for overwrites without original contents or a patch, `replace_all` without a patch, and unusually large fallback diffs (over four million line comparisons after shared edges are removed). Commit aliases, wrappers, dynamically constructed commands, quiet commits, and unrecognized/localized completion output may be omitted. These omissions favor reporting supported activity over inventing counts from incomplete records.

Imported metrics carry `import_source=local_jsonl`. Avoid summing overlapping imports and live telemetry as if they were independent activity. The normal import state skips unchanged files; forcing an import does not deduplicate against existing metrics. Use the existing `--purge` workflow when replacing previously imported data.

## Repository attribution in import and watch

Both full imports and incremental `watch` attach `repository` to Claude Code and Codex token/cost metrics and logs when the session provides enough evidence. This includes Codex session-start, user/agent event, transcript, tool-call, tool-result, and reasoning logs. The first recorded Git branch is attached as `git_branch` when available.

Repository selection uses this order:

1. Codex's recorded `session_meta.git.repository_url`.
2. The local working tree's `origin` remote, including Git worktrees and Claude's recorded original working directory.
3. An unambiguous repository candidate whose name matches the working directory. An exact directory-name match takes precedence over a matching ancestor path segment.
4. The working directory's name. If no working directory is known, a single distinct candidate can identify the repository; multiple candidates leave it unset.

Candidates come from structured Claude `pr-link` entries and references in user/assistant message text, text tool results, and supported shell tool inputs (`Bash`, `exec_command`, `shell_command`, and `shell`). Mining recognizes GitHub repository/PR URLs and literal `gh pr ... --repo OWNER/REPO` or `-R` arguments, including quoted values and `--repo=OWNER/REPO`. Codex string tool outputs and user/agent event text are included. System/developer messages, reasoning summaries, arbitrary tool arguments, and structured Codex output objects are excluded. Markdown/prose can supply URLs; command flags are extracted only when the text parses as a shell command.

Mined examples such as `owner/repo`, lookalike GitHub hosts, and dynamically constructed repository names are ignored. References are fallback evidence: they cannot replace a recorded Git URL or local remote. Conflicting owners for a matching repository name fall back to the directory name. Text references never create PR or commit activity metrics. A structured PR in another repository keeps that repository on its own activity metric without overriding session attribution.

Full import can use evidence anywhere in a file. Watch mode uses evidence available through the end of each parsed batch and saves that context, including a discovered local remote, across restarts. A reference arriving later can improve subsequent batches; previously stored records are not rewritten. When upgrading older watch state or starting after a full import/without backfill, the watcher reads the already-skipped prefix once to recover context and missing token baselines without emitting its old records. Existing database records are not automatically backfilled with new attributes.

On another machine or without Git installed, attribution may fall back to matching references or the directory name. Repository identifiers contain the normalized owner/name, not credentials from Git URLs. The activity-metric differences between full import and watch described above still apply.

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
- `transcript.message` - User and assistant content, including tool calls and results

**Metrics:**

- `claude_code.token.usage` - Token counts by type (input, output, cache_creation, cache_read)
- `claude_code.cost.usage` - Cost in USD per request
- `claude_code.token.usage_user_facing` - Token counts for user-facing API calls
- `claude_code.cost.usage_user_facing` - Cost for user-facing API calls
- `claude_code.lines_of_code.count` - Lines added/deleted from supported successful edits and writes
- `claude_code.commit.count` - Commits confirmed by supported successful Git commit results
- `claude_code.pull_request.count` - Distinct repository/PR links per file, not proof of PR creation
- `claude_code.session.count` - One event per non-empty parsed session file, including separate agent files
- `claude_code.active_time.total` - Positive recorded turn durations converted to seconds

The five activity metrics are reconstructed by full imports only. See [Claude Code activity metrics](#claude-code-activity-metrics) for evidence requirements, omissions, and overlapping import/live-data limits. Logs and token/cost metrics receive `repository` and `git_branch` when available, as described in [repository attribution](#repository-attribution-in-import-and-watch).

### Codex CLI

**Log events:**

- `codex.conversation_starts` - Session start with model and CLI version
- `codex.user_message` - User prompts
- `codex.agent_message` - Agent responses
- `transcript.message` - Conversation messages, tool calls, tool results, and reasoning summaries

**Metrics:**

- `codex_cli_rs.token.usage` - Token counts by type (input, output, cache_creation, cache_read, reasoning, tool)
- `codex_cli_rs.cost.usage` - Cost in USD per token count event

All these log categories and token/cost metrics receive `repository` and `git_branch` when available in both import and watch mode. Reasoning logs receive attribution, but their contents are not used to infer the repository.

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
