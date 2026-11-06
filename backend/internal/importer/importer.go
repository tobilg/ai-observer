package importer

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/deleter"
	"github.com/tobilg/ai-observer/internal/storage"
)

// Importer orchestrates the import process
type Importer struct {
	store   *storage.DuckDBStore
	state   *StateManager
	parsers map[SourceType]SessionParser
	verbose bool
	out     io.Writer // progress and summaries
	in      io.Reader // confirmation answers
}

// NewImporter creates a new importer that reports to stdout and asks for
// confirmation on stdin
func NewImporter(store *storage.DuckDBStore, verbose bool) *Importer {
	return &Importer{
		store:   store,
		state:   NewStateManager(store),
		parsers: make(map[SourceType]SessionParser),
		verbose: verbose,
		out:     os.Stdout,
		in:      os.Stdin,
	}
}

// SetIO sets where the importer reports progress and reads confirmations
func (i *Importer) SetIO(out io.Writer, in io.Reader) {
	i.out = out
	i.in = in
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

// Import performs the import for the specified sources.
//
// The preview (counts and date ranges) requires parsing every file, and the
// files are parsed again when importing so only one file's records are held in
// memory at a time. The preview is only needed for the confirmation prompt, a
// dry run, or deriving the --purge date range; otherwise each file is parsed once.
func (i *Importer) Import(ctx context.Context, sources []SourceType, opts Options) error {
	if opts.SkipConfirm && !opts.DryRun && !opts.Purge {
		return i.importOnce(ctx, sources, opts)
	}

	// Collect summaries for all sources
	var allSummaries []*ImportSummary
	var deleteSummary *deleter.Summary

	// With --purge and an explicit range, unchanged files in the range are
	// rebuilt too
	scanOpts := opts
	if opts.Purge && (opts.FromDate != nil || opts.ToDate != nil) {
		scanOpts.Force = true
	}

	// First pass: scan and collect summaries
	failedSources := 0
	for _, source := range sources {
		parser, ok := i.parsers[source]
		if !ok {
			fmt.Fprintf(i.out, "Warning: No parser registered for %s, skipping\n", source)
			continue
		}

		summary, err := i.scanSource(ctx, parser, scanOpts)
		if err != nil {
			fmt.Fprintf(i.out, "Error scanning %s: %v\n", source, err)
			failedSources++
			continue
		}

		allSummaries = append(allSummaries, summary)
	}

	// Check if there's anything to import
	totalNew := 0
	totalModified := 0
	unparsable := 0 // files the preview could not parse
	for _, s := range allSummaries {
		totalNew += s.NewFiles
		totalModified += s.ModifiedFiles
		unparsable += len(s.Errors)
	}

	if totalNew == 0 && totalModified == 0 {
		fmt.Fprintln(i.out, "No new or modified files found to import.")
		if opts.Purge && !opts.Force {
			fmt.Fprintln(i.out, "To rebuild unchanged files, pass --from/--to or --force with --purge.")
		}
		for _, s := range allSummaries {
			i.printImportErrors(s.Errors, "  ")
		}
		return importFailure(failedSources, unparsable)
	}

	// With --purge, each imported tool's data in its range is deleted and then
	// rebuilt from all of its files in that range, including unchanged ones
	var purges map[SourceType]purgeRange
	if opts.Purge {
		purges = purgeRanges(allSummaries, opts)
		var err error
		deleteSummary, err = i.previewPurges(ctx, purges)
		if err != nil {
			return fmt.Errorf("preview delete failed: %w", err)
		}
	}

	// Print summary
	i.printImportSummary(allSummaries, deleteSummary, purges, opts)

	// If dry run, stop here
	if opts.DryRun {
		fmt.Fprintln(i.out, "\nDry run - no changes made.")
		return importFailure(failedSources, unparsable)
	}

	// Ask for confirmation
	if !opts.SkipConfirm {
		if !i.confirmImport(opts.Purge) {
			fmt.Fprintln(i.out, "Aborted.")
			return nil
		}
	}

	// Import. A purged tool's range is deleted and rebuilt in one
	// transaction, so if any of its files fails, nothing of it changes.
	fmt.Fprintln(i.out, "\nImporting data...")
	failedFiles := 0
	for _, source := range sources {
		parser, ok := i.parsers[source]
		if !ok {
			continue
		}

		p, purge := purges[source]
		if !purge {
			stats, err := i.importSource(ctx, parser, opts, i.store)
			failedFiles += stats.failed
			if err != nil {
				fmt.Fprintf(i.out, "Error importing %s: %v\n", source, err)
				failedSources++
			}
			continue
		}

		var stats importStats
		err := i.store.InImportTx(ctx, func(tx *storage.ImportTx) error {
			deleted, err := tx.DeleteAllInRange(ctx, p.from, p.to, source.ServiceName())
			if err != nil {
				return fmt.Errorf("delete failed: %w", err)
			}
			fmt.Fprintf(i.out, "[%s] Deleted %d logs, %d metrics, %d spans\n", source, deleted.LogCount, deleted.MetricCount, deleted.SpanCount)
			if stats, err = i.importSource(ctx, parser, p.importOptions(opts), tx); err != nil {
				return err
			}
			if stats.failed > 0 {
				return errPurgeFailed
			}
			return nil
		})
		failedFiles += stats.failed
		if err != nil {
			fmt.Fprintf(i.out, "[%s] Nothing was changed: %v\n", source, err)
			failedSources++
		}
	}

	fmt.Fprintln(i.out, "\nImport complete.")
	return importFailure(failedSources, failedFiles)
}

// errPurgeFailed rolls back a purge when some of the files rebuilding its
// range could not be imported, so their data is not lost
var errPurgeFailed = errors.New("some files could not be imported, so the purge was rolled back")

// fileWriter stores the records of one imported file: the store, or the
// transaction of a purge
type fileWriter interface {
	InsertImportedFile(ctx context.Context, logs []api.LogRecord, metrics []api.MetricDataPoint, spans []api.Span,
		state *storage.ImportState, replace *storage.FileRecords) error
}

// droppedRecords reports whether result has records before from or after to
func droppedRecords(result *ImportResult, from, to *time.Time) (before, after bool) {
	check := func(ts time.Time) {
		before = before || (from != nil && ts.Before(*from))
		after = after || (to != nil && ts.After(*to))
	}
	for _, l := range result.Logs {
		check(l.Timestamp)
	}
	for _, m := range result.Metrics {
		check(m.Timestamp)
	}
	for _, sp := range result.Spans {
		check(sp.Timestamp)
	}
	return before, after
}

// ErrIncomplete is returned when some tools or files could not be imported.
// Everything else was imported, and the failures have been printed.
var ErrIncomplete = errors.New("import incomplete")

// importFailure returns the error that makes an incomplete import exit with
// a non-zero status, or nil when everything was imported
func importFailure(failedSources, failedFiles int) error {
	switch {
	case failedSources > 0 && failedFiles > 0:
		return fmt.Errorf("%w: %d tools and %d files failed (see above)", ErrIncomplete, failedSources, failedFiles)
	case failedSources > 0:
		return fmt.Errorf("%w: %d tools failed (see above)", ErrIncomplete, failedSources)
	case failedFiles > 0:
		return fmt.Errorf("%w: %d files could not be imported (see above)", ErrIncomplete, failedFiles)
	default:
		return nil
	}
}

// purgeRange is the time range of one tool's data that --purge replaces
type purgeRange struct {
	from, to time.Time
}

// purgeRanges returns the range to purge for each source: --from and --to,
// with a missing bound derived from the files that will be imported. Sources
// without a complete range are not purged.
func purgeRanges(summaries []*ImportSummary, opts Options) map[SourceType]purgeRange {
	ranges := make(map[SourceType]purgeRange)
	for _, s := range summaries {
		var from, to time.Time
		if opts.FromDate != nil {
			from = *opts.FromDate
		}
		if opts.ToDate != nil {
			to = *opts.ToDate
		}
		for _, file := range s.Files {
			if file.FirstTime.IsZero() || file.LastTime.IsZero() {
				continue
			}
			if opts.FromDate == nil && (from.IsZero() || file.FirstTime.Before(from)) {
				from = file.FirstTime
			}
			if opts.ToDate == nil && (to.IsZero() || file.LastTime.After(to)) {
				to = file.LastTime
			}
		}
		if !from.IsZero() && !to.IsZero() {
			ranges[s.Source] = purgeRange{from: from, to: to}
		}
	}
	return ranges
}

// deleteOptions deletes the source's own service data in the range, never
// data of other tools
func (p purgeRange) deleteOptions(source SourceType) deleter.Options {
	return deleter.Options{
		Scope:   deleter.ScopeAll,
		From:    p.from,
		To:      p.to,
		Service: source.ServiceName(),
	}
}

// importOptions returns the options that rebuild the purged range: every file
// is imported (unchanged ones too, since their records were deleted), limited
// to records in the range so data outside it is not duplicated
func (p purgeRange) importOptions(opts Options) Options {
	if p.from.IsZero() {
		return opts
	}
	from, to := p.from, p.to
	opts.Force = true
	opts.FromDate = &from
	opts.ToDate = &to
	return opts
}

// previewPurges returns the number of records the purges would delete
func (i *Importer) previewPurges(ctx context.Context, purges map[SourceType]purgeRange) (*deleter.Summary, error) {
	total := &deleter.Summary{}
	for source, p := range purges {
		s, err := deleter.Preview(ctx, i.store, p.deleteOptions(source))
		if err != nil {
			return nil, err
		}
		total.LogCount += s.LogCount
		total.MetricCount += s.MetricCount
		total.TraceCount += s.TraceCount
		total.SpanCount += s.SpanCount
	}
	return total, nil
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
		fmt.Fprintf(i.out, "Scanning %s: found %d files\n", source, len(files))
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

		// Determine status string for counting
		// When force-importing, count StatusCurrent files as "modified" instead of "skipped"
		// so they are properly counted and the import doesn't exit early
		statusStr := StatusToString(status)
		if status == StatusCurrent && opts.Force {
			statusStr = "modified"
		}
		summary.Add(result, statusStr)
	}

	return summary, nil
}

