package pricing

import "strings"

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
// Returns nil if the model is not in the pricing table.
func CalculateCodexCost(model string, inputTokens, cachedTokens, outputTokens int64) *float64 {
	pricing := GetCodexPricing(model)
	if pricing == nil {
		return nil
	}

	// Clamp values to non-negative
	input := max(0, inputTokens)
	cached := max(0, cachedTokens)
	output := max(0, outputTokens)

	// Cached tokens can't exceed input tokens
	cached = min(cached, input)

	// Non-cached input tokens
	nonCached := input - cached

	// Calculate cost
	cost := float64(nonCached)*pricing.InputCostPerToken +
		float64(cached)*pricing.CacheReadCostPerToken +
		float64(output)*pricing.OutputCostPerToken

	return &cost
}
