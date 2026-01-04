package importer

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/deleter"
	"github.com/tobilg/ai-observer/internal/storage"
)

// Importer orchestrates the import process
type Importer struct {
	store    *storage.DuckDBStore
	state    *StateManager
	parsers  map[SourceType]SessionParser
	verbose  bool
}

// NewImporter creates a new importer
func NewImporter(store *storage.DuckDBStore, verbose bool) *Importer {
	return &Importer{
		store:   store,
		state:   NewStateManager(store),
		parsers: make(map[SourceType]SessionParser),
		verbose: verbose,
	}
}

// RegisterParser registers a parser for a source type
func (i *Importer) RegisterParser(parser SessionParser) {
	i.parsers[parser.Source()] = parser
}

// RegisterAllParsers registers all available parsers
func (i *Importer) RegisterAllParsers() {
	i.RegisterParser(NewClaudeParser())
	i.RegisterParser(NewCodexParser())
	i.RegisterParser(NewGeminiParser())
}

// RegisterAllParsersWithOptions registers all parsers with import options
func (i *Importer) RegisterAllParsersWithOptions(opts Options) {
	claudeParser := NewClaudeParser()
	if opts.PricingMode != "" {
		claudeParser.SetPricingMode(opts.PricingMode)
	}
	i.RegisterParser(claudeParser)
	i.RegisterParser(NewCodexParser())
	i.RegisterParser(NewGeminiParser())
}

// Import performs the import for the specified sources
func (i *Importer) Import(ctx context.Context, sources []SourceType, opts Options) error {
	// Collect summaries for all sources
	var allSummaries []*ImportSummary
	var deleteSummary *deleter.Summary

	// First pass: scan and collect summaries
	for _, source := range sources {
		parser, ok := i.parsers[source]
		if !ok {
			fmt.Printf("Warning: No parser registered for %s, skipping\n", source)
			continue
		}

		summary, err := i.scanSource(ctx, parser, opts)
		if err != nil {
			fmt.Printf("Error scanning %s: %v\n", source, err)
			continue
		}

		allSummaries = append(allSummaries, summary)
	}

	// Check if there's anything to import
	totalNew := 0
	totalModified := 0
	for _, s := range allSummaries {
		totalNew += s.NewFiles
		totalModified += s.ModifiedFiles
	}

	if totalNew == 0 && totalModified == 0 {
		fmt.Println("No new or modified files found to import.")
		return nil
	}

	// If purge is requested, get delete counts
	if opts.Purge && opts.FromDate != nil && opts.ToDate != nil {
		var err error
		deleteSummary, err = deleter.Preview(ctx, i.store, deleter.Options{
			Scope:   deleter.ScopeAll,
			From:    *opts.FromDate,
			To:      *opts.ToDate,
			Service: "", // Delete all services in range
		})
		if err != nil {
			return fmt.Errorf("preview delete failed: %w", err)
		}
	}

	// Print summary
	printImportSummary(allSummaries, deleteSummary, opts)

	// If dry run, stop here
	if opts.DryRun {
		fmt.Println("\nDry run - no changes made.")
		return nil
	}

	// Ask for confirmation
	if !opts.SkipConfirm {
		if !confirmImport(opts.Purge) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Execute purge if requested
	if opts.Purge && deleteSummary != nil && !deleteSummary.IsEmpty() {
		fmt.Println("\nDeleting existing data...")
		_, err := deleter.Execute(ctx, i.store, deleter.Options{
			Scope:   deleter.ScopeAll,
			From:    *opts.FromDate,
			To:      *opts.ToDate,
			Service: "",
		})
		if err != nil {
			return fmt.Errorf("delete failed: %w", err)
		}
		fmt.Println("Deletion complete.")
	}

	// Execute import
	fmt.Println("\nImporting data...")
	for _, source := range sources {
		parser, ok := i.parsers[source]
		if !ok {
			continue
		}

		if err := i.importSource(ctx, parser, opts); err != nil {
			fmt.Printf("Error importing %s: %v\n", source, err)
			continue
		}
	}

	fmt.Println("\nImport complete.")
	return nil
}

// scanSource scans files for a single source
func (i *Importer) scanSource(ctx context.Context, parser SessionParser, opts Options) (*ImportSummary, error) {
	source := parser.Source()
	summary := &ImportSummary{Source: source}

	// Find all session files
	files, err := parser.FindSessionFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("finding session files: %w", err)
	}

	summary.TotalFiles = len(files)

	if i.verbose {
		fmt.Printf("Scanning %s: found %d files\n", source, len(files))
	}

	for _, filePath := range files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Check file status
		status, err := i.state.CheckFileStatus(ctx, source, filePath)
		if err != nil {
			summary.AddError(filePath, err)
			continue
		}

		// Skip if already imported and not forcing
		if !ShouldImportFile(status, opts.Force) {
			summary.SkippedFiles++
			continue
		}

		// Parse the file to get counts
		result, err := parser.ParseFile(ctx, filePath)
		if err != nil {
			summary.AddError(filePath, err)
			continue
		}

		// Filter by date range if specified
		if opts.FromDate != nil && result.LastTime.Before(*opts.FromDate) {
			summary.SkippedFiles++
			continue
		}
		if opts.ToDate != nil && result.FirstTime.After(*opts.ToDate) {
			summary.SkippedFiles++
			continue
		}

		summary.Add(result, StatusToString(status))
	}

	return summary, nil
}

