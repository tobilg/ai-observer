package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"sort"
	"time"
)

// models.dev (https://models.dev) is an open database of AI models and their
// per-token prices, maintained by the OpenCode developers under the MIT
// License. "ai-observer pricing update" merges its prices into the pricing
// files. None of the providers publishes prices through an API.
const (
	// ModelsDevURL is the models.dev catalog of all providers and models
	ModelsDevURL = "https://models.dev/api.json"
	// ModelsDevSource credits models.dev in the pricing files
	ModelsDevSource = "https://models.dev (models.dev contributors, MIT License)"

	// largeChangeFactor bounds the price changes an update applies without
	// --allow-large-changes: larger ones are more likely errors (for example
	// a price per token instead of per million tokens) than price changes
	largeChangeFactor = 10
)

// ModelsDevTarget maps a pricing file to its models.dev provider
type ModelsDevTarget struct {
	File     string   // pricing file in data/, such as "claude.json"
	Provider string   // models.dev provider ID, such as "anthropic"
	Pricing  Provider // the provider field of the pricing file
}

// ModelsDevTargets are the pricing files that pricing update maintains
var ModelsDevTargets = []ModelsDevTarget{
	{File: "claude.json", Provider: "anthropic", Pricing: ProviderAnthropic},
	{File: "codex.json", Provider: "openai", Pricing: ProviderOpenAI},
	{File: "gemini.json", Provider: "google", Pricing: ProviderGoogle},
	{File: "github_copilot.json", Provider: "github-copilot", Pricing: ProviderCopilot},
}

// ModelsDevCatalog is the models.dev catalog by provider ID. Only the fields
// AI Observer uses are read.
type ModelsDevCatalog map[string]struct {
	Models map[string]ModelsDevModel `json:"models"`
}

// ModelsDevModel is a model of the models.dev catalog
type ModelsDevModel struct {
	ID         string `json:"id"`
	Status     string `json:"status"` // "deprecated", "beta", "alpha" or ""
	Modalities *struct {
		Output []string `json:"output"`
	} `json:"modalities"`
	Cost *ModelsDevCost `json:"cost"`
}

// ModelsDevCost holds prices per million tokens
type ModelsDevCost struct {
	ModelsDevPrices
	// Tiers apply to prompts larger than their tier size
	Tiers []struct {
		ModelsDevPrices
		Tier struct {
			Type string `json:"type"` // "context"
			Size int64  `json:"size"` // prompt tokens
		} `json:"tier"`
	} `json:"tiers"`
	// ContextOver200K is the older form of a 200K-token context tier
	ContextOver200K *ModelsDevPrices `json:"context_over_200k"`
}

// ModelsDevPrices are prices per million tokens
type ModelsDevPrices struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cache_read"`
	CacheWrite float64 `json:"cache_write"`
}

// ModelsDevFetcher downloads the models.dev catalog
type ModelsDevFetcher struct {
	httpClient *http.Client
	url        string
}

// NewModelsDevFetcher creates a fetcher with a bounded timeout
func NewModelsDevFetcher() *ModelsDevFetcher {
	return &ModelsDevFetcher{httpClient: &http.Client{Timeout: 60 * time.Second}, url: ModelsDevURL}
}

// Fetch returns the models.dev catalog
func (f *ModelsDevFetcher) Fetch(ctx context.Context) (ModelsDevCatalog, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating models.dev request: %w", err)
	}
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching models.dev: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("fetching models.dev: %s returned status %d: %s", f.url, resp.StatusCode, body)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 128<<20))
	if err != nil {
		return nil, fmt.Errorf("reading models.dev: %w", err)
	}
	return ParseModelsDev(data)
}

// ParseModelsDev parses the models.dev catalog (api.json)
func ParseModelsDev(data []byte) (ModelsDevCatalog, error) {
	var catalog ModelsDevCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("parsing models.dev: %w", err)
	}
	if len(catalog) == 0 {
		return nil, fmt.Errorf("the models.dev catalog has no providers")
	}
	return catalog, nil
}

// modelsDevEntry converts a priced text model. It returns false for models
// without a text output price (images, speech, embeddings), which AI
// Observer does not price, and a note when tiers had to be left out.
func modelsDevEntry(m ModelsDevModel) (ModelEntry, string, bool) {
	if m.Cost == nil || m.Cost.Output <= 0 {
		return ModelEntry{}, "", false
	}
	if m.Modalities != nil && !slices.Contains(m.Modalities.Output, "text") {
		return ModelEntry{}, "", false
	}
	c := m.Cost
	entry := ModelEntry{
		InputCostPerMTok:      c.Input,
		OutputCostPerMTok:     c.Output,
		CacheReadCostPerMTok:  c.CacheRead,
		CacheWriteCostPerMTok: c.CacheWrite,
		Deprecated:            m.Status == "deprecated",
	}
	var note string
	var contextTiers int
	for _, t := range c.Tiers {
		if t.Tier.Type != "context" {
			continue
		}
		contextTiers++
		if entry.LongContext == nil || t.Tier.Size < entry.LongContext.ThresholdInputTokens {
			entry.LongContext = &LongContextEntry{
				ThresholdInputTokens:  t.Tier.Size,
				InputCostPerMTok:      t.Input,
				OutputCostPerMTok:     t.Output,
				CacheReadCostPerMTok:  t.CacheRead,
				CacheWriteCostPerMTok: t.CacheWrite,
			}
		}
	}
	if contextTiers > 1 {
		note = fmt.Sprintf("%s has %d context tiers; only the lowest is used", m.ID, contextTiers)
	}
	if entry.LongContext == nil && c.ContextOver200K != nil {
		p := c.ContextOver200K
		entry.LongContext = &LongContextEntry{
			ThresholdInputTokens:  200000,
			InputCostPerMTok:      p.Input,
			OutputCostPerMTok:     p.Output,
			CacheReadCostPerMTok:  p.CacheRead,
			CacheWriteCostPerMTok: p.CacheWrite,
		}
	}
	return entry, note, true
}

