package restore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/exporter"
	"github.com/tobilg/ai-observer/internal/storage"
)

var base = time.Date(2025, 6, 1, 10, 0, 0, 0, time.UTC)

func openStore(t *testing.T, name string) *storage.DuckDBStore {
	t.Helper()
	store, err := storage.NewDuckDBStore(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

// fillSource stores sessions of two tools with logs, spans, cumulative and
// delta metrics, a deleted day (leaving metric baselines) and a dashboard
func fillSource(t *testing.T, store *storage.DuckDBStore) {
	t.Helper()
	ctx := context.Background()
	var logs []api.LogRecord
	var spans []api.Span
	var metrics []api.MetricDataPoint
	cumulative, delta := int32(2), int32(1)
	for day := 0; day < 3; day++ {
		for i := 0; i < 5; i++ {
			at := base.Add(time.Duration(day)*24*time.Hour + time.Duration(i)*time.Minute)
			session := "claude-" + string(rune('a'+day))
			logs = append(logs,
				api.LogRecord{Timestamp: at, ServiceName: "claude-code", SeverityText: "INFO", Body: "claude_code.user_prompt",
					LogAttributes: map[string]string{"session.id": session, "event.name": "user_prompt", "prompt": "hello"}},
				api.LogRecord{Timestamp: at.Add(time.Second), ServiceName: "codex-app-server", SeverityText: "INFO", Body: "codex.user_prompt",
					LogAttributes: map[string]string{"conversation.id": "codex-" + session, "event.name": "codex.user_prompt"}},
			)
			spans = append(spans, api.Span{
				Timestamp: at, TraceID: "trace-" + session, SpanID: session + "-" + string(rune('0'+i)), SpanName: "chat",
				ServiceName: "copilot-chat", Duration: 1000,
				SpanAttributes: map[string]string{"gen_ai.conversation.id": session, "gen_ai.operation.name": "chat"},
				Events:         []api.SpanEvent{{Timestamp: at, Name: "e", Attributes: map[string]string{"k": "v"}}},
			})
			total := float64(day*100 + i*10)
			cost := 0.5
			count, sum := uint64(day*5+i+1), float64(day*500+i*100)
			metrics = append(metrics,
				// A cumulative histogram: its baseline keeps count and buckets
				api.MetricDataPoint{Timestamp: at, ServiceName: "copilot-chat", MetricName: "gen_ai.client.operation.duration", MetricType: "histogram",
					Count: &count, Sum: &sum, BucketCounts: []uint64{count, 0}, ExplicitBounds: []float64{10},
					AggregationTemporality: &cumulative, Attributes: map[string]string{"gen_ai.request.model": "gpt-4o"}},
				api.MetricDataPoint{Timestamp: at, ServiceName: "gemini_cli", MetricName: "gemini_cli.token.usage", MetricType: "sum",
					Value: &total, AggregationTemporality: &cumulative, Attributes: map[string]string{"type": "input", "model": "gemini-2.5-pro"}},
				api.MetricDataPoint{Timestamp: at, ServiceName: "claude-code", MetricName: "claude_code.cost.usage", MetricType: "sum",
					Value: &cost, AggregationTemporality: &delta, Attributes: map[string]string{"model": "claude-sonnet-4"}},
			)
		}
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertMetrics(ctx, metrics); err != nil {
		t.Fatal(err)
	}
	// Deleting the first day leaves the baselines of the cumulative series
	if _, err := store.DeleteAllInRange(ctx, time.Time{}, base.Add(12*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	d, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Costs", Description: "mine", IsDefault: true})
	if err != nil {
		t.Fatal(err)
	}
	stacked := true
	if _, err := store.CreateWidget(ctx, d.ID, &api.CreateWidgetRequest{WidgetType: "metric_chart", Title: "Tokens",
		GridColumn: 1, GridRow: 1, ColSpan: 2, RowSpan: 1,
		Config: api.WidgetConfig{MetricName: "gemini_cli.token.usage", ChartStacked: &stacked}}); err != nil {
		t.Fatal(err)
	}
}

func export(t *testing.T, store *storage.DuckDBStore, opts exporter.Options) string {
	t.Helper()
	if opts.OutputDir == "" {
		opts.OutputDir = t.TempDir()
	}
	if opts.Source == "" {
		opts.Source = exporter.SourceAll
	}
	if _, err := exporter.NewExporter(store, false).Export(context.Background(), opts); err != nil {
		t.Fatalf("Export: %v", err)
	}
	return opts.OutputDir
}

func restore(t *testing.T, store *storage.DuckDBStore, path string, replace bool) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(context.Background(), store, Options{Path: path, Replace: replace, SkipConfirm: true, Out: &out})
	return out.String(), err
}

// tableDigest identifies the rows of a table, all columns
func tableDigest(t *testing.T, store *storage.DuckDBStore, table string) string {
	t.Helper()
	var count int64
	var digest *string
	err := store.DB().QueryRow(`SELECT COUNT(*), md5(string_agg(CAST(r AS VARCHAR), '|' ORDER BY CAST(r AS VARCHAR)))
		FROM (SELECT * FROM `+table+`) r`).Scan(&count, &digest)
	if err != nil {
		t.Fatalf("digest of %s: %v", table, err)
	}
	if digest == nil {
		return "0 rows"
	}
	return fmt.Sprintf("%d:%s", count, *digest)
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// queryResults are the answers of the dashboard's queries over the data
func queryResults(t *testing.T, store *storage.DuckDBStore) map[string]string {
	t.Helper()
	ctx := context.Background()
	from, to := base.Add(-24*time.Hour), base.Add(5*24*time.Hour)
	results := map[string]string{}

	sessions, err := store.QuerySessions(ctx, "", from, to, storage.Page{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	results["sessions"] = jsonOf(t, sessions.Sessions)
	traces, err := store.QueryTraces(ctx, storage.TraceFilter{}, from, to, storage.Page{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	results["traces"] = jsonOf(t, traces.Traces)
	logs, err := store.QueryLogs(ctx, "", "", "", "", from, to, storage.Page{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	// Logs are ordered by time, then by row: compare them as a set
	results["logs"] = jsonOf(t, len(logs.Logs))
	for _, name := range []string{"gemini_cli.token.usage", "claude_code.cost.usage"} {
		series, err := store.QueryMetricSeries(ctx, name, "", from, to, 3600, false)
		if err != nil {
			t.Fatal(err)
		}
		results["series "+name] = jsonOf(t, series)
		total, err := store.QueryMetricSeries(ctx, name, "", base.Add(36*time.Hour), to, 0, true)
		if err != nil {
			t.Fatal(err)
		}
		results["total "+name] = jsonOf(t, total)
	}
	names, err := store.GetMetricNames(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	results["metric names"] = jsonOf(t, names)
	dashboards, err := store.GetDashboards(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dashboards {
		full, err := store.GetDashboardWithWidgets(ctx, d.ID)
		if err != nil {
			t.Fatal(err)
		}
		results["dashboard "+d.ID] = jsonOf(t, full)
	}
	for _, table := range []string{"otel_traces", "otel_logs", "otel_metrics", "metric_baselines", "metric_names", "dashboards", "dashboard_widgets"} {
		results["table "+table] = tableDigest(t, store, table)
	}
	return results
}

func compareResults(t *testing.T, want, got map[string]string) {
	t.Helper()
	for key, w := range want {
		if g := got[key]; g != w {
			t.Errorf("%s differs after restore:\n got  %.300s\n want %.300s", key, g, w)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("%s only after restore", key)
		}
	}
}

// export, then restore into a new database: every query answers the same
func TestRoundTripIntoNewDatabase(t *testing.T) {
	source := openStore(t, "source.duckdb")
	fillSource(t, source)
	want := queryResults(t, source)
	if want["table metric_baselines"] == "0 rows" {
		t.Fatal("the source has no metric baselines to restore")
	}
	var histograms int
	if err := source.DB().QueryRow(`SELECT COUNT(*) FROM metric_baselines WHERE Count IS NOT NULL AND BucketCounts IS NOT NULL`).Scan(&histograms); err != nil || histograms == 0 {
		t.Fatalf("the source has no histogram baseline to restore (%v)", err)
	}

	dir := export(t, source, exporter.Options{})
	target := openStore(t, "target.duckdb")
	out, err := restore(t, target, dir, false)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	compareResults(t, want, queryResults(t, target))
}

// An export written before metric baselines kept histograms' counts and
// buckets restores its baselines without them
func TestRestoreBaselinesWithoutHistogramColumns(t *testing.T) {
	source := openStore(t, "source.duckdb")
	fillSource(t, source)
	dir := export(t, source, exporter.Options{})
	file := filepath.Join(dir, exporter.BaselinesFile)
	old := filepath.Join(t.TempDir(), "old.parquet")
	if _, err := source.DB().Exec(fmt.Sprintf(`COPY (SELECT ServiceName, MetricName, Attributes, ResourceAttributes, Timestamp, Value
		FROM read_parquet('%s')) TO '%s' (FORMAT PARQUET)`, file, old)); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(old, file); err != nil {
		t.Fatal(err)
	}
	target := openStore(t, "target.duckdb")
	if out, err := restore(t, target, dir, false); err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	var baselines, counted int
	if err := target.DB().QueryRow(`SELECT COUNT(*), COUNT(Count) FROM metric_baselines`).Scan(&baselines, &counted); err != nil {
		t.Fatal(err)
	}
	if baselines == 0 || counted != 0 {
		t.Fatalf("%d baselines, %d with a count: want the export's, without counts", baselines, counted)
	}
}

func TestRoundTripFromZip(t *testing.T) {
	source := openStore(t, "source.duckdb")
	fillSource(t, source)
	dir := t.TempDir()
	export(t, source, exporter.Options{OutputDir: dir, CreateZip: true})
	zips, _ := filepath.Glob(filepath.Join(dir, "*.zip"))
	if len(zips) != 1 {
		t.Fatalf("zip archives: %v", zips)
	}
	target := openStore(t, "target.duckdb")
	if out, err := restore(t, target, zips[0], false); err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	compareResults(t, queryResults(t, source), queryResults(t, target))
}

// Exports of earlier versions hold only the Parquet files, some without the
// session ID columns: restoring them computes the session IDs
func TestRestoreOlderExport(t *testing.T) {
	source := openStore(t, "source.duckdb")
	fillSource(t, source)
	dir := export(t, source, exporter.Options{})
	manifest, _, err := exporter.ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{exporter.ManifestFile, exporter.DashboardsFile, exporter.BaselinesFile, exporter.ImportStateFile} {
		path := filepath.Join(dir, name)
		if err := os.Rename(path, path+".saved"); err != nil {
			t.Fatal(err)
		}
	}
	for name, exclude := range map[string]string{
		exporter.LogsFile: "SessionId, SessionIds, SourceFile", exporter.TracesFile: "SessionId, SessionIds, SourceFile", exporter.MetricsFile: "SourceFile, CodexMatch",
	} {
		file := filepath.Join(dir, name)
		old := file + ".old"
		if _, err := source.DB().Exec(`COPY (SELECT * EXCLUDE (` + exclude + `) FROM read_parquet('` + file + `')) TO '` + old + `' (FORMAT PARQUET)`); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(old, file); err != nil {
			t.Fatal(err)
		}
	}

	target := openStore(t, "target.duckdb")
	out, err := restore(t, target, dir, false)
	if err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Computed for otel_logs (missing in the export): SessionId, SessionIds") {
		t.Errorf("preview does not name the computed columns:\n%s", out)
	}
	if !strings.Contains(out, "carry no session file") || !strings.Contains(out, "--purge") || strings.Contains(out, "Session files:") {
		t.Errorf("restore of an export without session file paths does not warn about re-imports:\n%s", out)
	}
	want, got := queryResults(t, source), queryResults(t, target)
	for _, key := range []string{"sessions", "traces", "logs", "table otel_logs", "table otel_traces", "table otel_metrics", "metric names"} {
		if got[key] != want[key] {
			t.Errorf("%s differs after restoring an older export:\n got  %.300s\n want %.300s", key, got[key], want[key])
		}
	}
	if got["table dashboards"] != "0 rows" {
		t.Error("dashboards restored from an export without them")
	}
	// Format 1 added the manifest, dashboards and metric baselines.
	for _, name := range []string{exporter.DashboardsFile, exporter.BaselinesFile} {
		path := filepath.Join(dir, name)
		if err := os.Rename(path+".saved", path); err != nil {
			t.Fatal(err)
		}
	}
	manifest.Format = 1
	delete(manifest.Rows, exporter.ImportStateFile)
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, exporter.ManifestFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	formatOne := openStore(t, "format1.duckdb")
	if _, err := restore(t, formatOne, dir, false); err != nil {
		t.Fatal(err)
	}
	if got := tableDigest(t, formatOne, "dashboards"); got != tableDigest(t, source, "dashboards") {
		t.Fatal("format 1 dashboards differ after restore")
	}

}

// Restoring into a database that holds records of the export's services in
// its range would store logs and metric points twice: it is refused, or
// with Replace the records are replaced
func TestRestoreRefusesDuplicatesAndReplaces(t *testing.T) {
	source := openStore(t, "source.duckdb")
	fillSource(t, source)
	want := queryResults(t, source)
	dir := export(t, source, exporter.Options{})

	target := openStore(t, "target.duckdb")
	if out, err := restore(t, target, dir, false); err != nil {
		t.Fatalf("first restore: %v\n%s", err, out)
	}
	out, err := restore(t, target, dir, false)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("second restore without --replace: %v\n%s", err, out)
	}
	if !strings.Contains(out, "--replace") {
		t.Errorf("refusal does not mention --replace:\n%s", out)
	}
	compareResults(t, want, queryResults(t, target))

	out, err = restore(t, target, dir, true)
	if err != nil {
		t.Fatalf("restore --replace: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Replaced:") || !strings.Contains(out, "exist already, kept") {
		t.Errorf("result does not report the replacement:\n%s", out)
	}
	compareResults(t, want, queryResults(t, target))
}

// Restoring a range into a database whose records outside it stay: the
// series keep their per-interval increases at the range's start
func TestRestoreRangeKeepsCumulativeIncreases(t *testing.T) {
	source := openStore(t, "source.duckdb")
	fillSource(t, source)
	from, to := base.Add(48*time.Hour), base.Add(72*time.Hour)
	dir := export(t, source, exporter.Options{FromDate: &from, ToDate: &to})

	target := openStore(t, "target.duckdb")
	if out, err := restore(t, target, dir, false); err != nil {
		t.Fatalf("restore: %v\n%s", err, out)
	}
	ctx := context.Background()
	want, err := source.QueryMetricSeries(ctx, "gemini_cli.token.usage", "", from, to, 3600, false)
	if err != nil {
		t.Fatal(err)
	}
	got, err := target.QueryMetricSeries(ctx, "gemini_cli.token.usage", "", from, to, 3600, false)
	if err != nil {
		t.Fatal(err)
	}
	if jsonOf(t, got) != jsonOf(t, want) {
		t.Errorf("series of the restored range differ:\n got  %.400s\n want %.400s", jsonOf(t, got), jsonOf(t, want))
	}
}

func TestRestoreDryRunChangesNothing(t *testing.T) {
	source := openStore(t, "source.duckdb")
	fillSource(t, source)
	dir := export(t, source, exporter.Options{})
	target := openStore(t, "target.duckdb")
	var out bytes.Buffer
	if err := Run(context.Background(), target, Options{Path: dir, DryRun: true, Out: &out}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Dry run") || !strings.Contains(out.String(), "Records to restore") {
		t.Errorf("dry run output:\n%s", out.String())
	}
	if d := tableDigest(t, target, "otel_logs"); d != "0 rows" {
		t.Errorf("dry run stored logs: %s", d)
	}
}

func TestRestoreAsksForConfirmation(t *testing.T) {
	source := openStore(t, "source.duckdb")
	fillSource(t, source)
	dir := export(t, source, exporter.Options{})
	target := openStore(t, "target.duckdb")
	var out bytes.Buffer
	if err := Run(context.Background(), target, Options{Path: dir, Out: &out, In: strings.NewReader("n\n")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Aborted.") || tableDigest(t, target, "otel_logs") != "0 rows" {
		t.Errorf("declined restore stored records:\n%s", out.String())
	}
}

func TestRestoreRejectsOtherInput(t *testing.T) {
	target := openStore(t, "target.duckdb")
	ctx := context.Background()
	empty := t.TempDir()
	if err := Run(ctx, target, Options{Path: empty, SkipConfirm: true, Out: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "incomplete export") {
		t.Errorf("restore of an empty directory: %v", err)
	}
	db := filepath.Join(empty, "backup.duckdb")
	if err := os.WriteFile(db, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Run(ctx, target, Options{Path: db, Out: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "is a database, not an export") {
		t.Errorf("restore of a database file: %v", err)
	}
	if err := Run(ctx, target, Options{Path: filepath.Join(empty, "missing"), Out: &bytes.Buffer{}}); err == nil {
		t.Error("restore of a missing path succeeded")
	}
}

// Entries of an archive outside the export's file names are not extracted
func TestExtractExportIgnoresOtherEntries(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "export")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	evil := filepath.Join(source, "evil.txt")
	logs := filepath.Join(source, exporter.LogsFile)
	for _, f := range []string{evil, logs} {
		if err := os.WriteFile(f, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archive := filepath.Join(dir, "export.zip")
	if err := exporter.CreateZipArchive(source, []string{evil, logs}, archive); err != nil {
		t.Fatal(err)
	}
	target := t.TempDir()
	if err := extractExport(archive, target); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(target)
	if len(entries) != 1 || entries[0].Name() != exporter.LogsFile {
		t.Errorf("extracted %v", entries)
	}
}

func TestReplaceValidatesExportBeforeDeleting(t *testing.T) {
	src := openStore(t, "source.duckdb")
	fillSource(t, src)
	dir := export(t, src, exporter.Options{})
	dst := openStore(t, "target.duckdb")
	if _, err := restore(t, dst, dir, false); err != nil {
		t.Fatal(err)
	}
	before := tableDigest(t, dst, "otel_metrics")
	checkRejected := func(t *testing.T, name string) {
		t.Helper()
		if _, err := restore(t, dst, dir, true); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("incomplete export: got %v, want an error naming %s", err, name)
		}
		if got := tableDigest(t, dst, "otel_metrics"); got != before {
			t.Fatal("replace changed the existing metrics")
		}
	}
	for _, name := range exportFiles {
		path := filepath.Join(dir, name)
		t.Run("missing "+name, func(t *testing.T) {
			if err := os.Rename(path, path+".saved"); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := os.Rename(path+".saved", path); err != nil {
					t.Fatal(err)
				}
			}()
			checkRejected(t, name)
		})
	}
	m, _, err := exporter.ReadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.Rows[exporter.MetricsFile]++
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, exporter.ManifestFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	checkRejected(t, exporter.MetricsFile)
	// An interrupted format 2 export may look like a legacy directory
	// if only the three signal files have been written so far.
	for _, name := range []string{exporter.ManifestFile, exporter.BaselinesFile, exporter.DashboardsFile, exporter.ImportStateFile} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	checkRejected(t, exporter.ManifestFile)

}

func TestRestoreUsesLiteralServiceNames(t *testing.T) {
	ctx := context.Background()
	src := openStore(t, "source.duckdb")
	if err := src.InsertLogs(ctx, []api.LogRecord{
		{ServiceName: "codex", Timestamp: base},
		{ServiceName: "codex", Timestamp: base.Add(time.Minute)},
	}); err != nil {
		t.Fatal(err)
	}
	dir := export(t, src, exporter.Options{})
	dst := openStore(t, "target.duckdb")
	if err := dst.InsertLogs(ctx, []api.LogRecord{{ServiceName: "codex-app-server", Timestamp: base}}); err != nil {
		t.Fatal(err)
	}
	if _, err := restore(t, dst, dir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := restore(t, dst, dir, false); !errors.Is(err, ErrConflict) {
		t.Fatalf("repeat restore: got %v, want ErrConflict", err)
	}
	if _, err := restore(t, dst, dir, true); err != nil {
		t.Fatal(err)
	}
	if got := countRows(t, dst, "SELECT COUNT(*) FROM otel_logs WHERE ServiceName='codex'"); got != 2 {
		t.Errorf("codex logs = %d, want 2", got)
	}
	if got := countRows(t, dst, "SELECT COUNT(*) FROM otel_logs WHERE ServiceName='codex-app-server'"); got != 1 {
		t.Errorf("unrelated service logs = %d, want 1", got)
	}
}
