package mcpserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func TestQueryMetrics_GroupFilterStatZone(t *testing.T) {
	cs, store, now := testSession(t)

	var out queryMetricsOut
	if errText := call(t, cs, "query_metrics", map[string]any{"name": "claude_code.cost.usage", "aggregate": true, "group_by": []string{"repository"}}, &out); errText != "" {
		t.Fatal(errText)
	}
	byRepo := make(map[string]float64)
	for _, s := range out.Series {
		byRepo[s.Labels["repository"]] += s.Points[0].Value
	}
	if len(out.Series) != 2 || byRepo["acme/api"] != 1.25 || byRepo["acme/web"] != 0.75 {
		t.Errorf("cost by repository = %+v", out.Series)
	}
	call(t, cs, "query_metrics", map[string]any{"name": "claude_code.cost.usage", "aggregate": true, "filters": map[string]string{"model": "claude-opus-5-5"}}, &out)
	if len(out.Series) != 1 || out.Series[0].Points[0].Value != 0.75 {
		t.Errorf("opus cost = %+v", out.Series)
	}

	// Daily buckets start at the zone's midnight, and times carry its offset
	call(t, cs, "query_metrics", map[string]any{"name": "claude_code.cost.usage", "interval": "1d", "from": "3d", "timezone": "Asia/Kolkata"}, &out)
	if out.TimeZone != "Asia/Kolkata" || out.Interval != "24h0m0s" || len(out.Series) == 0 {
		t.Fatalf("daily = %+v", out)
	}
	for _, p := range out.Series[0].Points {
		if !strings.HasSuffix(p.Time, "T00:00:00+05:30") {
			t.Errorf("bucket at %s, want midnight in Kolkata", p.Time)
		}
	}
	// Without a zone: the server's
	call(t, cs, "query_metrics", map[string]any{"name": "claude_code.cost.usage", "interval": "1h", "from": "6h"}, &out)
	if out.TimeZone != store.LocalTimeZone() {
		t.Errorf("default zone = %q, want %q", out.TimeZone, store.LocalTimeZone())
	}

	// Histograms: avg by default; empty buckets have no point
	delta := int32(1)
	count, sum, lo, hi := uint64(2), 300.0, 100.0, 200.0
	sample := api.MetricDataPoint{
		Timestamp: now.Add(-90 * time.Minute), ServiceName: "gemini_cli", MetricName: "gemini_cli.api.request.latency", MetricType: "histogram",
		AggregationTemporality: &delta, Count: &count, Sum: &sum, Min: &lo, Max: &hi, ExplicitBounds: []float64{0, 1000}, BucketCounts: []uint64{0, 2, 0},
	}
	if err := store.InsertMetrics(context.Background(), []api.MetricDataPoint{sample}); err != nil {
		t.Fatal(err)
	}
	call(t, cs, "query_metrics", map[string]any{"name": "gemini_cli.api.request.latency", "from": "3h", "interval": "1h"}, &out)
	if out.Stat != "avg" || out.MetricType != "histogram" || len(out.Series) != 1 || len(out.Series[0].Points) != 1 ||
		out.Series[0].Points[0].Value != 150 || out.Note == "" {
		t.Errorf("latency = %+v", out)
	}
	call(t, cs, "query_metrics", map[string]any{"name": "gemini_cli.api.request.latency", "from": "3h", "aggregate": true, "stat": "max"}, &out)
	if out.Stat != "max" || out.Series[0].Points[0].Value != 200 {
		t.Errorf("latency max = %+v", out)
	}
	// A single sparse series still needs a line break when no other series
	// supplies the absent bucket's timestamp.
	sample.Timestamp = sample.Timestamp.Add(-2 * time.Hour)
	if err := store.InsertMetrics(t.Context(), []api.MetricDataPoint{sample}); err != nil {
		t.Fatal(err)
	}
	call(t, cs, "query_metrics", map[string]any{"name": sample.MetricName, "from": "5h", "interval": "1h", "timezone": "UTC", "stat": "p95"}, &out)
	wantGap := sample.Timestamp.UTC().Truncate(time.Hour).Add(time.Hour).Format(time.RFC3339)
	if len(out.Series) != 1 || len(out.Series[0].Points) != 2 || out.Series[0].Points[0].GapStart != "" || out.Series[0].Points[1].GapStart != wantGap {
		t.Fatalf("sparse percentile = %+v, want a gap at %s", out.Series, wantGap)
	}

	for args, want := range map[string]struct {
		args map[string]any
		text string
	}{
		"group_by": {map[string]any{"name": "claude_code.cost.usage", "group_by": []string{"bad key"}}, `invalid group_by "bad key"`},
		"stat":     {map[string]any{"name": "claude_code.cost.usage", "stat": "p99"}, "stat p99 applies to histogram metrics"},
		"timezone": {map[string]any{"name": "claude_code.cost.usage", "timezone": "Moon/Base"}, "unknown time zone"},
	} {
		if errText := call(t, cs, "query_metrics", want.args, &out); !strings.Contains(errText, want.text) {
			t.Errorf("%s error = %q, want %q", args, errText, want.text)
		}
	}
}

func TestUsageSummary_DaysInTimeZone(t *testing.T) {
	cs, _, now := testSession(t)
	var out usageSummaryOut
	if errText := call(t, cs, "usage_summary", map[string]any{"group_by": "day", "timezone": "Pacific/Kiritimati"}, &out); errText != "" {
		t.Fatal(errText)
	}
	// The costs were recorded 2 hours ago: their day in UTC+14
	want := now.Add(-2 * time.Hour).In(time.FixedZone("", 14*3600)).Format("2006-01-02")
	if out.TimeZone != "Pacific/Kiritimati" || len(out.Rows) != 1 || out.Rows[0].Day != want || out.TotalCostUSD != 2 {
		t.Errorf("by day = %+v, want one row of %s", out, want)
	}
	if call(t, cs, "usage_summary", map[string]any{}, &out); out.TimeZone != "" {
		t.Errorf("by tool has a time zone: %+v", out)
	}
	if errText := call(t, cs, "usage_summary", map[string]any{"group_by": "day", "timezone": "Nowhere"}, &out); !strings.Contains(errText, "unknown time zone") {
		t.Errorf("zone error = %q", errText)
	}
}
