package otlp

import (
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/pricing"
)

// Metric names
const (
	GeminiTokenUsageMetric = "gemini_cli.token.usage"
	GeminiCostUsageMetric  = "gemini_cli.cost.usage"
)

// CalculateGeminiCostForTokenType calculates cost for a specific token type.
// Returns nil if the model is not in the pricing table or token count is zero/negative.
// Delegates to the pricing package for actual calculation.
func CalculateGeminiCostForTokenType(model string, tokenType string, tokenCount int64) *float64 {
	return pricing.CalculateGeminiCostForTokenType(model, tokenType, tokenCount)
}

// DeriveGeminiCostMetric creates a cost metric from a token usage metric.
// Returns nil if cost cannot be calculated (wrong metric name, missing attributes, unknown model, etc.).
func DeriveGeminiCostMetric(tokenMetric api.MetricDataPoint) *api.MetricDataPoint {
	// Only process gemini_cli.token.usage metrics
	if tokenMetric.MetricName != GeminiTokenUsageMetric {
		return nil
	}

	// Extract model and type from attributes
	model := tokenMetric.Attributes["model"]
	tokenType := tokenMetric.Attributes["type"]
	if model == "" || tokenType == "" {
		return nil
	}

	// Get token count from Value
	if tokenMetric.Value == nil {
		return nil
	}
	tokenCount := int64(*tokenMetric.Value)

	// Calculate cost
	cost := CalculateGeminiCostForTokenType(model, tokenType, tokenCount)
	if cost == nil {
		return nil
	}

	// Create cost metric
	metricType := "sum"
	isMonotonic := true
	aggregationTemporality := int32(1) // DELTA - each cost is per-event, not cumulative

	costAttrs := map[string]string{
		"model": model,
	}

	return &api.MetricDataPoint{
		Timestamp:              tokenMetric.Timestamp,
		ServiceName:            tokenMetric.ServiceName,
		MetricName:             GeminiCostUsageMetric,
		MetricDescription:      "Total cost in USD for Gemini CLI usage",
		MetricUnit:             "USD",
		ResourceAttributes:     tokenMetric.ResourceAttributes,
		ScopeName:              tokenMetric.ScopeName,
		ScopeVersion:           tokenMetric.ScopeVersion,
		Attributes:             costAttrs,
		MetricType:             metricType,
		Value:                  cost,
		IsMonotonic:            &isMonotonic,
		AggregationTemporality: &aggregationTemporality,
	}
}
