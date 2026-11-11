package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/deleter"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/tools"
)

func cmdDelete(args []string) {
	exitOnError(runDelete(args))
}

// DeleteFlags holds the parsed flags for the delete command
type DeleteFlags struct {
	From    string
	To      string
	Service string
	Yes     bool
	Scope   string
	// DB is the database file (--db), overriding AI_OBSERVER_DATABASE_PATH
	DB string
}

// parseDeleteFlags parses command line arguments into DeleteFlags
func parseDeleteFlags(args []string) (*DeleteFlags, error) {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)

	flags := &DeleteFlags{}
	registerDateFlags(fs, &flags.From, &flags.To, "delete", true)
	fs.Var(valueFlag{&flags.Service, "tool"}, "service", "Only the telemetry of this tool or service ("+strings.ReplaceAll(toolList(false), "|", ", ")+")")
	registerYesFlag(fs, &flags.Yes)
	registerDBFlag(fs, &flags.DB)

	fs.Usage = func() {
		fmt.Print(`Delete telemetry in a time range

Usage: ai-observer delete [logs|metrics|traces|all] --from <date> --to <date> [options]

`)
		printArguments(os.Stdout, []argument{
			{"logs", "Delete log records"},
			{"metrics", "Delete metric points"},
			{"traces", "Delete spans"},
			{"all", "Delete all three"},
		})
		fmt.Println("Options:")
		printFlags(fs)
	}

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return nil, err
	}

	flags.Scope = fs.Arg(0)
	return flags, nil
}

func runDelete(args []string) error {
	flags, err := parseDeleteFlags(args)
	if err != nil {
		return err
	}

	if flags.Scope == "" {
		return fmt.Errorf("scope argument is required\nUsage: ai-observer delete [logs|metrics|traces|all] --from <date> --to <date>")
	}

	if flags.From == "" || flags.To == "" {
		return fmt.Errorf("--from and --to are required\nUsage: ai-observer delete [logs|metrics|traces|all] --from <date> --to <date>")
	}

	// Parse scope
	scope, err := deleter.ParseScope(flags.Scope)
	if err != nil {
		return err
	}

	// Parse dates (days in the local time zone, like import and export)
	from, err := importer.ParseDateArg(flags.From)
	if err != nil {
		return fmt.Errorf("invalid --from date: %v", err)
	}
	to, err := importer.ParseToDateArg(flags.To)
	if err != nil {
		return fmt.Errorf("invalid --to date: %v", err)
	}
	fromTime, toTime := *from, *to

	// Validate date range
	if fromTime.After(toTime) {
		return fmt.Errorf("--from date must be before --to date")
	}

	// A tool name selects all of the tool's services (codex: codex_cli_rs,
	// codex-app-server, codex-exec-server), a service name only that one
	serviceName := flags.Service
	if serviceName != "" {
		filter := tools.ResolveFilter(serviceName)
		if filter == "" {
			return fmt.Errorf("unknown tool/service: %s\nSupported tools: claude-code, codex, gemini, opencode, copilot-chat, github-copilot (or one of their service names)", serviceName)
		}
		serviceName = filter
	}

	// Load config and initialize store
	initLogging()
	cfg := config.Load()
	if flags.DB != "" {
		cfg.DatabasePath = flags.DB
	}
	store, err := storage.NewDuckDBStoreWithOptions(cfg.DatabasePath, cfg.StorageOptions())
	if err != nil {
		body := map[string]any{"from": fromTime.Format(time.RFC3339), "to": toTime.Format(time.RFC3339), "scope": string(scope), "dryRun": true}
		if serviceName != "" {
			body["service"] = serviceName
		}
		if storage.IsLocked(err) {
			return lockedError(err, cfg, "delete", body, `The dry run returns the counts; send "dryRun": false to delete.`)
		}
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer store.Close()

	// Run the delete operation
	opts := deleter.Options{
		Scope:       scope,
		From:        fromTime,
		To:          toTime,
		Service:     serviceName,
		SkipConfirm: flags.Yes,
	}

	ctx := context.Background()
	return deleter.Run(ctx, store, opts)
}
