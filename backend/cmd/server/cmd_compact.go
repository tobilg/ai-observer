package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/maintenance"
)

func cmdCompact(args []string) {
	if err := runCompact(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

// CompactFlags holds the parsed flags of the compact command
type CompactFlags struct {
	DryRun       bool
	Yes          bool
	KeepOriginal bool
	DB           string
}

func parseCompactFlags(args []string) (*CompactFlags, error) {
	fs := flag.NewFlagSet("compact", flag.ContinueOnError)
	flags := &CompactFlags{}
	registerDryRunFlag(fs, &flags.DryRun, "the size and the space compaction would return")
	registerYesFlag(fs, &flags.Yes)
	fs.BoolVar(&flags.KeepOriginal, "keep-original", false, "Keep the database as it was at <database>.pre-compact")
	registerDBFlag(fs, &flags.DB)
	fs.Usage = func() {
		fmt.Print(`Rewrite the database into a new file without its free space

Deleting telemetry (delete, AI_OBSERVER_RETENTION_DAYS, AI_OBSERVER_MAX_DB_SIZE)
frees space inside the database file, which new data reuses, but the file keeps
its size. compact copies the database into a new file, checks the copy (tables,
row counts, indexes, schema version) and replaces the database with it; the
original is kept until the copy has been checked.

AI Observer must not be running: the database can only be opened by one process.
The copy needs free disk space of about the size of the data.

Usage: ai-observer compact [options]

Options:
`)
		printFlags(fs)
	}
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}
	return flags, nil
}

func runCompact(args []string) error {
	flags, err := parseCompactFlags(args)
	if err != nil {
		return err
	}
	initLogging()
	cfg := config.Load()
	if flags.DB != "" {
		cfg.DatabasePath = flags.DB
	}
	ctx, stop := shutdownSignals()
	defer stop()
	_, err = maintenance.Compact(ctx, maintenance.CompactOptions{
		DatabasePath: cfg.DatabasePath,
		Storage:      cfg.StorageOptions(),
		KeepOriginal: flags.KeepOriginal,
		DryRun:       flags.DryRun,
		SkipConfirm:  flags.Yes,
	})
	if errors.Is(err, maintenance.ErrLocked) {
		return fmt.Errorf("the database %s is in use by another process, such as a running AI Observer server or watch.\n"+
			"Compaction replaces the database file, which cannot be done while it is open: stop AI Observer and run\n"+
			"ai-observer compact again. A server can free the deleted space for reuse with POST /api/admin/checkpoint\n"+
			"(AI_OBSERVER_ADMIN_API=true), but not shrink the file", cfg.DatabasePath)
	}
	return err
}
