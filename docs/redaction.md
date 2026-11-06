# Redaction

Prompts, tool inputs and tool outputs that AI coding tools record often contain API keys, tokens and passwords (an `.env` file the agent read, a `curl` command with an `Authorization` header, a database URL). AI Observer replaces them **before telemetry is stored**, for every way telemetry arrives: OTLP, `import` and `watch`.

```bash
AI_OBSERVER_REDACT=secrets   # default: credentials
AI_OBSERVER_REDACT=all       # credentials and personal data
AI_OBSERVER_REDACT=off       # store telemetry as received
```

An unknown value logs a warning and uses the default.

## What is replaced

Replacements name what was replaced, for example `[REDACTED:github_token]`. They are deterministic: the same input always gives the same output, so metric series keep their identity and repeated values stay recognizable.

**Secrets** (modes `secrets` and `all`):

| Kind | Recognized by |
|------|---------------|
| `anthropic_key`, `openai_key` | `sk-ant-…`, `sk-…`, `sk-proj-…` keys |
| `github_token`, `gitlab_token` | `ghp_`, `gho_`, `ghu_`, `ghs_`, `ghr_`, `github_pat_`, `glpat-` tokens |
| `slack_token`, `slack_webhook` | `xoxb-`/`xoxp-`… tokens, `hooks.slack.com` URLs |
| `aws_access_key` | `AKIA…`/`ASIA…` access key IDs |
| `google_api_key` | `AIza…` keys |
| `stripe_key`, `npm_token` | `sk_live_`/`rk_test_`… keys, `npm_` tokens |
| `jwt` | JSON Web Tokens (`eyJ….eyJ….…`) |
| `private_key` | PEM private key blocks |
| `token` | The value of `Authorization` headers (`Bearer`, `Basic` or `Token`), also in `curl` commands and JSON |
| `password` | The password in `scheme://user:password@host`, or a token as the user (`https://<token>@host`) |
| `secret` | The value of assignments to keys such as `password`, `secret`, `api_key`, `access_token`, `client_secret` (`KEY=value`, `key: value`, `"key": "value"`), when the value looks like a credential (letters and digits) |

The key of an assignment and the rest of a URL are kept: `DB_PASSWORD=[REDACTED:secret]`, `postgres://admin:[REDACTED:password]@db:5432/app`, `login?password=[REDACTED:secret]&next=/home`. Code that only names a credential stays: `password = getPassword()`, `apiKey = process.env.API_KEY`. So do the shell's working directories `PWD` and `OLDPWD`.

**Personal data** (mode `all` only):

| Kind | Recognized by |
|------|---------------|
| `email` | Email addresses, replaced by a pseudonym with a short hash (`[REDACTED:email:1a2b3c4d]`), so per-user breakdowns (for example Claude Code's `user.email` attribute) keep working. `git@host:repo` SSH remotes are kept. |
| `card_number` | Payment card numbers with card prefixes, 15 or 16 digits and a valid Luhn checksum |
| `iban` | IBANs with a valid checksum |
| `phone` | Phone numbers in international format (`+49 151 …`, `+1 (555) …`) |
| `us_ssn` | US social security numbers (`123-45-6789`) |
| `ip_address` | Public IPv4 addresses (loopback, private and link-local addresses are kept) |

Names and postal addresses cannot be recognized reliably by patterns and are not redacted.

## What is covered

Log bodies and attributes, span attributes, event and link attributes and status messages, and metric attributes, including resource attributes. Session IDs are read before redaction, so sessions stay complete. Metric names, span names and numeric values are not changed.

## Limits

- **Only new telemetry:** what was stored before redaction was enabled stays as it was. The [MCP tools](mcp.md) redact their results again, so agents do not see it; the dashboard does. To clean up old data, delete it (`ai-observer delete`) and import the session files again.
- **Patterns, not certainty:** a secret in an unknown format (for example a random password without a recognizable key name) is not found, and occasionally ordinary text that looks like a credential is replaced. Treat redaction as a safety net, not a guarantee.
- **Changing the mode while a tool runs:** tools that send cumulative counters (Gemini CLI, GitHub Copilot, OpenCode) identify a series by its attributes. When a new mode replaces a value in a running session's metric attributes (for example `user.email` with `all`), its next points start a new series, whose first point counts the session's total so far once more. Change the mode between sessions.
- **Exports from files:** `ai-observer export --from-files` converts session files directly to Parquet without storing them, so it is not redacted.

## Cost

Redaction runs on every stored string. On a laptop, `secrets` processes about 130 MB of text per second; `all` about 120 MB/s for prose and about 10 MB/s for text dense with numbers (JSON tool output), where the number patterns run. Ingestion of live telemetry is not noticeably affected; a large `import` takes a little longer.
