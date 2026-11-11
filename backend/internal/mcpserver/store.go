package mcpserver

import (
	"context"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/redact"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/traceview"
	"github.com/tobilg/ai-observer/internal/transcript"
)

// Store is the storage the tools read: storage.DuckDBStore. It lists
// exactly the methods the tools call, so tests can fail single calls with a
// fake (embed Store in a struct and override what the test needs).
type Store interface {
	// Redactor redacts the tools' text again (data stored before redaction)
	Redactor() *redact.Redactor

	SearchSessions(ctx context.Context, terms []string, service string, from, to time.Time, limit int) ([]storage.SessionMatch, error)
	QuerySessions(ctx context.Context, service string, from, to time.Time, page storage.Page) (*api.SessionsResponse, error)
	QueryLogs(ctx context.Context, service, severity, traceID, search string, from, to time.Time, page storage.Page) (*api.LogsResponse, error)
	QueryTraces(ctx context.Context, filter storage.TraceFilter, from, to time.Time, page storage.Page) (*api.TracesResponse, error)

	GetMetricNames(ctx context.Context, service string) ([]string, error)
	QuerySeries(ctx context.Context, q api.MetricQuery, r storage.SeriesRange) (*api.TimeSeriesResponse, error)
	QueryMetricTotals(ctx context.Context, metricName, service string, from, to time.Time, groupBy string) (*api.TimeSeriesResponse, error)
	ValidateTimeZone(name string) error
	LocalTimeZone() string

	// get_trace
	traceview.Store
	// get_transcript and the transcript resource (through transcript.Cache)
	transcript.GenerationStore
}

var _ Store = (*storage.DuckDBStore)(nil)
