package pricing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFiles(t *testing.T) {
	savedOverrides := loadedOverrides
	savedModels := registry.gemini.models
	t.Cleanup(func() {
		loadedOverrides = savedOverrides
		registry.gemini.models = savedModels
	})
	loadedOverrides = map[string]FileInfo{}
	models := make(map[string]*ModelPricing, len(savedModels))
	for k, v := range savedModels {
		models[k] = v
	}
	registry.gemini.models = models

	files := Files()
	if len(files) != 4 {
		t.Fatalf("files = %+v", files)
	}
	want := map[string]Provider{"claude.json": ProviderAnthropic, "codex.json": ProviderOpenAI, "gemini.json": ProviderGoogle, "github_copilot.json": ProviderCopilot}
	for _, f := range files {
		if want[f.File] != f.Provider || f.LastUpdated == "" || f.Models == 0 || f.Override != "" {
			t.Errorf("built-in file = %+v", f)
		}
	}

	dir := t.TempDir()
	override := `{"provider": "google", "lastUpdated": "2026-11-10T00:00:00Z", "models": {"gemini-future": {"inputCostPerMTok": 1, "outputCostPerMTok": 2}}}`
	if err := os.WriteFile(filepath.Join(dir, "gemini.json"), []byte(override), 0o644); err != nil {
		t.Fatal(err)
	}
	before := len(savedModels)
	if _, err := LoadOverrides(dir); err != nil {
		t.Fatal(err)
	}
	for _, f := range Files() {
		if f.File != "gemini.json" {
			if f.Override != "" {
				t.Errorf("%s has override %q", f.File, f.Override)
			}
			continue
		}
		if f.Override != filepath.Join(dir, "gemini.json") || f.OverrideLastUpdated != "2026-11-10T00:00:00Z" || f.Models != before+1 {
			t.Errorf("gemini.json with override = %+v, want %d models", f, before+1)
		}
	}
}
