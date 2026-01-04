package pricing

import (
	"testing"
)

func TestClaudePricingLoaded(t *testing.T) {
	// Verify provider is loaded
	if registry.claude == nil {
		t.Fatal("Claude provider not loaded")
	}

	// Verify expected models exist
	models := []string{
		"claude-sonnet-4-5-20250929",
		"claude-haiku-4-5-20251001",
		"claude-opus-4-5-20251101",
		"claude-sonnet-4-20250514",
		"claude-opus-4-20250514",
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
		"claude-3-opus-20240229",
		"claude-3-haiku-20240307",
	}

	for _, model := range models {
		pricing := GetClaudePricing(model)
		if pricing == nil {
			t.Errorf("Claude model %q not found", model)
			continue
		}
		if pricing.InputCostPerToken <= 0 {
			t.Errorf("Claude model %q has invalid input cost: %v", model, pricing.InputCostPerToken)
		}
		if pricing.OutputCostPerToken <= 0 {
			t.Errorf("Claude model %q has invalid output cost: %v", model, pricing.OutputCostPerToken)
		}
	}
}

func TestClaudeAliasLookup(t *testing.T) {
	tests := []struct {
		alias    string
		expected string
	}{
		{"claude-sonnet-4-5", "claude-sonnet-4-5-20250929"},
		{"claude-haiku-4-5", "claude-haiku-4-5-20251001"},
		{"claude-opus-4-5", "claude-opus-4-5-20251101"},
		{"claude-sonnet-4", "claude-sonnet-4-20250514"},
		{"claude-3-5-sonnet", "claude-3-5-sonnet-20241022"},
		{"claude-haiku-3-5", "claude-3-5-haiku-20241022"},
	}

	for _, tc := range tests {
		pricing := GetClaudePricing(tc.alias)
		if pricing == nil {
			t.Errorf("Claude alias %q not resolved", tc.alias)
		}
	}
}

func TestCodexPricingLoaded(t *testing.T) {
	// Verify provider is loaded
	if registry.codex == nil {
		t.Fatal("Codex provider not loaded")
	}

	// Verify expected models exist
	models := []string{
		"gpt-5",
		"gpt-5.1",
		"gpt-5-mini",
		"gpt-4.1",
		"gpt-4o",
		"gpt-4o-mini",
		"o1",
		"o3",
		"o4-mini",
	}

	for _, model := range models {
		pricing := GetCodexPricing(model)
		if pricing == nil {
			t.Errorf("Codex model %q not found", model)
			continue
		}
		if pricing.InputCostPerToken <= 0 {
			t.Errorf("Codex model %q has invalid input cost: %v", model, pricing.InputCostPerToken)
		}
		if pricing.OutputCostPerToken <= 0 {
			t.Errorf("Codex model %q has invalid output cost: %v", model, pricing.OutputCostPerToken)
		}
	}
}

func TestGeminiPricingLoaded(t *testing.T) {
	// Verify provider is loaded
	if registry.gemini == nil {
		t.Fatal("Gemini provider not loaded")
	}

	// Verify expected models exist
	models := []string{
		"gemini-3-pro-preview",
		"gemini-3-flash-preview",
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.0-flash",
	}

	for _, model := range models {
		pricing := GetGeminiPricing(model)
		if pricing == nil {
			t.Errorf("Gemini model %q not found", model)
			continue
		}
		if pricing.InputCostPerToken <= 0 {
			t.Errorf("Gemini model %q has invalid input cost: %v", model, pricing.InputCostPerToken)
		}
		if pricing.OutputCostPerToken <= 0 {
			t.Errorf("Gemini model %q has invalid output cost: %v", model, pricing.OutputCostPerToken)
		}
	}
}

func TestCalculateClaudeCost(t *testing.T) {
	usage := ClaudeTokenUsage{
		InputTokens:              1000,
		OutputTokens:             500,
		CacheCreationInputTokens: 100,
		CacheReadInputTokens:     50,
	}

	cost := CalculateClaudeCost("claude-sonnet-4-5-20250929", usage)
	if cost == nil {
		t.Fatal("Failed to calculate Claude cost")
	}

	// Expected: 1000 * 3e-6 + 500 * 15e-6 + 100 * 3.75e-6 + 50 * 0.3e-6
	// = 0.003 + 0.0075 + 0.000375 + 0.000015 = 0.01089
	expected := 0.01089
	if *cost < expected*0.99 || *cost > expected*1.01 {
		t.Errorf("Expected cost ~%v, got %v", expected, *cost)
	}
}

func TestCalculateCodexCost(t *testing.T) {
	cost := CalculateCodexCost("gpt-5", 1000, 100, 500)
	if cost == nil {
		t.Fatal("Failed to calculate Codex cost")
	}

	// Expected: (1000-100) * 1.25e-6 + 100 * 0.125e-6 + 500 * 10e-6
	// = 900 * 1.25e-6 + 100 * 0.125e-6 + 500 * 10e-6
	// = 0.001125 + 0.0000125 + 0.005 = 0.0061375
	expected := 0.0061375
	if *cost < expected*0.99 || *cost > expected*1.01 {
		t.Errorf("Expected cost ~%v, got %v", expected, *cost)
	}
}

