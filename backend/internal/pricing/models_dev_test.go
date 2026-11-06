package pricing

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// An excerpt of models.dev: a tier, the older context_over_200k form, two IDs
// of one model, a deprecated model, and models AI Observer does not price
const modelsDevCatalog = `{
  "openai": {"models": {
    "gpt-9": {"id": "gpt-9", "modalities": {"input": ["text"], "output": ["text"]},
      "cost": {"input": 2, "output": 10, "cache_read": 0.2, "cache_write": 2.5,
        "tiers": [{"input": 4, "output": 15, "cache_read": 0.4, "tier": {"type": "context", "size": 272000}}]}},
    "gpt-9-mini": {"id": "gpt-9-mini", "cost": {"input": 0.5, "output": 2, "context_over_200k": {"input": 1, "output": 4}}},
    "gpt-old": {"id": "gpt-old", "status": "deprecated", "cost": {"input": 1, "output": 2}},
    "gpt-image-9": {"id": "gpt-image-9", "modalities": {"output": ["image"]}, "cost": {"input": 5, "output": 30}},
    "text-embedding-9": {"id": "text-embedding-9", "modalities": {"output": ["text"]}, "cost": {"input": 0.02, "output": 0}},
    "tts-9": {"id": "tts-9", "cost": null},
    "gpt-tiered": {"id": "gpt-tiered", "cost": {"input": 1, "output": 2, "tiers": [
      {"input": 3, "output": 6, "tier": {"type": "context", "size": 1000000}},
      {"input": 2, "output": 4, "tier": {"type": "context", "size": 200000}}]}}
  }}
}`

func TestModelsDevEntry(t *testing.T) {
	catalog, err := ParseModelsDev([]byte(modelsDevCatalog))
	if err != nil {
		t.Fatal(err)
	}
	models := catalog["openai"].Models

	gpt9, _, ok := modelsDevEntry(models["gpt-9"])
	if !ok || gpt9.InputCostPerMTok != 2 || gpt9.CacheWriteCostPerMTok != 2.5 || gpt9.LongContext == nil ||
		gpt9.LongContext.ThresholdInputTokens != 272000 || gpt9.LongContext.OutputCostPerMTok != 15 {
		t.Errorf("gpt-9 = %+v (%+v)", gpt9, gpt9.LongContext)
	}
	mini, _, _ := modelsDevEntry(models["gpt-9-mini"])
	if mini.LongContext == nil || mini.LongContext.ThresholdInputTokens != 200000 || mini.LongContext.InputCostPerMTok != 1 {
		t.Errorf("gpt-9-mini long context = %+v, want the context_over_200k prices", mini.LongContext)
	}
	if old, _, _ := modelsDevEntry(models["gpt-old"]); !old.Deprecated {
		t.Error("a deprecated models.dev model is not deprecated")
	}
	tiered, note, _ := modelsDevEntry(models["gpt-tiered"])
	if tiered.LongContext.ThresholdInputTokens != 200000 || !strings.Contains(note, "2 context tiers") {
		t.Errorf("tiered = %+v, note %q; want the lowest tier and a note", tiered.LongContext, note)
	}
	for _, id := range []string{"gpt-image-9", "text-embedding-9", "tts-9"} {
		if _, _, ok := modelsDevEntry(models[id]); ok {
			t.Errorf("%s is priced; only models with text output prices are", id)
		}
	}
}

const currentCodex = `{
  "provider": "openai",
  "lastUpdated": "2026-10-27T00:00:00Z",
  "models": {
    "gpt-9-2026-01-01": {
      "aliases": ["gpt-9"],
      "inputCostPerMTok": 1,
      "outputCostPerMTok": 10,
      "cacheReadCostPerMTok": 0.2,
      "cacheWriteCostPerMTok": 2.5,
      "longContext": {
        "thresholdInputTokens": 272000,
        "inputCostPerMTok": 4,
        "outputCostPerMTok": 15,
        "cacheReadCostPerMTok": 0.4
      }
    },
    "gpt-9-mini": {
      "inputCostPerMTok": 0.04,
      "outputCostPerMTok": 2
    },
    "gpt-old": {
      "inputCostPerMTok": 1,
      "outputCostPerMTok": 2
    },
    "gpt-retired": {
      "inputCostPerMTok": 3,
      "outputCostPerMTok": 6,
      "deprecated": true
    }
  }
}
`

