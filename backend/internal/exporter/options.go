package exporter

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/redact"
	"github.com/tobilg/ai-observer/internal/tools"
)

// SourceType defines the data source for export
type SourceType string

const (
	SourceClaude        SourceType = SourceType(tools.Claude)
	SourceCodex         SourceType = SourceType(tools.Codex)
	SourceGemini        SourceType = SourceType(tools.Gemini)
	SourceOpenCode      SourceType = SourceType(tools.OpenCode)
	SourceCopilotVSCode SourceType = SourceType(tools.CopilotVSCode)
	SourceCopilotCLI    SourceType = SourceType(tools.CopilotCLI)
	SourceAll           SourceType = "all" // Export-specific: no filter
)

// Options configures the export operation
type Options struct {
	Source      SourceType // Tool to export (claude, codex, gemini, all)
	OutputDir   string     // Output directory path
	FromDate    *time.Time // Optional start date filter
	ToDate      *time.Time // Optional end date filter
	FromFiles   bool       // Read from raw files instead of database
	CreateZip   bool       // Create ZIP archive of output
	DryRun      bool       // Preview without exporting
	SkipConfirm bool       // Skip confirmation prompt
	Verbose     bool       // Show detailed progress

	// Redactor redacts the records read from session files (FromFiles), as
	// an import does; nil stores them as read
	Redactor *redact.Redactor

	// Out receives progress, previews and prompts and In the confirmation
	// answer; nil means stdout and stdin
	Out io.Writer
	In  io.Reader
}

func (o Options) out() io.Writer {
	if o.Out == nil {
		return os.Stdout
	}
	return o.Out
}

func (o Options) in() io.Reader {
	if o.In == nil {
		return os.Stdin
	}
	return o.In
}

// ServiceName returns the ServiceName filter value for this source
// Returns empty string for SourceAll (no filter)
// Delegates to the centralized tools package
func (o *Options) ServiceName() string {
	if o.Source == SourceAll {
		return ""
	}
	return tools.Tool(o.Source).ServiceName()
}

// ServiceFilter returns the service filter of this source: the tool name,
// which selects all of the tool's services (codex: codex_cli_rs,
// codex-app-server, codex-exec-server), or "" for SourceAll
func (o *Options) ServiceFilter() string {
	if o.Source == SourceAll {
		return ""
	}
	return string(o.Source)
}

// DateRangeString returns a formatted string for the date range
// Returns "all" if no date filter is set
func (o *Options) DateRangeString() string {
	if o.FromDate == nil && o.ToDate == nil {
		return "all"
	}
	if o.FromDate != nil && o.ToDate != nil {
		return fmt.Sprintf("%s-%s", o.FromDate.Format("2006-01-02"), o.ToDate.Format("2006-01-02"))
	}
	if o.FromDate != nil {
		return fmt.Sprintf("%s-now", o.FromDate.Format("2006-01-02"))
	}
	return fmt.Sprintf("start-%s", o.ToDate.Format("2006-01-02"))
}

// Summary contains export statistics
type Summary struct {
	TracesCount  int64 // Number of trace spans exported
	LogsCount    int64 // Number of log records exported
	MetricsCount int64 // Number of metric data points exported
	// BaselinesCount is the number of metric baselines exported (the
	// previous points of cumulative series), DashboardsCount that of
	// dashboards
	BaselinesCount  int64
	DashboardsCount int
	// ImportStateCount is the number of session files whose import state
	// is exported (import_state.parquet)
	ImportStateCount int64
	OutputFiles      []string // List of output file paths
	TotalSize        int64    // Total size of exported files in bytes
}

// IsEmpty returns true if there's nothing to export
func (s *Summary) IsEmpty() bool {
	return s.TracesCount == 0 && s.LogsCount == 0 && s.MetricsCount == 0
}

// ParseSourceArg parses the source argument from CLI
func ParseSourceArg(s string) (SourceType, error) {
	name := strings.ToLower(s)
	if name == string(SourceAll) {
		return SourceAll, nil
	}
	if tool, ok := tools.Parse(name); ok {
		return SourceType(tool), nil
	}
	return "", fmt.Errorf("invalid source: %s (valid: %s)", s, strings.Join(ValidSources(), ", "))
}

// ValidSources returns a list of valid source names for help text
func ValidSources() []string {
	return append(tools.Names(), string(SourceAll))
}
