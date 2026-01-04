package otlp

import (
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// Metric name constants for Claude Code
const (
	ClaudeTokenUsageMetric           = "claude_code.token.usage"
	ClaudeUserFacingTokenUsageMetric = "claude_code.token.usage_user_facing"
	ClaudeCostMetric                 = "claude_code.cost.usage"
	ClaudeUserFacingCostMetric       = "claude_code.cost.usage_user_facing"
)

// tokenGroup holds all token and cost metrics for a single API request (same timestamp+model)
type tokenGroup struct {
	timestamp     time.Time
	model         string
	serviceName   string
	resourceAttrs map[string]string
	scopeName     string
	scopeVersion  string

	// Token values by type
	input         *api.MetricDataPoint
	output        *api.MetricDataPoint
	cacheRead     *api.MetricDataPoint
	cacheCreation *api.MetricDataPoint

	// Cost metric for this API call
	cost *api.MetricDataPoint
}

// groupKey creates a unique key for grouping metrics by timestamp+model
func groupKey(timestamp time.Time, model string) string {
	return timestamp.Format(time.RFC3339Nano) + "|" + model
}

// hasCache returns true if this group has cache activity (user-facing call)
func (g *tokenGroup) hasCache() bool {
	return (g.cacheRead != nil && g.cacheRead.Value != nil && *g.cacheRead.Value > 0) ||
		(g.cacheCreation != nil && g.cacheCreation.Value != nil && *g.cacheCreation.Value > 0)
}

// DeriveClaudeUserFacingMetrics processes Claude Code token and cost metrics and creates
// user-facing variants for API calls that have cache activity.
//
// Logic:
//  1. Group metrics by timestamp + model
//  2. For groups where cacheRead > 0 OR cacheCreation > 0, create user-facing metrics
//  3. Skip groups without cache (tool-routing calls)
//
// This ensures the derived metrics match ccusage output, which only records
// user-facing API calls (assistant messages with cache tokens).
func DeriveClaudeUserFacingMetrics(metrics []api.MetricDataPoint) []api.MetricDataPoint {
	// Group metrics by timestamp+model
	groups := make(map[string]*tokenGroup)

	for i := range metrics {
		m := &metrics[i]

		// Process claude_code.token.usage metrics
		if m.MetricName == ClaudeTokenUsageMetric {
			// Get model and type from attributes
			model := m.Attributes["model"]
			tokenType := m.Attributes["type"]
			if model == "" || tokenType == "" {
				continue
			}

			key := groupKey(m.Timestamp, model)

			if groups[key] == nil {
				groups[key] = &tokenGroup{
					timestamp:     m.Timestamp,
					model:         model,
					serviceName:   m.ServiceName,
					resourceAttrs: m.ResourceAttributes,
					scopeName:     m.ScopeName,
					scopeVersion:  m.ScopeVersion,
				}
			}

			g := groups[key]
			switch tokenType {
			case "input":
				g.input = m
			case "output":
				g.output = m
			case "cacheRead":
				g.cacheRead = m
			case "cacheCreation":
				g.cacheCreation = m
			}
			continue
		}

		// Process claude_code.cost.usage metrics
		if m.MetricName == ClaudeCostMetric {
			model := m.Attributes["model"]
			if model == "" {
				continue
			}

			key := groupKey(m.Timestamp, model)

			if groups[key] == nil {
				groups[key] = &tokenGroup{
					timestamp:     m.Timestamp,
					model:         model,
					serviceName:   m.ServiceName,
					resourceAttrs: m.ResourceAttributes,
					scopeName:     m.ScopeName,
					scopeVersion:  m.ScopeVersion,
				}
			}

			groups[key].cost = m
		}
	}

	// Create user-facing metrics for groups with cache activity
	var derived []api.MetricDataPoint

	for _, g := range groups {
		if !g.hasCache() {
			continue // Skip tool-routing calls (no cache tokens)
		}

		// Include input tokens
		if g.input != nil && g.input.Value != nil && *g.input.Value > 0 {
			derived = append(derived, createUserFacingTokenMetric(*g.input))
		}

		// Include output tokens
		if g.output != nil && g.output.Value != nil && *g.output.Value > 0 {
			derived = append(derived, createUserFacingTokenMetric(*g.output))
		}

		// Include cache tokens (already user-facing by definition)
		if g.cacheRead != nil && g.cacheRead.Value != nil && *g.cacheRead.Value > 0 {
			derived = append(derived, createUserFacingTokenMetric(*g.cacheRead))
		}
		if g.cacheCreation != nil && g.cacheCreation.Value != nil && *g.cacheCreation.Value > 0 {
			derived = append(derived, createUserFacingTokenMetric(*g.cacheCreation))
		}

		// Include cost metric
		if g.cost != nil && g.cost.Value != nil && *g.cost.Value > 0 {
			derived = append(derived, createUserFacingCostMetric(*g.cost))
		}
	}

	return derived
}

// createUserFacingTokenMetric creates a user-facing variant of a token metric
func createUserFacingTokenMetric(original api.MetricDataPoint) api.MetricDataPoint {
	m := original // Copy
	m.MetricName = ClaudeUserFacingTokenUsageMetric
	m.MetricDescription = "Token usage for user-facing API calls (excludes tool-routing)"
	return m
}

// createUserFacingCostMetric creates a user-facing variant of a cost metric
func createUserFacingCostMetric(original api.MetricDataPoint) api.MetricDataPoint {
	m := original // Copy
	m.MetricName = ClaudeUserFacingCostMetric
	m.MetricDescription = "Cost for user-facing API calls (excludes tool-routing)"
	return m
}
