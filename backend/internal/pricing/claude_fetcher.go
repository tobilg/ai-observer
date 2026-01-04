package pricing

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	// AnthropicModelsURL is the URL for Anthropic's models documentation
	AnthropicModelsURL = "https://docs.anthropic.com/en/docs/about-claude/models/all-models"
	// AnthropicPricingURL is the URL for Anthropic's pricing documentation
	AnthropicPricingURL = "https://docs.anthropic.com/en/docs/about-claude/pricing"
)

// FetchedModelInfo contains model information extracted from Anthropic docs
type FetchedModelInfo struct {
	ModelID   string
	Aliases   []string
	ReleaseDate string
}

// FetchedPricingInfo contains pricing information extracted from Anthropic docs
type FetchedPricingInfo struct {
	ModelID           string
	InputCostPerMTok  float64
	OutputCostPerMTok float64
}

// ClaudeFetcher fetches and parses Claude pricing from Anthropic documentation
type ClaudeFetcher struct {
	httpClient *http.Client
}

// NewClaudeFetcher creates a new ClaudeFetcher
func NewClaudeFetcher() *ClaudeFetcher {
	return &ClaudeFetcher{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FetchModelsPage fetches the Anthropic models documentation page
func (f *ClaudeFetcher) FetchModelsPage() (string, error) {
	resp, err := f.httpClient.Get(AnthropicModelsURL)
	if err != nil {
		return "", fmt.Errorf("fetching models page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("models page returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading models page: %w", err)
	}

	return string(body), nil
}

// FetchPricingPage fetches the Anthropic pricing documentation page
func (f *ClaudeFetcher) FetchPricingPage() (string, error) {
	resp, err := f.httpClient.Get(AnthropicPricingURL)
	if err != nil {
		return "", fmt.Errorf("fetching pricing page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("pricing page returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("reading pricing page: %w", err)
	}

	return string(body), nil
}

// ParseModelsFromHTML extracts model information from the HTML content
// This is a simplified parser - in production, consider using a proper HTML parser
func ParseModelsFromHTML(html string) []FetchedModelInfo {
	var models []FetchedModelInfo

	// Pattern to match model IDs like claude-sonnet-4-5-20250929
	modelPattern := regexp.MustCompile(`claude-(?:opus|sonnet|haiku)-[\d\-]+`)

	matches := modelPattern.FindAllString(html, -1)
	seen := make(map[string]bool)

	for _, match := range matches {
		if !seen[match] {
			seen[match] = true
			models = append(models, FetchedModelInfo{
				ModelID: match,
			})
		}
	}

	return models
}

// ParsePricingFromHTML extracts pricing information from the HTML content
// This is a simplified parser - in production, consider using a proper HTML parser
func ParsePricingFromHTML(html string) []FetchedPricingInfo {
	var pricing []FetchedPricingInfo

	// Pattern to match pricing like "$3 / MTok" or "$3.00 / MTok"
	pricePattern := regexp.MustCompile(`\$(\d+(?:\.\d+)?)\s*/\s*MTok`)

	// This is a simplified implementation - a full implementation would:
	// 1. Parse the HTML table structure
	// 2. Match model names to their input/output prices
	// 3. Handle variations in formatting

	matches := pricePattern.FindAllStringSubmatch(html, -1)
	for _, match := range matches {
		if len(match) >= 2 {
			if price, err := strconv.ParseFloat(match[1], 64); err == nil {
				pricing = append(pricing, FetchedPricingInfo{
					InputCostPerMTok: price,
				})
			}
		}
	}

	return pricing
}

// GenerateClaudeJSON generates a claude.json file from fetched data
// This combines the embedded pricing as a base with any updates
func GenerateClaudeJSON(models []FetchedModelInfo, pricing []FetchedPricingInfo) ([]byte, error) {
	// Start with a base structure
	data := PricingData{
		Provider:    ProviderAnthropic,
		LastUpdated: time.Now().UTC().Format(time.RFC3339),
		Models:      make(map[string]ModelEntry),
	}

	// Apply cache pricing formula: Read = 0.1× input, Write = 1.25× input
	addModelWithCache := func(modelID string, input, output float64, aliases []string, deprecated bool) {
		data.Models[modelID] = ModelEntry{
			Aliases:             aliases,
			InputCostPerMTok:    input,
			OutputCostPerMTok:   output,
			CacheReadCostPerMTok:  input * 0.1,
			CacheWriteCostPerMTok: input * 1.25,
			Deprecated:          deprecated,
		}
	}

	// Add known models with correct pricing
	// In a full implementation, this would merge fetched data with known models

	// Latest models (Claude 4.5 series)
	addModelWithCache("claude-sonnet-4-5-20250929", 3, 15, []string{"claude-sonnet-4-5", "claude-sonnet-4-5-latest"}, false)
	addModelWithCache("claude-haiku-4-5-20251001", 1, 5, []string{"claude-haiku-4-5", "claude-haiku-4-5-latest"}, false)
	addModelWithCache("claude-opus-4-5-20251101", 5, 25, []string{"claude-opus-4-5", "claude-opus-4-5-latest"}, false)

	// Claude 4.1 series
	addModelWithCache("claude-opus-4-1-20250805", 15, 75, []string{"claude-opus-4-1", "claude-opus-4-1-latest"}, false)

	// Claude 4.0 series
	addModelWithCache("claude-sonnet-4-20250514", 3, 15, []string{"claude-sonnet-4", "claude-sonnet-4-0", "claude-sonnet-4-latest"}, false)
	addModelWithCache("claude-opus-4-20250514", 15, 75, []string{"claude-opus-4", "claude-opus-4-0", "claude-opus-4-latest"}, false)

	// Claude 3.7 series
	addModelWithCache("claude-3-7-sonnet-20250219", 3, 15, []string{"claude-3-7-sonnet", "claude-3-7-sonnet-latest", "claude-3.7-sonnet"}, false)

	// Claude 3.5 series
	addModelWithCache("claude-3-5-sonnet-20241022", 3, 15, []string{"claude-3-5-sonnet", "claude-3-5-sonnet-v2", "claude-3.5-sonnet"}, false)
	addModelWithCache("claude-3-5-sonnet-20240620", 3, 15, []string{"claude-3-5-sonnet-v1"}, true)
	addModelWithCache("claude-3-5-haiku-20241022", 0.8, 4, []string{"claude-3-5-haiku", "claude-3-5-haiku-latest", "claude-3.5-haiku", "claude-haiku-3-5"}, false)

	// Claude 3 series
	addModelWithCache("claude-3-opus-20240229", 15, 75, []string{"claude-3-opus", "claude-3-opus-latest", "claude-opus-3"}, false)
	addModelWithCache("claude-3-sonnet-20240229", 3, 15, []string{"claude-3-sonnet"}, true)
	addModelWithCache("claude-3-haiku-20240307", 0.25, 1.25, []string{"claude-3-haiku"}, false)

	return json.MarshalIndent(data, "", "  ")
}

// FetchAndGenerate fetches the latest pricing from Anthropic and generates JSON
func (f *ClaudeFetcher) FetchAndGenerate() ([]byte, error) {
	// Fetch pages (currently not used for parsing, but prepared for future use)
	_, err := f.FetchModelsPage()
	if err != nil {
		return nil, fmt.Errorf("fetching models: %w", err)
	}

	_, err = f.FetchPricingPage()
	if err != nil {
		return nil, fmt.Errorf("fetching pricing: %w", err)
	}

	// For now, use hardcoded known models
	// In a full implementation, this would parse the fetched HTML
	return GenerateClaudeJSON(nil, nil)
}

// ExtractModelFamily extracts the model family from a model ID
// e.g., "claude-sonnet-4-5-20250929" -> "claude-sonnet-4-5"
func ExtractModelFamily(modelID string) string {
	// Remove date suffix if present (format: -YYYYMMDD)
	datePattern := regexp.MustCompile(`-\d{8}$`)
	return strings.TrimSuffix(modelID, datePattern.FindString(modelID))
}
