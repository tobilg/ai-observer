# Pricing System

AI Observer includes a unified pricing system for calculating costs across Claude, Codex (OpenAI), and Gemini (Google) models. Pricing data is embedded in the binary as JSON files and loaded at startup.

## Pricing Data

### Location

Pricing data is stored in `internal/pricing/data/`:

| File | Provider | Models |
|------|----------|--------|
| `claude.json` | Anthropic | 13 models |
| `codex.json` | OpenAI | 44 models |
| `gemini.json` | Google | 10 models |

### JSON Schema

```json
{
  "provider": "anthropic",
  "lastUpdated": "2026-01-02T00:00:00Z",
  "models": {
    "claude-sonnet-4-5-20250929": {
      "aliases": ["claude-sonnet-4-5"],
      "inputCostPerMTok": 3.0,
      "outputCostPerMTok": 15.0,
      "cacheReadCostPerMTok": 0.3,
      "cacheWriteCostPerMTok": 3.75,
      "deprecated": false
    }
  }
}
```

**Fields:**
- `inputCostPerMTok` - Cost per million input tokens (USD)
- `outputCostPerMTok` - Cost per million output tokens (USD)
- `cacheReadCostPerMTok` - Cost per million cache read tokens (USD)
- `cacheWriteCostPerMTok` - Cost per million cache write tokens (USD)
- `aliases` - Alternative model names that map to this model
- `deprecated` - Whether the model is deprecated

### Conversion

Prices in JSON are stored as per-million-tokens for readability. At startup, they are converted to per-token:

```
perTokenRate = perMTokRate × 1e-6
```

## Cost Calculation

### Formula

```
Cost = (uncached_input × input_rate)
     + (cached_input × cache_read_rate)
     + (cache_write × cache_write_rate)
     + (output × output_rate)
```

Where:
- `uncached_input = input_tokens - cached_tokens`
- All rates are per-token (after conversion from per-MTok)

### Model Normalization

Models are normalized before pricing lookup:

| Provider | Normalization |
|----------|---------------|
| Claude | Strip whitespace |
| Codex | Strip `openai/` prefix, extract base model from variants (e.g., `gpt-5.1-codex-max` → `gpt-5.1`) |
| Gemini | Strip whitespace |

## Pricing Modes (Claude Only)

Claude Code JSONL files include a `costUSD` field with the cost reported by the API. The `--pricing-mode` flag controls how costs are calculated during import:

| Mode | Behavior | Use Case |
|------|----------|----------|
| `auto` (default) | Use `costUSD` if present, calculate otherwise | Balanced approach |
| `calculate` | Always calculate from tokens, ignore `costUSD` | Verify against your pricing expectations |
| `display` | Always use `costUSD`, return $0 if missing | Show exactly what Claude reported |

### Usage

```bash
ai-observer import claude --pricing-mode auto      # default
ai-observer import claude --pricing-mode calculate
ai-observer import claude --pricing-mode display
```

### Decision Flow

