package storage

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/otlp"
)

func fileLogs(ts time.Time, n int) []api.LogRecord {
	logs := make([]api.LogRecord, n)
	for i := range logs {
		logs[i] = api.LogRecord{Timestamp: ts.Add(time.Duration(i) * time.Minute), ServiceName: "claude-code", Body: "line"}
	}
	return logs
}

func countRows(t *testing.T, store *DuckDBStore, query string, args ...any) int {
	t.Helper()
	var n int
	if err := store.db.QueryRowContext(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// Re-importing a file that grew replaces its records instead of adding them again
func TestInsertImportedFileReplacesTheFilesRecords(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ts := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)

	a := &ImportState{Source: "claude-code", FilePath: "/a.jsonl", FileHash: "h1", ImportedAt: ts, SourceTracked: true}
	b := &ImportState{Source: "claude-code", FilePath: "/b.jsonl", FileHash: "h2", ImportedAt: ts, SourceTracked: true}
	if err := store.InsertImportedFile(ctx, fileLogs(ts, 2), nil, nil, a, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertImportedFile(ctx, fileLogs(ts, 1), nil, nil, b, nil); err != nil {
		t.Fatal(err)
	}

	a.FileHash = "h1-grown"
	if err := store.InsertImportedFile(ctx, fileLogs(ts, 3), nil, nil, a, &FileRecords{}); err != nil {
		t.Fatal(err)
	}

	if got := countRows(t, store, "SELECT COUNT(*) FROM otel_logs WHERE SourceFile = '/a.jsonl'"); got != 3 {
		t.Errorf("file a has %d logs, want 3", got)
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM otel_logs WHERE SourceFile = '/b.jsonl'"); got != 1 {
		t.Errorf("file b has %d logs, want 1 (other files are kept)", got)
	}
}

// A ranged re-import replaces only the file's records in the range
func TestInsertImportedFileReplacesOnlyTheRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ts := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)

	state := &ImportState{Source: "claude-code", FilePath: "/a.jsonl", FileHash: "h1", ImportedAt: ts, SourceTracked: true}
	if err := store.InsertImportedFile(ctx, fileLogs(ts, 4), nil, nil, state, nil); err != nil {
		t.Fatal(err)
	}

	// Minutes 2 and 3 are re-imported
	from := ts.Add(2 * time.Minute)
	if err := store.InsertImportedFile(ctx, fileLogs(from, 2), nil, nil, state, &FileRecords{From: &from}); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM otel_logs"); got != 4 {
		t.Errorf("got %d logs, want 4", got)
	}
}

func TestInsertWatcherBatchReplacesARewrittenFile(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ts := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)

	state := &ImportState{Source: "claude-code", FilePath: "/a.jsonl", ByteOffset: 100}
	if err := store.InsertWatcherBatch(ctx, fileLogs(ts, 3), nil, nil, state, false); err != nil {
		t.Fatal(err)
	}
	state.ByteOffset = 20
	if err := store.InsertWatcherBatch(ctx, fileLogs(ts, 1), nil, nil, state, true); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM otel_logs WHERE SourceFile = '/a.jsonl'"); got != 1 {
		t.Errorf("got %d logs, want 1", got)
	}

	got, err := store.GetImportState(ctx, "claude-code", "/a.jsonl")
	if err != nil || got == nil {
		t.Fatalf("GetImportState: %v, %v", got, err)
	}
	if !got.SourceTracked {
		t.Error("a file first stored by watch should be source-tracked")
	}
}

