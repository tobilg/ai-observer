package otlp

import (
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/pricing"
)

func TestCalculateGeminiCostForTokenType_KnownModels(t *testing.T) {
	tests := []struct {
		model        string
		tokenType    string
		tokenCount   int64
		expectedCost float64
	}{
		// gemini-2.5-flash: input=0.3e-6, output=2.5e-6, cache=0.03e-6
		{"gemini-2.5-flash", "input", 1000, 1000 * 0.3e-6},
		{"gemini-2.5-flash", "output", 1000, 1000 * 2.5e-6},
		{"gemini-2.5-flash", "cache", 1000, 1000 * 0.03e-6},
		{"gemini-2.5-flash", "thought", 1000, 1000 * 2.5e-6}, // thought uses output pricing
		// gemini-2.5-pro: input=1.25e-6, output=10e-6, cache=0.125e-6
		{"gemini-2.5-pro", "input", 1000, 1000 * 1.25e-6},
		{"gemini-2.5-pro", "output", 1000, 1000 * 10e-6},
		{"gemini-2.5-pro", "cache", 1000, 1000 * 0.125e-6},
		// gemini-3-pro-preview: input=2e-6, output=12e-6, cache=0.2e-6
		{"gemini-3-pro-preview", "input", 1000, 1000 * 2e-6},
		{"gemini-3-pro-preview", "output", 1000, 1000 * 12e-6},
		{"gemini-3-pro-preview", "cache", 1000, 1000 * 0.2e-6},
		// gemini-2.0-flash: input=0.1e-6, output=0.4e-6, cache=0.025e-6
		{"gemini-2.0-flash", "input", 1000, 1000 * 0.1e-6},
		{"gemini-2.0-flash", "output", 1000, 1000 * 0.4e-6},
		{"gemini-2.0-flash", "cache", 1000, 1000 * 0.025e-6},
		// gemini-2.0-flash-lite: input=0.075e-6, output=0.3e-6, cache=0 (no cache pricing)
		{"gemini-2.0-flash-lite", "input", 1000, 1000 * 0.075e-6},
		{"gemini-2.0-flash-lite", "output", 1000, 1000 * 0.3e-6},
	}

	for _, tc := range tests {
		t.Run(tc.model+"_"+tc.tokenType, func(t *testing.T) {
			cost := pricing.CalculateGeminiCostForTokenType(tc.model, tc.tokenType, tc.tokenCount)
			if cost == nil {
				t.Fatal("Expected cost to be calculated")
			}
			if diff := *cost - tc.expectedCost; diff > 1e-12 || diff < -1e-12 {
				t.Errorf("pricing.CalculateGeminiCostForTokenType(%s, %s, %d) = %e, expected %e",
					tc.model, tc.tokenType, tc.tokenCount, *cost, tc.expectedCost)
			}
		})
	}
}

func TestCalculateGeminiCostForTokenType_UnknownModel(t *testing.T) {
	cost := pricing.CalculateGeminiCostForTokenType("unknown-model", "input", 1000)
	if cost != nil {
		t.Errorf("Expected nil cost for unknown model, got %f", *cost)
	}
}

func TestCalculateGeminiCostForTokenType_AllTokenTypes(t *testing.T) {
	model := "gemini-2.5-flash"

	// Input should return cost
	inputCost := pricing.CalculateGeminiCostForTokenType(model, pricing.GeminiTokenTypeInput, 1000)
	if inputCost == nil {
		t.Error("Expected cost for input token type")
	}

	// Output should return cost
	outputCost := pricing.CalculateGeminiCostForTokenType(model, pricing.GeminiTokenTypeOutput, 1000)
	if outputCost == nil {
		t.Error("Expected cost for output token type")
	}

	// Cache should return cost
	cacheCost := pricing.CalculateGeminiCostForTokenType(model, pricing.GeminiTokenTypeCache, 1000)
	if cacheCost == nil {
		t.Error("Expected cost for cache token type")
	}

	// Thought should return cost (uses output pricing)
	thoughtCost := pricing.CalculateGeminiCostForTokenType(model, pricing.GeminiTokenTypeThought, 1000)
	if thoughtCost == nil {
		t.Error("Expected cost for thought token type")
	}

	// Tool should NOT return cost
	toolCost := pricing.CalculateGeminiCostForTokenType(model, pricing.GeminiTokenTypeTool, 1000)
	if toolCost != nil {
		t.Errorf("Expected nil cost for tool token type, got %f", *toolCost)
	}

	// Unknown type should NOT return cost
	unknownCost := pricing.CalculateGeminiCostForTokenType(model, "unknown-type", 1000)
	if unknownCost != nil {
		t.Errorf("Expected nil cost for unknown token type, got %f", *unknownCost)
	}
}

