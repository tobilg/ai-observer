package pricing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOverridesReplacesAndAddsModels(t *testing.T) {
	saved := *registry.claude
	savedModels := make(map[string]*ModelPricing, len(saved.models))
	for k, v := range saved.models {
		savedModels[k] = v
	}
	savedAliases := make(map[string]string, len(saved.aliases))
	for k, v := range saved.aliases {
		savedAliases[k] = v
	}
	t.Cleanup(func() {
		registry.claude.models = savedModels
		registry.claude.aliases = savedAliases
	})

	existing := registry.claude.ListModels()[0]
	dir := t.TempDir()
	override := `{
		"provider": "anthropic",
		"models": {
			"` + existing + `": {"inputCostPerMTok": 1, "outputCostPerMTok": 2},
			"claude-future-9": {"aliases": ["future"], "inputCostPerMTok": 3, "outputCostPerMTok": 4}
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, "claude.json"), []byte(override), 0644); err != nil {
		t.Fatalf("writing override: %v", err)
	}

	loaded, err := LoadOverrides(dir)
	if err != nil {
		t.Fatalf("LoadOverrides: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("loaded %v, want only claude.json", loaded)
	}

	if p := registry.claude.GetPricing(existing); p == nil || p.InputCostPerToken != 1e-6 {
		t.Errorf("%s pricing = %+v, want the override's input price", existing, p)
	}
	if p := registry.claude.GetPricing("future"); p == nil || p.OutputCostPerToken != 4e-6 {
		t.Errorf("new model alias pricing = %+v, want the override", p)
	}
}

func TestLoadOverridesRejectsInvalidFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex.json"), []byte("{not json"), 0644); err != nil {
		t.Fatalf("writing override: %v", err)
	}
	if _, err := LoadOverrides(dir); err == nil {
		t.Error("expected an error for an invalid override file")
	}
}
