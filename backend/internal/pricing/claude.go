package pricing

import (
	"fmt"
	"strings"
)

// PricingMode controls how costs are calculated for Claude Code imports
type PricingMode string

const (
	// PricingModeAuto uses costUSD from JSONL if present, calculates from tokens otherwise
	PricingModeAuto PricingMode = "auto"
	// PricingModeCalculate always calculates from tokens, ignores costUSD
	PricingModeCalculate PricingMode = "calculate"
	// PricingModeDisplay always uses costUSD, returns 0 if missing
	PricingModeDisplay PricingMode = "display"
)

// ParsePricingMode parses a string into a PricingMode
func ParsePricingMode(s string) (PricingMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "auto", "":
		return PricingModeAuto, nil
	case "calculate":
		return PricingModeCalculate, nil
	case "display":
		return PricingModeDisplay, nil
	default:
		return "", fmt.Errorf("invalid pricing mode: %q (valid: auto, calculate, display)", s)
	}
}

// NormalizeClaudeModel normalizes a Claude model name for pricing lookup.
// It handles variations like provider prefixes and common aliases.
func NormalizeClaudeModel(model string) string {
	trimmed := strings.TrimSpace(model)

	// Strip "anthropic/" prefix
	if strings.HasPrefix(trimmed, "anthropic/") {
		trimmed = strings.TrimPrefix(trimmed, "anthropic/")
	}

	return trimmed
}

// ClaudeTokenUsage represents token usage for a Claude API call
type ClaudeTokenUsage struct {
	InputTokens              int64
	OutputTokens             int64
	CacheCreationInputTokens int64
	CacheReadInputTokens     int64
}

// CalculateClaudeCost calculates the cost in USD for Claude token usage.
// Returns nil if the model is not in the pricing table.
func CalculateClaudeCost(model string, usage ClaudeTokenUsage) *float64 {
	pricing := GetClaudePricing(model)
	if pricing == nil {
		return nil
	}

	// Clamp values to non-negative
	input := max(0, usage.InputTokens)
	output := max(0, usage.OutputTokens)
	cacheCreation := max(0, usage.CacheCreationInputTokens)
	cacheRead := max(0, usage.CacheReadInputTokens)

	// Calculate cost
	// Input tokens are the base input minus any cache tokens
	// Cache creation is charged at cache write rate
	// Cache read is charged at cache read rate
	cost := float64(input)*pricing.InputCostPerToken +
		float64(output)*pricing.OutputCostPerToken +
		float64(cacheCreation)*pricing.CacheWriteCostPerToken +
		float64(cacheRead)*pricing.CacheReadCostPerToken

	return &cost
}

// GetClaudeCostWithMode returns the cost based on the pricing mode.
// - auto: Use costUSD if non-nil and > 0, otherwise calculate
// - calculate: Always calculate from tokens
// - display: Always use costUSD (returns 0 if nil)
func GetClaudeCostWithMode(mode PricingMode, model string, usage ClaudeTokenUsage, costUSD *float64) float64 {
	switch mode {
	case PricingModeDisplay:
		if costUSD != nil {
			return *costUSD
		}
		return 0

	case PricingModeCalculate:
		calculated := CalculateClaudeCost(model, usage)
		if calculated != nil {
			return *calculated
		}
		return 0

	case PricingModeAuto:
		fallthrough
	default:
		// Use costUSD if available and positive
		if costUSD != nil && *costUSD > 0 {
			return *costUSD
		}
		// Fall back to calculation
		calculated := CalculateClaudeCost(model, usage)
		if calculated != nil {
			return *calculated
		}
		return 0
	}
}
