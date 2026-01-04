package otlp

import (
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func floatPtr(v float64) *float64 {
	return &v
}

func TestDeriveClaudeUserFacingMetrics_WithCache(t *testing.T) {
	ts := time.Now()

	metrics := []api.MetricDataPoint{
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "input", "model": "claude-sonnet-4-5"},
			Value:       floatPtr(100),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "output", "model": "claude-sonnet-4-5"},
			Value:       floatPtr(50),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "cacheRead", "model": "claude-sonnet-4-5"},
			Value:       floatPtr(50000),
		},
	}

	derived := DeriveClaudeUserFacingMetrics(metrics)

	if len(derived) != 3 {
		t.Fatalf("Expected 3 user-facing metrics, got %d", len(derived))
	}

	for _, m := range derived {
		if m.MetricName != ClaudeUserFacingTokenUsageMetric {
			t.Errorf("Expected metric name %s, got %s", ClaudeUserFacingTokenUsageMetric, m.MetricName)
		}
	}
}

func TestDeriveClaudeUserFacingMetrics_WithoutCache(t *testing.T) {
	ts := time.Now()

	// No cacheRead or cacheCreation - this is a tool-routing call
	metrics := []api.MetricDataPoint{
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "input", "model": "claude-haiku-4-5"},
			Value:       floatPtr(100),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "output", "model": "claude-haiku-4-5"},
			Value:       floatPtr(50),
		},
	}

	derived := DeriveClaudeUserFacingMetrics(metrics)

	if len(derived) != 0 {
		t.Fatalf("Expected 0 user-facing metrics for tool-routing call, got %d", len(derived))
	}
}

func TestDeriveClaudeUserFacingMetrics_WithZeroCache(t *testing.T) {
	ts := time.Now()

	// Cache tokens present but with 0 values - should not be considered user-facing
	metrics := []api.MetricDataPoint{
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "input", "model": "claude-haiku-4-5"},
			Value:       floatPtr(100),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "cacheRead", "model": "claude-haiku-4-5"},
			Value:       floatPtr(0),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "cacheCreation", "model": "claude-haiku-4-5"},
			Value:       floatPtr(0),
		},
	}

	derived := DeriveClaudeUserFacingMetrics(metrics)

	if len(derived) != 0 {
		t.Fatalf("Expected 0 user-facing metrics when cache tokens are 0, got %d", len(derived))
	}
}

func TestDeriveClaudeUserFacingMetrics_MixedCalls(t *testing.T) {
	ts1 := time.Now()
	ts2 := ts1.Add(time.Second)

	// First call: user-facing (has cache)
	// Second call: tool-routing (no cache)
	metrics := []api.MetricDataPoint{
		// User-facing call
		{
			Timestamp:   ts1,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "input", "model": "claude-sonnet-4-5"},
			Value:       floatPtr(100),
		},
		{
			Timestamp:   ts1,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "cacheRead", "model": "claude-sonnet-4-5"},
			Value:       floatPtr(50000),
		},
		// Tool-routing call
		{
			Timestamp:   ts2,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "input", "model": "claude-haiku-4-5"},
			Value:       floatPtr(100),
		},
	}

	derived := DeriveClaudeUserFacingMetrics(metrics)

	// Only the user-facing call should produce metrics (2: input + cacheRead)
	if len(derived) != 2 {
		t.Fatalf("Expected 2 user-facing metrics, got %d", len(derived))
	}

	// Verify all derived metrics are from the user-facing call
	for _, m := range derived {
		if !m.Timestamp.Equal(ts1) {
			t.Errorf("Expected timestamp %v, got %v", ts1, m.Timestamp)
		}
		if m.Attributes["model"] != "claude-sonnet-4-5" {
			t.Errorf("Expected model claude-sonnet-4-5, got %s", m.Attributes["model"])
		}
	}
}

func TestDeriveClaudeUserFacingMetrics_NonClaudeMetrics(t *testing.T) {
	ts := time.Now()

	// Gemini metrics should be ignored
	metrics := []api.MetricDataPoint{
		{
			Timestamp:   ts,
			ServiceName: "gemini_cli",
			MetricName:  "gemini_cli.token.usage",
			Attributes:  map[string]string{"type": "input", "model": "gemini-2.5-flash"},
			Value:       floatPtr(100),
		},
	}

	derived := DeriveClaudeUserFacingMetrics(metrics)

	if len(derived) != 0 {
		t.Errorf("Expected 0 derived metrics for non-Claude metrics, got %d", len(derived))
	}
}