// importStats counts what an import stored
type importStats struct {
	files, failed        int
	logs, metrics, spans int
}

func (s *importStats) add(o importStats) {
	s.files += o.files
	s.failed += o.failed
	s.logs += o.logs
	s.metrics += o.metrics
	s.spans += o.spans
}

// importOnce imports without a preview: each file is parsed once, stored, and
// the totals are reported at the end. Used when there is nothing to confirm.
func (i *Importer) importOnce(ctx context.Context, sources []SourceType, opts Options) error {
	fmt.Fprintln(i.out, "Importing data...")

	var total importStats
	failedSources := 0
	for _, source := range sources {
		parser, ok := i.parsers[source]
		if !ok {
			fmt.Fprintf(i.out, "Warning: No parser registered for %s, skipping\n", source)
			continue
		}

		stats, err := i.importSource(ctx, parser, opts, i.store)
		total.add(stats)
		if err != nil {
			fmt.Fprintf(i.out, "Error importing %s: %v\n", source, err)
			failedSources++
		}
	}

	if total.files == 0 && total.failed == 0 {
		fmt.Fprintln(i.out, "No new or modified files found to import.")
		return importFailure(failedSources, 0)
	}

	fmt.Fprintf(i.out, "\nImport complete: %d files, %d logs, %d metrics, %d spans\n", total.files, total.logs, total.metrics, total.spans)
	return importFailure(failedSources, total.failed)
}

