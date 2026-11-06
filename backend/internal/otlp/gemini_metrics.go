package otlp

import (
	"sort"
	"strconv"
	"strings"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/pricing"
)

// Metric names
const (
	GeminiTokenUsageMetric = "gemini_cli.token.usage"
	GeminiCostUsageMetric  = "gemini_cli.cost.usage"
)

// DeriveGeminiCostMetrics creates the cost metrics of the Gemini token usage
// points among points, one per priced token point.
//
// Gemini's input tokens include the cached ones (promptTokenCount contains
// cachedContentTokenCount), so the input cost covers only the uncached part:
// input minus the cache point of the same export, i.e. the point with the same
// series attributes apart from the type and the same timestamp. Gemini CLI
// exports both in one request.
//
// Prices are per token, so the cost of a cumulative token counter is a
// cumulative cost counter (uncached input is a running total too). The cost
// point keeps the token point's temporality and series attributes (session.id
// separates Gemini processes), so metric series compute per-interval increases
// for both alike. The token type is renamed to token_type: series are split by
// "type", and one cost series per model is wanted.
func DeriveGeminiCostMetrics(points []api.MetricDataPoint) []api.MetricDataPoint {
	cached := make(map[string]float64)
	for _, p := range points {
		if p.MetricName == GeminiTokenUsageMetric && p.Value != nil &&
			p.Attributes["type"] == pricing.GeminiTokenTypeCache {
			cached[geminiExportKey(p)] += *p.Value
		}
	}

	var costs []api.MetricDataPoint
	for _, p := range points {
		if p.MetricName != GeminiTokenUsageMetric || p.Value == nil {
			continue
		}
		tokens := *p.Value
		if p.Attributes["type"] == pricing.GeminiTokenTypeInput {
			tokens = max(tokens-cached[geminiExportKey(p)], 0)
		}
		if cost := geminiCostMetric(p, tokens); cost != nil {
			costs = append(costs, *cost)
		}
	}
	return costs
}

// geminiExportKey identifies the points of one export of one series, apart
// from the token type
func geminiExportKey(p api.MetricDataPoint) string {
	var b strings.Builder
	b.WriteString(p.ServiceName)
	b.WriteByte(0)
	b.WriteString(strconv.FormatInt(p.Timestamp.UnixNano(), 10))
	b.WriteByte(0)
	if p.AggregationTemporality != nil {
		b.WriteString(strconv.Itoa(int(*p.AggregationTemporality)))
	}
	writeAttrsKey(&b, p.ResourceAttributes, "")
	writeAttrsKey(&b, p.Attributes, "type")
	return b.String()
}

func writeAttrsKey(b *strings.Builder, attrs map[string]string, skip string) {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		if k != skip {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteByte(0)
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(attrs[k])
	}
	b.WriteByte(1)
}

// geminiCostMetric returns the cost of tokens of the type of tokenMetric, or
// nil if it has no price (missing attributes, unknown model, tool tokens)
func geminiCostMetric(tokenMetric api.MetricDataPoint, tokens float64) *api.MetricDataPoint {
	model := tokenMetric.Attributes["model"]
	tokenType := tokenMetric.Attributes["type"]
	if model == "" || tokenType == "" {
		return nil
	}

	// No point for zero tokens: a cumulative series continues from its
	// previous point
	cost := pricing.CalculateGeminiCostForTokenType(model, tokenType, int64(tokens))
	if cost == nil {
		return nil
	}

	costAttrs := make(map[string]string, len(tokenMetric.Attributes))
	for k, v := range tokenMetric.Attributes {
		if k != "type" {
			costAttrs[k] = v
		}
	}
	costAttrs["token_type"] = tokenType

	isMonotonic := true
	temporality := int32(1) // DELTA unless the tokens are cumulative
	if tokenMetric.AggregationTemporality != nil {
		temporality = *tokenMetric.AggregationTemporality
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
		MetricType:             "sum",
		Value:                  cost,
		IsMonotonic:            &isMonotonic,
		AggregationTemporality: &temporality,
	}
}