func TestDeriveClaudeUserFacingMetrics_CacheCreationOnly(t *testing.T) {
	ts := time.Now()

	// Only cacheCreation token, no cacheRead - should still be user-facing
	metrics := []api.MetricDataPoint{
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "input", "model": "claude-opus-4-5"},
			Value:       floatPtr(100),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "output", "model": "claude-opus-4-5"},
			Value:       floatPtr(500),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "cacheCreation", "model": "claude-opus-4-5"},
			Value:       floatPtr(10000),
		},
	}

	derived := DeriveClaudeUserFacingMetrics(metrics)

	if len(derived) != 3 {
		t.Fatalf("Expected 3 user-facing metrics (cacheCreation alone makes it user-facing), got %d", len(derived))
	}
}

func TestDeriveClaudeUserFacingMetrics_PreservesAttributes(t *testing.T) {
	ts := time.Now()

	metrics := []api.MetricDataPoint{
		{
			Timestamp:          ts,
			ServiceName:        "claude_code",
			MetricName:         ClaudeTokenUsageMetric,
			MetricType:         "sum",
			Attributes:         map[string]string{"type": "input", "model": "claude-opus-4-5"},
			ResourceAttributes: map[string]string{"host.name": "test-host"},
			ScopeName:          "test-scope",
			ScopeVersion:       "1.0.0",
			Value:              floatPtr(100),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "cacheRead", "model": "claude-opus-4-5"},
			Value:       floatPtr(50000),
		},
	}

	derived := DeriveClaudeUserFacingMetrics(metrics)

	if len(derived) != 2 {
		t.Fatalf("Expected 2 user-facing metrics, got %d", len(derived))
	}

	// Find the input metric
	var inputMetric *api.MetricDataPoint
	for i := range derived {
		if derived[i].Attributes["type"] == "input" {
			inputMetric = &derived[i]
			break
		}
	}

	if inputMetric == nil {
		t.Fatal("Expected to find input metric in derived metrics")
	}

	// Verify attributes are preserved
	if inputMetric.ServiceName != "claude_code" {
		t.Errorf("Expected ServiceName claude_code, got %s", inputMetric.ServiceName)
	}
	if inputMetric.MetricType != "sum" {
		t.Errorf("Expected MetricType sum, got %s", inputMetric.MetricType)
	}
	if inputMetric.ResourceAttributes["host.name"] != "test-host" {
		t.Errorf("Expected ResourceAttributes host.name=test-host, got %v", inputMetric.ResourceAttributes)
	}
	if inputMetric.ScopeName != "test-scope" {
		t.Errorf("Expected ScopeName test-scope, got %s", inputMetric.ScopeName)
	}
	if inputMetric.MetricName != ClaudeUserFacingTokenUsageMetric {
		t.Errorf("Expected MetricName %s, got %s", ClaudeUserFacingTokenUsageMetric, inputMetric.MetricName)
	}
}

func TestDeriveClaudeUserFacingMetrics_DifferentModels(t *testing.T) {
	ts := time.Now()

	// Same timestamp but different models - should be grouped separately
	metrics := []api.MetricDataPoint{
		// Opus with cache
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "input", "model": "claude-opus-4-5"},
			Value:       floatPtr(100),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "cacheRead", "model": "claude-opus-4-5"},
			Value:       floatPtr(50000),
		},
		// Haiku without cache (same timestamp)
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "input", "model": "claude-haiku-4-5"},
			Value:       floatPtr(200),
		},
	}

	derived := DeriveClaudeUserFacingMetrics(metrics)

	// Only Opus call should produce metrics (2: input + cacheRead)
	if len(derived) != 2 {
		t.Fatalf("Expected 2 user-facing metrics (only Opus), got %d", len(derived))
	}

	for _, m := range derived {
		if m.Attributes["model"] != "claude-opus-4-5" {
			t.Errorf("Expected model claude-opus-4-5, got %s", m.Attributes["model"])
		}
	}
}

func TestDeriveClaudeUserFacingMetrics_CostWithCache(t *testing.T) {
	ts := time.Now()

	// Cost metric with cache tokens present - should be included
	metrics := []api.MetricDataPoint{
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "input", "model": "claude-sonnet-4-5"},
			Value:       floatPtr(100),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "cacheRead", "model": "claude-sonnet-4-5"},
			Value:       floatPtr(50000),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeCostMetric,
			Attributes:  map[string]string{"model": "claude-sonnet-4-5"},
			Value:       floatPtr(0.05),
		},
	}

	derived := DeriveClaudeUserFacingMetrics(metrics)

	// Should produce 3 metrics: input token, cacheRead token, and cost
	if len(derived) != 3 {
		t.Fatalf("Expected 3 user-facing metrics (including cost), got %d", len(derived))
	}

	// Find the cost metric
	var costMetric *api.MetricDataPoint
	for i := range derived {
		if derived[i].MetricName == ClaudeUserFacingCostMetric {
			costMetric = &derived[i]
			break
		}
	}

	if costMetric == nil {
		t.Fatal("Expected to find user-facing cost metric in derived metrics")
	}

	if *costMetric.Value != 0.05 {
		t.Errorf("Expected cost value 0.05, got %f", *costMetric.Value)
	}
}

