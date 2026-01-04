package main

import (
	"flag"
	"fmt"
	"os"
)

func cmdSetup(args []string) {
	if err := runSetup(args); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}

func runSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)

	fs.Usage = func() {
		fmt.Print(`Show setup instructions for AI tools

Usage: ai-observer setup [claude-code|codex|gemini]

Arguments:
  claude-code  Show Claude Code setup instructions
  codex        Show OpenAI Codex CLI setup instructions
  gemini       Show Gemini CLI setup instructions
`)
	}

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Get tool argument
	tool := fs.Arg(0)
	if tool == "" {
		return fmt.Errorf("tool argument is required\nUsage: ai-observer setup [claude-code|codex|gemini]")
	}

	return printSetupInstructionsWithError(tool)
}

func printSetupInstructionsWithError(tool string) error {
	switch tool {
	case "claude-code":
		printSetupInstructions(tool)
		return nil
	case "gemini":
		printSetupInstructions(tool)
		return nil
	case "codex":
		printSetupInstructions(tool)
		return nil
	default:
		return fmt.Errorf("unknown tool: %s\n\nSupported tools: claude-code, gemini, codex", tool)
	}
}

func printSetupInstructions(tool string) {
	switch tool {
	case "claude-code":
		fmt.Print(`Claude Code Setup
=================

Add to ~/.bashrc or ~/.zshrc:

# 1. Enable telemetry
export CLAUDE_CODE_ENABLE_TELEMETRY=1

# 2. Choose exporters (both are optional - configure only what you need)
export OTEL_METRICS_EXPORTER=otlp       # Options: otlp, prometheus, console
export OTEL_LOGS_EXPORTER=otlp          # Options: otlp, console

# 3. Configure OTLP endpoint (for OTLP exporter)
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318

# 4. For faster visibilty: reduce export intervals
export OTEL_METRIC_EXPORT_INTERVAL=10000  # 10 seconds (default: 60000ms)
export OTEL_LOGS_EXPORT_INTERVAL=5000     # 5 seconds (default: 5000ms)

# 5. Log prompts
export OTEL_LOG_USER_PROMPTS=1
`)
	case "gemini":
		fmt.Print(`Gemini CLI Setup
================

1. Add to ~/.gemini/settings.json:

{
  "telemetry": {
    "enabled": true,
    "target": "local",
    "otlpProtocol": "http",
    "otlpEndpoint": "http://localhost:4318",
    "logPrompts": true,
    "useCollector": true
  }
}

2. Add to ~/.bashrc or ~/.zshrc:

# Mitigate Gemini CLI bug
export OTEL_METRIC_EXPORT_TIMEOUT=10000
export OTEL_LOGS_EXPORT_TIMEOUT=5000
`)
	case "codex":
		fmt.Print(`OpenAI Codex CLI Setup
======================

Add to ~/.codex/config.toml:

[otel]
exporter = { otlp-http = { endpoint = "http://localhost:4318/v1/logs", protocol = "binary" } }
trace_exporter = { otlp-http = { endpoint = "http://localhost:4318/v1/traces", protocol = "binary" } }
log_user_prompt = true
`)
	default:
		fmt.Printf("Unknown tool: %s\n\nSupported tools: claude-code, gemini, codex\n", tool)
		os.Exit(1)
	}
}