```
┌─────────────────────────────────────────────────────────────────┐
│                  Claude Cost Calculation                        │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│  Input: mode, model, token_usage, costUSD                       │
│                           │                                     │
│                           ▼                                     │
│              ┌────────────────────────┐                         │
│              │   What is the mode?    │                         │
│              └────────────┬───────────┘                         │
│                           │                                     │
│         ┌─────────────────┼─────────────────┐                   │
│         ▼                 ▼                 ▼                   │
│    ┌─────────┐       ┌─────────┐       ┌─────────┐              │
│    │  auto   │       │calculate│       │ display │              │
│    └────┬────┘       └────┬────┘       └────┬────┘              │
│         │                 │                 │                   │
│         ▼                 │                 ▼                   │
│   ┌───────────┐           │          ┌───────────┐              │
│   │ costUSD   │           │          │ costUSD   │              │
│   │ present?  │           │          │ present?  │              │
│   └─────┬─────┘           │          └─────┬─────┘              │
│    yes/ \no               │           yes/ \no                  │
│       /   \               │              /   \                  │
│      ▼     ▼              ▼             ▼     ▼                 │
│  ┌───────┐ ┌──────┐   ┌──────┐    ┌───────┐ ┌──────┐            │
│  │return │ │calc  │   │calc  │    │return │ │return│            │
│  │costUSD│ │from  │   │from  │    │costUSD│ │ $0   │            │
│  │       │ │tokens│   │tokens│    │       │ │      │            │
│  └───────┘ └──────┘   └──────┘    └───────┘ └──────┘            │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              STARTUP                                        │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌──────────────────┐     ┌──────────────────┐     ┌──────────────────┐     │
│  │ data/claude.json │     │ data/codex.json  │     │ data/gemini.json │     │
│  └────────┬─────────┘     └────────┬─────────┘     └────────┬─────────┘     │
│           │                        │                        │               │
│           └────────────────────────┼────────────────────────┘               │
│                                    ▼                                        │
│                         ┌─────────────────────┐                             │
│                         │   loader.go init()  │                             │
│                         │  • Parse JSON       │                             │
│                         │  • Convert MTok→tok │                             │
│                         │  • Build alias map  │                             │
│                         └──────────┬──────────┘                             │
│                                    ▼                                        │
│                         ┌─────────────────────┐                             │
│                         │   Global Registry   │                             │
│                         │  • claudeProvider   │                             │
│                         │  • codexProvider    │                             │
│                         │  • geminiProvider   │                             │
│                         └─────────────────────┘                             │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│                           COST CALCULATION                                  │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  ┌─────────────────────────────────────────────────────────────────┐        │
│  │                         Callers                                 │        │
│  │                                                                 │        │
│  │  Import Command              Live OTLP Ingestion                │        │
│  │  ───────────────             ────────────────────               │        │
│  │  ClaudeParser ─┐             OTLP Log Handler ─┐                │        │
│  │  CodexParser ──┼──►          ExtractCodexMetrics() ──►          │        │
│  │  GeminiParser ─┘             DeriveGeminiCostMetric() ──►       │        │
│  │                                                                 │        │
│  └──────────────────────────────┬──────────────────────────────────┘        │
│                                 │                                           │
│                                 ▼                                           │
│  ┌─────────────────────────────────────────────────────────────────┐        │
│  │                    pricing package                              │        │
│  │                                                                 │        │
│  │  GetClaudeCostWithMode(mode, model, usage, costUSD) float64     │        │
│  │  CalculateCodexCost(model, input, cached, output) *float64      │        │
│  │  CalculateGeminiCostForTokenType(model, type, count) *float64   │        │
│  │                                                                 │        │
│  │                           │                                     │        │
│  │                           ▼                                     │        │
│  │  ┌─────────────────────────────────────────────────────────┐    │        │
│  │  │              GetPricing(model)                          │    │        │
│  │  │                                                         │    │        │
│  │  │  1. Normalize model name                                │    │        │
│  │  │  2. Look up in primary map                              │    │        │
│  │  │  3. If not found, check alias map                       │    │        │
│  │  │  4. Return ModelPricing or nil                          │    │        │
│  │  └─────────────────────────────────────────────────────────┘    │        │
│  │                           │                                     │        │
│  │                           ▼                                     │        │
│  │  ┌─────────────────────────────────────────────────────────┐    │        │
│  │  │              ModelPricing                               │    │        │
│  │  │                                                         │    │        │
│  │  │  InputCostPerToken      float64                         │    │        │
│  │  │  OutputCostPerToken     float64                         │    │        │
│  │  │  CacheReadCostPerToken  float64                         │    │        │
│  │  │  CacheWriteCostPerToken float64                         │    │        │
│  │  │  Deprecated             bool                            │    │        │
│  │  └─────────────────────────────────────────────────────────┘    │        │
│  │                                                                 │        │
│  └─────────────────────────────────────────────────────────────────┘        │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

## Current Pricing (January 2026)

### Claude (Anthropic)

| Model | Input/MTok | Output/MTok | Cache Read | Cache Write |
|-------|------------|-------------|------------|-------------|
| claude-opus-4-5-* | $5.00 | $25.00 | $0.50 | $6.25 |
| claude-sonnet-4-5-* | $3.00 | $15.00 | $0.30 | $3.75 |
| claude-haiku-4-5-* | $1.00 | $5.00 | $0.10 | $1.25 |
| claude-opus-4-* | $15.00 | $75.00 | $1.50 | $18.75 |
| claude-sonnet-4-* | $3.00 | $15.00 | $0.30 | $3.75 |
| claude-3-7-sonnet-* | $3.00 | $15.00 | $0.30 | $3.75 |
| claude-haiku-3-5-* | $0.80 | $4.00 | $0.08 | $1.00 |
| claude-3-haiku-* | $0.25 | $1.25 | $0.03 | $0.30 |

### Codex (OpenAI)

| Model | Input/MTok | Output/MTok | Cache Read |
|-------|------------|-------------|------------|
| gpt-5.2 | $1.75 | $14.00 | $0.175 |
| gpt-5.1 / gpt-5 | $1.25 | $10.00 | $0.125 |
| gpt-5-mini | $0.25 | $2.00 | $0.025 |
| gpt-4.1 | $2.00 | $8.00 | $0.50 |
| gpt-4.1-mini | $0.40 | $1.60 | $0.10 |
| gpt-4.1-nano | $0.10 | $0.40 | $0.025 |
| o3 | $2.00 | $8.00 | $0.50 |
| o4-mini | $1.10 | $4.40 | $0.275 |

### Gemini (Google)

| Model | Input/MTok | Output/MTok | Cache Read |
|-------|------------|-------------|------------|
| gemini-3-pro-preview | $2.00 | $12.00 | $0.20 |
| gemini-2.5-pro | $1.25 | $10.00 | $0.125 |
| gemini-2.5-flash | $0.30 | $2.50 | $0.03 |
| gemini-2.0-flash | $0.10 | $0.40 | $0.025 |
| gemini-2.0-flash-lite | $0.075 | $0.30 | — |

## Updating Pricing

To update pricing, edit the JSON files in `internal/pricing/data/` and rebuild:

```bash
# Edit pricing file
vim internal/pricing/data/claude.json

