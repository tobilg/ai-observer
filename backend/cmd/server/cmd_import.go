package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/pricing"
	"github.com/tobilg/ai-observer/internal/server"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/tools"
)

func cmdImport(args []string) {
	exitOnError(runImport(args))
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
	// PricingMode is the Claude Code cost mode: auto, calculate or display
	PricingMode string
	Tool        string
	// DB is the database file (--db), overriding AI_OBSERVER_DATABASE_PATH
	DB string
}

// parseImportFlags parses command line arguments into ImportFlags
func parseImportFlags(args []string) (*ImportFlags, error) {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)

	flags := &ImportFlags{}
	registerDateFlags(fs, &flags.From, &flags.To, "import", false)
	registerDryRunFlag(fs, &flags.DryRun, "what would be imported")
	fs.BoolVar(&flags.Force, "force", false, "Import files again also when they are unchanged")
	fs.BoolVar(&flags.Verbose, "verbose", false, "Show detailed progress")
	fs.BoolVar(&flags.Purge, "purge", false, "Delete the tools' data in the time range first, then import all of their files in it")
	registerYesFlag(fs, &flags.Yes)
	fs.StringVar(&flags.PricingMode, "pricing-mode", "auto", "Claude Code cost: auto (recorded costUSD, else calculated), calculate or display")
	registerDBFlag(fs, &flags.DB)

	fs.Usage = func() {
		fmt.Printf(`Import local session files once (watch reads them as they change)

Usage: ai-observer import [%s|all] [options]

`, toolList(true))
		printArguments(os.Stdout, toolArguments(true, func(p tools.Provider) string { return "Import " + p.DisplayName + " session files" }, "Import the files of all of them"))
		fmt.Println("Options:")
		printFlags(fs)
	}

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
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

	// Validate date range if both specified
	if fromDate != nil && toDate != nil && fromDate.After(*toDate) {
		return fmt.Errorf("--from date must be before --to date")
	}

	pricingMode, err := pricing.ParsePricingMode(flags.PricingMode)
	if err != nil {
		return err
	}

	// Load config and initialize store
	initLogging()
	cfg := config.Load()
	if flags.DB != "" {
		cfg.DatabasePath = flags.DB
	}
	store, err := storage.NewDuckDBStoreWithOptions(cfg.DatabasePath, cfg.StorageOptions())
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer store.Close()
	store.SetRedactor(server.NewRedactor(cfg.Redaction))

	// Create importer and register parsers
	// Build options
	opts := importer.Options{
		DryRun:      flags.DryRun,
		Force:       flags.Force,
		FromDate:    fromDate,
		ToDate:      toDate,
		Purge:       flags.Purge,
		SkipConfirm: flags.Yes,
		PricingMode: pricingMode,
	}

	imp := importer.NewImporter(store, flags.Verbose)
	imp.RegisterAllParsersWithOptions(opts)

	// Run import
	ctx := context.Background()
	return imp.Import(ctx, sources, opts)
}
