package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/exporter"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

func cmdExport(args []string) {
	if err := runExport(args); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

// ExportFlags holds the parsed flags for the export command
type ExportFlags struct {
	Output    string
	From      string
	To        string
	FromFiles bool
	Zip       bool
	DryRun    bool
	Verbose   bool
	Yes       bool
	Source    string
}

// parseExportFlags parses command line arguments into ExportFlags
func parseExportFlags(args []string) (*ExportFlags, error) {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)

	flags := &ExportFlags{}
	fs.StringVar(&flags.Output, "output", "", "Output directory (required)")
	fs.StringVar(&flags.From, "from", "", "Start date filter (YYYY-MM-DD)")
	fs.StringVar(&flags.To, "to", "", "End date filter (YYYY-MM-DD)")
	fs.BoolVar(&flags.FromFiles, "from-files", false, "Read from raw files instead of database")
	fs.BoolVar(&flags.Zip, "zip", false, "Create ZIP archive of exported files")
	fs.BoolVar(&flags.DryRun, "dry-run", false, "Preview what would be exported")
	fs.BoolVar(&flags.Verbose, "verbose", false, "Show detailed progress")
	fs.BoolVar(&flags.Yes, "yes", false, "Skip confirmation prompts")

	fs.Usage = func() {
		fmt.Print(`Export telemetry data to Parquet files

Usage: ai-observer export [claude-code|codex|gemini|all] --output <directory> [options]

Arguments:
  claude-code  Export Claude Code data
  codex        Export Codex CLI data
  gemini       Export Gemini CLI data
  all          Export all data

Options:
`)
		printFlags(fs)
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	flags.Source = fs.Arg(0)
	return flags, nil
}

func runExport(args []string) error {
	flags, err := parseExportFlags(args)
	if err != nil {
		return err
	}

	if flags.Source == "" {
		return fmt.Errorf("source argument is required\nUsage: ai-observer export [claude-code|codex|gemini|all] --output <directory>")
	}

	if flags.Output == "" {
		return fmt.Errorf("--output is required for export\nUsage: ai-observer export <source> --output <directory>")
	}

	// Parse source
	source, err := exporter.ParseSourceArg(flags.Source)
	if err != nil {
		return err
	}

	// Parse optional dates
	fromDate, err := importer.ParseDateArg(flags.From)
	if err != nil {
		return err
	}

	toDate, err := importer.ParseToDateArg(flags.To)
	if err != nil {
		return err
	}

	// Validate date range if both specified
	if fromDate != nil && toDate != nil && fromDate.After(*toDate) {
		return fmt.Errorf("--from date must be before --to date")
	}

	// Build options
	opts := exporter.Options{
		Source:      source,
		OutputDir:   flags.Output,
		FromDate:    fromDate,
		ToDate:      toDate,
		FromFiles:   flags.FromFiles,
		CreateZip:   flags.Zip,
		DryRun:      flags.DryRun,
		SkipConfirm: flags.Yes,
		Verbose:     flags.Verbose,
	}

	ctx := context.Background()

	// For from-files mode, we don't need the main database
	if flags.FromFiles {
		return exporter.Run(ctx, nil, opts)
	}

	// Load config and initialize store for database export
	cfg := config.Load()
	store, err := storage.NewDuckDBStore(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer store.Close()

	// Run export
	return exporter.Run(ctx, store, opts)
}
