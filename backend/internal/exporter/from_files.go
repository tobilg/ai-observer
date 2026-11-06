package exporter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/tools"
)

// runFromFiles handles the --from-files mode by processing raw JSON/JSONL files.
// It imports the files into a temporary DuckDB database, then exports to
// Parquet. The database is a file, not in memory, so DuckDB can keep large
// histories on disk.
func runFromFiles(ctx context.Context, opts Options) error {
	// Convert our source type to importer source types
	var sources []importer.SourceType
	if opts.Source == SourceAll {
		sources = importer.AllSources()
	} else {
		provider, ok := tools.Lookup(string(opts.Source))
		if !ok {
			return fmt.Errorf("invalid source: %s", opts.Source)
		}
		if !provider.SessionFiles {
			return fmt.Errorf("from-files export is not supported for %s because %s is OTLP-only", opts.Source, provider.DisplayName)
		}
		sources = []importer.SourceType{importer.SourceType(provider.Tool)}
	}

	// Create the temporary database
	tempDir, err := os.MkdirTemp("", "ai-observer-export-*")
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)
	tempStore, err := storage.NewDuckDBStore(filepath.Join(tempDir, "import.duckdb"))
	if err != nil {
		return fmt.Errorf("creating temp database: %w", err)
	}
	defer tempStore.Close()

	// Create importer and register parsers
	imp := importer.NewImporter(tempStore, opts.Verbose)
	imp.SetIO(opts.out(), opts.in())
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
		fmt.Fprintln(opts.out(), "Reading source files...")
	}

	// Import files into temp database. Files that cannot be read are listed
	// and left out; the others are still exported.
	if err := imp.Import(ctx, sources, importOpts); err != nil {
		if !errors.Is(err, importer.ErrIncomplete) {
			return fmt.Errorf("importing from files: %w", err)
		}
		fmt.Fprintf(opts.out(), "Warning: %v\n", err)
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
		fmt.Fprintln(opts.out(), "No data found in source files.")
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
