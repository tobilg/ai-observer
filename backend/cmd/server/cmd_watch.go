package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/server"
	"github.com/tobilg/ai-observer/internal/tools"
	"github.com/tobilg/ai-observer/internal/watcher"
)

func cmdWatch(args []string) {
	ctx, stop := shutdownSignals()
	defer stop()
	if err := runWatch(ctx, args); err != nil && !errors.Is(err, flag.ErrHelp) {
		fmt.Printf("Error: %v\n", err)
		stop()
		os.Exit(1)
	}
}

type watchArgs struct {
	ToolArg  string
	Tools    []string
	Backfill bool
	MCP      bool
	// Server overrides the environment's host, API port and database
	Server serverFlags
}

// runWatch watches session files and serves the API until ctx is cancelled
func runWatch(ctx context.Context, args []string) error {
	parsed, err := parseWatchArgs(args)
	if err != nil {
		return err
	}

	// Initialize logging
	initLogging()
	log := logger.Logger()

	cfg := config.Load()
	cfg.MCPEnabled = cfg.MCPEnabled || parsed.MCP
	parsed.Server.apply(cfg)

	srv, err := server.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Start file watcher; it scans existing files (and backfills) in the
	// background while the API server serves
	watchCfg := watcher.Config{
		Tools:    parsed.Tools,
		Backfill: parsed.Backfill,
	}
	if err := srv.StartWatcher(watchCfg); err != nil {
		srv.Shutdown(context.Background())
		return fmt.Errorf("failed to start watcher: %w", err)
	}

	log.Info("AI Observer starting (watch mode)",
		"database", cfg.DatabasePath,
		"host", cfg.Host,
		"api_port", cfg.APIPort,
		"tools", parsed.ToolArg,
	)

	// Start only the API server (no OTLP)
	if err := serveUntilDone(ctx, srv, srv.ListenAndServeAPIOnly); err != nil {
		return err
	}
	log.Info("Shutdown complete")
	return nil
}

func parseWatchArgs(args []string) (*watchArgs, error) {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)

	var backfill, mcp bool
	fs.BoolVar(&backfill, "backfill", false, backfillHelp)
	fs.BoolVar(&mcp, "mcp", false, mcpHelp)
	var server serverFlags
	server.register(fs, false)

	fs.Usage = func() {
		fmt.Printf(`Read local session files as they change and serve the dashboard, without OTLP
(serve --watch also receives OTLP)

Usage: ai-observer watch [%s|all] [options]

`, toolList(true))
		printArguments(os.Stdout, toolArguments(true, func(p tools.Provider) string { return "Watch " + p.DisplayName + " session files" }, "Watch the files of all of them (default)"))
		fmt.Println("Options:")
		printFlags(fs)
	}

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return nil, err
	}

	if fs.NArg() > 1 {
		return nil, fmt.Errorf("unexpected argument: %s", fs.Arg(1))
	}
	toolArg, tools, err := parseWatchTool(fs.Arg(0))
	if err != nil {
		return nil, err
	}

	return &watchArgs{
		ToolArg:  toolArg,
		Tools:    tools,
		Backfill: backfill,
		MCP:      mcp,
		Server:   server,
	}, nil
}

// parseWatchTool reads the tool argument of watch and serve --watch: a tool
// with session files, or all (also when arg is empty). It returns the
// argument as given and the tools to watch, none for all.
func parseWatchTool(arg string) (toolArg string, tools []string, err error) {
	if arg == "" || arg == "all" {
		return "all", nil, nil
	}
	if _, ok := importer.ParseSourceType(arg); !ok {
		return "", nil, fmt.Errorf("invalid tool: %s (valid: claude-code, codex, gemini, all)", arg)
	}
	return arg, []string{arg}, nil
}