func TestImportStateKeepsFileStats(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()

	state := &ImportState{Source: "codex", FilePath: "/c.jsonl", FileHash: "h", ImportedAt: time.Now(),
		FileSize: 1234, FileModTimeNs: 1735812000123456789, SourceTracked: true}
	if err := store.SetImportState(ctx, state); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetImportState(ctx, "codex", "/c.jsonl")
	if err != nil || got == nil {
		t.Fatalf("GetImportState: %v, %v", got, err)
	}
	if got.FileSize != 1234 || got.FileModTimeNs != state.FileModTimeNs || !got.SourceTracked {
		t.Errorf("got %+v", got)
	}

	// Updating watch fields keeps the import's file stats
	if err := store.SetWatchFields(ctx, "codex", "/c.jsonl", 2000, 0, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = store.GetImportState(ctx, "codex", "/c.jsonl")
	if got.FileSize != 1234 || got.ByteOffset != 2000 {
		t.Errorf("after watch update got %+v", got)
	}
}

func TestDeleteAllInRangeDeletesNothingOnFailure(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ts := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)

	if err := store.InsertLogs(ctx, fileLogs(ts, 2)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, "DROP TABLE otel_traces"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteAllInRange(ctx, ts.Add(-time.Hour), ts.Add(time.Hour), ""); err == nil {
		t.Fatal("expected the delete to fail")
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM otel_logs"); got != 2 {
		t.Errorf("got %d logs after a failed delete, want 2", got)
	}
}

func geminiTokens(ts time.Time, value float64, session string) api.MetricDataPoint {
	cumulative := int32(2)
	return api.MetricDataPoint{
		Timestamp: ts, ServiceName: "gemini_cli", MetricName: otlp.GeminiTokenUsageMetric, MetricType: "sum",
		Attributes:             map[string]string{"model": "gemini-2.5-flash", "type": "input", "session.id": session},
		ResourceAttributes:     map[string]string{"service.name": "gemini_cli"},
		Value:                  &value,
		AggregationTemporality: &cumulative,
	}
}

func aggregateValue(t *testing.T, store *DuckDBStore, name string, from, to time.Time) float64 {
	t.Helper()
	resp, err := store.QueryMetricSeries(context.Background(), name, "", from, to, 60, true)
	if err != nil {
		t.Fatalf("QueryMetricSeries(%s): %v", name, err)
	}
	total := 0.0
	for _, series := range resp.Series {
		for _, p := range series.DataPoints {
			total += p[1]
		}
	}
	return total
}

const geminiFlashInputPrice = 0.3e-6

// Gemini sends running token totals every few seconds. The cost of a session
// is the cost of its final total, however often the total was exported.
func TestGeminiCostFromCumulativeTokensCountsEachTokenOnce(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ts := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)

	var points []api.MetricDataPoint
	for i, total := range []float64{1000, 1000, 1000, 1500, 1500} {
		token := geminiTokens(ts.Add(time.Duration(i)*10*time.Second), total, "s1")
		points = append(points, token)
		points = append(points, otlp.DeriveGeminiCostMetrics([]api.MetricDataPoint{token})...)
	}
	// A second process starts its own counter
	second := geminiTokens(ts.Add(time.Minute), 200, "s2")
	points = append(points, second)
	points = append(points, otlp.DeriveGeminiCostMetrics([]api.MetricDataPoint{second})...)
	if err := store.InsertMetrics(ctx, points); err != nil {
		t.Fatal(err)
	}

	from, to := ts.Add(-time.Minute), ts.Add(time.Hour)
	if got, want := aggregateValue(t, store, otlp.GeminiCostUsageMetric, from, to), 1700*geminiFlashInputPrice; math.Abs(got-want) > 1e-12 {
		t.Errorf("cost = %g, want %g", got, want)
	}
	// The ".delta" name of earlier versions reads the counter
	if got := aggregateValue(t, store, "gemini_cli.token.usage.delta", from, to); got != 1700 {
		t.Errorf("token.usage.delta = %g, want 1700", got)
	}
	names, err := store.GetMetricNames(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	// Names list the counters, not their aliases, which return the same series
	if strings.Join(names, ",") != "gemini_cli.cost.usage,gemini_cli.token.usage" {
		t.Errorf("metric names = %v", names)
	}
}

// Migrations 4 and 5 replace the overcounted cost points of earlier versions
func TestGeminiCostMigrationRebuildsCostFromTokens(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "legacy.duckdb")
	legacy, err := sql.Open("duckdb", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		schemaMetrics,
		// Running totals of one session
		`INSERT INTO otel_metrics (Timestamp, ServiceName, MetricName, Attributes, MetricType, Value, AggregationTemporality) VALUES
			('2025-01-02 10:00:00', 'gemini_cli', 'gemini_cli.token.usage', '{"model": "gemini-2.5-flash", "type": "input", "session.id": "s1"}', 'sum', 1000, 2),
			('2025-01-02 10:00:10', 'gemini_cli', 'gemini_cli.token.usage', '{"model": "gemini-2.5-flash", "type": "input", "session.id": "s1"}', 'sum', 1000, 2),
			('2025-01-02 10:00:20', 'gemini_cli', 'gemini_cli.token.usage', '{"model": "gemini-2.5-flash", "type": "input", "session.id": "s1"}', 'sum', 1500, 2)`,
		// The per-export costs and deltas earlier versions derived from them
		`INSERT INTO otel_metrics (Timestamp, ServiceName, MetricName, Attributes, MetricType, Value, AggregationTemporality) VALUES
			('2025-01-02 10:00:00', 'gemini_cli', 'gemini_cli.cost.usage', '{"model": "gemini-2.5-flash"}', 'sum', 0.0003, 1),
			('2025-01-02 10:00:10', 'gemini_cli', 'gemini_cli.cost.usage', '{"model": "gemini-2.5-flash"}', 'sum', 0.0003, 1),
			('2025-01-02 10:00:20', 'gemini_cli', 'gemini_cli.cost.usage', '{"model": "gemini-2.5-flash"}', 'sum', 0.00045, 1),
			('2025-01-02 10:00:20', 'gemini_cli', 'gemini_cli.token.usage.delta', '{"model": "gemini-2.5-flash", "type": "input", "session.id": "s1"}', 'sum', 500, 1)`,
		// An imported per-event cost without token points, which is kept
		`INSERT INTO otel_metrics (Timestamp, ServiceName, MetricName, Attributes, MetricType, Value) VALUES
			('2025-01-03 10:00:00', 'gemini_cli', 'gemini_cli.cost.usage', '{"model": "gemini-2.5-flash", "import_source": "local_jsonl"}', 'sum', 0.01)`,
		// An imported response whose cost charged its cached tokens twice:
		// 1000 input tokens, of which 800 cached, and 100 thought tokens
		`INSERT INTO otel_metrics (Timestamp, ServiceName, MetricName, Attributes, MetricType, Value) VALUES
			('2025-01-04 10:00:00', 'gemini_cli', 'gemini_cli.token.usage', '{"type": "input", "model": "gemini-2.5-flash", "import_source": "local_jsonl"}', 'sum', 1000),
			('2025-01-04 10:00:00', 'gemini_cli', 'gemini_cli.token.usage', '{"type": "cached", "model": "gemini-2.5-flash", "import_source": "local_jsonl"}', 'sum', 800),
			('2025-01-04 10:00:00', 'gemini_cli', 'gemini_cli.token.usage', '{"type": "thoughts", "model": "gemini-2.5-flash", "import_source": "local_jsonl"}', 'sum', 100),
			('2025-01-04 10:00:00', 'gemini_cli', 'gemini_cli.cost.usage', '{"model": "gemini-2.5-flash", "import_source": "local_jsonl"}', 'sum', 0.000574)`,
	} {
		if _, err := legacy.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	legacy.Close()

	store, err := NewDuckDBStore(dbPath)
	if err != nil {
		t.Fatalf("opening store: %v", err)
	}
	defer store.Close()

	if got := countRows(t, store, "SELECT COUNT(*) FROM otel_metrics WHERE MetricName LIKE '%.delta'"); got != 0 {
		t.Errorf("%d .delta points remain", got)
	}
	// Metrics stored before metric_names existed are listed
	if names, err := store.GetMetricNames(ctx, "gemini_cli"); err != nil || strings.Join(names, ",") != "gemini_cli.cost.usage,gemini_cli.token.usage" {
		t.Errorf("metric names after the migrations = %v (%v)", names, err)
	}
	ts := time.Date(2025, 1, 2, 9, 0, 0, 0, time.UTC)
	if got, want := aggregateValue(t, store, otlp.GeminiCostUsageMetric, ts, ts.Add(12*time.Hour)), 1500*geminiFlashInputPrice; math.Abs(got-want) > 1e-12 {
		t.Errorf("OTLP cost = %g, want %g", got, want)
	}
	if got := aggregateValue(t, store, otlp.GeminiCostUsageMetric, ts.Add(24*time.Hour), ts.Add(36*time.Hour)); math.Abs(got-0.01) > 1e-12 {
		t.Errorf("imported cost = %g, want 0.01", got)
	}
	// 200 uncached input, 800 cached and 100 thought tokens of gemini-2.5-flash
	want := 200*0.3e-6 + 800*0.03e-6 + 100*2.5e-6
	if got := aggregateValue(t, store, otlp.GeminiCostUsageMetric, ts.Add(48*time.Hour), ts.Add(60*time.Hour)); math.Abs(got-want) > 1e-12 {
		t.Errorf("rebuilt imported cost = %g, want %g", got, want)
	}
	if got := countRows(t, store, `SELECT COUNT(*) FROM otel_metrics
		WHERE json_extract_string(Attributes, '$.type') IN ('cached', 'thoughts')`); got != 0 {
		t.Errorf("%d token points keep the old type names", got)
	}
	if got := countRows(t, store, `SELECT COUNT(*) FROM otel_metrics
		WHERE json_extract_string(Attributes, '$.type') IN ('cache', 'thought')
			AND json_extract_string(Attributes, '$.import_source') = 'local_jsonl'`); got != 2 {
		t.Errorf("%d renamed token points, want 2", got)
	}
}

// Telemetry and the metrics derived from it are stored together or not at all
func TestInsertTelemetryIsAtomic(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.db.ExecContext(ctx, "DROP TABLE otel_metrics"); err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)
	value := 1.0
	err := store.InsertTelemetry(ctx, fileLogs(ts, 2), []api.MetricDataPoint{{
		Timestamp: ts, ServiceName: "codex_cli_rs", MetricName: "codex_cli_rs.token.usage", MetricType: "sum", Value: &value,
	}}, nil)
	if err == nil {
		t.Fatal("expected the insert to fail")
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM otel_logs"); got != 0 {
		t.Errorf("%d logs stored although their metrics failed", got)
	}
}

