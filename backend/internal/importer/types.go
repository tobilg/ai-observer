package importer

import (
	"context"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/pricing"
	"github.com/tobilg/ai-observer/internal/tools"
)

// SourceType identifies the AI tool source
type SourceType string

const (
	SourceClaude SourceType = SourceType(tools.Claude)
	SourceCodex  SourceType = SourceType(tools.Codex)
	SourceGemini SourceType = SourceType(tools.Gemini)
)

// AllSources returns all supported source types
func AllSources() []SourceType {
	all := tools.All()
	result := make([]SourceType, len(all))
	for i, t := range all {
		result[i] = SourceType(t)
	}
	return result
}

// ParseSourceType converts a string to a SourceType
func ParseSourceType(s string) (SourceType, bool) {
	if t, ok := tools.Parse(s); ok {
		return SourceType(t), true
	}
	return "", false
}

// ServiceName returns the OTLP service name for this source
// Delegates to the centralized tools package
func (s SourceType) ServiceName() string {
	return tools.Tool(s).ServiceName()
}

// ImportResult contains the transformed OTLP records from a single file
type ImportResult struct {
	FilePath    string
	SessionID   string
	Logs        []api.LogRecord
	Metrics     []api.MetricDataPoint
	Spans       []api.Span
	RecordCount int
	FirstTime   time.Time
	LastTime    time.Time
}

// SessionParser defines the interface for tool-specific parsers
type SessionParser interface {
	// Source returns the source type this parser handles
	Source() SourceType

	// FindSessionFiles returns all session files for this tool
	FindSessionFiles(ctx context.Context) ([]string, error)

	// ParseFile parses a single session file and returns the transformed data
	ParseFile(ctx context.Context, path string) (*ImportResult, error)
}

// Options configures the import behavior
type Options struct {
	DryRun      bool                // Show what would be imported without storing
	Force       bool                // Re-import already-imported sessions
	FromDate    *time.Time          // Only import sessions after this date
	ToDate      *time.Time          // Only import sessions before this date
	Purge       bool                // Delete existing data in time range before import
	SkipConfirm bool                // Skip confirmation prompts
	Verbose     bool                // Show detailed progress
	PricingMode pricing.PricingMode // Cost calculation mode for Claude (auto, calculate, display)
}

// FileState tracks import state for a single file
type FileState struct {
	Source      SourceType
	FilePath    string
	FileHash    string
	ImportedAt  time.Time
	RecordCount int
}

// FileSummary contains counts for a single file
type FileSummary struct {
	Path        string
	SessionID   string
	Logs        int
	Metrics     int
	Spans       int
	FirstTime   time.Time
	LastTime    time.Time
	Status      string // "new", "modified", "skipped"
}

// ImportSummary contains the overall import summary
type ImportSummary struct {
	Source        SourceType
	TotalFiles    int
	NewFiles      int
	ModifiedFiles int
	SkippedFiles  int
	TotalLogs     int
	TotalMetrics  int
	TotalSpans    int
	Files         []FileSummary
	Errors        []ImportError
}

// ImportError represents an error during import
type ImportError struct {
	FilePath string
	Error    string
}

// Add adds the counts from an ImportResult to this summary
func (s *ImportSummary) Add(result *ImportResult, status string) {
	s.Files = append(s.Files, FileSummary{
		Path:      result.FilePath,
		SessionID: result.SessionID,
		Logs:      len(result.Logs),
		Metrics:   len(result.Metrics),
		Spans:     len(result.Spans),
		FirstTime: result.FirstTime,
		LastTime:  result.LastTime,
		Status:    status,
	})

	s.TotalLogs += len(result.Logs)
	s.TotalMetrics += len(result.Metrics)
	s.TotalSpans += len(result.Spans)

	switch status {
	case "new":
		s.NewFiles++
	case "modified":
		s.ModifiedFiles++
	case "skipped":
		s.SkippedFiles++
	}
}

// AddError records an error for a file
func (s *ImportSummary) AddError(filePath string, err error) {
	s.Errors = append(s.Errors, ImportError{
		FilePath: filePath,
		Error:    err.Error(),
	})
}

// IsEmpty returns true if there's nothing to import
func (s *ImportSummary) IsEmpty() bool {
	return s.TotalLogs == 0 && s.TotalMetrics == 0 && s.TotalSpans == 0
}
