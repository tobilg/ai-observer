package otlp

import (
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/pricing"
)

const (
	CopilotVSCodeServiceName = "copilot-chat"
	CopilotCLIServiceName    = "github-copilot"

	CopilotTokenUsageMetric = "github_copilot.token.usage"
	CopilotCostUsageMetric  = "github_copilot.cost.usage"
)

// DeriveCopilotMetricsFromSpans creates AI Observer token and cost metrics from
// Copilot GenAI chat spans. Raw upstream spans and metrics are stored separately.
func DeriveCopilotMetricsFromSpans(spans []api.Span) []api.MetricDataPoint {
	var metrics []api.MetricDataPoint

	for _, span := range spans {
		if !IsCopilotService(span.ServiceName) || strings.ToLower(span.SpanAttributes["gen_ai.operation.name"]) != "chat" {
			continue
		}

		model := span.SpanAttributes["gen_ai.response.model"]
		if model == "" {
			model = span.SpanAttributes["gen_ai.request.model"]
		}
		if model == "" {
			model = "unknown"
		}

		usage := pricing.CopilotTokenUsage{
			InputTokens:         parseInt64Attr(span.SpanAttributes, "gen_ai.usage.input_tokens"),
			OutputTokens:        parseInt64Attr(span.SpanAttributes, "gen_ai.usage.output_tokens"),
			CacheReadTokens:     parseInt64Attr(span.SpanAttributes, "gen_ai.usage.cache_read.input_tokens"),
			CacheCreationTokens: parseInt64Attr(span.SpanAttributes, "gen_ai.usage.cache_creation.input_tokens"),
			ReasoningTokens: firstNonZeroInt64(
				parseInt64Attr(span.SpanAttributes, "gen_ai.usage.reasoning.output_tokens"),
				parseInt64Attr(span.SpanAttributes, "gen_ai.usage.reasoning_tokens"),
			),
		}

		metrics = appendTokenMetric(metrics, span, model, pricing.CopilotTokenTypeInput, usage.InputTokens)
		metrics = appendTokenMetric(metrics, span, model, pricing.CopilotTokenTypeOutput, usage.OutputTokens)
		metrics = appendTokenMetric(metrics, span, model, pricing.CopilotTokenTypeCacheRead, usage.CacheReadTokens)
		metrics = appendTokenMetric(metrics, span, model, pricing.CopilotTokenTypeCacheCreation, usage.CacheCreationTokens)
		metrics = appendTokenMetric(metrics, span, model, pricing.CopilotTokenTypeReasoning, usage.ReasoningTokens)

		if cost := pricing.CalculateCopilotCost(model, usage); cost != nil && *cost > 0 {
			metrics = append(metrics, createCopilotCostMetric(span.Timestamp, span.ServiceName, span.ResourceAttributes, model, *cost))
		}
	}

	return metrics
}

// IsCopilotService reports whether an OTLP service name belongs to GitHub Copilot.
func IsCopilotService(serviceName string) bool {
	return serviceName == CopilotVSCodeServiceName || serviceName == CopilotCLIServiceName
}

func appendTokenMetric(metrics []api.MetricDataPoint, span api.Span, model, tokenType string, value int64) []api.MetricDataPoint {
	if value <= 0 {
		return metrics
	}
	floatValue := float64(value)
	return append(metrics, api.MetricDataPoint{
		Timestamp:          span.Timestamp,
		ServiceName:        span.ServiceName,
		MetricName:         CopilotTokenUsageMetric,
		MetricDescription:  "Number of tokens consumed by GitHub Copilot",
		MetricUnit:         "tokens",
		ResourceAttributes: span.ResourceAttributes,
		Attributes: map[string]string{
			"type":  tokenType,
			"model": model,
		},
		MetricType: "sum",
		Value:      &floatValue,
	})
}

func createCopilotCostMetric(ts time.Time, serviceName string, resourceAttrs map[string]string, model string, cost float64) api.MetricDataPoint {
	return api.MetricDataPoint{
		Timestamp:          ts,
		ServiceName:        serviceName,
		MetricName:         CopilotCostUsageMetric,
		MetricDescription:  "Total cost in USD for GitHub Copilot usage",
		MetricUnit:         "USD",
		ResourceAttributes: resourceAttrs,
		Attributes: map[string]string{
			"model": model,
		},
		MetricType: "sum",
		Value:      &cost,
	}
}

func parseInt64Attr(attrs map[string]string, key string) int64 {
	val := strings.TrimSpace(attrs[key])
	if val == "" {
		return 0
	}
	if parsed, err := strconv.ParseInt(val, 10, 64); err == nil {
		return parsed
	}
	if parsed, err := strconv.ParseFloat(val, 64); err == nil {
		return int64(parsed)
	}
	return 0
}

func firstNonZeroInt64(values ...int64) int64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
