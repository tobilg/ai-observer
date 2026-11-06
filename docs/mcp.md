# Agent Access (MCP)

AI Observer can serve its data to AI agents over the [Model Context Protocol](https://modelcontextprotocol.io): an agent such as Claude Code, Codex CLI, Gemini CLI, GitHub Copilot or OpenCode can then search your earlier conversations, read transcripts, and look at logs, traces, metrics, token usage and costs.

The endpoint is `/mcp` on the API port (`http://localhost:8080/mcp`, Streamable HTTP). It is **off by default**.

## Enabling

Either set the environment variable or pass the flag to `serve` or `watch`:

```bash
AI_OBSERVER_MCP=true ai-observer
ai-observer serve --mcp
ai-observer watch all --mcp
```

The server logs `MCP endpoint for agents enabled` with the URL. Without it, `/mcp` answers `404` with a hint how to enable it.

## Connecting an agent

Replace the URL if you changed `AI_OBSERVER_API_PORT` or `AI_OBSERVER_HOST`. With `AI_OBSERVER_AUTH_TOKEN` set, add the token as a bearer header (shown in each example); without it, leave the header out.

**Claude Code**

```bash
claude mcp add --transport http ai-observer http://localhost:8080/mcp \
  --header "Authorization: Bearer $AI_OBSERVER_AUTH_TOKEN"
```

**OpenAI Codex CLI** (`~/.codex/config.toml`)

```toml
[mcp_servers.ai-observer]
url = "http://localhost:8080/mcp"
bearer_token_env_var = "AI_OBSERVER_AUTH_TOKEN"   # only with a token
```

**Gemini CLI** (`~/.gemini/settings.json`)

```json
{
  "mcpServers": {
    "ai-observer": {
      "httpUrl": "http://localhost:8080/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```

**GitHub Copilot in VS Code** (`.vscode/mcp.json` or the user `mcp.json`)

```json
{
  "servers": {
    "ai-observer": {
      "type": "http",
      "url": "http://localhost:8080/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```

**OpenCode** (`opencode.json`)

```json
{
  "mcp": {
    "ai-observer": {
      "type": "remote",
      "url": "http://localhost:8080/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```

Then ask, for example: *"What did I try last week to fix the flaky parser test?"*, *"What did my Claude Code usage cost per repository this month?"*, or *"Show the errors Codex logged today."*

## Tools

All tools are read-only. Time arguments (`from`, `to`) accept an RFC 3339 time (`2026-11-05T10:00:00Z`), a date (`2026-11-05`, UTC; for `to`, the end of that day) or an age relative to now (`30m`, `24h`, `7d`, `2w`). `tool` accepts `claude-code` (or `claude`), `codex`, `gemini`, `opencode`, `copilot-chat`, `github-copilot`, or an OTLP service name. Lists are paged: pass a result's `next_cursor` as `cursor` with the same arguments.

| Tool | What it returns | Main arguments (defaults) |
|------|-----------------|---------------------------|
| `search_conversations` | Sessions with a prompt, reply or tool call that contains all words of the query, in any order, newest match first, with a snippet around the match | `query`, `tool`, `from` (30 days before `to`), `to` (now), `limit` (10, max 50) |
| `list_sessions` | Sessions with activity in a range, most recently active first: tool, model, start, last activity, message count | `tool`, `from` (7 days), `to`, `limit` (20, max 100), `cursor` |
| `get_transcript` | A session's messages in order: role, content, model, tool name, input and output, tokens, cost | `session_id`, `roles` (all), `limit` (50, max 200), `cursor`, `max_chars` per text (2000, max 20000) |
| `usage_summary` | Tokens by type and cost in USD | `from` (7 days), `to`, `group_by`: `tool`, `model`, `day` (UTC) or `repository`, `tool` |
| `search_logs` | Log records, newest first | `query`, `tool`, `severity`, `trace_id`, `from` (24 hours), `to`, `limit` (20, max 100), `cursor` |
| `list_traces` | Traces, newest first: root span, duration, span count, status | `query`, `tool`, `from` (24 hours), `to`, `limit`, `cursor` |
| `get_trace` | A trace's spans as a tree (depth first): name, start, duration, status, attributes | `id`, `kind` (`otel_trace` or `codex_operation`), `max_spans` (200), `max_chars` per attribute (300) |
| `list_metric_names` | Stored metric names | `tool` |
| `query_metrics` | A metric as time buckets or totals per series (service, type, model); cumulative counters become increases | `name`, `tool`, `from` (24 hours), `to`, `aggregate`, `interval` (about 50 buckets) |

Results are JSON (also as structured content with an output schema). Long text is cut and marked (`… [1234 more characters]`, `"truncated": true`), so a single result does not fill the agent's context. Invalid arguments return a tool error that explains what to pass instead.

`usage_summary` reports what AI Observer recorded: sessions that were both imported from files and sent over OTLP count twice, and `repository` is known for imported and watched Claude Code and Codex sessions only (others show `unknown`). `tokens` lists every token type; `total_tokens` counts each token once, so it leaves out the types that another one includes: cached input of Codex, Gemini and GitHub Copilot (part of their input) and Codex reasoning tokens (part of its output).

### Searching conversations

`search_conversations` splits `query` into words at spaces and finds the prompts, replies and tool calls that contain **all** of them, in any order:

| Query | Finds records that contain |
|-------|----------------------------|
| `flaky parser test` | `flaky`, `parser` and `test`, e.g. *"the parser test is flaky on CI"* |
| `retr backoff` | `retry`, `retries` or `retrying`, and `backoff` |
| `"not found" timeout` | the phrase `not found` exactly as written, and `timeout` |
| `ErrOffsetTooLarge` | the identifier, also inside longer text such as `storage.ErrOffsetTooLarge` |

- **Case and word parts:** case is ignored, and a word also matches inside longer words: `retr` finds `retry`, `test` finds `parser_test.go`.
- **Same record:** all words must appear in the same prompt, reply or tool call: in its text or the values of its attributes, such as the command of a tool call, but not in attribute names (`id` does not match every record's `session.id`). A session whose words are spread over different messages does not match; search for fewer words, or for each one separately.
- **Phrases:** text in double quotes is one term and matches only as written, spaces included.
- **Limits:** at most 10 terms; repeated words count once. The result lists the `terms` the query was split into.
- **Text, not meaning:** `flaky` does not find `intermittent`, and `retries` does not find `retry` (the shorter `retr` finds both).
- **Order of results:** sessions with the newest matching record come first. `matches` counts a session's matching records in the range, and `snippet` shows the text around the words in the newest one.

### Transcripts as resources

The transcript of a session is also a resource, `ai-observer://sessions/{session_id}/transcript` (Markdown), for clients that attach resources to a prompt.

## Security

- **What agents see:** the stored prompts, code, tool output and file paths. Whatever a tool returns is sent to the agent's model provider. Enable MCP only when you are comfortable with that, and keep secret redaction on (`AI_OBSERVER_REDACT`, default `secrets`; see [Redaction](redaction.md)). Results are redacted again when they are returned, so telemetry stored before redaction was enabled is covered too.
- **Who can connect:** the endpoint is behind the same checks as the API: the `Host` check (DNS rebinding protection), the `Origin` check, and with `AI_OBSERVER_AUTH_TOKEN` a bearer token. Like the dashboard, it listens on `127.0.0.1` unless `AI_OBSERVER_HOST` says otherwise.
- **No writes, no SQL:** the tools only read, and there is no tool that runs SQL.

## Behavior

- **Transport:** Streamable HTTP, stateless, with JSON responses: no session ID, no server-sent events, and `GET`/`DELETE` on `/mcp` answer `405`.
- **No request timeout:** MCP requests are exempt from the API's 5-second timeout, so a search over a large database can finish. A request ends when the client cancels it or AI Observer shuts down.
- **Search speed:** `search_conversations` scans the text of every session log and span in the range. In a benchmark with 2 million log records over 30 days (on a laptop) it took about 0.2 s for 7 days and 0.5–0.7 s for the default 30 days. Additional words cost little when one of them is rare: longer words are checked first, since they usually are rarer, and the others only in the records that contain it. Three words that each appear in many records took about twice as long as one. Narrow `from`/`to` or pass `tool` for faster searches.
