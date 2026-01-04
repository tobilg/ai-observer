package otlp

import (
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/pricing"
)

func TestExtractCodexMetrics_ResponseCompleted(t *testing.T) {
	timestamp := time.Now()
	logAttrs := map[string]string{
		"event.kind":            "response.completed",
		"input_token_count":     "1000",
		"output_token_count":    "500",
		"cached_token_count":    "200",
		"reasoning_token_count": "100",
		"tool_token_count":      "50",
		"model":                 "gpt-5",
	}
	resourceAttrs := map[string]string{}

	metrics := ExtractCodexMetrics(logAttrs, timestamp, "codex_cli_rs", resourceAttrs, "trace123", "span456")

	if len(metrics) == 0 {
		t.Fatal("Expected metrics to be extracted")
	}

	// Should have 5 token metrics + 1 cost metric = 6 total
	expectedCount := 6
	if len(metrics) != expectedCount {
		t.Errorf("Expected %d metrics, got %d", expectedCount, len(metrics))
	}

	// Verify token metrics
	tokenMetrics := make(map[string]float64)
	var costMetric *float64
	for _, m := range metrics {
		if m.MetricName == CodexTokenUsageMetric {
			tokenType := m.Attributes["type"]
			if m.Value != nil {
				tokenMetrics[tokenType] = *m.Value
			}
		} else if m.MetricName == CodexCostUsageMetric {
			costMetric = m.Value
		}
	}

	// Check token values
	expectedTokens := map[string]float64{
		"input":     1000,
		"output":    500,
		"cacheRead": 200,
		"reasoning": 100,
		"tool":      50,
	}
	for tokenType, expected := range expectedTokens {
		if actual, ok := tokenMetrics[tokenType]; !ok {
			t.Errorf("Missing token type %s", tokenType)
		} else if actual != expected {
			t.Errorf("Token type %s: expected %f, got %f", tokenType, expected, actual)
		}
	}

	// Check cost metric exists
	if costMetric == nil {
		t.Error("Expected cost metric to be present")
	}
}

func TestExtractCodexMetrics_NotResponseCompleted(t *testing.T) {
	timestamp := time.Now()
	logAttrs := map[string]string{
		"event.kind":         "chunk",
		"input_token_count":  "1000",
		"output_token_count": "500",
	}
	resourceAttrs := map[string]string{}

	metrics := ExtractCodexMetrics(logAttrs, timestamp, "codex_cli_rs", resourceAttrs, "trace123", "span456")

	if len(metrics) != 0 {
		t.Errorf("Expected no metrics for non-response.completed event, got %d", len(metrics))
	}
}

func TestExtractCodexMetrics_MissingEventKind(t *testing.T) {
	timestamp := time.Now()
	logAttrs := map[string]string{
		"input_token_count":  "1000",
		"output_token_count": "500",
	}
	resourceAttrs := map[string]string{}

	metrics := ExtractCodexMetrics(logAttrs, timestamp, "codex_cli_rs", resourceAttrs, "trace123", "span456")

	if len(metrics) != 0 {
		t.Errorf("Expected no metrics without event.kind, got %d", len(metrics))
	}
}

func TestExtractCodexMetrics_ZeroTokens(t *testing.T) {
	timestamp := time.Now()
	logAttrs := map[string]string{
		"event.kind":         "response.completed",
		"input_token_count":  "0",
		"output_token_count": "0",
	}
	resourceAttrs := map[string]string{}

	metrics := ExtractCodexMetrics(logAttrs, timestamp, "codex_cli_rs", resourceAttrs, "trace123", "span456")

	if len(metrics) != 0 {
		t.Errorf("Expected no metrics for zero tokens, got %d", len(metrics))
	}
}

func TestExtractCodexMetrics_PartialTokens(t *testing.T) {
	timestamp := time.Now()
	logAttrs := map[string]string{
		"event.kind":         "response.completed",
		"input_token_count":  "1000",
		"output_token_count": "500",
		"model":              "gpt-5",
	}
	resourceAttrs := map[string]string{}

	metrics := ExtractCodexMetrics(logAttrs, timestamp, "codex_cli_rs", resourceAttrs, "trace123", "span456")

	// Should have 2 token metrics + 1 cost metric = 3 total
	if len(metrics) != 3 {
		t.Errorf("Expected 3 metrics, got %d", len(metrics))
	}
}

func TestExtractCodexMetrics_ModelAttribute(t *testing.T) {
	timestamp := time.Now()
	logAttrs := map[string]string{
		"event.kind":         "response.completed",
		"input_token_count":  "100",
		"output_token_count": "50",
		"model":              "gpt-5.2",
	}
	resourceAttrs := map[string]string{}

	metrics := ExtractCodexMetrics(logAttrs, timestamp, "codex_cli_rs", resourceAttrs, "trace123", "span456")

	// Check that model attribute is set correctly
	for _, m := range metrics {
		if m.Attributes["model"] != "gpt-5.2" {
			t.Errorf("Expected model attribute 'gpt-5.2', got '%s'", m.Attributes["model"])
		}
	}
}

