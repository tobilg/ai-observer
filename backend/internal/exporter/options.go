package exporter

import (
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/tools"
)

// SourceType defines the data source for export
type SourceType string

const (
	SourceClaude SourceType = SourceType(tools.Claude)
	SourceCodex  SourceType = SourceType(tools.Codex)
	SourceGemini SourceType = SourceType(tools.Gemini)
	SourceAll    SourceType = "all" // Export-specific: no filter
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
	TracesCount  int64    // Number of trace spans exported
	LogsCount    int64    // Number of log records exported
	MetricsCount int64    // Number of metric data points exported
	OutputFiles  []string // List of output file paths
	TotalSize    int64    // Total size of exported files in bytes
}

// IsEmpty returns true if there's nothing to export
func (s *Summary) IsEmpty() bool {
	return s.TracesCount == 0 && s.LogsCount == 0 && s.MetricsCount == 0
}

// ParseSourceArg parses the source argument from CLI
func ParseSourceArg(s string) (SourceType, error) {
	switch strings.ToLower(s) {
	case "claude-code":
		return SourceClaude, nil
	case "codex":
		return SourceCodex, nil
	case "gemini":
		return SourceGemini, nil
	case "all":
		return SourceAll, nil
	default:
		return "", fmt.Errorf("invalid source: %s (valid: claude-code, codex, gemini, all)", s)
	}
}

// ValidSources returns a list of valid source names for help text
func ValidSources() []string {
	return []string{"claude-code", "codex", "gemini", "all"}
}
