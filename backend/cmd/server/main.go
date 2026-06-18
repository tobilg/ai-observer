package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/server"
	"github.com/tobilg/ai-observer/internal/version"
)

func main() {
	// No arguments: start server (default behavior)
	if len(os.Args) < 2 {
		runServer()
		return
	}

	// Dispatch to subcommand
	switch os.Args[1] {
	case "import":
		cmdImport(os.Args[2:])
	case "export":
		cmdExport(os.Args[2:])
	case "delete":
		cmdDelete(os.Args[2:])
	case "setup":
		cmdSetup(os.Args[2:])
	case "watch":
		cmdWatch(os.Args[2:])
	case "serve":
		runServer()
	case "-v", "--version", "version":
		printVersion()
	case "-h", "--help", "help":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", os.Args[1])
		printHelp()
		os.Exit(1)
	}
}

func printVersion() {
	fmt.Printf("AI Observer %s\n", version.Version)
	fmt.Printf("Git Commit: %s\n", version.GitCommit)
	fmt.Printf("Build Date: %s\n", version.BuildDate)
}

func printHelp() {
	fmt.Print(`AI Observer - OpenTelemetry-compatible observability backend for AI coding tools

Usage: ai-observer [command] [options]

Commands:
  import    Import local sessions from AI tool files
  export    Export telemetry data to Parquet files
  delete    Delete telemetry data from database
  setup     Show setup instructions for AI tools
  watch     Watch local session files and import incrementally
  serve     Start the OTLP server (default if no command)

Options:
  -h, --help       Show this help message
  -v, --version    Show version information

Use "ai-observer [command] --help" for command-specific options.

Environment Variables:
  AI_OBSERVER_API_PORT       API server port (default: 8080)
  AI_OBSERVER_OTLP_PORT      OTLP ingestion port (default: 4318)
  AI_OBSERVER_DATABASE_PATH  DuckDB database path (default: ./data/ai-observer.duckdb)
  AI_OBSERVER_FRONTEND_URL   Frontend URL for CORS (default: http://localhost:5173)
  AI_OBSERVER_LOG_LEVEL      Log level: DEBUG, INFO, WARN, ERROR (default: INFO)
  AI_OBSERVER_CLAUDE_PATH    Custom Claude Code config directory
  AI_OBSERVER_CODEX_PATH     Custom Codex CLI home directory
  AI_OBSERVER_GEMINI_PATH    Custom Gemini CLI home directory
  COPILOT_OTEL_ENABLED       Enable GitHub Copilot OTLP export
  COPILOT_OTEL_CAPTURE_CONTENT Capture GitHub Copilot prompts and tool content
  OPENCODE_ENABLE_TELEMETRY  Enable OpenCode OTEL plugin export
  OPENCODE_OTLP_ENDPOINT     OpenCode OTLP endpoint (use http://localhost:4318)
  OPENCODE_OTLP_PROTOCOL     OpenCode OTLP protocol (use http/protobuf)
`)
}

func runServer() {
	// Initialize structured logging (text format for development readability)
	logLevel := parseLogLevel(os.Getenv("AI_OBSERVER_LOG_LEVEL"))
	logger.InitializeText(logLevel)
	log := logger.Logger()

	cfg := config.Load()

	srv, err := server.New(cfg)
	if err != nil {
		log.Error("Failed to create server", "error", err)
		os.Exit(1)
	}

	// Graceful shutdown on SIGINT/SIGTERM
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Info("Received shutdown signal")

		// Use a context with timeout for graceful shutdown
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("Error during shutdown", "error", err)
		}
		os.Exit(0)
	}()

	log.Info("AI Observer starting",
		"database", cfg.DatabasePath,
		"api_port", cfg.APIPort,
		"otlp_port", cfg.OTLPPort,
	)

	if err := srv.ListenAndServe(); err != nil {
		log.Error("Server error", "error", err)
		os.Exit(1)
	}
}

func parseLogLevel(level string) slog.Level {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// reorderArgs moves flags before positional arguments so Go's flag package parses them correctly.
// Example: ["all", "--output", "./export"] -> ["--output", "./export", "all"]
func reorderArgs(args []string) []string {
	var flags []string
	var positional []string

	i := 0
	for i < len(args) {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			// Check if next arg is a flag value (not another flag)
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				// Could be a flag value, include it with the flag
				flags = append(flags, args[i+1])
				i += 2
				continue
			}
			i++
		} else {
			positional = append(positional, arg)
			i++
		}
	}

	return append(flags, positional...)
}