// A batch an exporter sends again stores neither its spans nor the metrics
// derived from them a second time
func TestInsertTracesDerivesMetricsOnlyForNewSpans(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ts := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)
	span := func(id string) api.Span {
		return api.Span{Timestamp: ts, TraceID: "t1", SpanID: id, ServiceName: "copilot-chat", SpanName: "chat"}
	}
	derive := func(spans []api.Span) []api.MetricDataPoint {
		var metrics []api.MetricDataPoint
		for _, s := range spans {
			value := 1.0
			metrics = append(metrics, api.MetricDataPoint{Timestamp: s.Timestamp, ServiceName: s.ServiceName,
				MetricName: "github_copilot.token.usage", MetricType: "sum", Value: &value})
		}
		return metrics
	}

	if _, _, err := store.InsertTraces(ctx, []api.Span{span("a")}, derive); err != nil {
		t.Fatal(err)
	}
	stored, derived, err := store.InsertTraces(ctx, []api.Span{span("a"), span("b")}, derive)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 1 || stored[0].SpanID != "b" || len(derived) != 1 {
		t.Errorf("second batch stored %d spans and %d metrics, want only span b and its metric", len(stored), len(derived))
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM otel_metrics"); got != 2 {
		t.Errorf("%d derived metrics stored, want 2", got)
	}
}

