package otlp

import (
	"math"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/pricing"
)

func TestDeriveCopilotMetricsFromSpansChatSpan(t *testing.T) {
	timestamp := time.Now()
	spans := []api.Span{
		{
			Timestamp:   timestamp,
			ServiceName: CopilotVSCodeServiceName,
			ResourceAttributes: map[string]string{
				"service.version": "1.2.3",
			},
			SpanAttributes: map[string]string{
				"gen_ai.operation.name":                    "chat",
				"gen_ai.response.model":                    "openai/gpt-5-mini",
				"gen_ai.usage.input_tokens":                "1000",
				"gen_ai.usage.output_tokens":               "500",
				"gen_ai.usage.cache_read.input_tokens":     "100",
				"gen_ai.usage.reasoning.output_tokens":     "50",
				"gen_ai.usage.cache_creation.input_tokens": "10",
			},
		},
	}

	metrics := DeriveCopilotMetricsFromSpans(spans)

	if len(metrics) != 6 {
		t.Fatalf("expected 6 metrics, got %d", len(metrics))
	}

	tokenMetrics := make(map[string]float64)
	var costMetric *api.MetricDataPoint
	for i := range metrics {
		metric := metrics[i]
		if metric.Timestamp != timestamp {
			t.Errorf("expected timestamp %v, got %v", timestamp, metric.Timestamp)
		}
		if metric.ServiceName != CopilotVSCodeServiceName {
			t.Errorf("expected service %q, got %q", CopilotVSCodeServiceName, metric.ServiceName)
		}

		switch metric.MetricName {
		case CopilotTokenUsageMetric:
			if metric.MetricType != "sum" {
				t.Errorf("expected token metric type sum, got %q", metric.MetricType)
			}
			if metric.MetricUnit != "tokens" {
				t.Errorf("expected token metric unit tokens, got %q", metric.MetricUnit)
			}
			if metric.Attributes["model"] != "openai/gpt-5-mini" {
				t.Errorf("expected model attribute to be preserved, got %q", metric.Attributes["model"])
			}
			if metric.Value == nil {
				t.Fatalf("token metric %q has nil value", metric.Attributes["type"])
			}
			tokenMetrics[metric.Attributes["type"]] = *metric.Value
		case CopilotCostUsageMetric:
			costMetric = &metric
		default:
			t.Errorf("unexpected metric name %q", metric.MetricName)
		}
	}

	expectedTokens := map[string]float64{
		pricing.CopilotTokenTypeInput:         1000,
		pricing.CopilotTokenTypeOutput:        500,
		pricing.CopilotTokenTypeCacheRead:     100,
		pricing.CopilotTokenTypeCacheCreation: 10,
		pricing.CopilotTokenTypeReasoning:     50,
	}
	for tokenType, expected := range expectedTokens {
		if actual, ok := tokenMetrics[tokenType]; !ok {
			t.Errorf("missing token metric for type %q", tokenType)
		} else if actual != expected {
			t.Errorf("token metric %q = %v, expected %v", tokenType, actual, expected)
		}
	}

	if costMetric == nil {
		t.Fatal("expected cost metric")
	}
	if costMetric.MetricType != "sum" {
		t.Errorf("expected cost metric type sum, got %q", costMetric.MetricType)
	}
	if costMetric.MetricUnit != "USD" {
		t.Errorf("expected cost metric unit USD, got %q", costMetric.MetricUnit)
	}
	if costMetric.Value == nil {
		t.Fatal("cost metric has nil value")
	}
	// Expected: (1000-100) * 0.25e-6 + 100 * 0.025e-6 + 10 * 0.25e-6 + (500+50) * 2e-6
	expectedCost := 0.00133
	if math.Abs(*costMetric.Value-expectedCost) > 0.0000001 {
		t.Errorf("cost metric = %v, expected %v", *costMetric.Value, expectedCost)
	}
}

func TestDeriveCopilotMetricsFromSpansNonChatSpan(t *testing.T) {
	spans := []api.Span{
		{
			Timestamp:   time.Now(),
			ServiceName: CopilotVSCodeServiceName,
			SpanAttributes: map[string]string{
				"gen_ai.operation.name":     "embeddings",
				"gen_ai.response.model":     "gpt-5-mini",
				"gen_ai.usage.input_tokens": "1000",
			},
		},
	}

	metrics := DeriveCopilotMetricsFromSpans(spans)
	if len(metrics) != 0 {
		t.Errorf("expected no metrics for non-chat span, got %d", len(metrics))
	}
}

func TestDeriveCopilotMetricsFromSpansUnknownModelSkipsCost(t *testing.T) {
	spans := []api.Span{
		{
			Timestamp:   time.Now(),
			ServiceName: CopilotCLIServiceName,
			SpanAttributes: map[string]string{
				"gen_ai.operation.name":      "chat",
				"gen_ai.response.model":      "unknown-model",
				"gen_ai.usage.output_tokens": "42",
			},
		},
	}

	metrics := DeriveCopilotMetricsFromSpans(spans)
	if len(metrics) != 1 {
		t.Fatalf("expected one token metric, got %d", len(metrics))
	}
	if metrics[0].MetricName != CopilotTokenUsageMetric {
		t.Fatalf("expected token metric, got %q", metrics[0].MetricName)
	}
	if metrics[0].Attributes["type"] != pricing.CopilotTokenTypeOutput {
		t.Errorf("expected output token metric, got %q", metrics[0].Attributes["type"])
	}
}
