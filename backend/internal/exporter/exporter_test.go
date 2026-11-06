package exporter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
)

// setupTestStore creates an in-memory DuckDB store for testing
func setupTestStore(t *testing.T) (*storage.DuckDBStore, func()) {
	t.Helper()
	store, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	return store, func() { store.Close() }
}

// setupTestData inserts sample data into the test store
func setupTestData(t *testing.T, store *storage.DuckDBStore) {
	t.Helper()
	ctx := context.Background()
	now := time.Now()

	// Insert logs
	logs := []api.LogRecord{
		{Timestamp: now.Add(-1 * time.Hour), ServiceName: "claude-code", SeverityText: "INFO", Body: "log 1"},
		{Timestamp: now.Add(-30 * time.Minute), ServiceName: "claude-code", SeverityText: "INFO", Body: "log 2"},
		{Timestamp: now, ServiceName: "codex_cli_rs", SeverityText: "WARN", Body: "log 3"},
	}
	if err := store.InsertLogs(ctx, logs); err != nil {
		t.Fatalf("failed to insert logs: %v", err)
	}

	// Insert metrics
	metrics := []api.MetricDataPoint{
		{Timestamp: now.Add(-1 * time.Hour), ServiceName: "claude-code", MetricName: "token.usage", MetricType: "gauge", Value: ptrFloat64(100.0)},
		{Timestamp: now, ServiceName: "claude-code", MetricName: "cost.usage", MetricType: "gauge", Value: ptrFloat64(0.05)},
		{Timestamp: now, ServiceName: "gemini_cli", MetricName: "session.count", MetricType: "gauge", Value: ptrFloat64(1.0)},
	}
	if err := store.InsertMetrics(ctx, metrics); err != nil {
		t.Fatalf("failed to insert metrics: %v", err)
	}

	// Insert traces
	spans := []api.Span{
		{Timestamp: now, TraceID: "trace1", SpanID: "span1", SpanName: "test_span", ServiceName: "claude-code"},
		{Timestamp: now, TraceID: "trace1", SpanID: "span2", SpanName: "child_span", ServiceName: "claude-code", ParentSpanID: "span1"},
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("failed to insert spans: %v", err)
	}
}

func ptrFloat64(v float64) *float64 {
	return &v
}

