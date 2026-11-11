package otlp

import (
	"maps"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/attrval"
	"github.com/tobilg/ai-observer/internal/pricing"
)

// Codex token types (pricing.CodexTokenType*)
const (
	TokenTypeInput      = pricing.CodexTokenTypeInput
	TokenTypeOutput     = pricing.CodexTokenTypeOutput
	TokenTypeCacheRead  = pricing.CodexTokenTypeCacheRead
	TokenTypeCacheWrite = pricing.CodexTokenTypeCacheWrite
	TokenTypeReasoning  = pricing.CodexTokenTypeReasoning
)

// Metric names
const (
	CodexTokenUsageMetric = "codex_cli_rs.token.usage"
	CodexCostUsageMetric  = "codex_cli_rs.cost.usage"
)

// ExtractCodexMetrics extracts token usage and cost metrics from a
// codex.sse_event log record of any Codex service (codex_cli_rs,
// codex-app-server). Returns nil if the event is not a response.completed
// event or has no token data. tool_token_count is not read: Codex sends its
// total_tokens (input plus output) in it.
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

	inputTokens := int64(attrval.Int(logAttrs, "input_token_count"))
	outputTokens := int64(attrval.Int(logAttrs, "output_token_count"))
	cachedTokens := int64(attrval.Int(logAttrs, "cached_token_count"))
	cacheWriteTokens := int64(attrval.Int(logAttrs, "cache_write_token_count"))
	reasoningTokens := int64(attrval.Int(logAttrs, "reasoning_token_count"))
	if inputTokens == 0 && outputTokens == 0 && cachedTokens == 0 && cacheWriteTokens == 0 && reasoningTokens == 0 {
		return nil
	}

	model := logAttrs["model"]
	if model == "" {
		model = resourceAttrs["model"]
	}
	if model == "" {
		model = "unknown"
	}

	// originator tells the Codex app (Codex_Desktop) from the CLI
	// (codex-tui), which share the app server's service name
	baseAttrs := map[string]string{"model": model}
	if originator := logAttrs["originator"]; originator != "" {
		baseAttrs["originator"] = originator
	}

	var metrics []api.MetricDataPoint
	for _, t := range []struct {
		tokenType string
		value     int64
	}{
		{TokenTypeInput, inputTokens},
		{TokenTypeOutput, outputTokens},
		{TokenTypeCacheRead, cachedTokens},
		{TokenTypeCacheWrite, cacheWriteTokens},
		{TokenTypeReasoning, reasoningTokens},
	} {
		if t.value <= 0 {
			continue
		}
		attrs := maps.Clone(baseAttrs)
		attrs["type"] = t.tokenType
		value := float64(t.value)
		metrics = append(metrics, api.MetricDataPoint{
			Timestamp:          timestamp,
			ServiceName:        serviceName,
			MetricName:         CodexTokenUsageMetric,
			MetricDescription:  "Number of tokens consumed by Codex",
			MetricUnit:         "tokens",
			ResourceAttributes: resourceAttrs,
			Attributes:         attrs,
			MetricType:         "sum",
			Value:              &value,
		})
	}

	// Calculate and add cost metric if model is known
	if cost := CalculateCodexCost(model, inputTokens, cachedTokens, cacheWriteTokens, outputTokens); cost != nil {
		metrics = append(metrics, api.MetricDataPoint{
			Timestamp:          timestamp,
			ServiceName:        serviceName,
			MetricName:         CodexCostUsageMetric,
			MetricDescription:  "Total cost in USD for Codex usage",
			MetricUnit:         "USD",
			ResourceAttributes: resourceAttrs,
			Attributes:         maps.Clone(baseAttrs),
			MetricType:         "sum",
			Value:              cost,
		})
	}

	return metrics
}

// CalculateCodexCost calculates the cost in USD for Codex token usage.
// Returns nil if the model is not in the pricing table.
// Delegates to the pricing package for actual calculation.
func CalculateCodexCost(model string, inputTokens, cachedTokens, cacheWriteTokens, outputTokens int64) *float64 {
	return pricing.CalculateCodexCost(model, inputTokens, cachedTokens, cacheWriteTokens, outputTokens)
}
