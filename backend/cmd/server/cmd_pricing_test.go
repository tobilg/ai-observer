package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/pricing"
)

type fakeModelsDev struct {
	catalog pricing.ModelsDevCatalog
	err     error
}

func (f *fakeModelsDev) Fetch(context.Context) (pricing.ModelsDevCatalog, error) {
	return f.catalog, f.err
}

// A catalog with a built-in model with a new price and a new model per
// provider (claude-sonnet-5-5 is built in at $2/$10)
func testCatalog(t *testing.T) pricing.ModelsDevCatalog {
	t.Helper()
	catalog, err := pricing.ParseModelsDev([]byte(`{
		"anthropic": {"models": {
			"claude-sonnet-5-5": {"id": "claude-sonnet-5-5", "cost": {"input": 2, "output": 11, "cache_read": 0.1, "cache_write": 2.5}},
			"claude-future-9": {"id": "claude-future-9", "cost": {"input": 1, "output": 5}}
		}},
		"openai": {"models": {"gpt-future-9": {"id": "gpt-future-9", "cost": {"input": 1, "output": 5}}}},
		"google": {"models": {"gemini-future-9": {"id": "gemini-future-9", "cost": {"input": 1, "output": 5}}}},
		"github-copilot": {"models": {"copilot-future-9": {"id": "copilot-future-9", "cost": {"input": 1, "output": 5}}}}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func readPricingFile(t *testing.T, path string) pricing.PricingData {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var data pricing.PricingData
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPricingUpdateWritesAllProvidersToPricingDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pricing")
	t.Setenv("AI_OBSERVER_PRICING_DIR", dir)

	var out bytes.Buffer
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	if err := runPricingUpdate(context.Background(), nil, &out, &fakeModelsDev{catalog: testCatalog(t)}, now); err != nil {
		t.Fatal(err)
	}

	for _, target := range pricing.ModelsDevTargets {
		data := readPricingFile(t, filepath.Join(dir, target.File))
		if data.LastUpdated != "2026-11-05T00:00:00Z" || !strings.Contains(data.Source, "models.dev") || data.Provider != target.Pricing {
			t.Errorf("%s: provider %q, lastUpdated %q, source %q", target.File, data.Provider, data.LastUpdated, data.Source)
		}
	}
	claude := readPricingFile(t, filepath.Join(dir, "claude.json"))
	if claude.Models["claude-sonnet-5-5"].OutputCostPerMTok != 11 || claude.Models["claude-future-9"].InputCostPerMTok != 1 {
		t.Errorf("claude-sonnet-5-5 = %+v, claude-future-9 = %+v", claude.Models["claude-sonnet-5-5"], claude.Models["claude-future-9"])
	}
	// Built-in models models.dev does not list stay
	if _, ok := claude.Models["claude-3-opus-20240229"]; !ok {
		t.Error("a built-in model missing from models.dev was dropped")
	}

	text := out.String()
	for _, want := range []string{
		"claude.json: 1 added, 1 changed, 0 unchanged",
		"  + claude-future-9: $1 in / $5 out",
		"  ~ claude-sonnet-5-5: output 10 -> 11",
		"github_copilot.json: 1 added",
		"AI Observer uses the new prices from its next start",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}

	// A second run starts from the written files
	out.Reset()
	if err := runPricingUpdate(context.Background(), nil, &out, &fakeModelsDev{catalog: testCatalog(t)}, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "claude.json: 0 added, 0 changed, 2 unchanged") {
		t.Errorf("second run output:\n%s", out.String())
	}
}

func TestPricingUpdateOutputFlag(t *testing.T) {
	t.Setenv("AI_OBSERVER_PRICING_DIR", "")
	dir := t.TempDir()
	var out bytes.Buffer
	if err := runPricing(context.Background(), []string{"update", "--output", dir}, &out, &fakeModelsDev{catalog: testCatalog(t)}); err != nil {
		t.Fatal(err)
	}
	if data := readPricingFile(t, filepath.Join(dir, "codex.json")); data.Models["gpt-future-9"].OutputCostPerMTok != 5 {
		t.Errorf("gpt-future-9 = %+v", data.Models["gpt-future-9"])
	}
	if !strings.Contains(out.String(), "set AI_OBSERVER_PRICING_DIR to this directory") {
		t.Errorf("output does not say how to use the files:\n%s", out.String())
	}
}

func TestPricingUpdateWritesNothingOnFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AI_OBSERVER_PRICING_DIR", dir)

	incomplete := testCatalog(t)
	delete(incomplete, "google")
	unpriced := testCatalog(t)
	unpriced["openai"].Models["gpt-future-9"] = pricing.ModelsDevModel{ID: "gpt-future-9"}
	for name, fetcher := range map[string]*fakeModelsDev{
		"fetch error":      {err: errors.New("network down")},
		"missing provider": {catalog: incomplete},
		"no priced models": {catalog: unpriced},
	} {
		var out bytes.Buffer
		if err := runPricingUpdate(context.Background(), nil, &out, fetcher, time.Now()); err == nil {
			t.Errorf("%s: no error", name)
		}
		if entries, _ := os.ReadDir(dir); len(entries) != 0 {
			t.Errorf("%s: files were written: %v", name, entries)
		}
	}

	// A broken current file is reported, not overwritten
	if err := os.WriteFile(filepath.Join(dir, "gemini.json"), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runPricingUpdate(context.Background(), nil, &out, &fakeModelsDev{catalog: testCatalog(t)}, time.Now()); err == nil {
		t.Error("a broken pricing file was overwritten")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("files were written next to the broken one: %v", entries)
	}
}

func TestPricingUpdateRefusesLargeChanges(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AI_OBSERVER_PRICING_DIR", dir)
	catalog := testCatalog(t)
	// $2 -> $200 per million input tokens: likely a unit error
	catalog["anthropic"].Models["claude-sonnet-5-5"] = pricing.ModelsDevModel{ID: "claude-sonnet-5-5",
		Cost: &pricing.ModelsDevCost{ModelsDevPrices: pricing.ModelsDevPrices{Input: 200, Output: 10, CacheRead: 0.1, CacheWrite: 2.5}}}

	var out bytes.Buffer
	if err := runPricingUpdate(context.Background(), nil, &out, &fakeModelsDev{catalog: catalog}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := readPricingFile(t, filepath.Join(dir, "claude.json")).Models["claude-sonnet-5-5"].InputCostPerMTok; got != 2 {
		t.Errorf("input = %v, want the refused change not applied", got)
	}
	if !strings.Contains(out.String(), "refused (more than 10x): claude-sonnet-5-5") || !strings.Contains(out.String(), "--allow-large-changes") {
		t.Errorf("output:\n%s", out.String())
	}

	out.Reset()
	if err := runPricing(context.Background(), []string{"update", "--allow-large-changes"}, &out, &fakeModelsDev{catalog: catalog}); err != nil {
		t.Fatal(err)
	}
	if got := readPricingFile(t, filepath.Join(dir, "claude.json")).Models["claude-sonnet-5-5"].InputCostPerMTok; got != 200 {
		t.Errorf("input with --allow-large-changes = %v, want 200", got)
	}
}

func TestPricingCommandErrors(t *testing.T) {
	t.Setenv("AI_OBSERVER_PRICING_DIR", "")
	fetcher := &fakeModelsDev{catalog: testCatalog(t)}
	var out bytes.Buffer
	for name, args := range map[string][]string{
		"no command":      nil,
		"unknown command": {"refresh"},
		"no directory":    {"update"},
		"unexpected arg":  {"update", "extra"},
		"unknown flag":    {"update", "--bogus"},
	} {
		out.Reset()
		if err := runPricing(context.Background(), args, &out, fetcher); err == nil || errors.Is(err, flag.ErrHelp) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	if err := runPricing(context.Background(), []string{"--help"}, &out, fetcher); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("--help: error = %v, want flag.ErrHelp", err)
	}
	if !strings.Contains(out.String(), "Usage: ai-observer pricing update") {
		t.Errorf("help output:\n%s", out.String())
	}
}
