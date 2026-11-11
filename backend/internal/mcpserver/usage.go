package mcpserver

import (
	"context"
	"fmt"
	"math"
	"slices"
	"sort"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/otlp"
	"github.com/tobilg/ai-observer/internal/pricing"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/tools"
)

// usageMetrics are the cost and token metrics of each tool. GitHub Copilot's
// are derived from the spans of both of its services, Codex's from the
// events of all of its services. Token types in partOf are already counted
// in another type (as the cost calculations treat them: OpenAI, Gemini and
// GenAI input includes cached input, and Codex's also the cache writes;
// OpenAI output includes reasoning), so they are left out of total_tokens.
type usageMetric struct {
	tool         string
	services     []string
	cost, tokens string
	partOf       []string
}

var usageMetrics = []usageMetric{
	{"claude-code", []string{tools.Claude.ServiceName()}, "claude_code.cost.usage", "claude_code.token.usage", nil},
	{"codex", tools.Codex.ServiceNames(), otlp.CodexCostUsageMetric, otlp.CodexTokenUsageMetric,
		[]string{otlp.TokenTypeCacheRead, otlp.TokenTypeCacheWrite, otlp.TokenTypeReasoning}},
	{"gemini", []string{tools.Gemini.ServiceName()}, "gemini_cli.cost.usage", "gemini_cli.token.usage",
		[]string{pricing.GeminiTokenTypeCache}},
	{"copilot", []string{tools.CopilotVSCode.ServiceName(), tools.CopilotCLI.ServiceName()}, "github_copilot.cost.usage", "github_copilot.token.usage",
		[]string{pricing.CopilotTokenTypeCacheRead}},
	{"opencode", []string{tools.OpenCode.ServiceName()}, "opencode.cost.usage", "opencode.token.usage", nil},
}

type usageSummaryIn struct {
	From     string `json:"from,omitempty" jsonschema:"start of the range (default: 7 days before to)"`
	To       string `json:"to,omitempty" jsonschema:"end of the range (default: now)"`
	GroupBy  string `json:"group_by,omitempty" jsonschema:"tool (default), model, day or repository (repositories are known for imported and watched sessions)"`
	Tool     string `json:"tool,omitempty" jsonschema:"only this tool"`
	Timezone string `json:"timezone,omitempty" jsonschema:"IANA time zone whose days group_by day uses, such as Europe/Berlin (default: the server's time zone)"`
}

type usageRow struct {
	Tool        string             `json:"tool"`
	Model       string             `json:"model,omitempty"`
	Repository  string             `json:"repository,omitempty"`
	Day         string             `json:"day,omitempty"`
	CostUSD     float64            `json:"cost_usd"`
	Tokens      map[string]float64 `json:"tokens,omitempty"`
	TotalTokens float64            `json:"total_tokens"`
}

type usageSummaryOut struct {
	From    string `json:"from"`
	To      string `json:"to"`
	GroupBy string `json:"group_by"`
	// TimeZone is the zone of the days (group_by day)
	TimeZone     string     `json:"timezone,omitempty"`
	Rows         []usageRow `json:"rows"`
	TotalCostUSD float64    `json:"total_cost_usd"`
	// UnpricedModels have tokens but no cost in the range ("tool: model")
	UnpricedModels []string `json:"unpriced_models,omitempty"`
	Note           string   `json:"note"`
}

const usageNote = "Costs and tokens as AI Observer recorded them. A Claude Code, Codex or Gemini CLI request recorded both over OTLP and in its session file counts once; Claude Code reports its own cost, the other tools' costs are calculated from built-in prices, and models in unpriced_models have none, so their tokens add no cost (prices can be added, see docs/pricing.md). total_tokens leaves out token types that are part of another type: cached input of Codex (cacheRead, cacheWrite), Gemini (cache) and GitHub Copilot (cache_read), which their input includes, and Codex reasoning, which its output includes."

