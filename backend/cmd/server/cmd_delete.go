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
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/tools"
)

func cmdDelete(args []string) {
	if err := runDelete(args); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

// DeleteFlags holds the parsed flags for the delete command
type DeleteFlags struct {
	From    string
	To      string
	Service string
	Yes     bool
	Scope   string
}

// parseDeleteFlags parses command line arguments into DeleteFlags
func parseDeleteFlags(args []string) (*DeleteFlags, error) {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)

	flags := &DeleteFlags{}
	fs.StringVar(&flags.From, "from", "", "Start date (YYYY-MM-DD, required)")
	fs.StringVar(&flags.To, "to", "", "End date (YYYY-MM-DD, required)")
	fs.StringVar(&flags.Service, "service", "", "Filter by tool (claude-code, codex, gemini)")
	fs.BoolVar(&flags.Yes, "yes", false, "Skip confirmation prompts")

	fs.Usage = func() {
		fmt.Print(`Delete telemetry data from the database

Usage: ai-observer delete [logs|metrics|traces|all] --from DATE --to DATE [options]

Arguments:
  logs      Delete only log records
  metrics   Delete only metric data points
  traces    Delete only trace spans
  all       Delete all telemetry data

Options:
`)
		printFlags(fs)
	}

	// Extract positional argument (scope) from anywhere in args
	// Go's flag package stops parsing at first non-flag argument,
	// so we need to separate scope from flags before parsing
	var flagArgs []string
	skipNext := false
	for i, arg := range args {
		if skipNext {
			skipNext = false
			flagArgs = append(flagArgs, arg)
			continue
		}
		if isFlag(arg) {
			flagArgs = append(flagArgs, arg)
			// Check if this flag takes a value (not a boolean flag)
			// Skip the next arg if it's a value flag
			flagName := strings.TrimLeft(arg, "-")
			if flagName == "from" || flagName == "to" || flagName == "service" {
				skipNext = true
			}
			continue
		}
		// Non-flag argument - check if it's a valid scope
		if flags.Scope == "" && isValidScope(arg) {
			flags.Scope = arg
		} else {
			// Unknown positional arg, could be a flag value we missed
			// Check if previous arg was a flag
			if i > 0 && isFlag(args[i-1]) {
				flagArgs = append(flagArgs, arg)
			}
		}
	}

	if err := fs.Parse(flagArgs); err != nil {
		return nil, err
	}

	return flags, nil
}

func runDelete(args []string) error {
	flags, err := parseDeleteFlags(args)
	if err != nil {
		return err
	}

	if flags.Scope == "" {
		return fmt.Errorf("scope argument is required\nUsage: ai-observer delete [logs|metrics|traces|all] --from DATE --to DATE")
	}

	if flags.From == "" || flags.To == "" {
		return fmt.Errorf("--from and --to are required for delete operations\nUsage: ai-observer delete <scope> --from YYYY-MM-DD --to YYYY-MM-DD")
	}

	// Parse scope
	scope, err := deleter.ParseScope(flags.Scope)
	if err != nil {
		return err
	}

	// Parse dates
	fromTime, err := time.Parse("2006-01-02", flags.From)
	if err != nil {
		return fmt.Errorf("invalid --from date format: %v\nExpected format: YYYY-MM-DD", err)
	}

	toTime, err := time.Parse("2006-01-02", flags.To)
	if err != nil {
		return fmt.Errorf("invalid --to date format: %v\nExpected format: YYYY-MM-DD", err)
	}

	// Set to end of day for the 'to' date
	toTime = toTime.Add(24*time.Hour - time.Nanosecond)

	// Validate date range
	if fromTime.After(toTime) {
		return fmt.Errorf("--from date must be before --to date")
	}

	// Normalize service name (accept short names like "claude" or full names like "claude-code")
	serviceName := flags.Service
	if serviceName != "" {
		normalized := tools.NormalizeServiceName(serviceName)
		if normalized == "" {
			return fmt.Errorf("unknown tool/service: %s\nSupported tools: claude-code, codex, gemini", serviceName)
		}
		serviceName = normalized
	}

	// Load config and initialize store
	cfg := config.Load()
	store, err := storage.NewDuckDBStore(cfg.DatabasePath)
	if err != nil {
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

// isFlag returns true if the argument looks like a flag (starts with -)
func isFlag(arg string) bool {
	return strings.HasPrefix(arg, "-")
}

// isValidScope returns true if the argument is a valid delete scope
func isValidScope(arg string) bool {
	switch arg {
	case "logs", "metrics", "traces", "all":
		return true
	default:
		return false
	}
}
