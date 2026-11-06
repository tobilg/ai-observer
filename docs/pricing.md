# Pricing System

AI Observer includes a unified pricing system for calculating costs across Claude, Codex (OpenAI), Gemini (Google), and derived GitHub Copilot metrics. Pricing data is embedded in the binary as JSON files and loaded at startup.

OpenCode is different: AI Observer ingests the native cost metrics emitted by the `@devtheops/opencode-plugin-otel` plugin and does not derive additional OpenCode pricing rows.

## Pricing Data

### Location

Pricing data is stored in `internal/pricing/data/`:

| File | Provider | Models |
|------|----------|--------|
| `claude.json` | Anthropic | 26 models |
| `codex.json` | OpenAI | 80 models |
| `gemini.json` | Google | 32 models |
| `github_copilot.json` | GitHub Copilot | 46 models |
| `github_models_catalog.json` | Final GitHub Models catalog snapshot (the service was retired on July 30, 2026), used for Copilot alias matching | — |

The four price files are refreshed from [models.dev](https://models.dev) with `ai-observer pricing update` (see [Updating Pricing](#updating-pricing)); their `source` field credits it.

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
- `longContext` - Optional higher rates for long prompts: `thresholdInputTokens` plus the same cost fields (see [Long-Context Tiers](#long-context-tiers))

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

### Long-Context Tiers

A model entry can have a `longContext` block with higher rates for requests whose prompt exceeds `thresholdInputTokens`. The whole request is then priced at those rates. Prompt length follows each provider's billing rules:

| Provider | Prompt length | Applied |
|----------|---------------|---------|
| Claude | input + cache reads + cache writes | yes |
| Codex (OpenAI) | input tokens (which include cached tokens) | yes |
| GitHub Copilot | input tokens | yes |
| Gemini | — | no: Gemini costs are computed per token type, without the request's prompt length, so the base rates are used |

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
ai-observer import claude-code --pricing-mode auto      # default
ai-observer import claude-code --pricing-mode calculate
ai-observer import claude-code --pricing-mode display
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
│  │  GeminiParser ─┘             DeriveGeminiCostMetrics() ──►      │        │
│  │                                                                 │        │
│  └──────────────────────────────┬──────────────────────────────────┘        │
│                                 │                                           │
│                                 ▼                                           │
│  ┌─────────────────────────────────────────────────────────────────┐        │
│  │                    pricing package                              │        │
│  │                                                                 │        │
│  │  GetClaudeCostWithMode(mode, model, usage, costUSD) float64     │        │
│  │  CalculateCodexCost(model, input, cached, output) *float64      │        │
│  │  CalculateGeminiCost(model, in, cached, out, thought) *float64  │        │
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

## Current Pricing (November 2026)

Prices are per million tokens on each provider's standard tier, as reviewed against the official pricing pages on 2026-11-05 (models.dev agreed on every model it lists): [Anthropic](https://platform.claude.com/docs/en/about-claude/pricing), [OpenAI](https://developers.openai.com/api/docs/pricing), [Google](https://ai.google.dev/gemini-api/docs/pricing). Retired models keep their last published prices, marked `deprecated`, so older sessions can still be priced. Cache Write is the 5-minute (Anthropic) or 30-minute (OpenAI) cache write price.

### Claude (Anthropic)

| Model | Input/MTok | Output/MTok | Cache Read | Cache Write |
|-------|------------|-------------|------------|-------------|
| claude-fable-5-1 / claude-mythos-5-1 | $10.00 | $50.00 | $0.25 | $12.50 |
| claude-fable-5 / claude-mythos-5 | $10.00 | $50.00 | $1.00 | $12.50 |
| claude-opus-5-5 | $4.00 | $20.00 | $0.20 | $5.00 |
| claude-opus-5 / opus-4-8 / 4-7 / 4-6 / 4-5 | $5.00 | $25.00 | $0.50 | $6.25 |
| claude-sonnet-5-5 | $2.00 | $10.00 | $0.10 | $2.50 |
| claude-sonnet-5 | $2.00 | $10.00 | $0.20 | $2.50 |
| claude-sonnet-4-6 / 4-5 / 4 | $3.00 | $15.00 | $0.30 | $3.75 |
| claude-haiku-5-5 (prompt ≤ 100K) | $0.10 | $0.50 | $0.01 | $0.125 |
| claude-haiku-5-5 (prompt > 100K) | $0.50 | $2.50 | $0.05 | $0.625 |
| claude-haiku-4-5 | $1.00 | $5.00 | $0.10 | $1.25 |
| claude-opus-4-1 / opus-4 (retired) | $15.00 | $75.00 | $1.50 | $18.75 |
| claude-haiku-3-5 (retired) | $0.80 | $4.00 | $0.08 | $1.00 |

### Codex (OpenAI)

Models marked * have higher rates for prompts over 272K tokens (second price).

| Model | Input/MTok | Output/MTok | Cache Read | Cache Write |
|-------|------------|-------------|------------|-------------|
| gpt-6-astra* | $10.00 / $20.00 | $50.00 / $75.00 | $1.00 / $2.00 | $12.50 / $25.00 |
| gpt-6.1-sol* | $2.00 / $4.00 | $10.00 / $15.00 | $0.10 / $0.20 | $2.50 / $5.00 |
| gpt-6.1-terra* | $1.00 / $2.00 | $5.00 / $7.50 | $0.05 / $0.10 | $1.25 / $2.50 |
| gpt-6-sol* | $2.00 / $4.00 | $10.00 / $15.00 | $0.20 / $0.40 | $2.50 / $5.00 |
| gpt-6-luna* | $0.10 / $0.20 | $0.50 / $0.75 | $0.01 / $0.02 | $0.125 / $0.25 |
| gpt-5.6-sol* | $4.00 / $8.00 | $20.00 / $30.00 | $0.40 / $0.80 | $5.00 / $10.00 |
| gpt-5.6-terra* | $2.00 / $4.00 | $12.00 / $18.00 | $0.20 / $0.40 | $2.50 / $5.00 |
| gpt-5.6-luna* | $0.20 / $0.40 | $1.20 / $1.80 | $0.02 / $0.04 | $0.25 / $0.50 |
| gpt-6-cyber | $20.00 | $100.00 | $2.00 | $25.00 |
| gpt-5.6-cyber / gpt-5.5-cyber | $12.50 | $75.00 | $1.25 | $15.625 (5.6) |
| gpt-6-rosalind* | $15.00 / $30.00 | $75.00 / $112.50 | $1.50 / $3.00 | $18.75 / $37.50 |
| gpt-rosalind-research / gpt-rosalind-discovery | $5.00 | $25.00 | $0.50 | — |
| gpt-6-astra-law (promotional, at least through 2026) | $10.00 | $50.00 | $1.00 | — |
| gpt-5.5* | $5.00 / $10.00 | $30.00 / $45.00 | $0.50 / $1.00 | — |
| gpt-5.5-pro* / gpt-5.4-pro* | $30.00 / $60.00 | $180.00 / $270.00 | — | — |
| gpt-5.4* | $2.50 / $5.00 | $15.00 / $22.50 | $0.25 / $0.50 | — |
| gpt-5.4-mini | $0.75 | $4.50 | $0.075 | — |
| gpt-5.4-nano | $0.20 | $1.25 | $0.02 | — |
| gpt-5.3-codex / gpt-5.2 | $1.75 | $14.00 | $0.175 | — |
| gpt-5.1 / gpt-5 | $1.25 | $10.00 | $0.125 | — |
| gpt-5-mini | $0.25 | $2.00 | $0.025 | — |
| chat-latest | $5.00 | $30.00 | $0.50 | — |
| gpt-realtime-2.1 (text) | $4.00 | $24.00 | $0.40 | — |
| gpt-4.1 | $2.00 | $8.00 | $0.50 | — |
| o3 | $2.00 | $8.00 | $0.50 | — |
| o4-mini (retired) | $1.10 | $4.40 | $0.275 | — |

### Gemini (Google)

Gemini's input tokens include the cached ones (`promptTokenCount` contains `cachedContentTokenCount`), so only the uncached part is charged at the input rate and the cached tokens at the cache read rate; thought tokens are charged at the output rate. Imports price each response with `CalculateGeminiCost`. Over OTLP, the input cost of each export subtracts the cache counter of the same export (`DeriveGeminiCostMetrics`).

Gemini CLI sends cumulative token counters over OTLP. The derived `gemini_cli.cost.usage` keeps each counter's temporality and series attributes (the token type is stored as `token_type`), so cost charts show the cost added per interval, like the token charts. Earlier versions charged cached tokens at the input rate on top of the cache rate, and stored the cost of every running total as new cost; on startup the cost is recomputed once from the stored token points, imported ones included.

| Model | Input/MTok | Output/MTok | Cache Read |
|-------|------------|-------------|------------|
| gemini-4-argon | $2.00 | $10.00 | $0.10 |
| gemini-3.8-flash / gemini-3.6-flash | $0.75 | $3.75 | $0.075 |
| gemini-3.5-flash-lite | $0.30 | $2.50 | $0.03 |
| gemini-omni-1.2-flash / gemini-omni-1.1-flash (text output) | $1.50 | $9.00 | — |
| gemini-3.1-pro-preview (prompt ≤ 200K / > 200K) | $2.00 / $4.00 | $12.00 / $18.00 | $0.20 / $0.40 |
| gemini-3.1-flash-lite | $0.25 | $1.50 | $0.025 |
| gemini-3-flash-preview | $0.50 | $3.00 | $0.05 |
| gemini-2.5-pro (deprecated; prompt ≤ 200K / > 200K) | $1.25 / $2.50 | $10.00 / $15.00 | $0.125 / $0.25 |
| gemini-2.5-flash (deprecated) | $0.30 | $2.50 | $0.03 |
| gemini-2.5-flash-lite | $0.10 | $0.40 | $0.01 |

**Scheduled increases:** Google lists gemini-4-argon at $2.00 / $10.00 / $0.10 through November 30, 2026 and $4.00 / $20.00 / $0.20 from December 1, 2026; gemini-3.8-flash and gemini-3.6-flash at $0.75 / $3.75 / $0.075 through December 31, 2026 and $1.50 / $7.50 / $0.15 from January 1, 2027. From December 1, 2026, Google routes requests to the deprecated gemini-2.5-pro and gemini-2.5-flash to gemini-3.8-flash and gemini-3.5-flash-lite and bills them at those models' rates (gemini-omni-1.1-flash routes to gemini-omni-1.2-flash from December 3, at the same text rates). The tables hold one price per model, so update them when these take effect; sessions imported afterwards are priced at the new rates, and the 7-day freshness check asks for the review.

### GitHub Copilot

`github_copilot.json` holds GitHub's per-token prices for the models in Copilot (models.dev's `github-copilot` provider, which follows GitHub's published Copilot prices). They include models whose providers publish no API price, such as Grok, Kimi K3 and MAI-Code-1.1-Flash. Models models.dev does not list, such as Claude Opus 4.8 (fast mode) and Claude Sonnet 4, keep the prices of GitHub's own table from 2026-11-05. Models GitHub has retired stay in the file, so older telemetry keeps its cost.

## Overriding Prices

Set `AI_OBSERVER_PRICING_DIR` to a directory with any of `claude.json`, `codex.json`, `gemini.json` and `github_copilot.json`, in the same format as the built-in files (see [JSON Schema](#json-schema)). A model in an override file replaces the built-in model of the same name, new models are added, and their aliases are registered. `ai-observer pricing update` writes current versions of all four files there (see [Updating Pricing](#updating-pricing)). This lets you price a new model before the next release:

```bash
mkdir -p ~/.config/ai-observer/pricing
cat > ~/.config/ai-observer/pricing/claude.json <<'JSON'
{
  "provider": "anthropic",
  "models": {
    "claude-new-model": { "aliases": ["new-model"], "inputCostPerMTok": 3, "outputCostPerMTok": 15, "cacheReadCostPerMTok": 0.3, "cacheWriteCostPerMTok": 3.75 }
  }
}
JSON
AI_OBSERVER_PRICING_DIR=~/.config/ai-observer/pricing ai-observer
```

The overrides are read once at startup; an invalid file stops startup with an error. Costs are calculated when telemetry is stored, so costs that are already stored keep the prices they were calculated with. To recalculate imported sessions, re-import them with `ai-observer import <tool> --force`, which replaces their stored records (files imported by earlier versions need `--purge` once).

## Updating Pricing

None of the providers publishes its prices through an API: Anthropic's, OpenAI's and Google's model APIs return no prices. `ai-observer pricing update` takes them from [models.dev](https://models.dev) (`https://models.dev/api.json`), an open database of AI models and their per-token prices maintained by the OpenCode developers ([MIT License](https://github.com/anomalyco/models.dev)). It downloads the catalog once and merges the providers `anthropic`, `openai`, `google` and `github-copilot` into `claude.json`, `codex.json`, `gemini.json` and `github_copilot.json`:

- Models matched by key or alias get the models.dev prices (input, output, cache read, cache write, and a context tier as `longContext`); their key and aliases stay.
- New models are added after the existing ones, sorted by ID. Only models with a text output price are taken; image, speech and embedding models are not. For image models (such as `gemini-3-pro-image`) the output price is the image output price.
- Models models.dev marks deprecated become `deprecated`; deprecation is never undone. Models models.dev does not list keep their prices, so a model can still be priced by hand.
- A price change of more than 10x is refused and reported, since it is more likely an error (for example a price per token instead of per million) than a price change; `--allow-large-changes` applies it.
- `lastUpdated` becomes today. The files keep the built-in layout, so an update only changes the lines of changed prices.

Nothing is written when the download fails, a provider is missing or has no priced model, or a current file is not valid JSON.

```bash
# Use current prices without a new release: AI Observer reads them at startup
AI_OBSERVER_PRICING_DIR=~/.config/ai-observer/pricing ai-observer pricing update

# Refresh the built-in prices (maintainers), review the diff, then rebuild
make pricing-update
```

`--output DIR` writes to another directory than `AI_OBSERVER_PRICING_DIR`. The command does not load the pricing overrides first. Stored costs keep the prices they were calculated with.

models.dev mirrors the providers' pricing pages and can lag behind a price change. Changes scheduled for a date (see the Gemini notes above) appear once models.dev records them. A model can be priced by hand in its file or in an override file; an update keeps a model models.dev does not list, but overwrites one it lists.

### GitHub Models Catalog

`github_models_catalog.json` maps names from the GitHub Models catalog (for example `OpenAI GPT-4.1`) to priced models. GitHub retired GitHub Models, its catalog API included, on July 30, 2026, so this snapshot is final and is not refreshed.

### Freshness Check

`scripts/check-pricing-age.sh [DAYS]` (`make check-pricing`) fails when a price file in `internal/pricing/data/` has a `lastUpdated` more than 7 days (or `DAYS`) ago, or none; the frozen GitHub Models catalog is skipped. Stale files are cleared with `make pricing-update` and a review of its diff. The `Pricing freshness` workflow (`.github/workflows/pricing-freshness.yml`) runs it daily, on demand and on pull requests that change the pricing data; a failed run annotates each stale file. GitHub notifies about failed scheduled runs (the user who last changed the schedule, when failed-workflow notifications are enabled). Scheduled workflows run only on the default branch, and GitHub disables them in public repositories after 60 days without activity.

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
func CalculateGeminiCost(model string, inputTokens, cachedTokens, outputTokens, thoughtTokens int64) *float64
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