// Deleting the start of a cumulative series (retention, delete) does not make
// its first remaining point count as an increase of its whole running total
func TestDeletedCumulativePointsLeaveABaseline(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ts := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)

	var points []api.MetricDataPoint
	for i, total := range []float64{100, 150, 180} {
		points = append(points, geminiTokens(ts.Add(time.Duration(i)*time.Hour), total, "s1"))
	}
	if err := store.InsertMetrics(ctx, points); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteAllInRange(ctx, time.Time{}, ts.Add(30*time.Minute), ""); err != nil {
		t.Fatal(err)
	}

	got := aggregateValue(t, store, otlp.GeminiTokenUsageMetric, ts.Add(40*time.Minute), ts.Add(3*time.Hour))
	if got != 80 {
		t.Errorf("increase after the deletion = %v, want 80", got)
	}
	if got := countRows(t, store, "SELECT COUNT(*) FROM metric_baselines"); got != 1 {
		t.Errorf("%d baselines, want 1", got)
	}
}

// The ".delta" names of earlier versions also list points
func TestQueryMetricsResolvesDeltaAliases(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ts := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)
	if err := store.InsertMetrics(ctx, []api.MetricDataPoint{geminiTokens(ts, 100, "s1")}); err != nil {
		t.Fatal(err)
	}
	resp, err := store.QueryMetrics(ctx, "", "gemini_cli.token.usage.delta", "", ts.Add(-time.Hour), ts.Add(time.Hour), Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Total != 1 {
		t.Errorf("listed %d points for the alias, want 1", resp.Total)
	}
}

// Deleting all points of a metric removes it from the listed names
func TestDeletedMetricsAreNotListed(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ts := time.Date(2025, 1, 2, 10, 0, 0, 0, time.UTC)
	if err := store.InsertMetrics(ctx, []api.MetricDataPoint{geminiTokens(ts, 100, "s1")}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DeleteAllInRange(ctx, ts.Add(-time.Hour), ts.Add(time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	if names, err := store.GetMetricNames(ctx, ""); err != nil || len(names) != 0 {
		t.Errorf("metric names after deleting all points = %v (%v)", names, err)
	}
}