func (h *handlers) usageSummary(ctx context.Context, _ *mcp.CallToolRequest, in usageSummaryIn) (*mcp.CallToolResult, usageSummaryOut, error) {
	from, to, err := timeRange(in.From, in.To, h.now(), 7*24*time.Hour)
	if err != nil {
		return nil, usageSummaryOut{}, err
	}
	groupBy := in.GroupBy
	if groupBy == "" {
		groupBy = "tool"
	}
	switch groupBy {
	case "tool", "model", "day", "repository":
	default:
		return nil, usageSummaryOut{}, fmt.Errorf("invalid group_by %q: use tool, model, day or repository", groupBy)
	}
	service, err := serviceFor(in.Tool)
	if err != nil {
		return nil, usageSummaryOut{}, err
	}
	var zone string
	loc := time.UTC
	if groupBy == "day" {
		if zone, loc, err = h.timeZone(in.Timezone); err != nil {
			return nil, usageSummaryOut{}, err
		}
	}

	type key struct{ tool, group string }
	rows := make(map[key]*usageRow)
	row := func(tool, group string) *usageRow {
		k := key{tool, group}
		if rows[k] == nil {
			r := &usageRow{Tool: tool}
			switch groupBy {
			case "model":
				r.Model = group
			case "repository":
				r.Repository = group
			case "day":
				r.Day = group
			}
			rows[k] = r
		}
		return rows[k]
	}

	var filtered []usageMetric
	for _, m := range usageMetrics {
		if service == "" || slices.ContainsFunc(tools.FilterServices(service), func(s string) bool { return slices.Contains(m.services, s) }) {
			filtered = append(filtered, m)
		}
	}
	for _, m := range filtered {
		for _, metric := range []string{m.cost, m.tokens} {
			series, err := h.usageSeries(ctx, metric, service, from, to, groupBy, zone)
			if err != nil {
				return nil, usageSummaryOut{}, err
			}
			for _, s := range series.Series {
				group := ""
				switch groupBy {
				case "model":
					group = labelOr(s.Labels, "model", "unknown")
				case "repository":
					group = labelOr(s.Labels, "repository", "unknown")
				}
				for _, dp := range s.DataPoints {
					if dp[1] == 0 {
						continue
					}
					if groupBy == "day" {
						group = time.UnixMilli(int64(dp[0])).In(loc).Format("2006-01-02")
					}
					r := row(m.tool, group)
					if metric == m.cost {
						r.CostUSD += dp[1]
						continue
					}
					if r.Tokens == nil {
						r.Tokens = make(map[string]float64)
					}
					tokenType := labelOr(s.Labels, "type", "total")
					r.Tokens[tokenType] += dp[1]
					if !slices.Contains(m.partOf, tokenType) {
						r.TotalTokens += dp[1]
					}
				}
			}
		}
	}

	unpriced, err := h.unpricedModels(ctx, filtered, service, from, to)
	if err != nil {
		return nil, usageSummaryOut{}, err
	}

	out := usageSummaryOut{From: formatTime(from), To: formatTime(to), GroupBy: groupBy, TimeZone: zone, Rows: []usageRow{}, UnpricedModels: unpriced, Note: usageNote}
	for _, r := range rows {
		r.CostUSD = roundCost(r.CostUSD)
		out.TotalCostUSD += r.CostUSD
		out.Rows = append(out.Rows, *r)
	}
	out.TotalCostUSD = roundCost(out.TotalCostUSD)
	sort.Slice(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		if a.Day != b.Day {
			return a.Day < b.Day
		}
		if a.CostUSD != b.CostUSD {
			return a.CostUSD > b.CostUSD
		}
		return a.Tool+a.Model+a.Repository < b.Tool+b.Model+b.Repository
	})
	return nil, out, nil
}

// unpricedModels lists the models ("tool: model") with tokens but no cost in
// the range: models without a price, such as Codex's codex-auto-review
// (its automatic approval reviewer), whose tokens add nothing to the cost
func (h *handlers) unpricedModels(ctx context.Context, metrics []usageMetric, service string, from, to time.Time) ([]string, error) {
	var unpriced []string
	for _, m := range metrics {
		models := func(metric string) (map[string]float64, error) {
			series, err := h.store.QueryMetricTotals(ctx, metric, service, from, to, "")
			if err != nil {
				return nil, err
			}
			totals := make(map[string]float64)
			for _, s := range series.Series {
				for _, dp := range s.DataPoints {
					totals[labelOr(s.Labels, "model", "unknown")] += dp[1]
				}
			}
			return totals, nil
		}
		tokens, err := models(m.tokens)
		if err != nil {
			return nil, err
		}
		costs, err := models(m.cost)
		if err != nil {
			return nil, err
		}
		for model, n := range tokens {
			if _, priced := costs[model]; n > 0 && !priced {
				unpriced = append(unpriced, m.tool+": "+model)
			}
		}
	}
	slices.Sort(unpriced)
	return unpriced, nil
}

// usageSeries reads one usage metric: totals by model or repository, or
// the days of zone
func (h *handlers) usageSeries(ctx context.Context, metric, service string, from, to time.Time, groupBy, zone string) (*api.TimeSeriesResponse, error) {
	switch groupBy {
	case "day":
		return h.store.QuerySeries(ctx, api.MetricQuery{Name: metric, Service: service},
			storage.SeriesRange{From: from, To: to, Interval: 24 * 60 * 60, TimeZone: zone})
	case "repository":
		return h.store.QueryMetricTotals(ctx, metric, service, from, to, "repository")
	default:
		return h.store.QueryMetricTotals(ctx, metric, service, from, to, "")
	}
}

func labelOr(labels map[string]string, key, fallback string) string {
	if v := labels[key]; v != "" {
		return v
	}
	return fallback
}

// roundCost rounds to a hundredth of a cent
func roundCost(v float64) float64 {
	return math.Round(v*10000) / 10000
}
