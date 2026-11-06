package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/pricing"
	"github.com/tobilg/ai-observer/internal/server"
	"github.com/tobilg/ai-observer/internal/version"
)

func main() {
	// pricing writes an override file; loading the overrides first would
	// apply the prices it is about to replace
	if len(os.Args) > 1 && os.Args[1] == "pricing" {
		cmdPricing(os.Args[2:])
		return
	}

	if err := loadPricingOverrides(os.Getenv("AI_OBSERVER_PRICING_DIR")); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// No arguments: start server (default behavior)
	if len(os.Args) < 2 {
		cmdServe(nil)
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
		cmdServe(os.Args[2:])
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

// loadPricingOverrides merges the pricing files in dir, if set, over the
// embedded pricing. A broken override file is an error rather than silently
// falling back to the embedded prices.
func loadPricingOverrides(dir string) error {
	if dir == "" {
		return nil
	}
	loaded, err := pricing.LoadOverrides(dir)
	if err != nil {
		return fmt.Errorf("loading AI_OBSERVER_PRICING_DIR: %w", err)
	}
	for _, path := range loaded {
		fmt.Fprintf(os.Stderr, "Using pricing override %s\n", path)
	}
	return nil
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
  pricing   Update model prices from models.dev
  serve     Start the OTLP server (default if no command)

Options:
  -h, --help       Show this help message
  -v, --version    Show version information

Use "ai-observer [command] --help" for command-specific options.

Environment Variables:
  AI_OBSERVER_HOST           Interface to listen on (default: 127.0.0.1; 0.0.0.0 for all)
  AI_OBSERVER_API_PORT       API server port (default: 8080)
  AI_OBSERVER_OTLP_PORT      OTLP ingestion port (default: 4318)
  AI_OBSERVER_DATABASE_PATH  DuckDB database path (default: ./data/ai-observer.duckdb)
  AI_OBSERVER_FRONTEND_URL   Frontend URL for CORS (default: http://localhost:5173)
  AI_OBSERVER_ALLOWED_HOSTS  Extra host names accepted by the API (comma-separated, * = any)
  AI_OBSERVER_LOG_LEVEL      Log level: DEBUG, INFO, WARN, ERROR (default: INFO)
  AI_OBSERVER_PRICING_DIR    Directory with pricing files that override the built-in prices
  AI_OBSERVER_RETENTION_DAYS Delete telemetry older than this many days (default: keep all)
  AI_OBSERVER_REDACT         Redact stored telemetry: secrets (default), all (secrets and personal data), off
  AI_OBSERVER_MCP            Serve the MCP endpoint for agents at /mcp on the API port (true/false, default false)
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

func cmdServe(args []string) {
	ctx, stop := shutdownSignals()
	defer stop()
	if err := runServer(ctx, args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		logger.Error("Server error", "error", err)
		stop()
		os.Exit(1)
	}
}

// parseServeArgs reads the options of serve
func parseServeArgs(args []string) (mcp bool, err error) {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.BoolVar(&mcp, "mcp", false, "Serve the MCP endpoint (/mcp on the API port) for agents; also AI_OBSERVER_MCP=true")
	fs.Usage = func() {
		fmt.Print(`Start the OTLP server and the dashboard API

Usage: ai-observer serve [options]

Options:
`)
		printFlags(fs)
	}
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return false, err
	}
	if fs.NArg() > 0 {
		return false, fmt.Errorf("unexpected argument: %s", fs.Arg(0))
	}
	return mcp, nil
}

// shutdownSignals returns a context that is cancelled on SIGINT/SIGTERM, for
// a graceful shutdown. Commands install it before they open the database, so
// an interrupt during startup still closes it.
func shutdownSignals() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

// runServer serves OTLP and the API until ctx is cancelled, then shuts down
func runServer(ctx context.Context, args []string) error {
	mcp, err := parseServeArgs(args)
	if err != nil {
		return err
	}

	// Initialize structured logging (text format for development readability)
	logLevel := parseLogLevel(os.Getenv("AI_OBSERVER_LOG_LEVEL"))
	logger.InitializeText(logLevel)
	log := logger.Logger()

	cfg := config.Load()
	cfg.MCPEnabled = cfg.MCPEnabled || mcp

	srv, err := server.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	log.Info("AI Observer starting",
		"database", cfg.DatabasePath,
		"host", cfg.Host,
		"api_port", cfg.APIPort,
		"otlp_port", cfg.OTLPPort,
	)

	if err := serveUntilDone(ctx, srv, srv.ListenAndServe); err != nil {
		return err
	}
	log.Info("Shutdown complete")
	return nil
}

// shutdownTimeout bounds how long in-flight requests may take to finish
const shutdownTimeout = 10 * time.Second

type gracefulServer interface {
	Shutdown(ctx context.Context) error
}

// serveUntilDone runs serve until it returns or ctx is cancelled (on
// SIGINT/SIGTERM), then shuts srv down. It returns only after the shutdown has
// finished, so storage is closed before the process exits. A server stopped by
// the shutdown is not an error.
func serveUntilDone(ctx context.Context, srv gracefulServer, serve func() error) error {
	serveErr := make(chan error, 1)
	go func() { serveErr <- serve() }()

	var err error
	stopped := false
	select {
	case err = <-serveErr:
		// The server stopped on its own (e.g. a port was in use); still release resources.
		stopped = true
	case <-ctx.Done():
		logger.Info("Received shutdown signal")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	shutdownErr := srv.Shutdown(shutdownCtx)

	if !stopped {
		err = <-serveErr
	}
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return errors.Join(err, shutdownErr)
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

// reorderArgs moves flags before positional arguments so Go's flag package,
// which stops at the first positional argument, parses all of them.
// Example: ["all", "--output", "./export"] -> ["--output", "./export", "all"]
// fs tells which flags are boolean: those never take the next argument as value.
func reorderArgs(fs *flag.FlagSet, args []string) []string {
	var flags []string
	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--":
			// Everything after the terminator is positional
			flags = append(flags, args[i:]...)
			return append(flags, positional...)
		case !strings.HasPrefix(arg, "-") || arg == "-":
			positional = append(positional, arg)
		default:
			flags = append(flags, arg)
			if !strings.Contains(arg, "=") && flagTakesValue(fs, arg) && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		}
	}

	return append(flags, positional...)
}

// flagTakesValue reports whether the flag in arg ("-name" or "--name") reads
// the following argument as its value
func flagTakesValue(fs *flag.FlagSet, arg string) bool {
	f := fs.Lookup(strings.TrimLeft(arg, "-"))
	if f == nil {
		return false // unknown flags are reported by fs.Parse
	}
	if b, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
		return false
	}
	return true
}
