package otlp

import (
	"strconv"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/pricing"
)

// Token type constants matching claude_code.token.usage
const (
	TokenTypeInput     = "input"
	TokenTypeOutput    = "output"
	TokenTypeCacheRead = "cacheRead"
	TokenTypeReasoning = "reasoning"
	TokenTypeTool      = "tool"
)

// Metric names
const (
	CodexTokenUsageMetric = "codex_cli_rs.token.usage"
	CodexCostUsageMetric  = "codex_cli_rs.cost.usage"
)

// ExtractCodexMetrics extracts token usage and cost metrics from a codex.sse_event log record.
// Returns nil if the event is not a response.completed event or has no token data.
func ExtractCodexMetrics(
	logAttrs map[string]string,
	timestamp time.Time,
	serviceName string,
	resourceAttrs map[string]string,
	traceID string,
	spanID string,
) []api.MetricDataPoint {
	// Only process response.completed events
	eventKind, ok := logAttrs["event.kind"]
	if !ok || eventKind != "response.completed" {
		return nil
	}

	// Extract token counts
	inputTokens := parseIntAttr(logAttrs, "input_token_count")
	outputTokens := parseIntAttr(logAttrs, "output_token_count")
	cachedTokens := parseIntAttr(logAttrs, "cached_token_count")
	reasoningTokens := parseIntAttr(logAttrs, "reasoning_token_count")
	toolTokens := parseIntAttr(logAttrs, "tool_token_count")

	// If no tokens at all, skip
	if inputTokens == 0 && outputTokens == 0 && cachedTokens == 0 && reasoningTokens == 0 && toolTokens == 0 {
		return nil
	}

	// Extract model name
	model := logAttrs["model"]
	if model == "" {
		model = resourceAttrs["model"]
	}
	if model == "" {
		model = "unknown"
	}

	var metrics []api.MetricDataPoint

	// Create base metric attributes
	baseAttrs := map[string]string{
		"model": model,
	}

	// Helper to create a token usage metric data point
	createTokenMetric := func(tokenType string, value int64) api.MetricDataPoint {
		attrs := make(map[string]string)
		for k, v := range baseAttrs {
			attrs[k] = v
		}
		attrs["type"] = tokenType

		floatValue := float64(value)
		metricType := "sum"
		isMonotonic := true
		aggregationTemporality := int32(2) // CUMULATIVE

		return api.MetricDataPoint{
			Timestamp:              timestamp,
			ServiceName:            serviceName,
			MetricName:             CodexTokenUsageMetric,
			MetricDescription:      "Number of tokens consumed by Codex CLI",
			MetricUnit:             "tokens",
			ResourceAttributes:     resourceAttrs,
			Attributes:             attrs,
			MetricType:             metricType,
			Value:                  &floatValue,
			IsMonotonic:            &isMonotonic,
			AggregationTemporality: &aggregationTemporality,
		}
	}

	// Add token metrics for each type with non-zero values
	if inputTokens > 0 {
		metrics = append(metrics, createTokenMetric(TokenTypeInput, inputTokens))
	}
	if outputTokens > 0 {
		metrics = append(metrics, createTokenMetric(TokenTypeOutput, outputTokens))
	}
	if cachedTokens > 0 {
		metrics = append(metrics, createTokenMetric(TokenTypeCacheRead, cachedTokens))
	}
	if reasoningTokens > 0 {
		metrics = append(metrics, createTokenMetric(TokenTypeReasoning, reasoningTokens))
	}
	if toolTokens > 0 {
		metrics = append(metrics, createTokenMetric(TokenTypeTool, toolTokens))
	}

	// Calculate and add cost metric if model is known
	cost := CalculateCodexCost(model, inputTokens, cachedTokens, outputTokens)
	if cost != nil {
		metricType := "sum"
		isMonotonic := true
		aggregationTemporality := int32(2) // CUMULATIVE

		costMetric := api.MetricDataPoint{
			Timestamp:              timestamp,
			ServiceName:            serviceName,
			MetricName:             CodexCostUsageMetric,
			MetricDescription:      "Total cost in USD for Codex CLI usage",
			MetricUnit:             "USD",
			ResourceAttributes:     resourceAttrs,
			Attributes:             baseAttrs,
			MetricType:             metricType,
			Value:                  cost,
			IsMonotonic:            &isMonotonic,
			AggregationTemporality: &aggregationTemporality,
		}
		metrics = append(metrics, costMetric)
	}

	return metrics
}

// CalculateCodexCost calculates the cost in USD for Codex token usage.
// Returns nil if the model is not in the pricing table.
// Delegates to the pricing package for actual calculation.
func CalculateCodexCost(model string, inputTokens, cachedTokens, outputTokens int64) *float64 {
	return pricing.CalculateCodexCost(model, inputTokens, cachedTokens, outputTokens)
}

// parseIntAttr parses an integer attribute from a string map
func parseIntAttr(attrs map[string]string, key string) int64 {
	if val, ok := attrs[key]; ok {
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i
		}
	}
	return 0
}
