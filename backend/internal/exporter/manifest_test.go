package exporter

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
)

func TestExportWritesManifestDashboardsAndBaselines(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)
	ctx := context.Background()
	if _, err := store.CreateDashboard(ctx, &api.CreateDashboardRequest{Name: "Costs", IsDefault: true}); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	summary, err := NewExporter(store, false).Export(ctx, Options{Source: SourceAll, OutputDir: dir})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if summary.DashboardsCount != 1 {
		t.Errorf("exported %d dashboards, want 1", summary.DashboardsCount)
	}

	m, ok, err := ReadManifest(dir)
	if err != nil || !ok {
		t.Fatalf("ReadManifest: %v, %v", ok, err)
	}
	if m.Format != ManifestFormat || m.SchemaVersion != storage.LatestSchemaVersion() || m.Source != "all" || len(m.Services) != 0 {
		t.Errorf("manifest = %+v", m)
	}
	if m.Rows[LogsFile] != 3 || m.Rows[MetricsFile] != 3 || m.Rows[TracesFile] != 2 || m.Rows[DashboardsFile] != 1 {
		t.Errorf("manifest rows = %v", m.Rows)
	}

	dashboards, ok, err := ReadDashboards(dir)
	if err != nil || !ok || len(dashboards) != 1 || dashboards[0].Name != "Costs" || !dashboards[0].IsDefault {
		t.Errorf("ReadDashboards = %+v, %v, %v", dashboards, ok, err)
	}
	if dashboards[0].Widgets == nil {
		t.Error("dashboard without widgets list")
	}
}

// An export of a tool holds the records of all of its services
func TestExportToolIncludesAllServices(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	now := time.Now()
	if err := store.InsertLogs(ctx, []api.LogRecord{
		{Timestamp: now, ServiceName: "codex_cli_rs", Body: "cli"},
		{Timestamp: now, ServiceName: "codex-app-server", Body: "app"},
		{Timestamp: now, ServiceName: "claude-code", Body: "other"},
	}); err != nil {
		t.Fatal(err)
	}
	opts := Options{Source: SourceCodex, OutputDir: t.TempDir()}
	exporter := NewExporter(store, false)
	preview, err := exporter.Preview(ctx, opts)
	if err != nil || preview.LogsCount != 2 {
		t.Fatalf("Preview = %+v, %v; want 2 logs", preview, err)
	}
	summary, err := exporter.Export(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if summary.LogsCount != 2 {
		t.Errorf("exported %d codex logs, want 2", summary.LogsCount)
	}
	got := exportedServices(t, filepath.Join(opts.OutputDir, LogsFile))
	if !slices.Equal(got, []string{"codex-app-server", "codex_cli_rs"}) {
		t.Errorf("exported services = %v", got)
	}
	m, _, _ := ReadManifest(opts.OutputDir)
	if !slices.Equal(m.Services, []string{"codex_cli_rs", "codex-app-server", "codex-exec-server"}) {
		t.Errorf("manifest services = %v", m.Services)
	}
}

// A ranged export carries the last point before its start of each
// cumulative series, which the series' first exported point is the
// increase over
func TestExportBaselinesOfRange(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	start := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	temporality := int32(2)
	point := func(at time.Time, v float64, typ string) api.MetricDataPoint {
		return api.MetricDataPoint{
			Timestamp: at, ServiceName: "gemini_cli", MetricName: "gemini_cli.token.usage", MetricType: "sum",
			Value: &v, AggregationTemporality: &temporality, Attributes: map[string]string{"type": typ},
		}
	}
	if err := store.InsertMetrics(ctx, []api.MetricDataPoint{
		point(start.Add(-48*time.Hour), 1, "output"), // beyond the lookback
		point(start.Add(-2*time.Hour), 10, "input"),
		point(start.Add(-time.Hour), 20, "input"),
		point(start.Add(time.Hour), 35, "input"),
	}); err != nil {
		t.Fatal(err)
	}
	from, to := start, start.Add(24*time.Hour)
	dir := t.TempDir()
	summary, err := NewExporter(store, false).Export(ctx, Options{Source: SourceAll, OutputDir: dir, FromDate: &from, ToDate: &to})
	if err != nil {
		t.Fatal(err)
	}
	if summary.MetricsCount != 1 || summary.BaselinesCount != 1 {
		t.Fatalf("exported %d points and %d baselines, want 1 and 1", summary.MetricsCount, summary.BaselinesCount)
	}
	var ts time.Time
	var value float64
	if err := store.DB().QueryRow("SELECT Timestamp, Value FROM read_parquet(?)", filepath.Join(dir, BaselinesFile)).Scan(&ts, &value); err != nil {
		t.Fatal(err)
	}
	if !ts.Equal(start.Add(-time.Hour)) || value != 20 {
		t.Errorf("baseline = %v %v, want the point an hour before the start (20)", ts, value)
	}
}

// The views database is created without changing the working directory,
// which a running server shares with all its requests, and its views read
// the Parquet files next to it
func TestCreateViewsDatabaseKeepsWorkingDirectory(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)
	before, _ := os.Getwd()
	dir := t.TempDir()
	if _, err := NewExporter(store, false).Export(context.Background(), Options{Source: SourceAll, OutputDir: dir}); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Getwd(); after != before {
		t.Fatalf("working directory changed to %s", after)
	}

	views, _ := filepath.Glob(filepath.Join(dir, "ai-observer-export-*.duckdb"))
	if len(views) != 1 {
		t.Fatalf("views databases: %v", views)
	}
	t.Chdir(dir)
	db, err := sql.Open("duckdb", filepath.Base(views[0]))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var logs int
	if err := db.QueryRow("SELECT COUNT(*) FROM logs").Scan(&logs); err != nil || logs != 3 {
		t.Errorf("logs view: %d, %v", logs, err)
	}
}
