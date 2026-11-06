package deleter

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/storage"
)

// Scope defines what data types to delete
type Scope string

const (
	ScopeLogs    Scope = "logs"
	ScopeMetrics Scope = "metrics"
	ScopeTraces  Scope = "traces"
	ScopeAll     Scope = "all"
)

// Options configures the delete operation
type Options struct {
	Scope       Scope
	From        time.Time
	To          time.Time
	Service     string // Optional filter by service name
	SkipConfirm bool   // Skip confirmation prompt (--yes flag)

	// Out receives summaries and prompts and In the confirmation answer;
	// nil means stdout and stdin
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

// Summary contains the results of a delete operation
type Summary struct {
	LogCount    int64
	MetricCount int64
	TraceCount  int64
	SpanCount   int64
}

// Preview returns a summary of what would be deleted without actually deleting
func Preview(ctx context.Context, store *storage.DuckDBStore, opts Options) (*Summary, error) {
	summary := &Summary{}

	switch opts.Scope {
	case ScopeLogs:
		count, err := store.CountLogsInRange(ctx, opts.From, opts.To, opts.Service)
		if err != nil {
			return nil, fmt.Errorf("counting logs: %w", err)
		}
		summary.LogCount = count

	case ScopeMetrics:
		count, err := store.CountMetricsInRange(ctx, opts.From, opts.To, opts.Service)
		if err != nil {
			return nil, fmt.Errorf("counting metrics: %w", err)
		}
		summary.MetricCount = count

	case ScopeTraces:
		traces, spans, err := store.CountTracesInRange(ctx, opts.From, opts.To, opts.Service)
		if err != nil {
			return nil, fmt.Errorf("counting traces: %w", err)
		}
		summary.TraceCount = traces
		summary.SpanCount = spans

	case ScopeAll:
		storageSummary, err := store.CountAllInRange(ctx, opts.From, opts.To, opts.Service)
		if err != nil {
			return nil, fmt.Errorf("counting all: %w", err)
		}
		summary.LogCount = storageSummary.LogCount
		summary.MetricCount = storageSummary.MetricCount
		summary.TraceCount = storageSummary.TraceCount
		summary.SpanCount = storageSummary.SpanCount

	default:
		return nil, fmt.Errorf("unknown scope: %s", opts.Scope)
	}

	return summary, nil
}

// Execute performs the actual deletion
func Execute(ctx context.Context, store *storage.DuckDBStore, opts Options) (*Summary, error) {
	summary := &Summary{}

	switch opts.Scope {
	case ScopeLogs:
		count, err := store.DeleteLogsInRange(ctx, opts.From, opts.To, opts.Service)
		if err != nil {
			return nil, fmt.Errorf("deleting logs: %w", err)
		}
		summary.LogCount = count

	case ScopeMetrics:
		count, err := store.DeleteMetricsInRange(ctx, opts.From, opts.To, opts.Service)
		if err != nil {
			return nil, fmt.Errorf("deleting metrics: %w", err)
		}
		summary.MetricCount = count

	case ScopeTraces:
		count, err := store.DeleteTracesInRange(ctx, opts.From, opts.To, opts.Service)
		if err != nil {
			return nil, fmt.Errorf("deleting traces: %w", err)
		}
		summary.SpanCount = count

	case ScopeAll:
		storageSummary, err := store.DeleteAllInRange(ctx, opts.From, opts.To, opts.Service)
		if err != nil {
			return nil, fmt.Errorf("deleting all: %w", err)
		}
		summary.LogCount = storageSummary.LogCount
		summary.MetricCount = storageSummary.MetricCount
		summary.SpanCount = storageSummary.SpanCount

	default:
		return nil, fmt.Errorf("unknown scope: %s", opts.Scope)
	}

	return summary, nil
}

// PrintSummary prints the delete summary to opts.Out
func PrintSummary(summary *Summary, opts Options) {
	out := opts.out()
	fmt.Fprintln(out, "Delete Summary")
	fmt.Fprintln(out, "==============")
	fmt.Fprintf(out, "Time range: %s to %s\n", opts.From.Format("2006-01-02"), opts.To.Format("2006-01-02"))

	if opts.Service != "" {
		fmt.Fprintf(out, "Service: %s\n", opts.Service)
	} else {
		fmt.Fprintln(out, "Service: all")
	}

	fmt.Fprintln(out)
	fmt.Fprintln(out, "Records to be deleted:")

	switch opts.Scope {
	case ScopeLogs:
		fmt.Fprintf(out, "  Logs:    %d\n", summary.LogCount)
	case ScopeMetrics:
		fmt.Fprintf(out, "  Metrics: %d\n", summary.MetricCount)
	case ScopeTraces:
		fmt.Fprintf(out, "  Traces:  %d (spans: %d)\n", summary.TraceCount, summary.SpanCount)
	case ScopeAll:
		fmt.Fprintf(out, "  Logs:    %d\n", summary.LogCount)
		fmt.Fprintf(out, "  Metrics: %d\n", summary.MetricCount)
		fmt.Fprintf(out, "  Traces:  %d (spans: %d)\n", summary.TraceCount, summary.SpanCount)
	}
}

// PrintResult prints the deletion result to opts.Out
func PrintResult(summary *Summary, opts Options) {
	out := opts.out()
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Deletion complete:")

	switch opts.Scope {
	case ScopeLogs:
		fmt.Fprintf(out, "  Logs deleted:    %d\n", summary.LogCount)
	case ScopeMetrics:
		fmt.Fprintf(out, "  Metrics deleted: %d\n", summary.MetricCount)
	case ScopeTraces:
		fmt.Fprintf(out, "  Spans deleted:   %d\n", summary.SpanCount)
	case ScopeAll:
		fmt.Fprintf(out, "  Logs deleted:    %d\n", summary.LogCount)
		fmt.Fprintf(out, "  Metrics deleted: %d\n", summary.MetricCount)
		fmt.Fprintf(out, "  Spans deleted:   %d\n", summary.SpanCount)
	}
}

// ConfirmDelete asks for confirmation on opts.In
func ConfirmDelete(opts Options) bool {
	out := opts.out()
	fmt.Fprintln(out)
	fmt.Fprintln(out, "This action cannot be undone.")
	fmt.Fprint(out, "Continue? [y/N] ")

	reader := bufio.NewReader(opts.in())
	response, err := reader.ReadString('\n')
	if err != nil {
		return false
	}

	response = strings.TrimSpace(strings.ToLower(response))
	return response == "y" || response == "yes"
}

// IsEmpty returns true if the summary has no records to delete
func (s *Summary) IsEmpty() bool {
	return s.LogCount == 0 && s.MetricCount == 0 && s.TraceCount == 0 && s.SpanCount == 0
}

// Run executes the full delete workflow with preview and confirmation
func Run(ctx context.Context, store *storage.DuckDBStore, opts Options) error {
	// Preview what would be deleted
	summary, err := Preview(ctx, store, opts)
	if err != nil {
		return fmt.Errorf("preview failed: %w", err)
	}

	// Check if there's anything to delete
	if summary.IsEmpty() {
		fmt.Fprintln(opts.out(), "No records found in the specified time range.")
		return nil
	}

	// Print summary
	PrintSummary(summary, opts)

	// Ask for confirmation unless --yes flag is set
	if !opts.SkipConfirm {
		if !ConfirmDelete(opts) {
			fmt.Fprintln(opts.out(), "Aborted.")
			return nil
		}
	}

	// Execute deletion
	result, err := Execute(ctx, store, opts)
	if err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}

	// Print result
	PrintResult(result, opts)

	// Deleting keeps the import state, so a plain import skips the files
	fmt.Fprintln(opts.out(), "\nImported session files stay marked as imported. To restore their records,")
	fmt.Fprintln(opts.out(), "run `ai-observer import <tool> --force`: it replaces each file's remaining records.")

	return nil
}

// ParseScope converts a string to a Scope
func ParseScope(s string) (Scope, error) {
	switch strings.ToLower(s) {
	case "logs":
		return ScopeLogs, nil
	case "metrics":
		return ScopeMetrics, nil
	case "traces":
		return ScopeTraces, nil
	case "all":
		return ScopeAll, nil
	default:
		return "", fmt.Errorf("invalid scope: %s (valid: logs, metrics, traces, all)", s)
	}
}
