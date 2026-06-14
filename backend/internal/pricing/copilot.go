package pricing

const (
	CopilotTokenTypeInput         = "input"
	CopilotTokenTypeOutput        = "output"
	CopilotTokenTypeCacheRead     = "cache_read"
	CopilotTokenTypeCacheCreation = "cache_creation"
	CopilotTokenTypeReasoning     = "reasoning"
)

// CopilotTokenUsage represents token usage from a Copilot GenAI chat span.
type CopilotTokenUsage struct {
	InputTokens         int64
	OutputTokens        int64
	CacheReadTokens     int64
	CacheCreationTokens int64
	ReasoningTokens     int64
}

// NormalizeCopilotModel normalizes model names emitted by Copilot telemetry.
func NormalizeCopilotModel(model string) string {
	return normalizeGitHubModelAlias(model, false)
}

// CalculateCopilotCost calculates Copilot usage cost in USD.
// Returns nil if the model cannot be matched to known Copilot or source-provider pricing.
func CalculateCopilotCost(model string, usage CopilotTokenUsage) *float64 {
	pricing := GetCopilotPricing(model)
	if pricing == nil {
		return nil
	}

	input := max(0, usage.InputTokens)
	output := max(0, usage.OutputTokens)
	cacheRead := max(0, usage.CacheReadTokens)
	cacheCreation := max(0, usage.CacheCreationTokens)
	reasoning := max(0, usage.ReasoningTokens)

	if pricing.LongContextPricing != nil && pricing.LongContextThreshold > 0 && input > pricing.LongContextThreshold {
		pricing = pricing.LongContextPricing
	}

	cacheRead = min(cacheRead, input)
	nonCachedInput := input - cacheRead

	cacheWriteRate := pricing.CacheWriteCostPerToken
	if cacheWriteRate == 0 {
		cacheWriteRate = pricing.InputCostPerToken
	}

	cost := float64(nonCachedInput)*pricing.InputCostPerToken +
		float64(cacheRead)*pricing.CacheReadCostPerToken +
		float64(cacheCreation)*cacheWriteRate +
		float64(output+reasoning)*pricing.OutputCostPerToken

	return &cost
}
