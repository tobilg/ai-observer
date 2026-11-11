package pricing

import (
	"encoding/json"
	"sort"
	"sync"
)

// FileInfo describes a pricing file in use (Files)
type FileInfo struct {
	// File is the name of the pricing file, such as claude.json
	File     string
	Provider Provider
	// LastUpdated is the lastUpdated of the built-in file
	LastUpdated string
	// Models counts the models with prices, overrides included
	Models int
	// Override is the path of the override file merged over the built-in
	// one (LoadOverrides), "" without one
	Override string
	// OverrideLastUpdated is the lastUpdated of the override file
	OverrideLastUpdated string
}

// loadedOverrides records the override files LoadOverrides merged, by file
// name
var (
	loadedOverridesMu sync.Mutex
	loadedOverrides   = map[string]FileInfo{}
)

// recordOverride notes that path was merged over the built-in file name
func recordOverride(name, path string, data []byte) {
	var header struct {
		LastUpdated string `json:"lastUpdated"`
	}
	_ = json.Unmarshal(data, &header) // parseProvider has read it already
	loadedOverridesMu.Lock()
	defer loadedOverridesMu.Unlock()
	loadedOverrides[name] = FileInfo{Override: path, OverrideLastUpdated: header.LastUpdated}
}

// Files describes the pricing files in use: the built-in ones, with the
// override files LoadOverrides merged over them, sorted by file name
func Files() []FileInfo {
	names := make([]string, 0, len(overrideFiles))
	for name := range overrideFiles {
		names = append(names, name)
	}
	sort.Strings(names)

	loadedOverridesMu.Lock()
	defer loadedOverridesMu.Unlock()
	files := make([]FileInfo, 0, len(names))
	for _, name := range names {
		info := loadedOverrides[name]
		info.File = name
		if data, err := pricingFS.ReadFile("data/" + name); err == nil {
			var header struct {
				Provider    Provider `json:"provider"`
				LastUpdated string   `json:"lastUpdated"`
			}
			if json.Unmarshal(data, &header) == nil {
				info.Provider, info.LastUpdated = header.Provider, header.LastUpdated
			}
		}
		if provider := *overrideFiles[name](); provider != nil {
			info.Models = len(provider.models)
			if info.Provider == "" {
				info.Provider = provider.provider
			}
		}
		files = append(files, info)
	}
	return files
}
