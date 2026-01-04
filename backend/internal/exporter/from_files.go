package exporter

import (
	"context"
	"fmt"

	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

// runFromFiles handles the --from-files mode by processing raw JSON/JSONL files
// It creates a temporary in-memory DuckDB, imports the files, then exports to Parquet
func runFromFiles(ctx context.Context, opts Options) error {
	// Convert our source type to importer source types
	var sources []importer.SourceType
	switch opts.Source {
	case SourceClaude:
		sources = []importer.SourceType{importer.SourceClaude}
	case SourceCodex:
		sources = []importer.SourceType{importer.SourceCodex}
	case SourceGemini:
		sources = []importer.SourceType{importer.SourceGemini}
	case SourceAll:
		sources = importer.AllSources()
	default:
		return fmt.Errorf("invalid source: %s", opts.Source)
	}

	// Create temporary in-memory DuckDB
	tempStore, err := storage.NewDuckDBStore(":memory:")
	if err != nil {
		return fmt.Errorf("creating temp database: %w", err)
	}
	defer tempStore.Close()

	// Create importer and register parsers
	imp := importer.NewImporter(tempStore, opts.Verbose)
	imp.RegisterAllParsers()

	// Configure import options
	importOpts := importer.Options{
		DryRun:      false, // We need to actually import to the temp DB
		Force:       true,  // Always import since temp DB is empty
		FromDate:    opts.FromDate,
		ToDate:      opts.ToDate,
		Purge:       false,
		SkipConfirm: true, // Skip confirmation for temp import
		Verbose:     opts.Verbose,
	}

	if opts.Verbose {
		fmt.Println("Reading source files...")
	}

	// Import files into temp database
	if err := imp.Import(ctx, sources, importOpts); err != nil {
		return fmt.Errorf("importing from files: %w", err)
	}

	// Now export from temp database
	exporter := NewExporter(tempStore, opts.Verbose)

	// Preview
	summary, err := exporter.Preview(ctx, opts)
	if err != nil {
		return fmt.Errorf("preview failed: %w", err)
	}

	// Check if there's anything to export
	if summary.IsEmpty() {
		fmt.Println("No data found in source files.")
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
