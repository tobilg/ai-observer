package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"strings"
	"testing"
)

// captureOutput captures stdout during function execution
func captureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func TestPrintVersion(t *testing.T) {
	output := captureOutput(printVersion)

	if !strings.Contains(output, "AI Observer") {
		t.Error("expected version output to contain 'AI Observer'")
	}
	if !strings.Contains(output, "Git Commit") {
		t.Error("expected version output to contain 'Git Commit'")
	}
	if !strings.Contains(output, "Build Date") {
		t.Error("expected version output to contain 'Build Date'")
	}
}

func TestPrintHelp(t *testing.T) {
	output := captureOutput(printHelp)

	// Check for key sections
	expectedStrings := []string{
		"AI Observer",
		"Usage:",
		"Commands:",
		"import",
		"export",
		"delete",
		"setup",
		"serve",
		"Options:",
		"--help",
		"--version",
		"Environment Variables:",
		"AI_OBSERVER_API_PORT",
		"AI_OBSERVER_OTLP_PORT",
		"AI_OBSERVER_DATABASE_PATH",
	}

	for _, s := range expectedStrings {
		if !strings.Contains(output, s) {
			t.Errorf("expected help output to contain %q", s)
		}
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"DEBUG", "DEBUG"},
		{"debug", "DEBUG"},
		{"INFO", "INFO"},
		{"info", "INFO"},
		{"WARN", "WARN"},
		{"warn", "WARN"},
		{"WARNING", "WARN"},
		{"ERROR", "ERROR"},
		{"error", "ERROR"},
		{"", "INFO"},       // default
		{"invalid", "INFO"}, // default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level := parseLogLevel(tt.input)
			if level.String() != tt.expected {
				t.Errorf("parseLogLevel(%q) = %v, want %v", tt.input, level.String(), tt.expected)
			}
		})
	}
}

func TestPrintFlags(t *testing.T) {
	// Create a test flag set
	fs := newTestFlagSet()

	output := captureOutput(func() {
		printFlags(fs)
	})

	// Verify double-dash format
	if !strings.Contains(output, "--string-flag") {
		t.Error("expected output to contain '--string-flag'")
	}
	if !strings.Contains(output, "--bool-flag") {
		t.Error("expected output to contain '--bool-flag'")
	}
	if !strings.Contains(output, "--int-flag") {
		t.Error("expected output to contain '--int-flag'")
	}

	// Verify descriptions
	if !strings.Contains(output, "A string flag") {
		t.Error("expected output to contain flag description")
	}
}