func TestDeriveClaudeUserFacingMetrics_CostWithoutCache(t *testing.T) {
	ts := time.Now()

	// Cost metric without cache tokens - should be filtered out (tool-routing)
	metrics := []api.MetricDataPoint{
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "input", "model": "claude-haiku-4-5"},
			Value:       floatPtr(100),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "output", "model": "claude-haiku-4-5"},
			Value:       floatPtr(50),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeCostMetric,
			Attributes:  map[string]string{"model": "claude-haiku-4-5"},
			Value:       floatPtr(0.001),
		},
	}

	derived := DeriveClaudeUserFacingMetrics(metrics)

	if len(derived) != 0 {
		t.Fatalf("Expected 0 user-facing metrics for tool-routing call with cost, got %d", len(derived))
	}
}

func TestDeriveClaudeUserFacingMetrics_CostPreservesAttributes(t *testing.T) {
	ts := time.Now()

	metrics := []api.MetricDataPoint{
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "cacheRead", "model": "claude-opus-4-5"},
			Value:       floatPtr(50000),
		},
		{
			Timestamp:          ts,
			ServiceName:        "claude_code",
			MetricName:         ClaudeCostMetric,
			MetricType:         "sum",
			Attributes:         map[string]string{"model": "claude-opus-4-5"},
			ResourceAttributes: map[string]string{"host.name": "test-host"},
			ScopeName:          "test-scope",
			ScopeVersion:       "1.0.0",
			Value:              floatPtr(0.10),
		},
	}

	derived := DeriveClaudeUserFacingMetrics(metrics)

	// Find the cost metric
	var costMetric *api.MetricDataPoint
	for i := range derived {
		if derived[i].MetricName == ClaudeUserFacingCostMetric {
			costMetric = &derived[i]
			break
		}
	}

	if costMetric == nil {
		t.Fatal("Expected to find user-facing cost metric in derived metrics")
	}

	// Verify attributes are preserved
	if costMetric.ServiceName != "claude_code" {
		t.Errorf("Expected ServiceName claude_code, got %s", costMetric.ServiceName)
	}
	if costMetric.MetricType != "sum" {
		t.Errorf("Expected MetricType sum, got %s", costMetric.MetricType)
	}
	if costMetric.Attributes["model"] != "claude-opus-4-5" {
		t.Errorf("Expected model attribute claude-opus-4-5, got %s", costMetric.Attributes["model"])
	}
	if costMetric.ResourceAttributes["host.name"] != "test-host" {
		t.Errorf("Expected ResourceAttributes host.name=test-host, got %v", costMetric.ResourceAttributes)
	}
	if costMetric.ScopeName != "test-scope" {
		t.Errorf("Expected ScopeName test-scope, got %s", costMetric.ScopeName)
	}
	if *costMetric.Value != 0.10 {
		t.Errorf("Expected cost value 0.10, got %f", *costMetric.Value)
	}
}

func TestDeriveClaudeUserFacingMetrics_CostAndTokensTogether(t *testing.T) {
	ts := time.Now()

	// Full set of metrics: input, output, cacheRead, cacheCreation, and cost
	metrics := []api.MetricDataPoint{
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "input", "model": "claude-opus-4-5"},
			Value:       floatPtr(100),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "output", "model": "claude-opus-4-5"},
			Value:       floatPtr(500),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "cacheRead", "model": "claude-opus-4-5"},
			Value:       floatPtr(50000),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeTokenUsageMetric,
			Attributes:  map[string]string{"type": "cacheCreation", "model": "claude-opus-4-5"},
			Value:       floatPtr(10000),
		},
		{
			Timestamp:   ts,
			ServiceName: "claude_code",
			MetricName:  ClaudeCostMetric,
			Attributes:  map[string]string{"model": "claude-opus-4-5"},
			Value:       floatPtr(0.25),
		},
	}

	derived := DeriveClaudeUserFacingMetrics(metrics)

	// Should produce 5 metrics: 4 token types + 1 cost
	if len(derived) != 5 {
		t.Fatalf("Expected 5 user-facing metrics (4 tokens + 1 cost), got %d", len(derived))
	}

	// Count by metric type
	tokenCount := 0
	costCount := 0
	for _, m := range derived {
		if m.MetricName == ClaudeUserFacingTokenUsageMetric {
			tokenCount++
		} else if m.MetricName == ClaudeUserFacingCostMetric {
			costCount++
		}
	}

	if tokenCount != 4 {
		t.Errorf("Expected 4 token metrics, got %d", tokenCount)
	}
	if costCount != 1 {
		t.Errorf("Expected 1 cost metric, got %d", costCount)
	}
}