# Rebuild
cd backend && go build -o ../bin/ai-observer ./cmd/server
```

### Future: CLI Update Command

A future `ai-observer pricing update` command is planned to fetch current pricing from provider documentation automatically. The `claude_fetcher.go` file contains preparation for this feature.

## API Reference

### Public Functions

```go
// Get pricing for a model
func GetClaudePricing(model string) *ModelPricing
func GetCodexPricing(model string) *ModelPricing
func GetGeminiPricing(model string) *ModelPricing

// Calculate costs
func GetClaudeCostWithMode(mode PricingMode, model string, usage ClaudeTokenUsage, costUSD *float64) float64
func CalculateCodexCost(model string, inputTokens, cachedTokens, outputTokens int64) *float64
func CalculateGeminiCostForTokenType(model string, tokenType string, tokenCount int64) *float64

// Normalize model names
func NormalizeClaudeModel(model string) string
func NormalizeCodexModel(model string) string
func NormalizeGeminiModel(model string) string
```

### Types

```go
type PricingMode string

const (
    PricingModeAuto      PricingMode = "auto"
    PricingModeCalculate PricingMode = "calculate"
    PricingModeDisplay   PricingMode = "display"
)

type ModelPricing struct {
    InputCostPerToken      float64
    OutputCostPerToken     float64
    CacheReadCostPerToken  float64
    CacheWriteCostPerToken float64
    Deprecated             bool
}

type ClaudeTokenUsage struct {
    InputTokens              int64
    OutputTokens             int64
    CacheCreationInputTokens int64
    CacheReadInputTokens     int64
}
```
