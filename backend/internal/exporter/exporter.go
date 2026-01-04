package exporter

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

	// Get counts from the database
	service := opts.ServiceName()

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

// Export performs the actual export to Parquet files
func (e *Exporter) Export(ctx context.Context, opts Options) (*Summary, error) {
	summary := &Summary{}

	// Ensure output directory exists
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return nil, fmt.Errorf("creating output directory: %w", err)
	}

	// Export each table to Parquet
	tracesPath := filepath.Join(opts.OutputDir, "traces.parquet")
	logsPath := filepath.Join(opts.OutputDir, "logs.parquet")
	metricsPath := filepath.Join(opts.OutputDir, "metrics.parquet")

	service := opts.ServiceName()

	// Export traces
	if e.verbose {
		fmt.Print("Exporting traces... ")
	}
	tracesCount, err := e.exportToParquet(ctx, "otel_traces", tracesPath, opts.FromDate, opts.ToDate, service)
	if err != nil {
		return nil, fmt.Errorf("exporting traces: %w", err)
	}
	summary.TracesCount = tracesCount
	summary.OutputFiles = append(summary.OutputFiles, tracesPath)
	if e.verbose {
		fmt.Printf("done (%d rows)\n", tracesCount)
	}

	// Export logs
	if e.verbose {
		fmt.Print("Exporting logs... ")
	}
	logsCount, err := e.exportToParquet(ctx, "otel_logs", logsPath, opts.FromDate, opts.ToDate, service)
	if err != nil {
		return nil, fmt.Errorf("exporting logs: %w", err)
	}
	summary.LogsCount = logsCount
	summary.OutputFiles = append(summary.OutputFiles, logsPath)
	if e.verbose {
		fmt.Printf("done (%d rows)\n", logsCount)
	}

	// Export metrics
	if e.verbose {
		fmt.Print("Exporting metrics... ")
	}
	metricsCount, err := e.exportToParquet(ctx, "otel_metrics", metricsPath, opts.FromDate, opts.ToDate, service)
	if err != nil {
		return nil, fmt.Errorf("exporting metrics: %w", err)
	}
	summary.MetricsCount = metricsCount
	summary.OutputFiles = append(summary.OutputFiles, metricsPath)
	if e.verbose {
		fmt.Printf("done (%d rows)\n", metricsCount)
	}

	// Create views database
	if e.verbose {
		fmt.Print("Creating views database... ")
	}
	viewsDBPath := e.generateViewsDBPath(opts)
	if err := e.createViewsDatabase(ctx, viewsDBPath); err != nil {
		return nil, fmt.Errorf("creating views database: %w", err)
	}
	summary.OutputFiles = append(summary.OutputFiles, viewsDBPath)
	if e.verbose {
		fmt.Println("done")
	}

	// Create ZIP archive if requested
	if opts.CreateZip {
		if e.verbose {
			fmt.Print("Creating ZIP archive... ")
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
			fmt.Println("done")
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

// PrintPreview prints the export preview to stdout
func PrintPreview(summary *Summary, opts Options) {
	fmt.Println()
	fmt.Println("Export Preview")
	fmt.Println("==============")

	if opts.FromFiles {
		fmt.Printf("Source: %s (from files)\n", opts.Source)
	} else {
		fmt.Printf("Source: %s (from database)\n", opts.Source)
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
		fmt.Printf("Time range: %s to %s\n", from, to)
	} else {
		fmt.Println("Time range: all")
	}

	fmt.Println()
	fmt.Println("Data to export:")
	fmt.Printf("  Traces:  %d spans\n", summary.TracesCount)
	fmt.Printf("  Logs:    %d records\n", summary.LogsCount)
	fmt.Printf("  Metrics: %d data points\n", summary.MetricsCount)

	fmt.Println()
	fmt.Printf("Output directory: %s\n", opts.OutputDir)
	fmt.Println("Files to create:")
	fmt.Println("  - traces.parquet")
	fmt.Println("  - logs.parquet")
	fmt.Println("  - metrics.parquet")
	fmt.Printf("  - ai-observer-export-%s-%s.duckdb\n", opts.Source, opts.DateRangeString())

	if opts.CreateZip {
		fmt.Printf("  - ai-observer-export-%s-%s.zip (all files combined)\n", opts.Source, opts.DateRangeString())
	}
}

// PrintResult prints the export result to stdout
func PrintResult(summary *Summary, opts Options) {
	fmt.Println()
	fmt.Println("Export complete!")
	fmt.Printf("Output: %s (%d files, %s)\n", opts.OutputDir, len(summary.OutputFiles), formatSize(summary.TotalSize))
}

// ConfirmExport prompts the user for confirmation
func ConfirmExport() bool {
	fmt.Println()
	fmt.Print("Continue? [y/N] ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
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
		fmt.Println("No data found to export.")
		return nil
	}

	// Print preview
	PrintPreview(summary, opts)

	// If dry-run, stop here
	if opts.DryRun {
		fmt.Println()
		fmt.Println("Dry run - no files created.")
		return nil
	}

	// Ask for confirmation unless --yes flag is set
	if !opts.SkipConfirm {
		if !ConfirmExport() {
			fmt.Println("Aborted.")
			return nil
		}
	}

	fmt.Println()

	// Execute export
	result, err := exporter.Export(ctx, opts)
	if err != nil {
		return fmt.Errorf("export failed: %w", err)
	}

	// Print result
	PrintResult(result, opts)

	return nil
}

// formatSize formats a byte count as human-readable string
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
