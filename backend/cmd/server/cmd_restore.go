package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/restore"
	"github.com/tobilg/ai-observer/internal/server"
	"github.com/tobilg/ai-observer/internal/storage"
)

func cmdRestore(args []string) {
	if err := runRestore(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

// RestoreFlags holds the parsed flags of the restore command
type RestoreFlags struct {
	Path    string
	Replace bool
	DryRun  bool
	Yes     bool
	DB      string
	// MapPaths rewrite session file path prefixes (--map-path OLD=NEW)
	MapPaths []storage.PathMapping
}

// pathMappingsFlag collects the --map-path flags
type pathMappingsFlag struct {
	mappings *[]storage.PathMapping
}

func (f pathMappingsFlag) String() string {
	if f.mappings == nil {
		return ""
	}
	parts := make([]string, len(*f.mappings))
	for i, m := range *f.mappings {
		parts[i] = m.From + "=" + m.To
	}
	return strings.Join(parts, ",")
}

func (f pathMappingsFlag) Set(s string) error {
	m, err := storage.ParsePathMapping(s)
	if err != nil {
		return err
	}
	*f.mappings = append(*f.mappings, m)
	return nil
}

func (f pathMappingsFlag) TypeName() string { return "OLD=NEW" }

func parseRestoreFlags(args []string) (*RestoreFlags, error) {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags := &RestoreFlags{}
	fs.BoolVar(&flags.Replace, "replace", false, "Delete the stored records of the export's services in its time range first")
	fs.Var(pathMappingsFlag{&flags.MapPaths}, "map-path",
		"Replace the session file path prefix OLD with NEW, such as /Users/old=/Users/new (repeatable; the first match applies)")
	registerDryRunFlag(fs, &flags.DryRun, "what the export holds and what it would replace")
	registerYesFlag(fs, &flags.Yes)
	registerDBFlag(fs, &flags.DB)
	fs.Usage = func() {
		fmt.Print(`Load an export (ai-observer export) back into the database

Restores the spans, logs and metric points of an export directory or its ZIP
archive, with the metric baselines and dashboards of exports that have them,
in one transaction. Session IDs are computed for exports of earlier versions
that lack them, and text is redacted again (AI_OBSERVER_REDACT). Spans that
are stored already are skipped; logs and metric points have no identity, so a
restore into a database that holds records of the export's services in its
time range is refused, unless --replace deletes those records first.

Records keep the path of the session file they were imported from, and the
import state of those files is restored: importing or watching the same files
afterwards skips unchanged files and replaces the records of grown ones, so
nothing is stored twice. A file the database had imported with another state
is read again by the next import or watch. On another machine, or with
another home directory, --map-path OLD=NEW rewrites the paths. Exports of
earlier versions (without import_state.parquet) carry no paths: rebuild their
sessions with ai-observer import <tool> --purge instead of importing them
again.

To restore into a new database, set --db (or AI_OBSERVER_DATABASE_PATH) to a
new file. AI Observer must not be running on the database.

Usage: ai-observer restore <export directory or .zip> [options]

Options:
`)
		printFlags(fs)
	}
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return nil, err
	}
	if fs.NArg() != 1 {
		return nil, fmt.Errorf("the export to restore is required\nUsage: ai-observer restore <export directory or .zip>")
	}
	flags.Path = fs.Arg(0)
	return flags, nil
}

func runRestore(args []string) error {
	flags, err := parseRestoreFlags(args)
	if err != nil {
		return err
	}
	if _, err := os.Stat(flags.Path); err != nil {
		return fmt.Errorf("reading export: %w", err)
	}
	initLogging()
	cfg := config.Load()
	if flags.DB != "" {
		cfg.DatabasePath = flags.DB
	}
	store, err := storage.NewDuckDBStoreWithOptions(cfg.DatabasePath, cfg.StorageOptions())
	if err != nil {
		if storage.IsLocked(err) {
			return fmt.Errorf("the database %s is in use by another process, such as a running AI Observer server or watch: "+
				"stop it before restoring, or restore into another database with --db <new file>", cfg.DatabasePath)
		}
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer store.Close()
	// Restored text is redacted like ingested text
	store.SetRedactor(server.NewRedactor(cfg.Redaction))

	ctx, stop := shutdownSignals()
	defer stop()
	err = restore.Run(ctx, store, restore.Options{
		Path:        flags.Path,
		Replace:     flags.Replace,
		PathMap:     flags.MapPaths,
		DryRun:      flags.DryRun,
		SkipConfirm: flags.Yes,
	})
	if errors.Is(err, restore.ErrConflict) {
		return fmt.Errorf("%w: nothing was restored", err)
	}
	return err
}
