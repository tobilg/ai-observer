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
	"github.com/tobilg/ai-observer/internal/watcher"
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
	case "backup":
		cmdBackup(os.Args[2:])
	case "restore":
		cmdRestore(os.Args[2:])
	case "compact":
		cmdCompact(os.Args[2:])
	case "setup":
		cmdSetup(os.Args[2:])
	case "watch":
		cmdWatch(os.Args[2:])
	case "serve":
		cmdServe(os.Args[2:])
	case "healthcheck":
		cmdHealthcheck(os.Args[2:])
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
  serve        Receive OTLP telemetry and serve the dashboard (default); --watch also reads session files
  watch        Read local session files as they change and serve the dashboard, without OTLP
  import       Import local session files once
  export       Export telemetry to Parquet files
  delete       Delete telemetry in a time range
  backup       Write a consistent copy of the database to a new file
  restore      Load an export back into the database
  compact      Rewrite the database into a new file without its free space
  setup        Show how to send a tool's telemetry to AI Observer
  pricing      Update model prices from models.dev
  healthcheck  Exit 0 when the server on this machine is ready (container health check)

Options:
  -h, --help       Show this help
  -v, --version    Show the version

Use "ai-observer <command> --help" for the options of a command. Options of a
command override the settings of the same name below.

Settings (environment variables):
`)
	printSettings(os.Stdout)
	fmt.Print(`
Variables of the AI tools (set them where the tool runs; AI Observer does not
read them, see "ai-observer setup <tool>"):
  COPILOT_OTEL_ENABLED          Enable GitHub Copilot OTLP export
  COPILOT_OTEL_CAPTURE_CONTENT  Capture GitHub Copilot prompts and tool content
  OPENCODE_ENABLE_TELEMETRY     Enable the OpenCode OTEL plugin's export
  OPENCODE_OTLP_ENDPOINT        OpenCode OTLP endpoint (http://localhost:4318, or :4317 for gRPC)
  OPENCODE_OTLP_PROTOCOL        OpenCode OTLP protocol (http/protobuf, or grpc)
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

// serveArgs are the options of serve
type serveArgs struct {
	MCP bool
	// Watch also watches session files (serve --watch [tool]): ToolArg as
	// given, Tools to watch (none for all), Backfill to import existing data
	Watch    bool
	ToolArg  string
	Tools    []string
	Backfill bool
	// Server overrides the environment's host, ports and database
	Server serverFlags
}

// parseServeArgs reads the options of serve
func parseServeArgs(args []string) (*serveArgs, error) {
	parsed := &serveArgs{}
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.BoolVar(&parsed.MCP, "mcp", false, mcpHelp)
	fs.BoolVar(&parsed.Watch, "watch", false, "Also read local session files as they change (all tools, or the one named)")
	fs.BoolVar(&parsed.Backfill, "backfill", false, "With --watch, import the existing files on the first start")
	parsed.Server.register(fs, true)
	fs.Usage = func() {
		fmt.Printf(`Receive OTLP telemetry (HTTP and gRPC) and serve the dashboard and its API,
optionally reading local session files as well

Usage: ai-observer serve [options]
       ai-observer serve --watch [%s|all] [--backfill] [options]

Token usage and cost of a Claude Code, Codex or Gemini CLI session that
arrives both over OTLP and from its session file are counted once (from OTLP).

Options:
`, toolList(true))
		printFlags(fs)
	}
	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return nil, err
	}
	if !parsed.Watch {
		if fs.NArg() > 0 {
			return nil, fmt.Errorf("unexpected argument: %s (to watch session files as well, use --watch %s)", fs.Arg(0), fs.Arg(0))
		}
		if parsed.Backfill {
			return nil, fmt.Errorf("--backfill requires --watch")
		}
		return parsed, nil
	}
	if fs.NArg() > 1 {
		return nil, fmt.Errorf("unexpected argument: %s", fs.Arg(1))
	}
	var err error
	if parsed.ToolArg, parsed.Tools, err = parseWatchTool(fs.Arg(0)); err != nil {
		return nil, err
	}
	return parsed, nil
}

// shutdownSignals returns a context that is cancelled on SIGINT/SIGTERM, for
// a graceful shutdown. Commands install it before they open the database, so
// an interrupt during startup still closes it.
func shutdownSignals() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

// runServer serves OTLP and the API until ctx is cancelled, then shuts down
func runServer(ctx context.Context, args []string) error {
	parsed, err := parseServeArgs(args)
	if err != nil {
		return err
	}

	// Structured logging: text by default, JSON with AI_OBSERVER_LOG_FORMAT=json
	initLogging()
	log := logger.Logger()

	cfg := config.Load()
	cfg.MCPEnabled = cfg.MCPEnabled || parsed.MCP
	parsed.Server.apply(cfg)

	srv, err := server.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	watching := "off"
	if parsed.Watch {
		watching = parsed.ToolArg
		// The watcher scans existing files (and backfills) in the
		// background while the servers start
		if err := srv.StartWatcher(watcher.Config{Tools: parsed.Tools, Backfill: parsed.Backfill}); err != nil {
			srv.Shutdown(context.Background())
			return fmt.Errorf("failed to start watcher: %w", err)
		}
	}

	log.Info("AI Observer starting",
		"database", cfg.DatabasePath,
		"host", cfg.Host,
		"api_port", cfg.APIPort,
		"otlp_port", cfg.OTLPPort,
		"otlp_grpc_port", cfg.OTLPGRPCPort,
		"watch", watching,
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