func TestExtractCodexMetrics_UnknownModel_NoCost(t *testing.T) {
	timestamp := time.Now()
	logAttrs := map[string]string{
		"event.kind":         "response.completed",
		"input_token_count":  "1000",
		"output_token_count": "500",
		"model":              "unknown-model",
	}
	resourceAttrs := map[string]string{}

	metrics := ExtractCodexMetrics(logAttrs, timestamp, "codex_cli_rs", resourceAttrs, "trace123", "span456")

	// Should have 2 token metrics but no cost metric
	if len(metrics) != 2 {
		t.Errorf("Expected 2 metrics (no cost), got %d", len(metrics))
	}

	for _, m := range metrics {
		if m.MetricName == CodexCostUsageMetric {
			t.Error("Expected no cost metric for unknown model")
		}
	}
}

func TestNormalizeCodexModel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// Strips openai/ prefix
		{"gpt-5", "gpt-5"},
		{"openai/gpt-5", "gpt-5"},
		{"openai/gpt-5.1-codex-max", "gpt-5.1-codex-max"},
		{"openai/o4-mini", "o4-mini"},
		// Preserves model names as-is
		{"gpt-5.1-codex-max", "gpt-5.1-codex-max"},
		{"gpt-5.1-codex-mini", "gpt-5.1-codex-mini"},
		{"gpt-5-chat-latest", "gpt-5-chat-latest"},
		{"o4-mini", "o4-mini"},
		{"unknown-model", "unknown-model"},
		// Trims whitespace
		{"  gpt-5  ", "gpt-5"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := pricing.NormalizeCodexModel(tc.input)
			if result != tc.expected {
				t.Errorf("NormalizeCodexModel(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestCalculateCodexCost_KnownModels(t *testing.T) {
	tests := []struct {
		model        string
		input        int64
		cached       int64
		output       int64
		expectedCost float64
	}{
		// gpt-5: input=1.25e-6, output=1e-5, cacheRead=1.25e-7
		{"gpt-5", 1000, 0, 500, 1000*1.25e-6 + 500*1e-5},
		{"gpt-5", 1000, 200, 500, 800*1.25e-6 + 200*1.25e-7 + 500*1e-5},
		// gpt-5.1 (same as gpt-5)
		{"gpt-5.1", 1000, 0, 500, 1000*1.25e-6 + 500*1e-5},
		// gpt-5.2: input=1.75e-6, output=1.4e-5, cacheRead=1.75e-7
		{"gpt-5.2", 1000, 0, 500, 1000*1.75e-6 + 500*1.4e-5},
		{"gpt-5.2", 1000, 200, 500, 800*1.75e-6 + 200*1.75e-7 + 500*1.4e-5},
		// gpt-5-mini: input=2.5e-7, output=2e-6, cacheRead=2.5e-8
		{"gpt-5-mini", 1000, 0, 500, 1000*2.5e-7 + 500*2e-6},
		// gpt-4.1: input=2e-6, output=8e-6, cacheRead=5e-7
		{"gpt-4.1", 1000, 0, 500, 1000*2e-6 + 500*8e-6},
		{"gpt-4.1", 1000, 200, 500, 800*2e-6 + 200*5e-7 + 500*8e-6},
		// o3: input=2e-6, output=8e-6, cacheRead=5e-7
		{"o3", 1000, 0, 500, 1000*2e-6 + 500*8e-6},
		// o4-mini: input=1.1e-6, output=4.4e-6, cacheRead=2.75e-7
		{"o4-mini", 1000, 0, 500, 1000*1.1e-6 + 500*4.4e-6},
		{"o4-mini", 1000, 200, 500, 800*1.1e-6 + 200*2.75e-7 + 500*4.4e-6},
		// Test model normalization: gpt-5.1-codex-max -> gpt-5.1
		{"gpt-5.1-codex-max", 1000, 0, 500, 1000*1.25e-6 + 500*1e-5},
	}

	for _, tc := range tests {
		t.Run(tc.model, func(t *testing.T) {
			cost := CalculateCodexCost(tc.model, tc.input, tc.cached, tc.output)
			if cost == nil {
				t.Fatal("Expected cost to be calculated")
			}
			// Use approximate comparison for floating point
			if diff := *cost - tc.expectedCost; diff > 1e-12 || diff < -1e-12 {
				t.Errorf("CalculateCodexCost(%s, %d, %d, %d) = %e, expected %e",
					tc.model, tc.input, tc.cached, tc.output, *cost, tc.expectedCost)
			}
		})
	}
}

func TestCalculateCodexCost_UnknownModel(t *testing.T) {
	cost := CalculateCodexCost("unknown-model", 1000, 0, 500)
	if cost != nil {
		t.Errorf("Expected nil cost for unknown model, got %f", *cost)
	}
}

func TestCalculateCodexCost_NegativeTokens(t *testing.T) {
	// Should handle negative values gracefully by clamping to 0
	cost := CalculateCodexCost("gpt-5", -100, -50, -25)
	if cost == nil {
		t.Fatal("Expected cost to be calculated")
	}
	if *cost != 0 {
		t.Errorf("Expected 0 cost for negative tokens, got %f", *cost)
	}
}

func TestCalculateCodexCost_CachedExceedsInput(t *testing.T) {
	// Cached tokens should be clamped to input tokens
	cost := CalculateCodexCost("gpt-5", 100, 200, 50)
	if cost == nil {
		t.Fatal("Expected cost to be calculated")
	}

	// All input should be treated as cached when cached > input
	// cost = 0 * inputRate + 100 * cacheReadRate + 50 * outputRate
	expected := 100*1.25e-7 + 50*1e-5
	if diff := *cost - expected; diff > 1e-12 || diff < -1e-12 {
		t.Errorf("Expected cost %e, got %e", expected, *cost)
	}
}
