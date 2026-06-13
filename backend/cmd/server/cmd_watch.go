package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/importer"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/server"
	"github.com/tobilg/ai-observer/internal/watcher"
)

func cmdWatch(args []string) {
	if err := runWatch(args); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

type watchArgs struct {
	ToolArg  string
	Tools    []string
	Backfill bool
}

func runWatch(args []string) error {
	parsed, err := parseWatchArgs(args)
	if err != nil {
		return err
	}

	// Initialize logging
	logLevel := parseLogLevel(os.Getenv("AI_OBSERVER_LOG_LEVEL"))
	logger.InitializeText(logLevel)
	log := logger.Logger()

	cfg := config.Load()

	srv, err := server.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Start file watcher
	watchCfg := watcher.Config{
		Tools:    parsed.Tools,
		Backfill: parsed.Backfill,
	}
	if err := srv.StartWatcher(watchCfg); err != nil {
		return fmt.Errorf("failed to start watcher: %w", err)
	}

	// Graceful shutdown on SIGINT/SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info("Received shutdown signal")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("Error during shutdown", "error", err)
		}
		os.Exit(0)
	}()

	log.Info("AI Observer starting (watch mode)",
		"database", cfg.DatabasePath,
		"api_port", cfg.APIPort,
		"tools", parsed.ToolArg,
	)

	// Start only the API server (no OTLP)
	return srv.ListenAndServeAPIOnly()
}

func parseWatchArgs(args []string) (*watchArgs, error) {
	fs := flag.NewFlagSet("watch", flag.ContinueOnError)

	var backfill bool
	fs.BoolVar(&backfill, "backfill", false, "Import all existing data on first start")

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

	if err := fs.Parse(reorderArgs(args)); err != nil {
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
	}, nil
}
