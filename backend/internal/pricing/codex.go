package pricing

import "strings"

// Codex token types (attribute "type" of codex_cli_rs.token.usage), for
// OTLP and imports alike. Input includes the cached input and the cache
// writes, output the reasoning, as in OpenAI's usage.
const (
	CodexTokenTypeInput      = "input"
	CodexTokenTypeOutput     = "output"
	CodexTokenTypeCacheRead  = "cacheRead"
	CodexTokenTypeCacheWrite = "cacheWrite"
	CodexTokenTypeReasoning  = "reasoning"
)

// NormalizeCodexModel normalizes a Codex model name for pricing lookup.
// It strips provider prefixes like "openai/".
func NormalizeCodexModel(model string) string {
	trimmed := strings.TrimSpace(model)

	// Strip "openai/" prefix
	if strings.HasPrefix(trimmed, "openai/") {
		trimmed = strings.TrimPrefix(trimmed, "openai/")
	}

	return trimmed
}

// CalculateCodexCost calculates the cost in USD for Codex token usage.
// OpenAI's input tokens include the cached ones and the cache writes, which
// are charged at their own rates (cache writes at the input rate when the
// model has no cache write price). Returns nil if the model is not in the
// pricing table.
func CalculateCodexCost(model string, inputTokens, cachedTokens, cacheWriteTokens, outputTokens int64) *float64 {
	pricing := GetCodexPricing(model)
	if pricing == nil {
		return nil
	}

	// Clamp values to non-negative
	input := max(0, inputTokens)
	cached := max(0, cachedTokens)
	cacheWrite := max(0, cacheWriteTokens)
	output := max(0, outputTokens)

	// OpenAI input tokens include cached tokens, so input is the prompt length
	pricing = pricing.forPromptSize(input)

	// Cached and cache write tokens can't exceed input tokens
	cached = min(cached, input)
	cacheWrite = min(cacheWrite, input-cached)

	// Input tokens neither read from nor written to the cache
	uncached := input - cached - cacheWrite

	cacheWriteRate := pricing.CacheWriteCostPerToken
	if cacheWriteRate == 0 {
		cacheWriteRate = pricing.InputCostPerToken
	}

	cost := float64(uncached)*pricing.InputCostPerToken +
		float64(cached)*pricing.CacheReadCostPerToken +
		float64(cacheWrite)*cacheWriteRate +
		float64(output)*pricing.OutputCostPerToken

	return &cost
}
