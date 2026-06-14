package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const (
	// GitHubModelsCatalogURL is the REST endpoint for the GitHub Models catalog.
	GitHubModelsCatalogURL = "https://models.github.ai/catalog/models"
	// GitHubModelsCatalogAPIVersion is the API version used for the embedded catalog snapshot.
	GitHubModelsCatalogAPIVersion = "2026-03-10"
)

var (
	githubModelsParentheticalPattern = regexp.MustCompile(`\([^)]*\)`)
	githubModelsNonAliasPattern      = regexp.MustCompile(`[^a-z0-9.\-]+`)
	githubModelsRepeatedDashPattern  = regexp.MustCompile(`-+`)
	githubModelsProviderPrefixes     = []string{
		"openai/",
		"anthropic/",
		"google/",
		"microsoft/",
		"github/",
		"cohere/",
		"deepseek/",
		"meta/",
		"mistral-ai/",
	}
)

// GitHubModelsCatalogData represents the embedded GitHub Models catalog snapshot.
type GitHubModelsCatalogData struct {
	Source      string                     `json:"source"`
	APIVersion  string                     `json:"apiVersion"`
	LastUpdated string                     `json:"lastUpdated"`
	Models      []GitHubModelsCatalogEntry `json:"models"`
}

// GitHubModelsCatalogEntry contains the catalog fields needed for alias derivation.
type GitHubModelsCatalogEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Publisher string `json:"publisher"`
	Version   string `json:"version"`
}

// GitHubModelsCatalogFetcher fetches the current catalog from GitHub's REST API.
type GitHubModelsCatalogFetcher struct {
	httpClient *http.Client
}

// NewGitHubModelsCatalogFetcher creates a catalog fetcher with a bounded timeout.
func NewGitHubModelsCatalogFetcher() *GitHubModelsCatalogFetcher {
	return &GitHubModelsCatalogFetcher{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Fetch returns the current GitHub Models catalog. The token is optional for callers
// that run in an environment where the endpoint is accessible without auth.
func (f *GitHubModelsCatalogFetcher) Fetch(ctx context.Context, token string) ([]GitHubModelsCatalogEntry, error) {
	if f == nil || f.httpClient == nil {
		f = NewGitHubModelsCatalogFetcher()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, GitHubModelsCatalogURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating GitHub Models catalog request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", GitHubModelsCatalogAPIVersion)
	if token = strings.TrimSpace(token); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := f.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching GitHub Models catalog: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("GitHub Models catalog returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var entries []GitHubModelsCatalogEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decoding GitHub Models catalog: %w", err)
	}
	return entries, nil
}

func loadGitHubModelsCatalog(filename string) (*GitHubModelsCatalogData, error) {
	data, err := pricingFS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", filename, err)
	}

	var catalog GitHubModelsCatalogData
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", filename, err)
	}
	return &catalog, nil
}

// GenerateGitHubModelAliases derives all known normalized aliases for a catalog entry.
func GenerateGitHubModelAliases(entry GitHubModelsCatalogEntry) []string {
	var aliases []string
	seen := make(map[string]struct{})

	add := func(value string) {
		for _, stripParenthetical := range []bool{false, true} {
			alias := normalizeGitHubModelAlias(value, stripParenthetical)
			if alias == "" {
				continue
			}
			if _, ok := seen[alias]; ok {
				continue
			}
			seen[alias] = struct{}{}
			aliases = append(aliases, alias)
		}
	}

	baseValues := githubCatalogBaseValues(entry)
	for _, value := range baseValues {
		add(value)
	}

	version := normalizeGitHubModelAlias(entry.Version, false)
	if version != "" && version != "1" {
		compactVersion := strings.ReplaceAll(version, "-", "")
		for _, value := range baseValues {
			add(value + "-" + version)
			if compactVersion != version {
				add(value + "-" + compactVersion)
			}
			if isNumericGitHubCatalogVersion(version) {
				add(value + "-v" + version)
			}
		}
	}

	return aliases
}