func TestCalculateGeminiCostForTokenType(t *testing.T) {
	tests := []struct {
		model     string
		tokenType string
		count     int64
		expected  float64
	}{
		{"gemini-2.5-pro", GeminiTokenTypeInput, 1000000, 1.25},
		{"gemini-2.5-pro", GeminiTokenTypeOutput, 1000000, 10.0},
		{"gemini-2.5-pro", GeminiTokenTypeCache, 1000000, 0.125},
	}

	for _, tc := range tests {
		cost := CalculateGeminiCostForTokenType(tc.model, tc.tokenType, tc.count)
		if cost == nil {
			t.Errorf("Failed to calculate Gemini cost for %s/%s", tc.model, tc.tokenType)
			continue
		}
		if *cost < tc.expected*0.99 || *cost > tc.expected*1.01 {
			t.Errorf("For %s/%s: expected ~%v, got %v", tc.model, tc.tokenType, tc.expected, *cost)
		}
	}
}

func TestNormalizeCodexModel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"gpt-5", "gpt-5"},
		{"openai/gpt-5", "gpt-5"},
		{"  gpt-5  ", "gpt-5"},
		{"openai/gpt-4o-mini", "gpt-4o-mini"},
	}

	for _, tc := range tests {
		result := NormalizeCodexModel(tc.input)
		if result != tc.expected {
			t.Errorf("NormalizeCodexModel(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestNormalizeClaudeModel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-sonnet-4-5-20250929", "claude-sonnet-4-5-20250929"},
		{"anthropic/claude-sonnet-4-5-20250929", "claude-sonnet-4-5-20250929"},
		{"  claude-3-opus-20240229  ", "claude-3-opus-20240229"},
	}

	for _, tc := range tests {
		result := NormalizeClaudeModel(tc.input)
		if result != tc.expected {
			t.Errorf("NormalizeClaudeModel(%q) = %q, expected %q", tc.input, result, tc.expected)
		}
	}
}

func TestParsePricingMode(t *testing.T) {
	tests := []struct {
		input    string
		expected PricingMode
		hasError bool
	}{
		{"auto", PricingModeAuto, false},
		{"Auto", PricingModeAuto, false},
		{"", PricingModeAuto, false},
		{"calculate", PricingModeCalculate, false},
		{"CALCULATE", PricingModeCalculate, false},
		{"display", PricingModeDisplay, false},
		{"invalid", "", true},
	}

	for _, tc := range tests {
		result, err := ParsePricingMode(tc.input)
		if tc.hasError {
			if err == nil {
				t.Errorf("ParsePricingMode(%q) expected error, got nil", tc.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParsePricingMode(%q) unexpected error: %v", tc.input, err)
			}
			if result != tc.expected {
				t.Errorf("ParsePricingMode(%q) = %q, expected %q", tc.input, result, tc.expected)
			}
		}
	}
}

func TestGetClaudeCostWithMode(t *testing.T) {
	usage := ClaudeTokenUsage{
		InputTokens:  1000,
		OutputTokens: 500,
	}
	model := "claude-sonnet-4-5-20250929"

	costUSD := 0.05
	noCostUSD := 0.0

	// Test auto mode with costUSD
	result := GetClaudeCostWithMode(PricingModeAuto, model, usage, &costUSD)
	if result != 0.05 {
		t.Errorf("Auto mode with costUSD: expected 0.05, got %v", result)
	}

	// Test auto mode without costUSD - should calculate
	result = GetClaudeCostWithMode(PricingModeAuto, model, usage, nil)
	if result <= 0 {
		t.Errorf("Auto mode without costUSD: expected calculated cost, got %v", result)
	}

	// Test calculate mode - should ignore costUSD
	result = GetClaudeCostWithMode(PricingModeCalculate, model, usage, &costUSD)
	if result == 0.05 {
		t.Errorf("Calculate mode: should not use costUSD")
	}

	// Test display mode with costUSD
	result = GetClaudeCostWithMode(PricingModeDisplay, model, usage, &costUSD)
	if result != 0.05 {
		t.Errorf("Display mode with costUSD: expected 0.05, got %v", result)
	}

	// Test display mode without costUSD - should return 0
	result = GetClaudeCostWithMode(PricingModeDisplay, model, usage, nil)
	if result != 0 {
		t.Errorf("Display mode without costUSD: expected 0, got %v", result)
	}

	// Test display mode with zero costUSD - should return 0
	result = GetClaudeCostWithMode(PricingModeDisplay, model, usage, &noCostUSD)
	if result != 0 {
		t.Errorf("Display mode with zero costUSD: expected 0, got %v", result)
	}
}

func TestListModels(t *testing.T) {
	// Claude
	if registry.claude != nil {
		models := registry.claude.ListModels()
		if len(models) == 0 {
			t.Error("Claude ListModels returned empty list")
		}
	}

	// Codex
	if registry.codex != nil {
		models := registry.codex.ListModels()
		if len(models) == 0 {
			t.Error("Codex ListModels returned empty list")
		}
	}

	// Gemini
	if registry.gemini != nil {
		models := registry.gemini.ListModels()
		if len(models) == 0 {
			t.Error("Gemini ListModels returned empty list")
		}
	}
}