// PricingUpdate is the result of merging models.dev into a pricing file
type PricingUpdate struct {
	Data  PricingData
	Order []string // model keys in file order
	// Added are new models, Changed models with new prices, Deprecated
	// models models.dev newly marks deprecated
	Added, Changed, Deprecated []string
	Unchanged                  int
	// Kept are the models models.dev does not list; they keep their prices
	Kept []string
	// Refused are price changes larger than largeChangeFactor, not applied
	Refused []string
	// Notes explain prices that could only be taken in part
	Notes []string
}

// UpdateOptions controls UpdatePricingFile
type UpdateOptions struct {
	// AllowLargeChanges applies price changes beyond largeChangeFactor
	AllowLargeChanges bool
}

// UpdatePricingFile merges the models of a models.dev provider into the
// pricing file current. Models matched by key or alias get the models.dev
// prices and keep their key and aliases; deprecation is never undone. New
// models are added after the existing ones, sorted by ID. Models models.dev
// does not list keep their prices. lastUpdated becomes the day of now.
func UpdatePricingFile(current []byte, provider Provider, models map[string]ModelsDevModel, opts UpdateOptions, now time.Time) (*PricingUpdate, error) {
	var data PricingData
	if err := json.Unmarshal(current, &data); err != nil {
		return nil, fmt.Errorf("parsing the current pricing file: %w", err)
	}
	if data.Models == nil {
		data.Models = make(map[string]ModelEntry)
	}
	if data.Provider == "" {
		data.Provider = provider
	}
	order, err := modelKeyOrder(current)
	if err != nil {
		return nil, err
	}

	normalize := func(name string) string { return normalizeGitHubModelAlias(name, false) }
	index := make(map[string]string)
	for _, key := range order {
		index[normalize(key)] = key
	}
	for _, key := range order {
		for _, alias := range data.Models[key].Aliases {
			if _, taken := index[normalize(alias)]; !taken {
				index[normalize(alias)] = key
			}
		}
	}

	ids := make([]string, 0, len(models))
	for id := range models {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	update := &PricingUpdate{}
	touched := make(map[string]bool)
	for _, id := range ids {
		fetched, note, ok := modelsDevEntry(models[id])
		if !ok {
			continue
		}
		if note != "" {
			update.Notes = append(update.Notes, note)
		}
		key, exists := index[normalize(id)]
		if !exists {
			data.Models[id] = fetched
			order = append(order, id)
			index[normalize(id)] = id
			touched[id] = true
			added := id + ": " + describePrices(fetched)
			if fetched.Deprecated {
				added += " (deprecated)"
			}
			update.Added = append(update.Added, added)
			continue
		}

		existing := data.Models[key]
		fetched.Aliases = existing.Aliases
		newlyDeprecated := fetched.Deprecated && !existing.Deprecated
		fetched.Deprecated = fetched.Deprecated || existing.Deprecated
		changes := priceChanges(existing, fetched)
		refused := changes != "" && !opts.AllowLargeChanges && largePriceChange(existing, fetched)
		if refused {
			update.Refused = append(update.Refused, fmt.Sprintf("%s (models.dev %s): %s", key, id, changes))
			fetched = existing
			fetched.Deprecated = existing.Deprecated || newlyDeprecated
		}
		data.Models[key] = fetched
		switch {
		case touched[key], refused:
			// Another models.dev ID of the same model (an alias) came first,
			// or the change is reported as refused
		case changes != "":
			update.Changed = append(update.Changed, key+": "+changes)
		default:
			update.Unchanged++
		}
		if newlyDeprecated && !touched[key] {
			update.Deprecated = append(update.Deprecated, key)
		}
		touched[key] = true
	}
	for _, key := range order {
		if !touched[key] {
			update.Kept = append(update.Kept, key)
		}
	}

	data.Source = ModelsDevSource
	data.LastUpdated = now.UTC().Truncate(24 * time.Hour).Format(time.RFC3339)
	update.Data = data
	update.Order = order
	return update, nil
}

// largePriceChange reports whether a set price changes by more than
// largeChangeFactor in either direction
func largePriceChange(before, after ModelEntry) bool {
	large := func(a, b float64) bool {
		return a > 0 && b > 0 && (b > a*largeChangeFactor || a > b*largeChangeFactor)
	}
	if large(before.InputCostPerMTok, after.InputCostPerMTok) || large(before.OutputCostPerMTok, after.OutputCostPerMTok) ||
		large(before.CacheReadCostPerMTok, after.CacheReadCostPerMTok) || large(before.CacheWriteCostPerMTok, after.CacheWriteCostPerMTok) {
		return true
	}
	if before.LongContext != nil && after.LongContext != nil {
		return large(before.LongContext.InputCostPerMTok, after.LongContext.InputCostPerMTok) ||
			large(before.LongContext.OutputCostPerMTok, after.LongContext.OutputCostPerMTok)
	}
	return false
}