func TestUpdatePricingFile(t *testing.T) {
	catalog, err := ParseModelsDev([]byte(modelsDevCatalog))
	if err != nil {
		t.Fatal(err)
	}
	models := catalog["openai"].Models
	// A second ID of the same model (an alias), and a model that is no longer
	// deprecated on models.dev
	models["gpt-9-2026-01-01"] = models["gpt-9"]
	models["gpt-retired"] = ModelsDevModel{ID: "gpt-retired", Cost: &ModelsDevCost{ModelsDevPrices: ModelsDevPrices{Input: 3, Output: 6}}}

	now := time.Date(2026, 11, 5, 15, 0, 0, 0, time.UTC)
	update, err := UpdatePricingFile([]byte(currentCodex), ProviderOpenAI, models, UpdateOptions{}, now)
	if err != nil {
		t.Fatal(err)
	}
	m := update.Data.Models

	// Matched through its alias, keeping key and aliases; counted once
	if got := m["gpt-9-2026-01-01"]; got.InputCostPerMTok != 2 || len(got.Aliases) != 1 {
		t.Errorf("gpt-9-2026-01-01 = %+v", got)
	}
	if len(update.Changed) != 1 || !strings.Contains(update.Changed[0], "gpt-9-2026-01-01: input 1 -> 2") {
		t.Errorf("changed = %v", update.Changed)
	}
	// A change of more than 10x (0.04 -> 0.5) is refused
	if m["gpt-9-mini"].InputCostPerMTok != 0.04 || len(update.Refused) != 1 || !strings.Contains(update.Refused[0], "gpt-9-mini") {
		t.Errorf("gpt-9-mini = %+v, refused = %v", m["gpt-9-mini"], update.Refused)
	}
	// Deprecation comes from models.dev and is never undone
	if !m["gpt-old"].Deprecated || len(update.Deprecated) != 1 || update.Deprecated[0] != "gpt-old" {
		t.Errorf("gpt-old = %+v, deprecated = %v", m["gpt-old"], update.Deprecated)
	}
	if !m["gpt-retired"].Deprecated {
		t.Error("gpt-retired lost its deprecation")
	}
	if update.Unchanged != 2 {
		t.Errorf("unchanged = %d, want 2 (gpt-old's prices, gpt-retired)", update.Unchanged)
	}
	// New models after the existing ones, sorted; unpriced ones left out
	wantOrder := "gpt-9-2026-01-01,gpt-9-mini,gpt-old,gpt-retired,gpt-tiered"
	if got := strings.Join(update.Order, ","); got != wantOrder {
		t.Errorf("order = %s, want %s", got, wantOrder)
	}
	if len(update.Added) != 1 || len(update.Notes) != 1 || len(update.Kept) != 0 {
		t.Errorf("added = %v, notes = %v, kept = %v", update.Added, update.Notes, update.Kept)
	}
	if update.Data.LastUpdated != "2026-11-05T00:00:00Z" || !strings.Contains(update.Data.Source, "models.dev") {
		t.Errorf("lastUpdated = %q, source = %q", update.Data.LastUpdated, update.Data.Source)
	}

	// The refused change is applied when allowed
	allowed, err := UpdatePricingFile([]byte(currentCodex), ProviderOpenAI, models, UpdateOptions{AllowLargeChanges: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if allowed.Data.Models["gpt-9-mini"].InputCostPerMTok != 0.5 || len(allowed.Refused) != 0 {
		t.Errorf("with large changes allowed: gpt-9-mini = %+v", allowed.Data.Models["gpt-9-mini"])
	}

	// Models models.dev does not list keep their prices
	delete(models, "gpt-old")
	partial, err := UpdatePricingFile([]byte(currentCodex), ProviderOpenAI, models, UpdateOptions{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(partial.Kept) != 1 || partial.Kept[0] != "gpt-old" || partial.Data.Models["gpt-old"].InputCostPerMTok != 1 {
		t.Errorf("kept = %v, gpt-old = %+v", partial.Kept, partial.Data.Models["gpt-old"])
	}

	// The written file loads and resolves the models
	provider, err := parseProvider(FormatPricingFile(update.Data, update.Order), "updated")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"gpt-9", "gpt-9-2026-01-01", "gpt-tiered"} {
		if provider.GetPricing(name) == nil {
			t.Errorf("%q does not resolve in the written file", name)
		}
	}
}

func TestFetchModelsDev(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api.json":
			w.Write([]byte(modelsDevCatalog))
		case "/empty.json":
			w.Write([]byte("{}"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	f := NewModelsDevFetcher()
	f.url = server.URL + "/api.json"
	catalog, err := f.Fetch(context.Background())
	if err != nil || len(catalog["openai"].Models) != 7 {
		t.Fatalf("catalog = %v, %v", catalog, err)
	}
	f.url = server.URL + "/missing"
	if _, err := f.Fetch(context.Background()); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %v, want the status", err)
	}
	f.url = server.URL + "/empty.json"
	if _, err := f.Fetch(context.Background()); err == nil {
		t.Error("an empty catalog was accepted")
	}
}

// Every pricing file is maintained by pricing update
func TestModelsDevTargetsCoverThePricingFiles(t *testing.T) {
	for _, target := range ModelsDevTargets {
		raw, err := EmbeddedPricingFile(target.File)
		if err != nil {
			t.Fatal(err)
		}
		var data PricingData
		if err := json.Unmarshal(raw, &data); err != nil {
			t.Fatal(err)
		}
		if data.Provider != target.Pricing {
			t.Errorf("%s: provider %q, want %q", target.File, data.Provider, target.Pricing)
		}
	}
}

// The built-in files come out byte for byte, so updates only change the lines
// of changed prices
func TestFormatPricingFileKeepsTheBuiltInLayout(t *testing.T) {
	for _, name := range []string{"claude.json", "codex.json", "gemini.json", "github_copilot.json"} {
		embedded, err := EmbeddedPricingFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var data PricingData
		if err := json.Unmarshal(embedded, &data); err != nil {
			t.Fatal(err)
		}
		order, err := modelKeyOrder(embedded)
		if err != nil {
			t.Fatal(err)
		}
		if got := FormatPricingFile(data, order); string(got) != string(embedded) {
			t.Errorf("%s: formatted file differs from the built-in one", name)
		}
	}
}

// The built-in Copilot prices include the models GitHub prices but providers
// do not publish
func TestBuiltInCopilotPricesCoverGitHubOnlyModels(t *testing.T) {
	for _, model := range []string{"Grok 4.7", "grok-4.5", "Kimi K3", "MAI-Code-1.1-Flash", "Claude Opus 4.8 (fast mode) (preview)"} {
		if GetCopilotPricing(model) == nil {
			t.Errorf("%q has no Copilot price", model)
		}
	}
	// $2 / $6 per 1M tokens, and $4 / $12 above 200K input tokens
	for _, c := range []struct {
		input, output int64
		want          float64
	}{{100_000, 100_000, 0.8}, {1_000_000, 1_000_000, 16}} {
		cost := CalculateCopilotCost("Grok 4.7", CopilotTokenUsage{InputTokens: c.input, OutputTokens: c.output})
		if cost == nil || math.Abs(*cost-c.want) > 1e-9 {
			t.Errorf("Grok 4.7 cost for %d in + %d out = %v, want $%v", c.input, c.output, derefCost(cost), c.want)
		}
	}
}

func derefCost(cost *float64) any {
	if cost == nil {
		return nil
	}
	return *cost
}
