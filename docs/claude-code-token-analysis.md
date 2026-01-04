# Claude Code Token Usage: OTLP vs Local JSONL

This document explains the differences between token usage data captured via OpenTelemetry (OTLP) and the local JSONL conversation logs used by tools like ccusage.

## Overview

Claude Code records token usage through two mechanisms:

1. **OTLP Telemetry** - Records ALL API calls including internal system calls
2. **JSONL Conversation Logs** - Records user-facing assistant messages only

These two sources will show different token totals, and both are correct for their intended purpose.

## Token Discrepancy Summary

When comparing the same time period, you may observe:

| Token Type | Typical Ratio (OTLP / JSONL) |
|------------|------------------------------|
| input | 5-10x higher in OTLP |
| output | 10-100x higher in OTLP |
| cacheRead | ~1x (matches closely) |
| cacheCreation | ~1.2x (matches closely) |

## Root Cause: Internal Tool-Routing Calls

Claude Code uses a smaller model (Haiku) for internal tool-routing decisions. These API calls:

- **Are recorded in OTLP** (complete API usage tracking)
- **Are NOT recorded in JSONL** (only `type: "assistant"` messages are logged)
- **Do not use prompt caching** (hence 0 cache tokens)

### Example Data Breakdown

| Model | Type | Requests | Input Tokens | Output Tokens |
|-------|------|----------|--------------|---------------|
| Haiku | Internal (no cache) | 131 | 449,694 | 5,759 |
| Haiku | User-facing (with cache) | 44 | 190,610 | 76,427 |
| Opus | User-facing (with cache) | 533 | 41,365 | 362,957 |

The 131 internal Haiku calls account for the majority of the "extra" tokens visible in OTLP but not in JSONL.

## Why Cache Tokens Match

Cache tokens (cacheRead, cacheCreation) match closely between sources (~5% variance) because:

1. Only main conversation API calls use prompt caching
2. Tool-routing calls don't use the cache (0 cache tokens)
3. Both OTLP and JSONL capture the same cache-using calls

**Cache tokens are a reliable cross-reference point** for validating data between sources.

## JSONL Deduplication

JSONL files may contain duplicate entries with the same `messageId:requestId` combination. Tools like ccusage deduplicate these entries, which further reduces the token counts compared to raw JSONL totals.

Example:
- Raw JSONL entries: 1,865
- After deduplication: 762 unique entries

## Data Flow

```
Claude Code API Request
         │
         ├── Tool-routing call (Haiku, internal)
         │   ├── OTLP: Recorded
         │   └── JSONL: NOT recorded
         │
         └── Main response (Opus/Haiku, user-facing)
             ├── OTLP: Recorded
             └── JSONL: Recorded as type: "assistant"
```

## Which Source to Use

| Use Case | Recommended Source |
|----------|-------------------|
| Billing/cost tracking | OTLP (complete API usage) |
| Conversation analysis | JSONL/ccusage (user-facing only) |
| Debugging token usage | OTLP (full visibility) |
| Usage quotas/limits | OTLP (actual API consumption) |

## Recommendations

### For AI Observer Users

1. **Use cache tokens for validation** - When comparing OTLP data with other sources, cache tokens should match within ~5%

2. **Expect higher input/output in OTLP** - This is normal behavior due to internal tool-routing calls

3. **Filter by cache presence** - To get user-facing metrics only, filter OTLP data to include only records where `cacheRead > 0 OR cacheCreation > 0`

### For Dashboard Design

When displaying token usage:

- **Total API Usage**: Use OTLP totals directly
- **Conversation Usage**: Filter to cache-having records or use JSONL-derived metrics
- **Cost Calculations**: Use OTLP totals (reflects actual billing)

### Query Example

To get user-facing tokens only from OTLP data:

```sql
WITH token_pivot AS (
  SELECT
    Timestamp,
    json_extract_string(Attributes, '$.model') as model,
    MAX(CASE WHEN json_extract_string(Attributes, '$.type') = 'input' THEN Value END) as input_val,
    MAX(CASE WHEN json_extract_string(Attributes, '$.type') = 'cacheRead' THEN Value END) as cache_read_val,
    MAX(CASE WHEN json_extract_string(Attributes, '$.type') = 'cacheCreation' THEN Value END) as cache_create_val
  FROM otel_metrics
  WHERE MetricName = 'claude_code.token.usage'
  GROUP BY Timestamp, model
)
SELECT
  SUM(input_val) as user_facing_input
FROM token_pivot
WHERE cache_read_val > 0 OR cache_create_val > 0;
```

## Technical Reference

### OTLP Token Recording

Claude Code records tokens via OpenTelemetry for every API response:

```javascript
// Simplified from cli.js
function recordTokenUsage(cost, usage, model) {
  otelCost?.add(cost, {model});
  otelTokens?.add(usage.input_tokens, {type: "input", model});
  otelTokens?.add(usage.output_tokens, {type: "output", model});
  otelTokens?.add(usage.cache_read_input_tokens ?? 0, {type: "cacheRead", model});
  otelTokens?.add(usage.cache_creation_input_tokens ?? 0, {type: "cacheCreation", model});
}
```

### JSONL Entry Structure

JSONL conversation logs contain entries with this structure:

```json
{
  "type": "assistant",
  "timestamp": "2026-01-02T12:00:00Z",
  "message": {
    "id": "msg_xxx",
    "model": "claude-opus-4-5-20251101",
    "usage": {
      "input_tokens": 10,
      "output_tokens": 500,
      "cache_read_input_tokens": 50000,
      "cache_creation_input_tokens": 1000
    }
  },
  "requestId": "req_xxx"
}
```

Only entries with `type: "assistant"` and valid `message.usage` are included in conversation logs.
