package pricing

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// overrideFiles maps the pricing files that LoadOverrides reads to the
// provider they extend. The names and format match the embedded data files.
var overrideFiles = map[string]func() **providerData{
	"claude.json":         func() **providerData { return &registry.claude },
	"codex.json":          func() **providerData { return &registry.codex },
	"gemini.json":         func() **providerData { return &registry.gemini },
	"github_copilot.json": func() **providerData { return &registry.copilot },
}

// LoadOverrides merges the pricing files in dir over the embedded pricing, so
// new models and price changes can be used without a new release. A model in
// an override file replaces the embedded model of the same name, and its
// aliases are added. Files that do not exist are skipped. Costs already stored
// keep the prices they were calculated with; re-import files to recalculate.
//
// It must be called before costs are calculated, at startup.
func LoadOverrides(dir string) ([]string, error) {
	var loaded []string
	for name, target := range overrideFiles {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return loaded, fmt.Errorf("reading pricing override: %w", err)
		}
		override, err := parseProvider(data, path)
		if err != nil {
			return loaded, err
		}

		provider := target()
		if *provider == nil {
			*provider = override
		} else {
			(*provider).merge(override)
		}
		loaded = append(loaded, path)
	}

	// Match the GitHub Models catalog names again, so that they also resolve
	// to models the overrides added
	if len(loaded) > 0 && registry.copilot != nil {
		catalog, err := loadGitHubModelsCatalog("data/github_models_catalog.json")
		if err != nil {
			return loaded, err
		}
		applyGitHubModelsCatalogAliases(registry.copilot, catalog.Models)
	}
	return loaded, nil
}

// merge adds the models and aliases of other, replacing models of the same name
func (p *providerData) merge(other *providerData) {
	for name, pricing := range other.models {
		p.models[name] = pricing
	}
	for alias, canonical := range other.aliases {
		p.aliases[alias] = canonical
	}
}