// importSource imports files for a single source
func (i *Importer) importSource(ctx context.Context, parser SessionParser, opts Options) error {
	source := parser.Source()

	// Find all session files
	files, err := parser.FindSessionFiles(ctx)
	if err != nil {
		return fmt.Errorf("finding session files: %w", err)
	}

	imported := 0
	for _, filePath := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Check file status
		status, err := i.state.CheckFileStatus(ctx, source, filePath)
		if err != nil {
			if i.verbose {
				fmt.Printf("  Error checking %s: %v\n", filePath, err)
			}
			continue
		}

		// Skip if already imported and not forcing
		if !ShouldImportFile(status, opts.Force) {
			continue
		}

		// Parse the file
		result, err := parser.ParseFile(ctx, filePath)
		if err != nil {
			if i.verbose {
				fmt.Printf("  Error parsing %s: %v\n", filePath, err)
			}
			continue
		}

		// Filter by date range if specified - skip entire file if completely outside range
		if opts.FromDate != nil && result.LastTime.Before(*opts.FromDate) {
			continue
		}
		if opts.ToDate != nil && result.FirstTime.After(*opts.ToDate) {
			continue
		}

		// Filter individual records by date range (handles files spanning date boundaries)
		logs := filterLogsByDateRange(result.Logs, opts.FromDate, opts.ToDate)
		metrics := filterMetricsByDateRange(result.Metrics, opts.FromDate, opts.ToDate)
		spans := filterSpansByDateRange(result.Spans, opts.FromDate, opts.ToDate)

		// Skip if no records remain after filtering
		if len(logs) == 0 && len(metrics) == 0 && len(spans) == 0 {
			continue
		}

		// Store logs
		if len(logs) > 0 {
			if err := i.store.InsertLogs(ctx, logs); err != nil {
				if i.verbose {
					fmt.Printf("  Error inserting logs from %s: %v\n", filePath, err)
				}
				continue
			}
		}

		// Store metrics
		if len(metrics) > 0 {
			if err := i.store.InsertMetrics(ctx, metrics); err != nil {
				if i.verbose {
					fmt.Printf("  Error inserting metrics from %s: %v\n", filePath, err)
				}
				continue
			}
		}

		// Store spans
		if len(spans) > 0 {
			if err := i.store.InsertSpans(ctx, spans); err != nil {
				if i.verbose {
					fmt.Printf("  Error inserting spans from %s: %v\n", filePath, err)
				}
				continue
			}
		}

		// Record import state
		if err := i.state.RecordImport(ctx, source, filePath, result.RecordCount); err != nil {
			if i.verbose {
				fmt.Printf("  Error recording import state for %s: %v\n", filePath, err)
			}
		}

		imported++
		if i.verbose {
			fmt.Printf("  [%s] %s: %d logs, %d metrics\n", source, result.SessionID, len(result.Logs), len(result.Metrics))
		}
	}

	fmt.Printf("[%s] Imported %d files\n", source, imported)
	return nil
}