func TestCalculateGeminiCostForTokenType_ZeroTokens(t *testing.T) {
	cost := pricing.CalculateGeminiCostForTokenType("gemini-2.5-flash", "input", 0)
	if cost != nil {
		t.Errorf("Expected nil cost for zero tokens, got %f", *cost)
	}
}

func TestCalculateGeminiCostForTokenType_NegativeTokens(t *testing.T) {
	cost := pricing.CalculateGeminiCostForTokenType("gemini-2.5-flash", "input", -100)
	if cost != nil {
		t.Errorf("Expected nil cost for negative tokens, got %f", *cost)
	}
}

func TestCalculateGeminiCostForTokenType_NoCachePrice(t *testing.T) {
	// Models with no cache pricing should return 0 for cache tokens
	cost := pricing.CalculateGeminiCostForTokenType("gemini-2.0-flash-lite", "cache", 1000)
	if cost == nil {
		t.Fatal("Expected cost to be calculated (even if 0)")
	}
	if *cost != 0 {
		t.Errorf("Expected 0 cost for model with no cache pricing, got %f", *cost)
	}
}

func TestNormalizeGeminiModel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"gemini-2.5-flash", "gemini-2.5-flash"},
		{"  gemini-2.5-flash  ", "gemini-2.5-flash"},
		{"gemini-3-pro-preview", "gemini-3-pro-preview"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := pricing.NormalizeGeminiModel(tc.input)
			if result != tc.expected {
				t.Errorf("NormalizeGeminiModel(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestDeriveGeminiCostMetrics_ValidMetric(t *testing.T) {
	timestamp := time.Now()
	floatValue := float64(1000)
	isMonotonic := true
	aggTemp := int32(1) // DELTA

	tokenMetric := api.MetricDataPoint{
		Timestamp:              timestamp,
		ServiceName:            "gemini_cli",
		MetricName:             GeminiTokenUsageMetric,
		MetricDescription:      "Token usage",
		MetricUnit:             "tokens",
		ResourceAttributes:     map[string]string{"service.name": "gemini_cli"},
		ScopeName:              "test",
		ScopeVersion:           "1.0",
		Attributes:             map[string]string{"model": "gemini-2.5-flash", "type": "input"},
		MetricType:             "sum",
		Value:                  &floatValue,
		IsMonotonic:            &isMonotonic,
		AggregationTemporality: &aggTemp,
	}

	costMetric := deriveOne(tokenMetric)

	if costMetric == nil {
		t.Fatal("Expected cost metric to be derived")
	}

	if costMetric.MetricName != GeminiCostUsageMetric {
		t.Errorf("Expected metric name %s, got %s", GeminiCostUsageMetric, costMetric.MetricName)
	}

	if costMetric.MetricUnit != "USD" {
		t.Errorf("Expected unit USD, got %s", costMetric.MetricUnit)
	}

	if costMetric.Attributes["model"] != "gemini-2.5-flash" {
		t.Errorf("Expected model attribute gemini-2.5-flash, got %s", costMetric.Attributes["model"])
	}

	if costMetric.Value == nil {
		t.Fatal("Expected value to be set")
	}

	expectedCost := 1000 * 0.3e-6
	if diff := *costMetric.Value - expectedCost; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("Expected cost %e, got %e", expectedCost, *costMetric.Value)
	}

	if costMetric.Timestamp != timestamp {
		t.Errorf("Expected timestamp to be preserved")
	}

	if costMetric.ServiceName != "gemini_cli" {
		t.Errorf("Expected service name to be preserved")
	}
}

func TestDeriveGeminiCostMetrics_WrongMetricName(t *testing.T) {
	floatValue := float64(1000)

	tokenMetric := api.MetricDataPoint{
		MetricName: "wrong.metric.name",
		Attributes: map[string]string{"model": "gemini-2.5-flash", "type": "input"},
		Value:      &floatValue,
	}

	costMetric := deriveOne(tokenMetric)

	if costMetric != nil {
		t.Error("Expected nil for wrong metric name")
	}
}

func TestDeriveGeminiCostMetrics_MissingModelAttribute(t *testing.T) {
	floatValue := float64(1000)

	tokenMetric := api.MetricDataPoint{
		MetricName: GeminiTokenUsageMetric,
		Attributes: map[string]string{"type": "input"},
		Value:      &floatValue,
	}

	costMetric := deriveOne(tokenMetric)

	if costMetric != nil {
		t.Error("Expected nil for missing model attribute")
	}
}

func TestDeriveGeminiCostMetrics_MissingTypeAttribute(t *testing.T) {
	floatValue := float64(1000)

	tokenMetric := api.MetricDataPoint{
		MetricName: GeminiTokenUsageMetric,
		Attributes: map[string]string{"model": "gemini-2.5-flash"},
		Value:      &floatValue,
	}

	costMetric := deriveOne(tokenMetric)

	if costMetric != nil {
		t.Error("Expected nil for missing type attribute")
	}
}

func TestDeriveGeminiCostMetrics_NilValue(t *testing.T) {
	tokenMetric := api.MetricDataPoint{
		MetricName: GeminiTokenUsageMetric,
		Attributes: map[string]string{"model": "gemini-2.5-flash", "type": "input"},
		Value:      nil,
	}

	costMetric := deriveOne(tokenMetric)

	if costMetric != nil {
		t.Error("Expected nil for nil value")
	}
}

func TestDeriveGeminiCostMetrics_UnknownModel(t *testing.T) {
	floatValue := float64(1000)

	tokenMetric := api.MetricDataPoint{
		MetricName: GeminiTokenUsageMetric,
		Attributes: map[string]string{"model": "unknown-model", "type": "input"},
		Value:      &floatValue,
	}

	costMetric := deriveOne(tokenMetric)

	if costMetric != nil {
		t.Error("Expected nil for unknown model")
	}
}

func TestDeriveGeminiCostMetrics_ToolTokenType(t *testing.T) {
	floatValue := float64(1000)

	tokenMetric := api.MetricDataPoint{
		MetricName: GeminiTokenUsageMetric,
		Attributes: map[string]string{"model": "gemini-2.5-flash", "type": "tool"},
		Value:      &floatValue,
	}

	costMetric := deriveOne(tokenMetric)

	if costMetric != nil {
		t.Error("Expected nil for tool token type (no cost)")
	}
}

// deriveOne derives the cost of a single token point
func deriveOne(tokenMetric api.MetricDataPoint) *api.MetricDataPoint {
	costs := DeriveGeminiCostMetrics([]api.MetricDataPoint{tokenMetric})
	if len(costs) == 0 {
		return nil
	}
	return &costs[0]
}

// Gemini's input tokens include the cached ones, which are charged at the
// cache rate only
func TestDeriveGeminiCostMetrics_InputExcludesCachedTokens(t *testing.T) {
	ts := time.Now()
	cumulative := int32(2)
	point := func(tokenType string, value float64) api.MetricDataPoint {
		return api.MetricDataPoint{
			Timestamp:              ts,
			ServiceName:            "gemini_cli",
			MetricName:             GeminiTokenUsageMetric,
			Attributes:             map[string]string{"model": "gemini-2.5-pro", "type": tokenType, "session.id": "s1"},
			Value:                  &value,
			AggregationTemporality: &cumulative,
		}
	}
	otherSession := point("cache", 900_000)
	otherSession.Attributes["session.id"] = "s2"
	later := point("cache", 500_000)
	later.Timestamp = ts.Add(time.Minute)

	costs := DeriveGeminiCostMetrics([]api.MetricDataPoint{
		point("input", 1_000_000), point("cache", 800_000), otherSession, later,
	})

	byType := make(map[string]float64)
	for _, c := range costs {
		if c.Attributes["session.id"] == "s1" && c.Timestamp.Equal(ts) {
			byType[c.Attributes["token_type"]] = *c.Value
		}
	}
	// 200k uncached at $1.25/M plus 800k cached at $0.125/M
	if got := byType["input"]; got < 0.25-1e-9 || got > 0.25+1e-9 {
		t.Errorf("input cost = %v, want 0.25", got)
	}
	if got := byType["cache"]; got < 0.1-1e-9 || got > 0.1+1e-9 {
		t.Errorf("cache cost = %v, want 0.1", got)
	}
}