// importSource imports files for a single source, storing them with dst
func (i *Importer) importSource(ctx context.Context, parser SessionParser, opts Options, dst fileWriter) (importStats, error) {
	source := parser.Source()
	var stats importStats

	// Find all session files
	files, err := parser.FindSessionFiles(ctx)
	if err != nil {
		return stats, fmt.Errorf("finding session files: %w", err)
	}

	var failed []ImportError
	fail := func(filePath string, err error) {
		failed = append(failed, ImportError{FilePath: filePath, Error: err.Error()})
	}

	for _, filePath := range files {
		if ctx.Err() != nil {
			return stats, ctx.Err()
		}

		// Check file status
		status, prev, err := i.state.fileStatus(ctx, source, filePath)
		if err != nil {
			fail(filePath, fmt.Errorf("checking import state: %w", err))
			continue
		}

		// Skip if already imported and not forcing
		if !ShouldImportFile(status, opts.Force) {
			continue
		}

		// Size and modification time before parsing: if the file changes
		// while it is parsed, they no longer match and the next import
		// hashes it
		info, err := os.Stat(filePath)
		if err != nil {
			fail(filePath, err)
			continue
		}

		// Parse the file
		result, err := parser.ParseFile(ctx, filePath)
		if err != nil {
			fail(filePath, fmt.Errorf("parsing: %w", err))
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

		// Record the hash of the parsed content, not of the file as it is now
		state, err := i.state.NewImportState(source, filePath, result.FileHash, result.RecordCount)
		if err != nil {
			fail(filePath, err)
			continue
		}
		// Watch mode continues where this import stopped reading
		state.ByteOffset = result.ParsedBytes
		state.MessageCount = result.MessageCount
		state.FileSize = info.Size()
		state.FileModTimeNs = info.ModTime().UnixNano()

		// A date range that left out records of the file makes the state
		// partial, so the next import reads the file again. Watch only reads
		// forward, so for records after the range it starts over too.
		before, after := droppedRecords(result, opts.FromDate, opts.ToDate)
		state.Partial = before || after
		if after {
			state.ByteOffset = 0
			state.MessageCount = 0
		}
		// Every stored record of the file now carries its path, unless
		// records of an earlier version remain outside the range
		state.SourceTracked = prev == nil || prev.SourceTracked || !state.Partial

		// A file imported before is replaced: its records in the range are
		// deleted in the same transaction, including those an earlier
		// version stored without the file's path
		var replace *storage.FileRecords
		if prev != nil {
			replace = &storage.FileRecords{From: opts.FromDate, To: opts.ToDate, Legacy: !prev.SourceTracked}
		}

		// Store all records and the import state in one transaction
		if err := dst.InsertImportedFile(ctx, logs, metrics, spans, state, replace); err != nil {
			fail(filePath, fmt.Errorf("storing: %w", err))
			continue
		}

		stats.files++
		stats.logs += len(logs)
		stats.metrics += len(metrics)
		stats.spans += len(spans)
		if i.verbose {
			fmt.Fprintf(i.out, "  [%s] %s: %d logs, %d metrics\n", source, result.SessionID, len(result.Logs), len(result.Metrics))
		}
	}

	stats.failed = len(failed)
	if len(failed) > 0 {
		fmt.Fprintf(i.out, "[%s] Imported %d files, %d failed:\n", source, stats.files, len(failed))
		i.printImportErrors(failed, "  ")
	} else {
		fmt.Fprintf(i.out, "[%s] Imported %d files\n", source, stats.files)
	}
	return stats, nil
}

// printImportErrors lists files that could not be imported, with the reason
func (i *Importer) printImportErrors(errs []ImportError, indent string) {
	for _, e := range errs {
		fmt.Fprintf(i.out, "%s- %s: %s\n", indent, e.FilePath, e.Error)
	}
}

// printImportSummary prints the import summary
func (i *Importer) printImportSummary(summaries []*ImportSummary, deleteSummary *deleter.Summary, purges map[SourceType]purgeRange, opts Options) {
	fmt.Fprintln(i.out, "\nImport Summary")
	fmt.Fprintln(i.out, "==============")

	if opts.FromDate != nil || opts.ToDate != nil {
		from := "beginning"
		to := "now"
		if opts.FromDate != nil {
			from = opts.FromDate.Format("2006-01-02")
		}
		if opts.ToDate != nil {
			to = opts.ToDate.Format("2006-01-02")
		}
		fmt.Fprintf(i.out, "Time range: %s to %s\n", from, to)
	}

	// Print delete summary if purging
	if opts.Purge && deleteSummary != nil {
		fmt.Fprintln(i.out, "\nData to DELETE (existing):")
		for _, s := range summaries {
			if p, ok := purges[s.Source]; ok {
				fmt.Fprintf(i.out, "  [%s] %s to %s\n", s.Source.ServiceName(),
					p.from.Format(time.RFC3339), p.to.Format(time.RFC3339))
			}
		}
		fmt.Fprintf(i.out, "  Logs:    %d\n", deleteSummary.LogCount)
		fmt.Fprintf(i.out, "  Metrics: %d\n", deleteSummary.MetricCount)
		fmt.Fprintf(i.out, "  Traces:  %d (spans: %d)\n", deleteSummary.TraceCount, deleteSummary.SpanCount)
	}

	// Print import summary per source
	fmt.Fprintln(i.out, "\nData to IMPORT (from files):")
	totalLogs := 0
	totalMetrics := 0
	totalSpans := 0
	totalNew := 0
	totalModified := 0
	totalSkipped := 0

	for _, s := range summaries {
		fmt.Fprintf(i.out, "\n  [%s]\n", s.Source)
		fmt.Fprintf(i.out, "    Files: %d total (%d new, %d modified, %d skipped)\n",
			s.TotalFiles, s.NewFiles, s.ModifiedFiles, s.SkippedFiles)
		fmt.Fprintf(i.out, "    Logs:    %d\n", s.TotalLogs)
		fmt.Fprintf(i.out, "    Metrics: %d\n", s.TotalMetrics)
		if s.TotalSpans > 0 {
			fmt.Fprintf(i.out, "    Spans:   %d\n", s.TotalSpans)
		}

		totalLogs += s.TotalLogs
		totalMetrics += s.TotalMetrics
		totalSpans += s.TotalSpans
		totalNew += s.NewFiles
		totalModified += s.ModifiedFiles
		totalSkipped += s.SkippedFiles

		if len(s.Errors) > 0 {
			fmt.Fprintf(i.out, "    Errors: %d (these files will not be imported)\n", len(s.Errors))
			i.printImportErrors(s.Errors, "      ")
		}
	}

	fmt.Fprintln(i.out, "\n  Total:")
	fmt.Fprintf(i.out, "    Files: %d new, %d modified\n", totalNew, totalModified)
	fmt.Fprintf(i.out, "    Logs:    %d\n", totalLogs)
	fmt.Fprintf(i.out, "    Metrics: %d\n", totalMetrics)
	if totalSpans > 0 {
		fmt.Fprintf(i.out, "    Spans:   %d\n", totalSpans)
	}
}

// confirmImport prompts the user for confirmation
func (i *Importer) confirmImport(withPurge bool) bool {
	fmt.Fprintln(i.out)
	if withPurge {
		fmt.Fprintln(i.out, "This will DELETE the existing data of the imported tools in the time range")
		fmt.Fprintln(i.out, "and re-import it from all of their files in that range.")
	}
	fmt.Fprint(i.out, "Continue? [y/N] ")

	reader := bufio.NewReader(i.in)
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

	// Dates are days in the local time zone, like the session times shown
	t, err := time.ParseInLocation("2006-01-02", dateStr, time.Local)
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

	// Set to end of day (AddDate keeps days with a DST change whole)
	endOfDay := t.AddDate(0, 0, 1).Add(-time.Nanosecond)
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
