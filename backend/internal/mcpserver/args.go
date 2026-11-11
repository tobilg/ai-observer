package mcpserver

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/tobilg/ai-observer/internal/tools"
)

// timeHelp documents the time arguments in the tool descriptions
const timeHelp = `RFC 3339 time (2026-11-05T10:00:00Z), date (2026-11-05, UTC) or age relative to now (30m, 24h, 7d, 2w)`

// parseTime reads a time argument: RFC 3339, a date (UTC, the day's end for
// endOfDay) or an age such as "7d" (now minus 7 days)
func parseTime(value string, now time.Time, endOfDay bool) (time.Time, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "now") {
		return now, nil
	}
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", value); err == nil {
		if endOfDay {
			return t.Add(24*time.Hour - time.Nanosecond), nil
		}
		return t, nil
	}
	if d, err := parseAge(value); err == nil {
		return now.Add(-d), nil
	}
	return time.Time{}, fmt.Errorf("invalid time %q: use an %s", value, timeHelp)
}

// parseAge reads "90m", "36h", "7d" or "2w"
func parseAge(value string) (time.Duration, error) {
	if len(value) < 2 {
		return 0, fmt.Errorf("invalid age %q", value)
	}
	unit := value[len(value)-1]
	n, err := strconv.ParseFloat(value[:len(value)-1], 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("invalid age %q", value)
	}
	switch unit {
	case 'm':
		return time.Duration(n * float64(time.Minute)), nil
	case 'h':
		return time.Duration(n * float64(time.Hour)), nil
	case 'd':
		return time.Duration(n * 24 * float64(time.Hour)), nil
	case 'w':
		return time.Duration(n * 7 * 24 * float64(time.Hour)), nil
	}
	return 0, fmt.Errorf("invalid age %q", value)
}

// timeRange reads from and to: to defaults to now, from to defaultSpan
// before to
func timeRange(fromArg, toArg string, now time.Time, defaultSpan time.Duration) (time.Time, time.Time, error) {
	to := now
	if toArg != "" {
		t, err := parseTime(toArg, now, true)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("to: %w", err)
		}
		to = t
	}
	from := to.Add(-defaultSpan)
	if fromArg != "" {
		t, err := parseTime(fromArg, now, false)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("from: %w", err)
		}
		from = t
	}
	if from.After(to) {
		return time.Time{}, time.Time{}, fmt.Errorf("from (%s) is after to (%s)", from.Format(time.RFC3339), to.Format(time.RFC3339))
	}
	return from, to, nil
}

// toolHelp documents the tool argument
const toolHelp = "claude-code (or claude), codex, gemini, opencode, copilot-chat (VS Code) or github-copilot (CLI); OTLP service names work too and select only that service (Codex sends codex_cli_rs, codex-app-server and codex-exec-server)"

// serviceFor maps a tool argument to the storage service filter ("" for
// all): a tool name, which covers all of the tool's services (Codex sends
// three), or a service name, which covers only itself
func serviceFor(tool string) (string, error) {
	tool = strings.ToLower(strings.TrimSpace(tool))
	switch tool {
	case "":
		return "", nil
	case "claude", "claude code":
		return string(tools.Claude), nil
	}
	if filter := tools.ResolveFilter(tool); filter != "" {
		return filter, nil
	}
	return "", fmt.Errorf("unknown tool %q: use %s", tool, toolHelp)
}

// toolName is the tool of an OTLP service name, for results
func toolName(service string) string {
	if tool, ok := tools.ToolForService(service); ok {
		return string(tool)
	}
	return service
}

// limitArg bounds a page size argument
func limitArg(limit, defaultLimit, maxLimit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	return min(limit, maxLimit)
}

// truncate cuts text to max characters (runes), marking the cut
func truncate(text string, max int) (string, bool) {
	if max <= 0 || utf8.RuneCountInString(text) <= max {
		return text, false
	}
	runes := []rune(text)
	return string(runes[:max]) + fmt.Sprintf("… [%d more characters]", len(runes)-max), true
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// formatTimeMillis formats a time like formatTime with milliseconds, for
// span starts, which are often in the same second
func formatTimeMillis(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05.000Z07:00")
}
