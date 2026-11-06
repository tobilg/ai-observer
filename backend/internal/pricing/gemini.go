package pricing

import "strings"

// Gemini token type constants
const (
	GeminiTokenTypeInput   = "input"
	GeminiTokenTypeOutput  = "output"
	GeminiTokenTypeCache   = "cache"
	GeminiTokenTypeThought = "thought"
	GeminiTokenTypeTool    = "tool"
)

// NormalizeGeminiModel normalizes a Gemini model name for pricing lookup.
func NormalizeGeminiModel(model string) string {
	return strings.TrimSpace(model)
}

// CalculateGeminiCostForTokenType calculates cost for a specific token type.
// Returns nil if the model is not in the pricing table or token count is zero/negative.
func CalculateGeminiCostForTokenType(model string, tokenType string, tokenCount int64) *float64 {
	pricing := GetGeminiPricing(model)
	if pricing == nil {
		return nil
	}

	if tokenCount <= 0 {
		return nil
	}

	var cost float64
	switch tokenType {
	case GeminiTokenTypeInput:
		cost = float64(tokenCount) * pricing.InputCostPerToken
	case GeminiTokenTypeOutput, GeminiTokenTypeThought:
		// Thought tokens are charged at output rate
		cost = float64(tokenCount) * pricing.OutputCostPerToken
	case GeminiTokenTypeCache:
		cost = float64(tokenCount) * pricing.CacheReadCostPerToken
	case GeminiTokenTypeTool:
		// Tool tokens don't have direct cost
		return nil
	default:
		return nil
	}

	return &cost
}

// CalculateGeminiCost calculates the cost of one Gemini response. Gemini's
// input tokens include the cached ones (promptTokenCount contains
// cachedContentTokenCount), so only the uncached part is charged at the input
// rate. Thought tokens are charged at the output rate. Returns nil if the model
// is not in the pricing table.
func CalculateGeminiCost(model string, inputTokens, cachedTokens, outputTokens, thoughtTokens int64) *float64 {
	pricing := GetGeminiPricing(model)
	if pricing == nil {
		return nil
	}

	input := max(0, inputTokens)
	cached := min(max(0, cachedTokens), input)
	output := max(0, outputTokens) + max(0, thoughtTokens)

	cost := float64(input-cached)*pricing.InputCostPerToken +
		float64(cached)*pricing.CacheReadCostPerToken +
		float64(output)*pricing.OutputCostPerToken

	return &cost
}
