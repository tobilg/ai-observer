package handlers

import (
	"context"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/traceview"
	"github.com/tobilg/ai-observer/internal/transcript"
)

// Store is the storage the handlers read and write: storage.DuckDBStore.
// It lists exactly the methods the handlers call, so tests can fail single
// calls with a fake (embed Store in a struct and override what the test
// needs).
type Store interface {
	ingestStore
	queryStore
	dashboardStore
	statusStore
	// GET /api/traces/{id}
	traceview.Store
	// GET /api/sessions/{id}/transcript (through transcript.Cache)
	transcript.GenerationStore
}

// ingestStore stores received OTLP telemetry (otlp_ingest.go)
type ingestStore interface {
	InsertTelemetry(ctx context.Context, logs []api.LogRecord, metrics []api.MetricDataPoint, spans []api.Span) error
	InsertTraces(ctx context.Context, spans []api.Span, derive func([]api.Span) []api.MetricDataPoint) ([]api.Span, []api.MetricDataPoint, error)
	InsertMetrics(ctx context.Context, metrics []api.MetricDataPoint) error
}

// queryStore answers the query API (query.go)
type queryStore interface {
	QueryTraces(ctx context.Context, filter storage.TraceFilter, from, to time.Time, page storage.Page) (*api.TracesResponse, error)
	GetRecentTraces(ctx context.Context, limit int, from, to time.Time) (*api.TracesResponse, error)

	QueryMetrics(ctx context.Context, service, metricName, metricType string, from, to time.Time, page storage.Page) (*api.MetricsResponse, error)
	GetMetricNames(ctx context.Context, service string) ([]string, error)
	GetBreakdownValues(ctx context.Context, metricName, attribute, service string) ([]string, error)
	GetMetricAttributeKeys(ctx context.Context, metricName, service string) ([]string, error)
	QuerySeries(ctx context.Context, q api.MetricQuery, r storage.SeriesRange) (*api.TimeSeriesResponse, error)
	QuerySeriesBatch(ctx context.Context, queries []api.MetricQuery, r storage.SeriesRange) *api.BatchMetricSeriesResponse
	ValidateTimeZone(name string) error

	QueryLogs(ctx context.Context, service, severity, traceID, search string, from, to time.Time, page storage.Page) (*api.LogsResponse, error)
	GetLogLevelsInRange(ctx context.Context, r storage.TimeRange) (map[string]int64, error)

	QuerySessions(ctx context.Context, service string, from, to time.Time, page storage.Page) (*api.SessionsResponse, error)
	SearchSessionsPage(ctx context.Context, terms []string, service string, from, to time.Time, page storage.Page) (*api.SessionsResponse, error)

	GetServices(ctx context.Context) ([]string, error)
	GetStats(ctx context.Context) (*api.StatsResponse, error)
	GetStatsInRange(ctx context.Context, r storage.TimeRange) (*api.StatsResponse, error)
}

// dashboardStore keeps dashboards and their widgets (dashboard.go)
type dashboardStore interface {
	CreateDashboard(ctx context.Context, req *api.CreateDashboardRequest) (*api.Dashboard, error)
	GetDashboards(ctx context.Context) ([]api.Dashboard, error)
	GetOrCreateDefaultDashboard(ctx context.Context) (*api.DashboardWithWidgets, bool, error)
	GetDashboardWithWidgets(ctx context.Context, id string) (*api.DashboardWithWidgets, error)
	UpdateDashboard(ctx context.Context, id string, req *api.UpdateDashboardRequest) (*api.Dashboard, error)
	DeleteDashboard(ctx context.Context, id string) error
	SetDefaultDashboard(ctx context.Context, id string) error
	CreateWidget(ctx context.Context, dashboardID string, req *api.CreateWidgetRequest) (*api.DashboardWidget, error)
	UpdateWidget(ctx context.Context, dashboardID, widgetID string, req *api.UpdateWidgetRequest) (*api.DashboardWidget, error)
	UpdateWidgetPositions(ctx context.Context, dashboardID string, positions []api.WidgetPosition) error
	DeleteWidget(ctx context.Context, dashboardID, widgetID string) error
}

// statusStore describes the database for GET /api/status and GET /ready
// (status.go, health.go)
type statusStore interface {
	TelemetryBounds(ctx context.Context) (oldest, newest time.Time, err error)
	ImportSummaries(ctx context.Context) ([]storage.ImportSummary, error)
	DatabaseSize(ctx context.Context) (storage.DatabaseSize, error)
	Ping(ctx context.Context) error
}

var _ Store = (*storage.DuckDBStore)(nil)
