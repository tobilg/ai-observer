package pricing

// Provider represents an AI model provider
type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
	ProviderGoogle    Provider = "google"
)

// ModelPricing contains per-token pricing in USD
type ModelPricing struct {
	InputCostPerToken      float64
	OutputCostPerToken     float64
	CacheReadCostPerToken  float64
	CacheWriteCostPerToken float64
	Deprecated             bool
}

// ModelEntry represents the JSON format for a model's pricing (per million tokens)
type ModelEntry struct {
	Aliases             []string `json:"aliases,omitempty"`
	InputCostPerMTok    float64  `json:"inputCostPerMTok"`
	OutputCostPerMTok   float64  `json:"outputCostPerMTok"`
	CacheReadCostPerMTok  float64  `json:"cacheReadCostPerMTok,omitempty"`
	CacheWriteCostPerMTok float64  `json:"cacheWriteCostPerMTok,omitempty"`
	Deprecated          bool     `json:"deprecated,omitempty"`
}

// PricingData represents the root structure of a pricing JSON file
type PricingData struct {
	Provider    Provider              `json:"provider"`
	LastUpdated string                `json:"lastUpdated"`
	Models      map[string]ModelEntry `json:"models"`
}

// PricingProvider interface for each provider
type PricingProvider interface {
	GetPricing(model string) *ModelPricing
	GetProvider() Provider
	ListModels() []string
}

// providerData holds loaded pricing data for a provider
type providerData struct {
	provider Provider
	models   map[string]*ModelPricing // normalized model name -> pricing
	aliases  map[string]string        // alias -> canonical model name
}

// GetPricing returns pricing for a model
func (p *providerData) GetPricing(model string) *ModelPricing {
	// Try direct lookup first
	if pricing, ok := p.models[model]; ok {
		return pricing
	}
	// Try alias lookup
	if canonical, ok := p.aliases[model]; ok {
		if pricing, ok := p.models[canonical]; ok {
			return pricing
		}
	}
	return nil
}

// GetProvider returns the provider type
func (p *providerData) GetProvider() Provider {
	return p.provider
}

// ListModels returns all canonical model names
func (p *providerData) ListModels() []string {
	models := make([]string, 0, len(p.models))
	for name := range p.models {
		models = append(models, name)
	}
	return models
}

// Registry holds all loaded pricing providers
type Registry struct {
	claude *providerData
	codex  *providerData
	gemini *providerData
}

// Global registry instance
var registry = &Registry{}

// GetClaudePricing returns pricing for a Claude model
func GetClaudePricing(model string) *ModelPricing {
	if registry.claude == nil {
		return nil
	}
	normalized := NormalizeClaudeModel(model)
	return registry.claude.GetPricing(normalized)
}

// GetCodexPricing returns pricing for a Codex (OpenAI) model
func GetCodexPricing(model string) *ModelPricing {
	if registry.codex == nil {
		return nil
	}
	normalized := NormalizeCodexModel(model)
	return registry.codex.GetPricing(normalized)
}

// GetGeminiPricing returns pricing for a Gemini model
func GetGeminiPricing(model string) *ModelPricing {
	if registry.gemini == nil {
		return nil
	}
	normalized := NormalizeGeminiModel(model)
	return registry.gemini.GetPricing(normalized)
}

// GetClaudeProvider returns the Claude pricing provider
func GetClaudeProvider() PricingProvider {
	return registry.claude
}

// GetCodexProvider returns the Codex pricing provider
func GetCodexProvider() PricingProvider {
	return registry.codex
}

// GetGeminiProvider returns the Gemini pricing provider
func GetGeminiProvider() PricingProvider {
	return registry.gemini
}
