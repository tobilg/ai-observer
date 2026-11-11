package exporter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/tobilg/ai-observer/internal/diskspace"
	"github.com/tobilg/ai-observer/internal/prompt"
	"github.com/tobilg/ai-observer/internal/storage"
)

// Exporter handles exporting telemetry data to Parquet files
type Exporter struct {
	store   *storage.DuckDBStore
	verbose bool
}

// NewExporter creates a new Exporter
func NewExporter(store *storage.DuckDBStore, verbose bool) *Exporter {
	return &Exporter{
		store:   store,
		verbose: verbose,
	}
}

// Preview returns a summary of what would be exported without actually exporting
func (e *Exporter) Preview(ctx context.Context, opts Options) (*Summary, error) {
	summary := &Summary{}

	// Get counts from the database (a tool's records of all its services)
	service := opts.ServiceFilter()

	// For preview, we need from/to dates - if not set, use a wide range
	from, to := getDateRange(opts.FromDate, opts.ToDate)

	// Count traces
	_, spanCount, err := e.store.CountTracesInRange(ctx, from, to, service)
	if err != nil {
		return nil, fmt.Errorf("counting traces: %w", err)
	}
	summary.TracesCount = spanCount

	// Count logs
	logCount, err := e.store.CountLogsInRange(ctx, from, to, service)
	if err != nil {
		return nil, fmt.Errorf("counting logs: %w", err)
	}
	summary.LogsCount = logCount

	// Count metrics
	metricCount, err := e.store.CountMetricsInRange(ctx, from, to, service)
	if err != nil {
		return nil, fmt.Errorf("counting metrics: %w", err)
	}
	summary.MetricsCount = metricCount

	return summary, nil
}

// getDateRange returns time.Time values from optional pointers
// Uses a very wide range if either date is nil
func getDateRange(from, to *time.Time) (time.Time, time.Time) {
	// Default to year 2000 to year 2100 if not specified
	defaultFrom := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	defaultTo := time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)

	fromTime := defaultFrom
	toTime := defaultTo

	if from != nil {
		fromTime = *from
	}
	if to != nil {
		toTime = *to
	}

	return fromTime, toTime
}