func TestParseSourceArg(t *testing.T) {
	tests := []struct {
		input    string
		expected SourceType
		wantErr  bool
	}{
		{"claude-code", SourceClaude, false},
		{"codex", SourceCodex, false},
		{"gemini", SourceGemini, false},
		{"opencode", SourceOpenCode, false},
		{"copilot-chat", SourceCopilotVSCode, false},
		{"github-copilot", SourceCopilotCLI, false},
		{"all", SourceAll, false},
		{"CLAUDE-CODE", SourceClaude, false}, // case insensitive
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := ParseSourceArg(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for input %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error for input %q: %v", tt.input, err)
				return
			}
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestOptionsServiceName(t *testing.T) {
	tests := []struct {
		source   SourceType
		expected string
	}{
		{SourceClaude, "claude-code"},
		{SourceCodex, "codex_cli_rs"},
		{SourceGemini, "gemini_cli"},
		{SourceOpenCode, "opencode"},
		{SourceCopilotVSCode, "copilot-chat"},
		{SourceCopilotCLI, "github-copilot"},
		{SourceAll, ""},
	}

	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			opts := Options{Source: tt.source}
			result := opts.ServiceName()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestRunFromFilesRejectsCopilot(t *testing.T) {
	err := runFromFiles(context.Background(), Options{Source: SourceCopilotVSCode})
	if err == nil {
		t.Fatal("expected error for Copilot from-files export")
	}
	if err.Error() != "from-files export is not supported for copilot-chat because GitHub Copilot (VS Code) is OTLP-only" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunFromFilesRejectsOpenCode(t *testing.T) {
	err := runFromFiles(context.Background(), Options{Source: SourceOpenCode})
	if err == nil {
		t.Fatal("expected error for OpenCode from-files export")
	}
	if err.Error() != "from-files export is not supported for opencode because OpenCode is OTLP-only" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestOptionsDateRangeString(t *testing.T) {
	date1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name     string
		from     *time.Time
		to       *time.Time
		expected string
	}{
		{"no dates", nil, nil, "all"},
		{"both dates", &date1, &date2, "2025-01-01-2025-01-15"},
		{"only from", &date1, nil, "2025-01-01-now"},
		{"only to", nil, &date2, "start-2025-01-15"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := Options{FromDate: tt.from, ToDate: tt.to}
			result := opts.DateRangeString()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestSummaryIsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		summary  Summary
		expected bool
	}{
		{"empty", Summary{}, true},
		{"has traces", Summary{TracesCount: 1}, false},
		{"has logs", Summary{LogsCount: 1}, false},
		{"has metrics", Summary{MetricsCount: 1}, false},
		{"has all", Summary{TracesCount: 1, LogsCount: 2, MetricsCount: 3}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.summary.IsEmpty()
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestExporterPreview(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	exporter := NewExporter(store, false)

	t.Run("all services", func(t *testing.T) {
		opts := Options{Source: SourceAll}
		summary, err := exporter.Preview(ctx, opts)
		if err != nil {
			t.Fatalf("Preview failed: %v", err)
		}

		if summary.LogsCount != 3 {
			t.Errorf("expected 3 logs, got %d", summary.LogsCount)
		}
		if summary.MetricsCount != 3 {
			t.Errorf("expected 3 metrics, got %d", summary.MetricsCount)
		}
		if summary.TracesCount != 2 {
			t.Errorf("expected 2 traces, got %d", summary.TracesCount)
		}
	})

	t.Run("claude only", func(t *testing.T) {
		opts := Options{Source: SourceClaude}
		summary, err := exporter.Preview(ctx, opts)
		if err != nil {
			t.Fatalf("Preview failed: %v", err)
		}

		if summary.LogsCount != 2 {
			t.Errorf("expected 2 logs for claude, got %d", summary.LogsCount)
		}
		if summary.MetricsCount != 2 {
			t.Errorf("expected 2 metrics for claude, got %d", summary.MetricsCount)
		}
		if summary.TracesCount != 2 {
			t.Errorf("expected 2 traces for claude, got %d", summary.TracesCount)
		}
	})

	t.Run("codex only", func(t *testing.T) {
		opts := Options{Source: SourceCodex}
		summary, err := exporter.Preview(ctx, opts)
		if err != nil {
			t.Fatalf("Preview failed: %v", err)
		}

		if summary.LogsCount != 1 {
			t.Errorf("expected 1 log for codex, got %d", summary.LogsCount)
		}
		if summary.MetricsCount != 0 {
			t.Errorf("expected 0 metrics for codex, got %d", summary.MetricsCount)
		}
	})
}

func TestExporterExport(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	exporter := NewExporter(store, false)

	// Create temp directory for export
	tmpDir, err := os.MkdirTemp("", "exporter-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	opts := Options{
		Source:    SourceAll,
		OutputDir: tmpDir,
	}

	summary, err := exporter.Export(ctx, opts)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Verify counts
	if summary.LogsCount != 3 {
		t.Errorf("expected 3 logs, got %d", summary.LogsCount)
	}
	if summary.MetricsCount != 3 {
		t.Errorf("expected 3 metrics, got %d", summary.MetricsCount)
	}
	if summary.TracesCount != 2 {
		t.Errorf("expected 2 traces, got %d", summary.TracesCount)
	}

	// Verify files exist
	expectedFiles := []string{
		"traces.parquet",
		"logs.parquet",
		"metrics.parquet",
	}
	for _, file := range expectedFiles {
		path := filepath.Join(tmpDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s to exist", file)
		}
	}

	// Verify views database exists
	viewsFiles, _ := filepath.Glob(filepath.Join(tmpDir, "ai-observer-export-*.duckdb"))
	if len(viewsFiles) != 1 {
		t.Errorf("expected 1 views database file, got %d", len(viewsFiles))
	}
}

func TestExporterExportWithZip(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	exporter := NewExporter(store, false)

	// Create temp directory for export
	tmpDir, err := os.MkdirTemp("", "exporter-zip-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	opts := Options{
		Source:    SourceAll,
		OutputDir: tmpDir,
		CreateZip: true,
	}

	summary, err := exporter.Export(ctx, opts)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Verify only ZIP file exists (others should be cleaned up)
	if len(summary.OutputFiles) != 1 {
		t.Errorf("expected 1 output file (ZIP), got %d", len(summary.OutputFiles))
	}

	// Verify it's a ZIP file
	if len(summary.OutputFiles) > 0 {
		if filepath.Ext(summary.OutputFiles[0]) != ".zip" {
			t.Errorf("expected .zip extension, got %s", filepath.Ext(summary.OutputFiles[0]))
		}
		// Verify ZIP exists
		if _, err := os.Stat(summary.OutputFiles[0]); os.IsNotExist(err) {
			t.Errorf("expected ZIP file to exist at %s", summary.OutputFiles[0])
		}
	}

	// Verify parquet files are removed
	parquetFiles, _ := filepath.Glob(filepath.Join(tmpDir, "*.parquet"))
	if len(parquetFiles) != 0 {
		t.Errorf("expected parquet files to be removed after zipping, found %d", len(parquetFiles))
	}
}

func TestGetDateRange(t *testing.T) {
	date1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	t.Run("both nil", func(t *testing.T) {
		from, to := getDateRange(nil, nil)
		if from.Year() != 2000 {
			t.Errorf("expected default from year 2000, got %d", from.Year())
		}
		if to.Year() != 2100 {
			t.Errorf("expected default to year 2100, got %d", to.Year())
		}
	})

	t.Run("from only", func(t *testing.T) {
		from, to := getDateRange(&date1, nil)
		if !from.Equal(date1) {
			t.Errorf("expected from to equal input date")
		}
		if to.Year() != 2100 {
			t.Errorf("expected default to year 2100, got %d", to.Year())
		}
	})

	t.Run("to only", func(t *testing.T) {
		from, to := getDateRange(nil, &date2)
		if from.Year() != 2000 {
			t.Errorf("expected default from year 2000, got %d", from.Year())
		}
		if !to.Equal(date2) {
			t.Errorf("expected to to equal input date")
		}
	})

	t.Run("both set", func(t *testing.T) {
		from, to := getDateRange(&date1, &date2)
		if !from.Equal(date1) {
			t.Errorf("expected from to equal input date1")
		}
		if !to.Equal(date2) {
			t.Errorf("expected to to equal input date2")
		}
	})
}

func TestValidSources(t *testing.T) {
	sources := ValidSources()
	if len(sources) != 7 {
		t.Errorf("expected 7 valid sources, got %d", len(sources))
	}

	expected := map[string]bool{
		"claude-code":    true,
		"codex":          true,
		"gemini":         true,
		"opencode":       true,
		"copilot-chat":   true,
		"github-copilot": true,
		"all":            true,
	}

	for _, s := range sources {
		if !expected[s] {
			t.Errorf("unexpected source %q in ValidSources", s)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes    int64
		expected string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1023, "1023 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
		{1099511627776, "1.0 TB"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := formatSize(tt.bytes)
			if result != tt.expected {
				t.Errorf("formatSize(%d) = %q, want %q", tt.bytes, result, tt.expected)
			}
		})
	}
}

func TestExporterExportWithDateFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	exporter := NewExporter(store, false)

	// Create temp directory for export
	tmpDir, err := os.MkdirTemp("", "exporter-date-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Use a date range that includes our test data
	now := time.Now()
	from := now.Add(-2 * time.Hour)
	to := now.Add(1 * time.Hour)

	opts := Options{
		Source:    SourceAll,
		OutputDir: tmpDir,
		FromDate:  &from,
		ToDate:    &to,
	}

	summary, err := exporter.Export(ctx, opts)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Verify files were created
	if len(summary.OutputFiles) == 0 {
		t.Error("expected output files")
	}

	// Verify the database filename includes dates
	viewsFiles, _ := filepath.Glob(filepath.Join(tmpDir, "ai-observer-export-*.duckdb"))
	if len(viewsFiles) != 1 {
		t.Errorf("expected 1 views database file, got %d", len(viewsFiles))
	}
}

func TestExporterExportWithServiceFilter(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	exporter := NewExporter(store, false)

	// Create temp directory for export
	tmpDir, err := os.MkdirTemp("", "exporter-service-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Export only gemini data
	opts := Options{
		Source:    SourceGemini,
		OutputDir: tmpDir,
	}

	summary, err := exporter.Export(ctx, opts)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// We only have 1 gemini metric in test data
	if summary.MetricsCount != 1 {
		t.Errorf("expected 1 metric for gemini, got %d", summary.MetricsCount)
	}
	if summary.LogsCount != 0 {
		t.Errorf("expected 0 logs for gemini, got %d", summary.LogsCount)
	}
}

func TestExporterVerboseMode(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	exporter := NewExporter(store, true) // verbose mode

	// Create temp directory for export
	tmpDir, err := os.MkdirTemp("", "exporter-verbose-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	opts := Options{
		Source:    SourceAll,
		OutputDir: tmpDir,
	}

	// Just verify it doesn't error with verbose mode
	_, err = exporter.Export(ctx, opts)
	if err != nil {
		t.Fatalf("Export with verbose mode failed: %v", err)
	}
}

func TestCreateZipArchive(t *testing.T) {
	// Create temp directory with test files
	tmpDir, err := os.MkdirTemp("", "zip-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	testFiles := []string{
		filepath.Join(tmpDir, "file1.txt"),
		filepath.Join(tmpDir, "file2.txt"),
	}
	for i, f := range testFiles {
		if err := os.WriteFile(f, []byte("content "+string(rune('1'+i))), 0644); err != nil {
			t.Fatalf("failed to write test file: %v", err)
		}
	}

	zipPath := filepath.Join(tmpDir, "test.zip")
	err = CreateZipArchive(tmpDir, testFiles, zipPath)
	if err != nil {
		t.Fatalf("CreateZipArchive failed: %v", err)
	}

	// Verify ZIP exists
	if _, err := os.Stat(zipPath); os.IsNotExist(err) {
		t.Errorf("expected ZIP file to exist")
	}

	// Verify ZIP size is reasonable
	info, _ := os.Stat(zipPath)
	if info.Size() == 0 {
		t.Errorf("ZIP file is empty")
	}
}

func TestExporterExportEmptyDatabase(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	// Don't insert any data

	ctx := context.Background()
	exporter := NewExporter(store, false)

	// Create temp directory for export
	tmpDir, err := os.MkdirTemp("", "exporter-empty-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	opts := Options{
		Source:    SourceAll,
		OutputDir: tmpDir,
	}

	summary, err := exporter.Export(ctx, opts)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// All counts should be 0
	if summary.LogsCount != 0 || summary.MetricsCount != 0 || summary.TracesCount != 0 {
		t.Errorf("expected all counts to be 0 for empty database")
	}

	// Files should still be created (empty parquet files)
	if len(summary.OutputFiles) == 0 {
		t.Error("expected output files even for empty export")
	}
}

func TestPrintPreview(t *testing.T) {
	// Test with date range
	date1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	summary := &Summary{
		TracesCount:  100,
		LogsCount:    200,
		MetricsCount: 300,
	}

	t.Run("with dates", func(t *testing.T) {
		opts := Options{
			Source:    SourceClaude,
			OutputDir: "/tmp/test",
			FromDate:  &date1,
			ToDate:    &date2,
		}
		// Just verify it doesn't panic
		PrintPreview(summary, opts)
	})

	t.Run("without dates", func(t *testing.T) {
		opts := Options{
			Source:    SourceAll,
			OutputDir: "/tmp/test",
		}
		PrintPreview(summary, opts)
	})

	t.Run("with from only", func(t *testing.T) {
		opts := Options{
			Source:    SourceAll,
			OutputDir: "/tmp/test",
			FromDate:  &date1,
		}
		PrintPreview(summary, opts)
	})

	t.Run("with to only", func(t *testing.T) {
		opts := Options{
			Source:    SourceAll,
			OutputDir: "/tmp/test",
			ToDate:    &date2,
		}
		PrintPreview(summary, opts)
	})

	t.Run("with zip", func(t *testing.T) {
		opts := Options{
			Source:    SourceAll,
			OutputDir: "/tmp/test",
			CreateZip: true,
		}
		PrintPreview(summary, opts)
	})

	t.Run("from files mode", func(t *testing.T) {
		opts := Options{
			Source:    SourceClaude,
			OutputDir: "/tmp/test",
			FromFiles: true,
		}
		PrintPreview(summary, opts)
	})
}

func TestPrintResult(t *testing.T) {
	summary := &Summary{
		TracesCount:  100,
		LogsCount:    200,
		MetricsCount: 300,
		TotalSize:    1048576, // 1 MB
		OutputFiles:  []string{"/tmp/test/traces.parquet", "/tmp/test/logs.parquet"},
	}

	opts := Options{
		Source:    SourceAll,
		OutputDir: "/tmp/test",
	}

	// Just verify it doesn't panic
	PrintResult(summary, opts)
}

func TestRunDryRun(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()

	// Create temp directory for export
	tmpDir, err := os.MkdirTemp("", "exporter-run-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	opts := Options{
		Source:    SourceAll,
		OutputDir: tmpDir,
		DryRun:    true,
		Verbose:   true,
	}

	// Run with dry-run - should not create files
	err = Run(ctx, store, opts)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify no files were created
	files, _ := filepath.Glob(filepath.Join(tmpDir, "*"))
	if len(files) != 0 {
		t.Errorf("expected no files for dry-run, got %d", len(files))
	}
}

func TestRunWithSkipConfirm(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()

	// Create temp directory for export
	tmpDir, err := os.MkdirTemp("", "exporter-run-confirm-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	opts := Options{
		Source:      SourceAll,
		OutputDir:   tmpDir,
		SkipConfirm: true,
		Verbose:     true,
	}

	// Run with skip confirm
	err = Run(ctx, store, opts)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify files were created
	files, _ := filepath.Glob(filepath.Join(tmpDir, "*.parquet"))
	if len(files) != 3 {
		t.Errorf("expected 3 parquet files, got %d", len(files))
	}
}

func TestRunEmptyDatabase(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	// Don't insert data - empty database

	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "exporter-run-empty-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	opts := Options{
		Source:      SourceAll,
		OutputDir:   tmpDir,
		SkipConfirm: true,
	}

	// Run should handle empty database gracefully
	err = Run(ctx, store, opts)
	if err != nil {
		t.Fatalf("Run failed on empty database: %v", err)
	}
}

func TestGenerateViewsDBPath(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	exporter := NewExporter(store, false)

	date1 := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	date2 := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	t.Run("with dates", func(t *testing.T) {
		opts := Options{
			Source:    SourceClaude,
			OutputDir: "/tmp/export",
			FromDate:  &date1,
			ToDate:    &date2,
		}
		path := exporter.generateViewsDBPath(opts)
		expected := "/tmp/export/ai-observer-export-claude-code-2025-01-01-2025-01-15.duckdb"
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})

	t.Run("without dates", func(t *testing.T) {
		opts := Options{
			Source:    SourceAll,
			OutputDir: "/tmp/export",
		}
		path := exporter.generateViewsDBPath(opts)
		expected := "/tmp/export/ai-observer-export-all-all.duckdb"
		if path != expected {
			t.Errorf("expected %q, got %q", expected, path)
		}
	})
}

func TestGenerateZipPath(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	exporter := NewExporter(store, false)

	opts := Options{
		Source:    SourceGemini,
		OutputDir: "/tmp/export",
	}
	path := exporter.generateZipPath(opts)
	expected := "/tmp/export/ai-observer-export-gemini-all.zip"
	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestCreateZipArchiveWithNonexistentFile(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "zip-error-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Try to zip a non-existent file
	files := []string{filepath.Join(tmpDir, "nonexistent.txt")}
	zipPath := filepath.Join(tmpDir, "test.zip")

	err = CreateZipArchive(tmpDir, files, zipPath)
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestCreateViewsDatabase(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	ctx := context.Background()
	exporter := NewExporter(store, false)

	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "views-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// First export to create valid parquet files
	opts := Options{
		Source:    SourceAll,
		OutputDir: tmpDir,
	}

	_, err = exporter.Export(ctx, opts)
	if err != nil {
		t.Fatalf("Export failed: %v", err)
	}

	// Verify views database was created
	viewsFiles, _ := filepath.Glob(filepath.Join(tmpDir, "*.duckdb"))
	if len(viewsFiles) != 1 {
		t.Errorf("expected 1 views database file, got %d", len(viewsFiles))
	}

	// Verify the database file is not empty
	if len(viewsFiles) > 0 {
		info, err := os.Stat(viewsFiles[0])
		if err != nil {
			t.Errorf("failed to stat views database: %v", err)
		}
		if info.Size() == 0 {
			t.Error("views database is empty")
		}
	}
}

// Session file fixtures for every file-backed provider, in the layout each
// tool writes. GitHub Copilot and OpenCode are OTLP-only and have no files.
const (
	claudeFixtureSession = `{"type":"assistant","timestamp":"2025-01-02T10:00:00.000Z","sessionId":"claude-fixture","message":{"id":"msg-001","model":"claude-sonnet-4-20250514","role":"assistant","type":"message","usage":{"input_tokens":1000,"output_tokens":500}}}
`
	codexFixtureSession = `{"timestamp":"2025-01-02T11:00:00.000Z","type":"session_meta","payload":{"id":"codex-fixture","timestamp":"2025-01-02T11:00:00.000Z","cwd":"/home/user/project","originator":"cli","cli_version":"1.0.0","model_provider":"openai","model":"gpt-4o"}}
{"timestamp":"2025-01-02T11:01:00.000Z","type":"event_msg","payload":{"type":"user_message","message":"hello"}}
{"timestamp":"2025-01-02T11:02:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":500,"output_tokens":200,"cached_input_tokens":0,"reasoning_output_tokens":50}}}}
`
	geminiFixtureSession = `{
  "sessionId": "gemini-fixture",
  "projectHash": "fixtureproject",
  "startTime": "2025-01-02T12:00:00.000Z",
  "lastUpdated": "2025-01-02T12:05:00.000Z",
  "messages": [
    {"id": "msg-001", "timestamp": "2025-01-02T12:00:00.000Z", "type": "user", "content": "hello"},
    {"id": "msg-002", "timestamp": "2025-01-02T12:01:00.000Z", "type": "gemini", "model": "gemini-2.5-flash",
     "tokens": {"input": 500, "output": 200, "cached": 50, "thoughts": 30, "tool": 10, "total": 790}}
  ]
}
`
)

// setupFromFilesFixtures writes one session per file-backed provider into a
// temp directory and points the importers at it, so from-files exports never
// read the developer's real ~/.claude, ~/.codex or ~/.gemini data.
func setupFromFilesFixtures(t *testing.T) {
	t.Helper()
	root := t.TempDir()

	fixtures := []struct {
		envVar  string
		baseDir string
		file    string
		content string
	}{
		{"AI_OBSERVER_CLAUDE_PATH", "claude", filepath.Join("-home-user-project", "claude-fixture.jsonl"), claudeFixtureSession},
		{"AI_OBSERVER_CODEX_PATH", "codex", filepath.Join("2025", "01", "02", "rollout-2025-01-02T11-00-00-codex-fixture.jsonl"), codexFixtureSession},
		{"AI_OBSERVER_GEMINI_PATH", "gemini", filepath.Join("fixtureproject", "chats", "session-gemini-fixture.json"), geminiFixtureSession},
	}
	for _, f := range fixtures {
		path := filepath.Join(root, f.baseDir, f.file)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("failed to create fixture dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(f.content), 0644); err != nil {
			t.Fatalf("failed to write fixture %s: %v", path, err)
		}
		t.Setenv(f.envVar, filepath.Join(root, f.baseDir))
	}
}

// exportedServices returns the distinct service names in an exported Parquet file
func exportedServices(t *testing.T, parquetPath string) []string {
	t.Helper()
	reader, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		t.Fatalf("failed to open DuckDB: %v", err)
	}
	defer reader.Close()

	rows, err := reader.DB().Query(fmt.Sprintf("SELECT DISTINCT ServiceName FROM read_parquet('%s') ORDER BY 1", parquetPath))
	if err != nil {
		t.Fatalf("failed to read %s: %v", parquetPath, err)
	}
	defer rows.Close()

	var services []string
	for rows.Next() {
		var service string
		if err := rows.Scan(&service); err != nil {
			t.Fatalf("failed to scan service: %v", err)
		}
		services = append(services, service)
	}
	return services
}

func TestRunFromFiles(t *testing.T) {
	setupFromFilesFixtures(t)
	outputDir := t.TempDir()
	ctx := context.Background()

	t.Run("from files dry run", func(t *testing.T) {
		opts := Options{
			Source:      SourceClaude,
			OutputDir:   outputDir,
			FromFiles:   true,
			DryRun:      true,
			SkipConfirm: true,
			Verbose:     true,
		}

		err := runFromFiles(ctx, opts)
		if err != nil {
			t.Fatalf("runFromFiles failed: %v", err)
		}

		// Verify no files were created (dry run)
		files, _ := filepath.Glob(filepath.Join(outputDir, "*.parquet"))
		if len(files) != 0 {
			t.Errorf("expected no parquet files for dry run, got %d", len(files))
		}
	})

	t.Run("from files actual export", func(t *testing.T) {
		outputDir2 := t.TempDir()

		opts := Options{
			Source:      SourceClaude,
			OutputDir:   outputDir2,
			FromFiles:   true,
			SkipConfirm: true,
			Verbose:     false,
		}

		err := runFromFiles(ctx, opts)
		if err != nil {
			t.Fatalf("runFromFiles failed: %v", err)
		}

		// Verify files were created
		parquetFiles, _ := filepath.Glob(filepath.Join(outputDir2, "*.parquet"))
		if len(parquetFiles) == 0 {
			t.Error("expected parquet files to be created")
		}

		// Only the requested provider is exported
		got := exportedServices(t, filepath.Join(outputDir2, "metrics.parquet"))
		if strings.Join(got, ",") != "claude-code" {
			t.Errorf("exported services = %v, want [claude-code]", got)
		}
	})

	t.Run("from files with all sources", func(t *testing.T) {
		outputDir3 := t.TempDir()

		opts := Options{
			Source:      SourceAll,
			OutputDir:   outputDir3,
			FromFiles:   true,
			SkipConfirm: true,
		}

		err := runFromFiles(ctx, opts)
		if err != nil {
			t.Fatalf("runFromFiles failed: %v", err)
		}

		// Every file-backed provider's fixture is exported
		want := "claude-code,codex_cli_rs,gemini_cli"
		for _, file := range []string{"logs.parquet", "metrics.parquet"} {
			got := exportedServices(t, filepath.Join(outputDir3, file))
			if strings.Join(got, ",") != want {
				t.Errorf("%s services = %v, want %s", file, got, want)
			}
		}
	})

	t.Run("from files invalid source", func(t *testing.T) {
		opts := Options{
			Source:      SourceType("invalid"),
			OutputDir:   outputDir,
			FromFiles:   true,
			SkipConfirm: true,
		}

		err := runFromFiles(ctx, opts)
		if err == nil {
			t.Error("expected error for invalid source")
		}
	})
}

func TestConfirmExport(t *testing.T) {
	for answer, want := range map[string]bool{"y\n": true, "YES\n": true, "n\n": false, "\n": false, "": false} {
		var out strings.Builder
		got := ConfirmExport(Options{Out: &out, In: strings.NewReader(answer)})
		if got != want {
			t.Errorf("ConfirmExport(%q) = %v, want %v", answer, got, want)
		}
		if !strings.Contains(out.String(), "Continue? [y/N]") {
			t.Errorf("prompt not written to Out: %q", out.String())
		}
	}
}

func TestExporterExportToPathWithQuote(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	setupTestData(t, store)

	dir := filepath.Join(t.TempDir(), "Bob's exports")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	summary, err := NewExporter(store, false).Export(context.Background(), Options{Source: SourceAll, OutputDir: dir})
	if err != nil {
		t.Fatalf("Export to %q failed: %v", dir, err)
	}
	if summary.LogsCount != 3 {
		t.Errorf("expected 3 logs, got %d", summary.LogsCount)
	}
}
