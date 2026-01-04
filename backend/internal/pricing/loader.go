package pricing

import (
	"encoding/json"
	"fmt"
	"log"
)

// mTokToToken converts per-million-token cost to per-token cost
const mTokToToken = 1e-6

// loadProvider loads pricing data from an embedded JSON file
func loadProvider(filename string) (*providerData, error) {
	data, err := pricingFS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filename, err)
	}

	var pricingData PricingData
	if err := json.Unmarshal(data, &pricingData); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filename, err)
	}

	provider := &providerData{
		provider: pricingData.Provider,
		models:   make(map[string]*ModelPricing),
		aliases:  make(map[string]string),
	}

	// Convert each model entry
	for modelName, entry := range pricingData.Models {
		pricing := &ModelPricing{
			InputCostPerToken:      entry.InputCostPerMTok * mTokToToken,
			OutputCostPerToken:     entry.OutputCostPerMTok * mTokToToken,
			CacheReadCostPerToken:  entry.CacheReadCostPerMTok * mTokToToken,
			CacheWriteCostPerToken: entry.CacheWriteCostPerMTok * mTokToToken,
			Deprecated:             entry.Deprecated,
		}

		provider.models[modelName] = pricing

		// Register aliases
		for _, alias := range entry.Aliases {
			provider.aliases[alias] = modelName
		}
	}

	return provider, nil
}

// init loads all pricing data at startup
func init() {
	var err error

	// Load Claude pricing
	registry.claude, err = loadProvider("data/claude.json")
	if err != nil {
		log.Printf("Warning: failed to load Claude pricing: %v", err)
	}

	// Load Codex pricing
	registry.codex, err = loadProvider("data/codex.json")
	if err != nil {
		log.Printf("Warning: failed to load Codex pricing: %v", err)
	}

	// Load Gemini pricing
	registry.gemini, err = loadProvider("data/gemini.json")
	if err != nil {
		log.Printf("Warning: failed to load Gemini pricing: %v", err)
	}
}
