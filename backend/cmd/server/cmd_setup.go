package main

import (
	"flag"
	"fmt"
	"os"
)

func cmdSetup(args []string) {
	exitOnError(runSetup(args))
}

func runSetup(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)

	fs.Usage = func() {
		fmt.Print(`Show how to send a tool's telemetry to AI Observer

Usage: ai-observer setup <claude-code|codex|gemini|opencode|github-copilot>

`)
		printArguments(os.Stdout, []argument{
			{"claude-code", "Claude Code"},
			{"codex", "OpenAI Codex (CLI and app)"},
			{"gemini", "Gemini CLI"},
			{"opencode", "OpenCode, with its OTEL plugin"},
			{"github-copilot", "GitHub Copilot (VS Code and CLI)"},
		})
	}

	if err := fs.Parse(reorderArgs(fs, args)); err != nil {
		return err
	}

	// Get tool argument
	tool := fs.Arg(0)
	if tool == "" {
		return fmt.Errorf("tool argument is required\nUsage: ai-observer setup [claude-code|codex|gemini|opencode|github-copilot]")
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
	case "opencode":
		printSetupInstructions(tool)
		return nil
	case "github-copilot":
		printSetupInstructions(tool)
		return nil
	default:
		return fmt.Errorf("unknown tool: %s\n\nSupported tools: claude-code, gemini, codex, opencode, github-copilot", tool)
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
#    or OTLP/gRPC:
#    export OTEL_EXPORTER_OTLP_PROTOCOL=grpc
#    export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317

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

   For OTLP/gRPC use "otlpProtocol": "grpc" and "otlpEndpoint": "http://localhost:4317".

2. Add to ~/.bashrc or ~/.zshrc:

# Mitigate Gemini CLI bug
export OTEL_METRIC_EXPORT_TIMEOUT=10000
export OTEL_LOGS_EXPORT_TIMEOUT=5000
`)
	case "codex":
		fmt.Print(`OpenAI Codex CLI Setup
======================

Add to ~/.codex/config.toml (used by the CLI and the Codex app):

[otel]
exporter = { otlp-http = { endpoint = "http://localhost:4318/v1/logs", protocol = "binary" } }
trace_exporter = { otlp-http = { endpoint = "http://localhost:4318/v1/traces", protocol = "binary" } }
metrics_exporter = { otlp-http = { endpoint = "http://localhost:4318/v1/metrics", protocol = "binary" } }
log_user_prompt = true

For OTLP/gRPC, use otlp-grpc exporters instead, e.g.
exporter = { otlp-grpc = { endpoint = "http://localhost:4317" } }

Codex 0.167 and later run the CLI's conversations in a background app server,
which keeps the settings it started with. After changing [otel], quit Codex,
stop it with pkill -f app-server-daemon, and start Codex again. The CLI and
the Codex app then send their conversations as codex-app-server.
`)
	case "opencode":
		fmt.Print(`OpenCode OTEL Plugin Setup
==========================

1. Add the OTEL plugin to your OpenCode config:

{
  "$schema": "https://opencode.ai/config.json",
  "plugin": ["@devtheops/opencode-plugin-otel"]
}

2. Add to ~/.bashrc or ~/.zshrc:

export OPENCODE_ENABLE_TELEMETRY=1
export OPENCODE_OTLP_ENDPOINT=http://localhost:4318
export OPENCODE_OTLP_PROTOCOL=http/protobuf

AI Observer also receives the plugin's default, OTLP/gRPC on port 4317: leave
OPENCODE_OTLP_PROTOCOL unset and set OPENCODE_OTLP_ENDPOINT=http://localhost:4317.
`)
	case "github-copilot":
		fmt.Print(`GitHub Copilot Setup
====================

VS Code settings:

{
  "github.copilot.chat.otel.enabled": true,
  "github.copilot.chat.otel.exporterType": "otlp-http",
  "github.copilot.chat.otel.otlpEndpoint": "http://localhost:4318",
  "github.copilot.chat.otel.captureContent": true
}

Environment variables:

export COPILOT_OTEL_ENABLED=true
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export COPILOT_OTEL_CAPTURE_CONTENT=true

Content capture can include prompts, code, tool arguments, and tool results.
Only enable it in trusted local environments.
`)
	default:
		fmt.Printf("Unknown tool: %s\n\nSupported tools: claude-code, gemini, codex, opencode, github-copilot\n", tool)
		os.Exit(1)
	}
}