func TestFlagTypeName(t *testing.T) {
	fs := newTestFlagSet()

	var foundString, foundInt, foundBool bool

	fs.VisitAll(func(f *flag.Flag) {
		typeName := flagTypeName(f)
		switch f.Name {
		case "string-flag":
			if typeName != "string" {
				t.Errorf("expected 'string' for string-flag, got %q", typeName)
			}
			foundString = true
		case "int-flag":
			if typeName != "int" {
				t.Errorf("expected 'int' for int-flag, got %q", typeName)
			}
			foundInt = true
		case "bool-flag":
			// bool flags have empty type (indicated by "false" default)
			foundBool = true
		}
	})

	if !foundString || !foundInt || !foundBool {
		t.Error("expected to find all flag types")
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"123", true},
		{"0", true},
		{"", false},
		{"abc", false},
		{"12.3", false},
		{"-1", false},
		{"1a", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isNumeric(tt.input)
			if result != tt.expected {
				t.Errorf("isNumeric(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// newTestFlagSet creates a flag set for testing
func newTestFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("string-flag", "", "A string flag")
	fs.Bool("bool-flag", false, "A boolean flag")
	fs.Int("int-flag", 0, "An integer flag")
	return fs
}

func TestPrintSetupInstructions(t *testing.T) {
	tests := []struct {
		tool     string
		expected []string
	}{
		{
			"claude-code",
			[]string{
				"Claude Code Setup",
				"CLAUDE_CODE_ENABLE_TELEMETRY",
				"OTEL_METRICS_EXPORTER",
				"OTEL_EXPORTER_OTLP_ENDPOINT",
			},
		},
		{
			"gemini",
			[]string{
				"Gemini CLI Setup",
				"settings.json",
				"telemetry",
				"otlpEndpoint",
			},
		},
		{
			"codex",
			[]string{
				"OpenAI Codex CLI Setup",
				"config.toml",
				"[otel]",
				"exporter",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			output := captureOutput(func() {
				printSetupInstructions(tt.tool)
			})

			for _, s := range tt.expected {
				if !strings.Contains(output, s) {
					t.Errorf("expected setup instructions for %s to contain %q", tt.tool, s)
				}
			}
		})
	}
}

func TestPrintSetupInstructionsUnknown(t *testing.T) {
	err := printSetupInstructionsWithError("unknown")
	if err == nil {
		t.Error("expected error for unknown tool")
	}
	if !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("expected error message to contain 'unknown tool', got %q", err.Error())
	}
}

// Tests for runSetup
func TestRunSetup(t *testing.T) {
	t.Run("claude-code", func(t *testing.T) {
		err := runSetup([]string{"claude-code"})
		if err != nil {
			t.Errorf("runSetup(claude) failed: %v", err)
		}
	})

	t.Run("gemini", func(t *testing.T) {
		err := runSetup([]string{"gemini"})
		if err != nil {
			t.Errorf("runSetup(gemini) failed: %v", err)
		}
	})

	t.Run("codex", func(t *testing.T) {
		err := runSetup([]string{"codex"})
		if err != nil {
			t.Errorf("runSetup(codex) failed: %v", err)
		}
	})

	t.Run("missing tool", func(t *testing.T) {
		err := runSetup([]string{})
		if err == nil {
			t.Error("expected error for missing tool")
		}
	})

	t.Run("unknown tool", func(t *testing.T) {
		err := runSetup([]string{"unknown"})
		if err == nil {
			t.Error("expected error for unknown tool")
		}
	})

	t.Run("help flag", func(t *testing.T) {
		// -help flag causes flag.ErrHelp which is an error
		err := runSetup([]string{"-help"})
		if err == nil {
			t.Error("expected error for help flag")
		}
	})
}

// Tests for parseImportFlags
func TestParseImportFlags(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		flags, err := parseImportFlags([]string{"claude-code"})
		if err != nil {
			t.Fatalf("parseImportFlags failed: %v", err)
		}
		if flags.Tool != "claude-code" {
			t.Errorf("expected tool 'claude', got %q", flags.Tool)
		}
	})

	t.Run("all flags", func(t *testing.T) {
		flags, err := parseImportFlags([]string{
			"--from", "2025-01-01",
			"--to", "2025-01-31",
			"--dry-run",
			"--force",
			"--verbose",
			"--purge",
			"--yes",
			"all",
		})
		if err != nil {
			t.Fatalf("parseImportFlags failed: %v", err)
		}
		if flags.Tool != "all" {
			t.Errorf("expected tool 'all', got %q", flags.Tool)
		}
		if flags.From != "2025-01-01" {
			t.Errorf("expected from '2025-01-01', got %q", flags.From)
		}
		if flags.To != "2025-01-31" {
			t.Errorf("expected to '2025-01-31', got %q", flags.To)
		}
		if !flags.DryRun {
			t.Error("expected dry-run to be true")
		}
		if !flags.Force {
			t.Error("expected force to be true")
		}
		if !flags.Verbose {
			t.Error("expected verbose to be true")
		}
		if !flags.Purge {
			t.Error("expected purge to be true")
		}
		if !flags.Yes {
			t.Error("expected yes to be true")
		}
	})

	t.Run("invalid flag", func(t *testing.T) {
		_, err := parseImportFlags([]string{"--invalid-flag"})
		if err == nil {
			t.Error("expected error for invalid flag")
		}
	})
}

// Tests for runImport validation
func TestRunImportValidation(t *testing.T) {
	t.Run("missing tool", func(t *testing.T) {
		err := runImport([]string{})
		if err == nil {
			t.Error("expected error for missing tool")
		}
	})

	t.Run("invalid tool", func(t *testing.T) {
		err := runImport([]string{"invalid"})
		if err == nil {
			t.Error("expected error for invalid tool")
		}
	})

	t.Run("invalid from date", func(t *testing.T) {
		err := runImport([]string{"--from", "invalid", "claude-code"})
		if err == nil {
			t.Error("expected error for invalid from date")
		}
	})

	t.Run("invalid to date", func(t *testing.T) {
		err := runImport([]string{"--to", "invalid", "claude-code"})
		if err == nil {
			t.Error("expected error for invalid to date")
		}
	})

	t.Run("purge without dates", func(t *testing.T) {
		err := runImport([]string{"--purge", "claude-code"})
		if err == nil {
			t.Error("expected error for purge without dates")
		}
	})

	t.Run("from after to", func(t *testing.T) {
		err := runImport([]string{"--from", "2025-12-31", "--to", "2025-01-01", "claude-code"})
		if err == nil {
			t.Error("expected error for from after to")
		}
	})
}

