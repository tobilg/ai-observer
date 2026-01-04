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
			cost := CalculateGeminiCostForTokenType(tc.model, tc.tokenType, tc.tokenCount)
			if cost == nil {
				t.Fatal("Expected cost to be calculated")
			}
			if diff := *cost - tc.expectedCost; diff > 1e-12 || diff < -1e-12 {
				t.Errorf("CalculateGeminiCostForTokenType(%s, %s, %d) = %e, expected %e",
					tc.model, tc.tokenType, tc.tokenCount, *cost, tc.expectedCost)
			}
		})
	}
}

func TestCalculateGeminiCostForTokenType_UnknownModel(t *testing.T) {
	cost := CalculateGeminiCostForTokenType("unknown-model", "input", 1000)
	if cost != nil {
		t.Errorf("Expected nil cost for unknown model, got %f", *cost)
	}
}

func TestCalculateGeminiCostForTokenType_AllTokenTypes(t *testing.T) {
	model := "gemini-2.5-flash"

	// Input should return cost
	inputCost := CalculateGeminiCostForTokenType(model, pricing.GeminiTokenTypeInput, 1000)
	if inputCost == nil {
		t.Error("Expected cost for input token type")
	}

	// Output should return cost
	outputCost := CalculateGeminiCostForTokenType(model, pricing.GeminiTokenTypeOutput, 1000)
	if outputCost == nil {
		t.Error("Expected cost for output token type")
	}

	// Cache should return cost
	cacheCost := CalculateGeminiCostForTokenType(model, pricing.GeminiTokenTypeCache, 1000)
	if cacheCost == nil {
		t.Error("Expected cost for cache token type")
	}

	// Thought should return cost (uses output pricing)
	thoughtCost := CalculateGeminiCostForTokenType(model, pricing.GeminiTokenTypeThought, 1000)
	if thoughtCost == nil {
		t.Error("Expected cost for thought token type")
	}

	// Tool should NOT return cost
	toolCost := CalculateGeminiCostForTokenType(model, pricing.GeminiTokenTypeTool, 1000)
	if toolCost != nil {
		t.Errorf("Expected nil cost for tool token type, got %f", *toolCost)
	}

	// Unknown type should NOT return cost
	unknownCost := CalculateGeminiCostForTokenType(model, "unknown-type", 1000)
	if unknownCost != nil {
		t.Errorf("Expected nil cost for unknown token type, got %f", *unknownCost)
	}
}

func TestCalculateGeminiCostForTokenType_ZeroTokens(t *testing.T) {
	cost := CalculateGeminiCostForTokenType("gemini-2.5-flash", "input", 0)
	if cost != nil {
		t.Errorf("Expected nil cost for zero tokens, got %f", *cost)
	}
}

func TestCalculateGeminiCostForTokenType_NegativeTokens(t *testing.T) {
	cost := CalculateGeminiCostForTokenType("gemini-2.5-flash", "input", -100)
	if cost != nil {
		t.Errorf("Expected nil cost for negative tokens, got %f", *cost)
	}
}

func TestCalculateGeminiCostForTokenType_NoCachePrice(t *testing.T) {
	// Models with no cache pricing should return 0 for cache tokens
	cost := CalculateGeminiCostForTokenType("gemini-2.0-flash-lite", "cache", 1000)
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

func TestDeriveGeminiCostMetric_ValidMetric(t *testing.T) {
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

	costMetric := DeriveGeminiCostMetric(tokenMetric)

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

func TestDeriveGeminiCostMetric_WrongMetricName(t *testing.T) {
	floatValue := float64(1000)

	tokenMetric := api.MetricDataPoint{
		MetricName: "wrong.metric.name",
		Attributes: map[string]string{"model": "gemini-2.5-flash", "type": "input"},
		Value:      &floatValue,
	}

	costMetric := DeriveGeminiCostMetric(tokenMetric)

	if costMetric != nil {
		t.Error("Expected nil for wrong metric name")
	}
}

func TestDeriveGeminiCostMetric_MissingModelAttribute(t *testing.T) {
	floatValue := float64(1000)

	tokenMetric := api.MetricDataPoint{
		MetricName: GeminiTokenUsageMetric,
		Attributes: map[string]string{"type": "input"},
		Value:      &floatValue,
	}

	costMetric := DeriveGeminiCostMetric(tokenMetric)

	if costMetric != nil {
		t.Error("Expected nil for missing model attribute")
	}
}

func TestDeriveGeminiCostMetric_MissingTypeAttribute(t *testing.T) {
	floatValue := float64(1000)

	tokenMetric := api.MetricDataPoint{
		MetricName: GeminiTokenUsageMetric,
		Attributes: map[string]string{"model": "gemini-2.5-flash"},
		Value:      &floatValue,
	}

	costMetric := DeriveGeminiCostMetric(tokenMetric)

	if costMetric != nil {
		t.Error("Expected nil for missing type attribute")
	}
}

func TestDeriveGeminiCostMetric_NilValue(t *testing.T) {
	tokenMetric := api.MetricDataPoint{
		MetricName: GeminiTokenUsageMetric,
		Attributes: map[string]string{"model": "gemini-2.5-flash", "type": "input"},
		Value:      nil,
	}

	costMetric := DeriveGeminiCostMetric(tokenMetric)

	if costMetric != nil {
		t.Error("Expected nil for nil value")
	}
}

func TestDeriveGeminiCostMetric_UnknownModel(t *testing.T) {
	floatValue := float64(1000)

	tokenMetric := api.MetricDataPoint{
		MetricName: GeminiTokenUsageMetric,
		Attributes: map[string]string{"model": "unknown-model", "type": "input"},
		Value:      &floatValue,
	}

	costMetric := DeriveGeminiCostMetric(tokenMetric)

	if costMetric != nil {
		t.Error("Expected nil for unknown model")
	}
}

func TestDeriveGeminiCostMetric_ToolTokenType(t *testing.T) {
	floatValue := float64(1000)

	tokenMetric := api.MetricDataPoint{
		MetricName: GeminiTokenUsageMetric,
		Attributes: map[string]string{"model": "gemini-2.5-flash", "type": "tool"},
		Value:      &floatValue,
	}

	costMetric := DeriveGeminiCostMetric(tokenMetric)

	if costMetric != nil {
		t.Error("Expected nil for tool token type (no cost)")
	}
}
