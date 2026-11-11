package storage

import (
	"context"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func TestTelemetryBoundsAndImportSummaries(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	if oldest, newest, err := store.TelemetryBounds(ctx); err != nil || !oldest.IsZero() || !newest.IsZero() {
		t.Errorf("empty database: %v %v %v", oldest, newest, err)
	}
	if summaries, err := store.ImportSummaries(ctx); err != nil || summaries == nil || len(summaries) != 0 {
		t.Errorf("empty import state: %v %v", summaries, err)
	}

	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	value := 1.0
	if err := store.InsertTelemetry(ctx,
		[]api.LogRecord{{Timestamp: base, ServiceName: "claude-code"}},
		[]api.MetricDataPoint{{Timestamp: base.Add(2 * time.Hour), ServiceName: "claude-code", MetricName: "m", MetricType: "gauge", Value: &value}},
		[]api.Span{{Timestamp: base.Add(-time.Hour), TraceID: "t", SpanID: "s", SpanName: "op", ServiceName: "claude-code"}},
	); err != nil {
		t.Fatal(err)
	}
	oldest, newest, err := store.TelemetryBounds(ctx)
	if err != nil || !oldest.Equal(base.Add(-time.Hour)) || !newest.Equal(base.Add(2*time.Hour)) {
		t.Errorf("bounds = %v %v %v, want the span and the metric point", oldest, newest, err)
	}

	for _, state := range []*ImportState{
		{Source: "claude-code", FilePath: "/a.jsonl", FileHash: "h", ImportedAt: base, RecordCount: 10},
		{Source: "claude-code", FilePath: "/b.jsonl", FileHash: "h", ImportedAt: base.Add(time.Hour), RecordCount: 5, Partial: true},
		{Source: "gemini", FilePath: "/c.json", FileHash: "h", ImportedAt: base, RecordCount: 3},
	} {
		if err := store.SetImportState(ctx, state); err != nil {
			t.Fatal(err)
		}
	}
	summaries, err := store.ImportSummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []ImportSummary{
		{Source: "claude-code", Files: 2, Records: 15, LastImport: base.Add(time.Hour), PartialFiles: 1},
		{Source: "gemini", Files: 1, Records: 3, LastImport: base},
	}
	if len(summaries) != len(want) {
		t.Fatalf("summaries = %+v", summaries)
	}
	for i := range want {
		got := summaries[i]
		if got.Source != want[i].Source || got.Files != want[i].Files || got.Records != want[i].Records ||
			!got.LastImport.Equal(want[i].LastImport) || got.PartialFiles != want[i].PartialFiles {
			t.Errorf("summary %d = %+v, want %+v", i, got, want[i])
		}
	}
}
