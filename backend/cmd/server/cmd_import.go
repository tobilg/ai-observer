package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
)

func cmdImport(args []string) {
	if err := runImport(args); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

// ImportFlags holds the parsed flags for the import command
type ImportFlags struct {
	From    string
	To      string
	DryRun  bool
	Force   bool
	Verbose bool
	Purge   bool
	Yes     bool
	Tool    string
}

// parseImportFlags parses command line arguments into ImportFlags
func parseImportFlags(args []string) (*ImportFlags, error) {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)

	flags := &ImportFlags{}
	fs.StringVar(&flags.From, "from", "", "Only import sessions from DATE (YYYY-MM-DD)")
	fs.StringVar(&flags.To, "to", "", "Only import sessions up to DATE (YYYY-MM-DD)")
	fs.BoolVar(&flags.DryRun, "dry-run", false, "Show what would be imported without storing")
	fs.BoolVar(&flags.Force, "force", false, "Re-import already-imported sessions")
	fs.BoolVar(&flags.Verbose, "verbose", false, "Show detailed progress")
	fs.BoolVar(&flags.Purge, "purge", false, "Delete existing data in time range before import")
	fs.BoolVar(&flags.Yes, "yes", false, "Skip confirmation prompts")

	fs.Usage = func() {
		fmt.Print(`Import local sessions from AI tool files

Usage: ai-observer import [claude-code|codex|gemini|all] [options]

Arguments:
  claude-code  Import from Claude Code (~/.claude/projects/**/*.jsonl)
  codex        Import from Codex CLI (~/.codex/sessions/*.jsonl)
  gemini       Import from Gemini CLI (~/.gemini/tmp/**/session-*.json)
  all          Import from all tools

Options:
`)
		printFlags(fs)
	}

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	flags.Tool = fs.Arg(0)
	return flags, nil
}

func runImport(args []string) error {
	flags, err := parseImportFlags(args)
	if err != nil {
		return err
	}

	if flags.Tool == "" {
		return fmt.Errorf("tool argument is required\nUsage: ai-observer import [claude-code|codex|gemini|all] [options]")
	}

	// Parse tool/source
	sources, err := importer.ParseToolArg(flags.Tool)
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

	// Validate purge requires date range
	if flags.Purge && (fromDate == nil || toDate == nil) {
		return fmt.Errorf("--purge requires both --from and --to dates\nUsage: ai-observer import <tool> --purge --from YYYY-MM-DD --to YYYY-MM-DD")
	}

	// Validate date range if both specified
	if fromDate != nil && toDate != nil && fromDate.After(*toDate) {
		return fmt.Errorf("--from date must be before --to date")
	}

	// Load config and initialize store
	cfg := config.Load()
	store, err := storage.NewDuckDBStore(cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer store.Close()

	// Create importer and register parsers
	imp := importer.NewImporter(store, flags.Verbose)
	imp.RegisterAllParsers()

	// Build options
	opts := importer.Options{
		DryRun:      flags.DryRun,
		Force:       flags.Force,
		FromDate:    fromDate,
		ToDate:      toDate,
		Purge:       flags.Purge,
		SkipConfirm: flags.Yes,
	}

	// Run import
	ctx := context.Background()
	return imp.Import(ctx, sources, opts)
}