// Tests for parseExportFlags
func TestParseExportFlags(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		flags, err := parseExportFlags([]string{"--output", "/tmp/export", "claude-code"})
		if err != nil {
			t.Fatalf("parseExportFlags failed: %v", err)
		}
		if flags.Source != "claude-code" {
			t.Errorf("expected source 'claude', got %q", flags.Source)
		}
		if flags.Output != "/tmp/export" {
			t.Errorf("expected output '/tmp/export', got %q", flags.Output)
		}
	})

	t.Run("all flags", func(t *testing.T) {
		flags, err := parseExportFlags([]string{
			"--output", "/tmp/export",
			"--from", "2025-01-01",
			"--to", "2025-01-31",
			"--from-files",
			"--zip",
			"--dry-run",
			"--verbose",
			"--yes",
			"all",
		})
		if err != nil {
			t.Fatalf("parseExportFlags failed: %v", err)
		}
		if !flags.FromFiles {
			t.Error("expected from-files to be true")
		}
		if !flags.Zip {
			t.Error("expected zip to be true")
		}
		if !flags.DryRun {
			t.Error("expected dry-run to be true")
		}
	})
}

// Tests for runExport validation
func TestRunExportValidation(t *testing.T) {
	t.Run("missing source", func(t *testing.T) {
		err := runExport([]string{"--output", "/tmp"})
		if err == nil {
			t.Error("expected error for missing source")
		}
	})

	t.Run("missing output", func(t *testing.T) {
		err := runExport([]string{"claude-code"})
		if err == nil {
			t.Error("expected error for missing output")
		}
	})

	t.Run("invalid source", func(t *testing.T) {
		err := runExport([]string{"--output", "/tmp", "invalid"})
		if err == nil {
			t.Error("expected error for invalid source")
		}
	})

	t.Run("invalid from date", func(t *testing.T) {
		err := runExport([]string{"--output", "/tmp", "--from", "invalid", "claude-code"})
		if err == nil {
			t.Error("expected error for invalid from date")
		}
	})

	t.Run("from after to", func(t *testing.T) {
		err := runExport([]string{"--output", "/tmp", "--from", "2025-12-31", "--to", "2025-01-01", "claude-code"})
		if err == nil {
			t.Error("expected error for from after to")
		}
	})
}

// Tests for parseDeleteFlags
func TestParseDeleteFlags(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		flags, err := parseDeleteFlags([]string{"--from", "2025-01-01", "--to", "2025-01-31", "logs"})
		if err != nil {
			t.Fatalf("parseDeleteFlags failed: %v", err)
		}
		if flags.Scope != "logs" {
			t.Errorf("expected scope 'logs', got %q", flags.Scope)
		}
		if flags.From != "2025-01-01" {
			t.Errorf("expected from '2025-01-01', got %q", flags.From)
		}
		if flags.To != "2025-01-31" {
			t.Errorf("expected to '2025-01-31', got %q", flags.To)
		}
	})

	t.Run("with service", func(t *testing.T) {
		flags, err := parseDeleteFlags([]string{"--from", "2025-01-01", "--to", "2025-01-31", "--service", "claude_code", "logs"})
		if err != nil {
			t.Fatalf("parseDeleteFlags failed: %v", err)
		}
		if flags.Service != "claude_code" {
			t.Errorf("expected service 'claude_code', got %q", flags.Service)
		}
	})

	t.Run("with yes", func(t *testing.T) {
		flags, err := parseDeleteFlags([]string{"--from", "2025-01-01", "--to", "2025-01-31", "--yes", "all"})
		if err != nil {
			t.Fatalf("parseDeleteFlags failed: %v", err)
		}
		if !flags.Yes {
			t.Error("expected yes to be true")
		}
	})
}

// Tests for runDelete validation
func TestRunDeleteValidation(t *testing.T) {
	t.Run("missing scope", func(t *testing.T) {
		err := runDelete([]string{"--from", "2025-01-01", "--to", "2025-01-31"})
		if err == nil {
			t.Error("expected error for missing scope")
		}
	})

	t.Run("missing from", func(t *testing.T) {
		err := runDelete([]string{"--to", "2025-01-31", "logs"})
		if err == nil {
			t.Error("expected error for missing from")
		}
	})

	t.Run("missing to", func(t *testing.T) {
		err := runDelete([]string{"--from", "2025-01-01", "logs"})
		if err == nil {
			t.Error("expected error for missing to")
		}
	})

	t.Run("invalid scope", func(t *testing.T) {
		err := runDelete([]string{"--from", "2025-01-01", "--to", "2025-01-31", "invalid"})
		if err == nil {
			t.Error("expected error for invalid scope")
		}
	})

	t.Run("invalid from date", func(t *testing.T) {
		err := runDelete([]string{"--from", "invalid", "--to", "2025-01-31", "logs"})
		if err == nil {
			t.Error("expected error for invalid from date")
		}
	})

	t.Run("invalid to date", func(t *testing.T) {
		err := runDelete([]string{"--from", "2025-01-01", "--to", "invalid", "logs"})
		if err == nil {
			t.Error("expected error for invalid to date")
		}
	})

	t.Run("from after to", func(t *testing.T) {
		err := runDelete([]string{"--from", "2025-12-31", "--to", "2025-01-01", "logs"})
		if err == nil {
			t.Error("expected error for from after to")
		}
	})
}