// Export performs the actual export to Parquet files: the telemetry
// (traces.parquet, logs.parquet, metrics.parquet), the metric baselines its
// cumulative series need (metric_baselines.parquet), the import state of
// the session files its records were read from (import_state.parquet), all
// dashboards (dashboards.json), a manifest (manifest.json) and a database
// of views of the Parquet files. ai-observer restore reads them back. The
// Parquet files are written in one read transaction, so they show the
// database at one point in time while the server keeps storing telemetry.
func (e *Exporter) Export(ctx context.Context, opts Options) (*Summary, error) {
	summary := &Summary{}

	// Ensure output directory exists
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("creating output directory: %w", err)
	}

	service := opts.ServiceFilter()
	path := func(name string) string { return filepath.Join(opts.OutputDir, name) }
	step := func(what string, run func() (int64, error), file string) (int64, error) {
		if e.verbose {
			fmt.Fprintf(opts.out(), "Exporting %s... ", what)
		}
		count, err := run()
		if err != nil {
			return 0, fmt.Errorf("exporting %s: %w", what, err)
		}
		summary.OutputFiles = append(summary.OutputFiles, path(file))
		if e.verbose {
			fmt.Fprintf(opts.out(), "done (%d rows)\n", count)
		}
		return count, nil
	}

	if err := e.inSnapshot(ctx, func(snap snapshot) error {
		var err error
		files := []struct {
			what, file string
			count      *int64
			run        func(path string) (int64, error)
		}{
			{"traces", TracesFile, &summary.TracesCount, func(p string) (int64, error) {
				return snap.exportToParquet(ctx, "otel_traces", p, opts.FromDate, opts.ToDate, service)
			}},
			{"logs", LogsFile, &summary.LogsCount, func(p string) (int64, error) {
				return snap.exportToParquet(ctx, "otel_logs", p, opts.FromDate, opts.ToDate, service)
			}},
			{"metrics", MetricsFile, &summary.MetricsCount, func(p string) (int64, error) {
				return snap.exportToParquet(ctx, "otel_metrics", p, opts.FromDate, opts.ToDate, service)
			}},
			{"metric baselines", BaselinesFile, &summary.BaselinesCount, func(p string) (int64, error) {
				return snap.exportBaselines(ctx, p, opts.FromDate, opts.ToDate, service)
			}},
			{"session file state", ImportStateFile, &summary.ImportStateCount, func(p string) (int64, error) {
				return snap.exportImportState(ctx, p, opts.FromDate, opts.ToDate, service)
			}},
		}
		for _, f := range files {
			if *f.count, err = step(f.what, func() (int64, error) { return f.run(path(f.file)) }, f.file); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	dashboards, err := step("dashboards", func() (int64, error) {
		n, err := e.exportDashboards(ctx, path(DashboardsFile))
		return int64(n), err
	}, DashboardsFile)
	if err != nil {
		return nil, err
	}
	summary.DashboardsCount = int(dashboards)
	if err := e.writeManifest(ctx, path(ManifestFile), opts, summary); err != nil {
		return nil, fmt.Errorf("writing manifest: %w", err)
	}
	summary.OutputFiles = append(summary.OutputFiles, path(ManifestFile))

	// Create views database
	if e.verbose {
		fmt.Fprint(opts.out(), "Creating views database... ")
	}
	viewsDBPath := e.generateViewsDBPath(opts)
	if err := e.createViewsDatabase(ctx, viewsDBPath); err != nil {
		return nil, fmt.Errorf("creating views database: %w", err)
	}
	summary.OutputFiles = append(summary.OutputFiles, viewsDBPath)
	if e.verbose {
		fmt.Fprintln(opts.out(), "done")
	}

	// Create ZIP archive if requested
	if opts.CreateZip {
		if e.verbose {
			fmt.Fprint(opts.out(), "Creating ZIP archive... ")
		}
		zipPath := e.generateZipPath(opts)
		if err := CreateZipArchive(opts.OutputDir, summary.OutputFiles, zipPath); err != nil {
			return nil, fmt.Errorf("creating ZIP archive: %w", err)
		}

		// Remove original files after zipping
		for _, file := range summary.OutputFiles {
			os.Remove(file)
		}

		// Update output files to only include ZIP
		summary.OutputFiles = []string{zipPath}
		if e.verbose {
			fmt.Fprintln(opts.out(), "done")
		}
	}

	// Calculate total size
	for _, file := range summary.OutputFiles {
		if info, err := os.Stat(file); err == nil {
			summary.TotalSize += info.Size()
		}
	}

	return summary, nil
}

// inSnapshot runs fn in a read transaction on one connection: DuckDB gives
// a transaction one snapshot of the database, so the files fn writes agree
// with each other (an import's records with its import state, a log with
// the metrics derived from it) although writers commit meanwhile
func (e *Exporter) inSnapshot(ctx context.Context, fn func(snapshot) error) error {
	conn, err := e.store.DB().Conn(ctx)
	if err != nil {
		return fmt.Errorf("opening a connection: %w", err)
	}
	defer conn.Close()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning the export transaction: %w", err)
	}
	// Read only: rolling back ends it as well as a commit would
	defer tx.Rollback()
	return fn(snapshot{tx: tx})
}

// generateViewsDBPath generates the views database filename
func (e *Exporter) generateViewsDBPath(opts Options) string {
	filename := fmt.Sprintf("ai-observer-export-%s-%s.duckdb", opts.Source, opts.DateRangeString())
	return filepath.Join(opts.OutputDir, filename)
}

// generateZipPath generates the ZIP archive filename
func (e *Exporter) generateZipPath(opts Options) string {
	filename := fmt.Sprintf("ai-observer-export-%s-%s.zip", opts.Source, opts.DateRangeString())
	return filepath.Join(opts.OutputDir, filename)
}

// PrintPreview prints the export preview to opts.Out
func PrintPreview(summary *Summary, opts Options) {
	fmt.Fprintln(opts.out())
	fmt.Fprintln(opts.out(), "Export Preview")
	fmt.Fprintln(opts.out(), "==============")

	if opts.FromFiles {
		fmt.Fprintf(opts.out(), "Source: %s (from files)\n", opts.Source)
	} else {
		fmt.Fprintf(opts.out(), "Source: %s (from database)\n", opts.Source)
	}

	if opts.FromDate != nil || opts.ToDate != nil {
		from := "start"
		to := "now"
		if opts.FromDate != nil {
			from = opts.FromDate.Format("2006-01-02")
		}
		if opts.ToDate != nil {
			to = opts.ToDate.Format("2006-01-02")
		}
		fmt.Fprintf(opts.out(), "Time range: %s to %s\n", from, to)
	} else {
		fmt.Fprintln(opts.out(), "Time range: all")
	}

	fmt.Fprintln(opts.out())
	fmt.Fprintln(opts.out(), "Data to export:")
	fmt.Fprintf(opts.out(), "  Traces:  %d spans\n", summary.TracesCount)
	fmt.Fprintf(opts.out(), "  Logs:    %d records\n", summary.LogsCount)
	fmt.Fprintf(opts.out(), "  Metrics: %d data points\n", summary.MetricsCount)

	fmt.Fprintln(opts.out())
	fmt.Fprintf(opts.out(), "Output directory: %s\n", opts.OutputDir)
	fmt.Fprintln(opts.out(), "Files to create:")
	for _, file := range []string{TracesFile, LogsFile, MetricsFile, BaselinesFile, ImportStateFile, DashboardsFile, ManifestFile} {
		fmt.Fprintf(opts.out(), "  - %s\n", file)
	}
	fmt.Fprintf(opts.out(), "  - ai-observer-export-%s-%s.duckdb\n", opts.Source, opts.DateRangeString())

	if opts.CreateZip {
		fmt.Fprintf(opts.out(), "  - ai-observer-export-%s-%s.zip (all files combined)\n", opts.Source, opts.DateRangeString())
	}
}

// PrintResult prints the export result to opts.Out
func PrintResult(summary *Summary, opts Options) {
	fmt.Fprintln(opts.out())
	fmt.Fprintln(opts.out(), "Export complete!")
	fmt.Fprintf(opts.out(), "Output: %s (%d files, %s)\n", opts.OutputDir, len(summary.OutputFiles), diskspace.FormatBytes(summary.TotalSize))
}

// ConfirmExport prompts the user for confirmation
func ConfirmExport(opts Options) bool {
	fmt.Fprintln(opts.out())
	fmt.Fprint(opts.out(), "Continue? [y/N] ")

	return prompt.Confirmed(opts.in())
}

// Run executes the full export workflow with preview and confirmation
func Run(ctx context.Context, store *storage.DuckDBStore, opts Options) error {
	exporter := NewExporter(store, opts.Verbose)

	// For from-files mode, we need to use a different approach
	if opts.FromFiles {
		return runFromFiles(ctx, opts)
	}

	// Preview what would be exported
	summary, err := exporter.Preview(ctx, opts)
	if err != nil {
		return fmt.Errorf("preview failed: %w", err)
	}

	// Check if there's anything to export
	if summary.IsEmpty() {
		fmt.Fprintln(opts.out(), "No data found to export.")
		return nil
	}

	// Print preview
	PrintPreview(summary, opts)

	// If dry-run, stop here
	if opts.DryRun {
		fmt.Fprintln(opts.out())
		fmt.Fprintln(opts.out(), "Dry run - no files created.")
		return nil
	}

	// Ask for confirmation unless --yes flag is set
	if !opts.SkipConfirm {
		if !ConfirmExport(opts) {
			fmt.Fprintln(opts.out(), "Aborted.")
			return nil
		}
	}

	fmt.Fprintln(opts.out())

	// Execute export
	result, err := exporter.Export(ctx, opts)
	if err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	// Print result
	PrintResult(result, opts)

	return nil
}
