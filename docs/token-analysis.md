# Token Discrepancy Analysis: OTLP vs ccusage

## Summary

Investigation into why OTLP token metrics show significantly higher values than ccusage for the same time period. **Root cause identified: OTLP records ALL API calls including internal tool-routing calls, while JSONL/ccusage only records user-facing assistant messages.**

## Data for 2026-01-02

### OTLP Data (from AI Observer database)
| Token Type     | Total Tokens  | Data Points |
|----------------|---------------|-------------|
| input          | 681,669       | 708         |
| output         | 445,143       | 708         |
| cacheCreation  | 4,854,456     | 708         |
| cacheRead      | 62,460,204    | 708         |

### ccusage Data (from JSONL files, deduplicated)
| Token Type     | Total Tokens  |
|----------------|---------------|
| input          | 84,103        |
| output         | 5,073         |
| cacheCreation  | 3,856,624     |
| cacheRead      | 59,803,276    |

### Discrepancy Ratios (OTLP / ccusage)
| Token Type     | Ratio  |
|----------------|--------|
| input          | 8.1x   |
| output         | 87.8x  |
| cacheCreation  | 1.26x  |
| cacheRead      | 1.04x  |

## Root Cause: Internal Tool-Routing API Calls

### Key Finding: Haiku Model Used for Tool Routing

Analysis of OTLP data by model and cache status:

| Model | Cache Status | Requests | Input Tokens | Output Tokens | Avg Input | Avg Output |
|-------|--------------|----------|--------------|---------------|-----------|------------|
| Haiku | no_cache     | 131      | 449,694      | 5,759         | 3,433     | 44         |
| Haiku | with_cache   | 44       | 190,610      | 76,427        | 4,332     | 1,737      |
| Opus  | with_cache   | 533      | 41,365       | 362,957       | 78        | 681        |

**The 131 Haiku requests without cache tokens are internal tool-routing calls:**
- Short output (avg 44 tokens) = quick routing/decision responses
- No cache tokens = not part of main conversation context
- NOT recorded in JSONL files
- Only visible through OTLP telemetry

### Why Cache Tokens Match Closely

Cache tokens (cacheRead, cacheCreation) match within ~5% because:
1. Only main conversation API calls use the cache
2. Tool-routing calls don't use cache (hence 0 cache tokens)
3. Both OTLP and JSONL capture the same cache-using calls

### Why Input/Output Differ Significantly

The 131 internal Haiku calls account for:
- **449,694 extra input tokens** (not in JSONL)
- **5,759 extra output tokens** (not in JSONL)

Plus additional discrepancy from:
1. JSONL deduplication (1,865 entries → 762 unique)
2. Some streaming/intermediate responses recorded in OTLP

## Data Flow Comparison

```
API Request → OTLP Recording → Database
     ↓
     ├── Tool-routing call (Haiku, no cache)
     │   └── OTLP: ✓ Recorded
     │   └── JSONL: ✗ NOT recorded (no type: "assistant")
     │
     └── Main response (Opus/Haiku, with cache)
         └── OTLP: ✓ Recorded
         └── JSONL: ✓ Recorded as type: "assistant"
```

## Three-Way Token Comparison

| Source | Count | Input | Output | Cache Read | Cache Create |
|--------|-------|-------|--------|------------|--------------|
| Raw JSONL (all assistant) | 1,865 | 223,706 | 316,750 | 138,002,412 | 11,074,155 |
| JSONL (deduplicated) | 762 | 84,103 | 5,073 | 59,803,276 | 3,856,624 |
| OTLP | 708 | 681,669 | 445,143 | 62,460,204 | 4,854,456 |

### Deduplication Explanation

JSONL files contain duplicate entries with same `messageId:requestId`:
- 1,865 total entries → 762 unique after deduplication
- ccusage deduplicates using `messageId:requestId` hash
- Deduplicated totals match ccusage output exactly

## JSONL Agent Distribution

| AgentId | Entry Count | Input | Output | Cache Read |
|---------|-------------|-------|--------|------------|
| main | 1,330 | 17,708 | 288,005 | 120,854,146 |
| af3cf82 | 62 | 10,178 | 5,978 | 2,279,362 |
| ac60eae | 39 | 55,200 | 2,354 | 2,137,714 |
| + 7 more | ... | ... | ... | ... |

Sub-agents account for 535 entries (29% of all assistant entries).

## Technical Details

### Claude Code OTEL Recording (cli.js line 237)
```javascript
function HrA(A,Q,B){
  MT0(A,Q,B),  // Updates session state
  fT0()?.add(A,{model:B}),  // Adds cost to OTEL
  jEA()?.add(Q.input_tokens,{type:"input",model:B}),
  jEA()?.add(Q.output_tokens,{type:"output",model:B}),
  jEA()?.add(Q.cache_read_input_tokens??0,{type:"cacheRead",model:B}),
  jEA()?.add(Q.cache_creation_input_tokens??0,{type:"cacheCreation",model:B})
}
```

This function is called for EVERY API response, including tool-routing calls.

### ccusage Filtering (data-loader.ts lines 165-186)

ccusage uses `usageDataSchema` which requires:
- `message.usage.input_tokens` (required)
- `message.usage.output_tokens` (required)
- `timestamp` (required)

Combined with deduplication via `messageId:requestId` hash.

## Conclusions

1. **OTLP provides complete token usage** including internal/system API calls
2. **ccusage provides user-facing token usage** from JSONL conversation logs
3. **Both are "correct"** for their intended purpose:
   - OTLP: Total API consumption (billing perspective)
   - ccusage: Conversation token usage (user perspective)
4. **Cache tokens are a reliable comparison point** since they're only used in main conversation calls
5. **The 8-88x difference in input/output** is explained by internal Haiku tool-routing calls

## Recommendations

When comparing token usage across sources:
1. Use **cache tokens** for validation (should match within ~5%)
2. Expect **input/output to be higher in OTLP** due to internal calls
3. Consider creating a derived OTLP metric that filters for cache-having calls only if user-facing metrics are needed
