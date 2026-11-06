package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/server"
	"github.com/tobilg/ai-observer/internal/watcher"
)

func cmdWatch(args []string) {
	ctx, stop := shutdownSignals()
	defer stop()
	if err := runWatch(ctx, args); err != nil {
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
}

// runWatch watches session files and serves the API until ctx is cancelled
func runWatch(ctx context.Context, args []string) error {
	parsed, err := parseWatchArgs(args)
	if err != nil {
		return err
	}

	// Initialize logging
	logLevel := parseLogLevel(os.Getenv("AI_OBSERVER_LOG_LEVEL"))
	logger.InitializeText(logLevel)
	log := logger.Logger()

	cfg := config.Load()
	cfg.MCPEnabled = cfg.MCPEnabled || parsed.MCP

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
	fs.BoolVar(&backfill, "backfill", false, "Import all existing data on first start")
	fs.BoolVar(&mcp, "mcp", false, "Serve the MCP endpoint (/mcp on the API port) for agents; also AI_OBSERVER_MCP=true")

	fs.Usage = func() {
		fmt.Print(`Watch local AI tool session files for changes and import incrementally

Usage: ai-observer watch [all|claude-code|codex|gemini] [options]

Arguments:
  claude-code  Watch Claude Code session files
  codex        Watch Codex CLI session files
  gemini       Watch Gemini CLI session files
  all          Watch all tools (default if no argument)

Options:
`)
		printFlags(fs)
	}

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return nil, err
	}

	toolArg := fs.Arg(0)
	if toolArg == "" {
		toolArg = "all"
	}

	// Validate tool argument
	var tools []string
	if toolArg == "all" {
		// Empty slice means all tools
	} else {
		if _, ok := importer.ParseSourceType(toolArg); !ok {
			return nil, fmt.Errorf("invalid tool: %s (valid: claude-code, codex, gemini, all)", toolArg)
		}
		tools = []string{toolArg}
	}

	return &watchArgs{
		ToolArg:  toolArg,
		Tools:    tools,
		Backfill: backfill,
		MCP:      mcp,
	}, nil
}
