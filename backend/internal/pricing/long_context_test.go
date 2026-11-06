package pricing

import (
	"math"
	"testing"
)

func approxEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// Spot checks of the October 2026 price tables, per million tokens.
func TestCurrentModelPrices(t *testing.T) {
	tests := []struct {
		name                 string
		get                  func(string) *ModelPricing
		model                string
		input, output, cache float64
	}{
		{"claude", GetClaudePricing, "claude-opus-5-5", 4, 20, 0.2},
		{"claude", GetClaudePricing, "claude-sonnet-5", 2, 10, 0.2},
		{"claude", GetClaudePricing, "claude-haiku-5-5", 0.1, 0.5, 0.01},
		{"claude", GetClaudePricing, "claude-fable-5-1", 10, 50, 0.25},
		{"openai", GetCodexPricing, "gpt-6.1-sol", 2, 10, 0.1},
		{"openai", GetCodexPricing, "gpt-5.6-terra", 2, 12, 0.2},
		{"gemini", GetGeminiPricing, "gemini-3.8-flash", 0.75, 3.75, 0.075},
		{"gemini", GetGeminiPricing, "gemini-3.1-flash-lite", 0.25, 1.5, 0.025},
		{"copilot", GetCopilotPricing, "claude-sonnet-5.5", 2, 10, 0.1},
		{"copilot alias", GetCopilotPricing, "Claude Sonnet 5.5", 2, 10, 0.1},
		{"copilot alias", GetCopilotPricing, "gpt-6.1 sol", 2, 10, 0.1},
		{"copilot", GetCopilotPricing, "gemini-4-argon", 2, 10, 0.1},
	}
	for _, tc := range tests {
		p := tc.get(tc.model)
		if p == nil {
			t.Errorf("%s: no pricing for %q", tc.name, tc.model)
			continue
		}
		if !approxEqual(p.InputCostPerToken*1e6, tc.input) || !approxEqual(p.OutputCostPerToken*1e6, tc.output) || !approxEqual(p.CacheReadCostPerToken*1e6, tc.cache) {
			t.Errorf("%s %s: got %v/%v/%v per MTok, want %v/%v/%v", tc.name, tc.model,
				p.InputCostPerToken*1e6, p.OutputCostPerToken*1e6, p.CacheReadCostPerToken*1e6, tc.input, tc.output, tc.cache)
		}
	}
}

// Claude Haiku 5.5 is priced higher for prompts over 100k tokens, counting
// cache reads and writes toward the prompt length.
func TestClaudeLongContextTierCountsCachedTokens(t *testing.T) {
	below := CalculateClaudeCost("claude-haiku-5-5", ClaudeTokenUsage{InputTokens: 50_000, CacheReadInputTokens: 40_000})
	above := CalculateClaudeCost("claude-haiku-5-5", ClaudeTokenUsage{InputTokens: 50_000, CacheReadInputTokens: 60_000})
	if below == nil || above == nil {
		t.Fatal("expected Claude Haiku 5.5 to be priced")
	}

	// 90k prompt: 50k * $0.10 + 40k * $0.01 per MTok
	if want := 0.005 + 0.0004; !approxEqual(*below, want) {
		t.Errorf("90k prompt cost = %v, want %v", *below, want)
	}
	// 110k prompt: 50k * $0.50 + 60k * $0.05 per MTok
	if want := 0.025 + 0.003; !approxEqual(*above, want) {
		t.Errorf("110k prompt cost = %v, want %v", *above, want)
	}
}

func TestCodexLongContextTier(t *testing.T) {
	// 200k prompt: base tier, $5 input / $30 output per MTok
	below := CalculateCodexCost("gpt-5.5", 200_000, 0, 1_000)
	// 300k prompt (100k cached): $10 input, $1 cached, $45 output per MTok
	above := CalculateCodexCost("gpt-5.5", 300_000, 100_000, 1_000)
	if below == nil || above == nil {
		t.Fatal("expected gpt-5.5 to be priced")
	}
	if want := 1.0 + 0.03; !approxEqual(*below, want) {
		t.Errorf("200k prompt cost = %v, want %v", *below, want)
	}
	if want := 2.0 + 0.1 + 0.045; !approxEqual(*above, want) {
		t.Errorf("300k prompt cost = %v, want %v", *above, want)
	}

	// Models without a long-context tier keep their base price
	mini := CalculateCodexCost("gpt-5.4-mini", 300_000, 0, 0)
	if mini == nil || !approxEqual(*mini, 300_000*0.75e-6) {
		t.Errorf("gpt-5.4-mini 300k prompt cost = %v, want base price %v", mini, 300_000*0.75e-6)
	}
}
