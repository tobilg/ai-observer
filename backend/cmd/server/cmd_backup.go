package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/maintenance"
)

func cmdBackup(args []string) {
	if err := runBackup(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

// BackupFlags holds the parsed flags of the backup command
type BackupFlags struct {
	Output string
	DB     string
}

func parseBackupFlags(args []string) (*BackupFlags, error) {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	flags := &BackupFlags{}
	fs.Var(valueFlag{&flags.Output, "file"}, "output", "File to write the backup to, a new DuckDB database (required)")
	registerDBFlag(fs, &flags.DB)
	fs.Usage = func() {
		fmt.Print(`Write a consistent copy of the database to a new file

The backup is a DuckDB database without free space; its tables, row counts,
indexes and schema version are checked against the database. To restore it,
stop AI Observer and copy it over the database file (or point
AI_OBSERVER_DATABASE_PATH at it).

While a server runs, which has the database open, its admin API writes backups
instead (POST /api/admin/backup with AI_OBSERVER_ADMIN_API=true).

Usage: ai-observer backup --output <file> [options]

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
	if flags.Output == "" {
		return nil, fmt.Errorf("--output is required\nUsage: ai-observer backup --output <file>")
	}
	return flags, nil
}

func runBackup(args []string) error {
	flags, err := parseBackupFlags(args)
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
	_, err = maintenance.Backup(ctx, maintenance.BackupOptions{
		DatabasePath: cfg.DatabasePath,
		Storage:      cfg.StorageOptions(),
		Output:       flags.Output,
	})
	body := map[string]any{}
	if name := filepath.Base(flags.Output); name != "." && name != string(filepath.Separator) {
		body["name"] = name
	}
	return lockedError(err, cfg, "backup", body,
		"The server writes the backup to its backup directory (AI_OBSERVER_BACKUP_DIR, default: backups next to the database).")
}