// printImportSummary prints the import summary
func printImportSummary(summaries []*ImportSummary, deleteSummary *deleter.Summary, opts Options) {
	fmt.Println("\nImport Summary")
	fmt.Println("==============")

	if opts.FromDate != nil || opts.ToDate != nil {
		from := "beginning"
		to := "now"
		if opts.FromDate != nil {
			from = opts.FromDate.Format("2006-01-02")
		}
		if opts.ToDate != nil {
			to = opts.ToDate.Format("2006-01-02")
		}
		fmt.Printf("Time range: %s to %s\n", from, to)
	}

	// Print delete summary if purging
	if opts.Purge && deleteSummary != nil {
		fmt.Println("\nData to DELETE (existing):")
		fmt.Printf("  Logs:    %d\n", deleteSummary.LogCount)
		fmt.Printf("  Metrics: %d\n", deleteSummary.MetricCount)
		fmt.Printf("  Traces:  %d (spans: %d)\n", deleteSummary.TraceCount, deleteSummary.SpanCount)
	}

	// Print import summary per source
	fmt.Println("\nData to IMPORT (from files):")
	totalLogs := 0
	totalMetrics := 0
	totalSpans := 0
	totalNew := 0
	totalModified := 0
	totalSkipped := 0

	for _, s := range summaries {
		fmt.Printf("\n  [%s]\n", s.Source)
		fmt.Printf("    Files: %d total (%d new, %d modified, %d skipped)\n",
			s.TotalFiles, s.NewFiles, s.ModifiedFiles, s.SkippedFiles)
		fmt.Printf("    Logs:    %d\n", s.TotalLogs)
		fmt.Printf("    Metrics: %d\n", s.TotalMetrics)
		if s.TotalSpans > 0 {
			fmt.Printf("    Spans:   %d\n", s.TotalSpans)
		}

		totalLogs += s.TotalLogs
		totalMetrics += s.TotalMetrics
		totalSpans += s.TotalSpans
		totalNew += s.NewFiles
		totalModified += s.ModifiedFiles
		totalSkipped += s.SkippedFiles

		if len(s.Errors) > 0 {
			fmt.Printf("    Errors: %d\n", len(s.Errors))
		}
	}

	fmt.Println("\n  Total:")
	fmt.Printf("    Files: %d new, %d modified\n", totalNew, totalModified)
	fmt.Printf("    Logs:    %d\n", totalLogs)
	fmt.Printf("    Metrics: %d\n", totalMetrics)
	if totalSpans > 0 {
		fmt.Printf("    Spans:   %d\n", totalSpans)
	}
}

// confirmImport prompts the user for confirmation
func confirmImport(withPurge bool) bool {
	fmt.Println()
	if withPurge {
		fmt.Println("This will DELETE existing data in the time range and import new data.")
	}
	fmt.Print("Continue? [y/N] ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// GetParser returns the parser for a source type
func (i *Importer) GetParser(source SourceType) (SessionParser, bool) {
	parser, ok := i.parsers[source]
	return parser, ok
}

// ParseToolArg parses the tool argument and returns the source types
func ParseToolArg(toolArg string) ([]SourceType, error) {
	if toolArg == "all" {
		return AllSources(), nil
	}

	source, ok := ParseSourceType(toolArg)
	if !ok {
		return nil, fmt.Errorf("invalid tool: %s (valid: claude-code, codex, gemini, all)", toolArg)
	}

	return []SourceType{source}, nil
}

// ParseDateArg parses a date argument in YYYY-MM-DD format
func ParseDateArg(dateStr string) (*time.Time, error) {
	if dateStr == "" {
		return nil, nil
	}

	t, err := time.Parse("2006-01-02", dateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid date format: %s (expected YYYY-MM-DD)", dateStr)
	}

	return &t, nil
}

// ParseToDateArg parses a to-date argument and sets it to end of day
func ParseToDateArg(dateStr string) (*time.Time, error) {
	t, err := ParseDateArg(dateStr)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, nil
	}

	// Set to end of day
	endOfDay := t.Add(24*time.Hour - time.Nanosecond)
	return &endOfDay, nil
}

// filterLogsByDateRange filters log records to only include those within the date range
func filterLogsByDateRange(logs []api.LogRecord, from, to *time.Time) []api.LogRecord {
	if from == nil && to == nil {
		return logs
	}

	filtered := make([]api.LogRecord, 0, len(logs))
	for _, log := range logs {
		if from != nil && log.Timestamp.Before(*from) {
			continue
		}
		if to != nil && log.Timestamp.After(*to) {
			continue
		}
		filtered = append(filtered, log)
	}
	return filtered
}

// filterMetricsByDateRange filters metric records to only include those within the date range
func filterMetricsByDateRange(metrics []api.MetricDataPoint, from, to *time.Time) []api.MetricDataPoint {
	if from == nil && to == nil {
		return metrics
	}

	filtered := make([]api.MetricDataPoint, 0, len(metrics))
	for _, m := range metrics {
		if from != nil && m.Timestamp.Before(*from) {
			continue
		}
		if to != nil && m.Timestamp.After(*to) {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

// filterSpansByDateRange filters span records to only include those within the date range
func filterSpansByDateRange(spans []api.Span, from, to *time.Time) []api.Span {
	if from == nil && to == nil {
		return spans
	}

	filtered := make([]api.Span, 0, len(spans))
	for _, s := range spans {
		if from != nil && s.Timestamp.Before(*from) {
			continue
		}
		if to != nil && s.Timestamp.After(*to) {
			continue
		}
		filtered = append(filtered, s)
	}
	return filtered
}
