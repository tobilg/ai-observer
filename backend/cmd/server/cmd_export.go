package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/exporter"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/server"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/tools"
)

func cmdExport(args []string) {
	exitOnError(runExport(args))
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
	// DB is the database file (--db), overriding AI_OBSERVER_DATABASE_PATH
	DB string
}

// parseExportFlags parses command line arguments into ExportFlags
func parseExportFlags(args []string) (*ExportFlags, error) {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)

	flags := &ExportFlags{}
	fs.Var(valueFlag{&flags.Output, "directory"}, "output", "Directory to write the export to (required)")
	registerDateFlags(fs, &flags.From, &flags.To, "export", false)
	fs.BoolVar(&flags.FromFiles, "from-files", false, "Read the tools' session files instead of the database")
	fs.BoolVar(&flags.Zip, "zip", false, "Also write a ZIP archive of the exported files")
	registerDryRunFlag(fs, &flags.DryRun, "what would be exported")
	fs.BoolVar(&flags.Verbose, "verbose", false, "Show detailed progress")
	registerYesFlag(fs, &flags.Yes)
	registerDBFlag(fs, &flags.DB)

	fs.Usage = func() {
		fmt.Printf(`Export telemetry to Parquet files (restore loads them back)

Records keep the path of the session file they were imported from, and the
import state of those files is exported too, so importing or watching the same
files after a restore stores nothing twice. The export holds these local paths.

Usage: ai-observer export [%s|all] --output <directory> [options]

`, toolList(false))
		printArguments(os.Stdout, toolArguments(false, func(p tools.Provider) string { return "Export the telemetry of " + p.DisplayName }, "Export all telemetry"))
		fmt.Println("Options:")
		printFlags(fs)
	}

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
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
		return fmt.Errorf("source argument is required\nUsage: ai-observer export [claude-code|codex|gemini|opencode|copilot-chat|github-copilot|all] --output <directory>")
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

	initLogging()
	cfg := config.Load()

	// For from-files mode, we don't need the main database. The session
	// files are read into a temporary database, redacted as an import is.
	if flags.FromFiles {
		opts.Redactor = server.NewRedactor(cfg.Redaction)
		return exporter.Run(ctx, nil, opts)
	}

	// Initialize store for database export
	if flags.DB != "" {
		cfg.DatabasePath = flags.DB
	}
	store, err := storage.NewDuckDBStoreWithOptions(cfg.DatabasePath, cfg.StorageOptions())
	if err != nil {
		if storage.IsLocked(err) {
			body := map[string]any{"source": string(source)}
			if fromDate != nil {
				body["from"] = fromDate.Format(time.RFC3339)
			}
			if toDate != nil {
				body["to"] = toDate.Format(time.RFC3339)
			}
			if flags.Zip {
				body["zip"] = true
			}
			return lockedError(err, cfg, "export", body,
				"The server writes the export to a new directory in its backup directory (AI_OBSERVER_BACKUP_DIR,\n"+
					"default: backups next to the database) instead of --output.")
		}
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer store.Close()

	// Run export
	return exporter.Run(ctx, store, opts)
}