func githubCatalogBaseValues(entry GitHubModelsCatalogEntry) []string {
	values := []string{entry.ID}

	if baseID := githubCatalogBaseID(entry.ID); baseID != "" && baseID != entry.ID {
		values = append(values, baseID)
	}

	if name := strings.TrimSpace(entry.Name); name != "" {
		values = append(values, name)
		if withoutPublisher := trimGitHubCatalogPublisherName(name, entry.Publisher); withoutPublisher != "" && withoutPublisher != name {
			values = append(values, withoutPublisher)
		}
	}

	return values
}

func githubCatalogBaseID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if slash := strings.LastIndex(id, "/"); slash >= 0 && slash+1 < len(id) {
		return id[slash+1:]
	}
	return id
}

func trimGitHubCatalogPublisherName(name, publisher string) string {
	name = strings.TrimSpace(name)
	publisher = strings.TrimSpace(publisher)
	if name == "" || publisher == "" || len(name) <= len(publisher) {
		return name
	}

	if !strings.EqualFold(name[:len(publisher)], publisher) {
		return name
	}

	separator := name[len(publisher)]
	if separator != ' ' && separator != '-' && separator != '_' {
		return name
	}
	return strings.TrimLeft(strings.TrimSpace(name[len(publisher)+1:]), "-_ ")
}

func normalizeGitHubModelAlias(value string, stripParenthetical bool) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}

	for _, prefix := range githubModelsProviderPrefixes {
		normalized = strings.TrimPrefix(normalized, prefix)
	}

	if stripParenthetical {
		normalized = githubModelsParentheticalPattern.ReplaceAllString(normalized, " ")
	} else {
		normalized = strings.NewReplacer("(", " ", ")", " ").Replace(normalized)
	}

	normalized = strings.ReplaceAll(normalized, "_", "-")
	normalized = strings.ReplaceAll(normalized, "/", "-")
	normalized = githubModelsNonAliasPattern.ReplaceAllString(normalized, "-")
	normalized = githubModelsRepeatedDashPattern.ReplaceAllString(normalized, "-")
	return strings.Trim(normalized, "-.")
}

func isNumericGitHubCatalogVersion(version string) bool {
	if version == "" {
		return false
	}
	for _, r := range version {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func applyGitHubModelsCatalogAliases(copilot *providerData, entries []GitHubModelsCatalogEntry) {
	if copilot == nil {
		return
	}

	for _, entry := range entries {
		aliases := GenerateGitHubModelAliases(entry)
		if len(aliases) == 0 {
			continue
		}

		canonical, pricing := resolveGitHubCatalogPricing(copilot, entry, aliases)
		if canonical == "" || pricing == nil {
			continue
		}
		if _, ok := copilot.models[canonical]; !ok {
			copilot.models[canonical] = pricing
		}

		for _, alias := range aliases {
			copilot.registerAlias(alias, canonical)
		}
	}
}

func resolveGitHubCatalogPricing(copilot *providerData, entry GitHubModelsCatalogEntry, aliases []string) (string, *ModelPricing) {
	for _, alias := range aliases {
		if canonical, ok := copilot.resolveCanonical(alias); ok {
			return canonical, copilot.models[canonical]
		}
	}

	sourceProvider := sourceProviderForGitHubCatalogPublisher(entry.Publisher)
	if sourceProvider == nil {
		return "", nil
	}

	for _, alias := range aliases {
		if canonical, ok := sourceProvider.resolveCanonical(alias); ok {
			return canonical, sourceProvider.models[canonical]
		}
	}

	return "", nil
}

func sourceProviderForGitHubCatalogPublisher(publisher string) *providerData {
	switch strings.ToLower(strings.TrimSpace(publisher)) {
	case "anthropic":
		return registry.claude
	case "google":
		return registry.gemini
	case "openai":
		return registry.codex
	default:
		return nil
	}
}
