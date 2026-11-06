package storage

import (
	"context"
	"fmt"
	"strings"
)

const schemaTraces = `
CREATE TABLE IF NOT EXISTS otel_traces (
    Timestamp               TIMESTAMP NOT NULL,
    TraceId                 VARCHAR NOT NULL,
    SpanId                  VARCHAR NOT NULL,
    ParentSpanId            VARCHAR,
    TraceState              VARCHAR,
    SpanName                VARCHAR NOT NULL,
    SpanKind                VARCHAR,
    ServiceName             VARCHAR NOT NULL,
    ResourceAttributes      JSON,
    ScopeName               VARCHAR,
    ScopeVersion            VARCHAR,
    SpanAttributes          JSON,
    Duration                BIGINT,
    StatusCode              VARCHAR,
    StatusMessage           VARCHAR,
    "Events.Timestamp"      JSON,
    "Events.Name"           JSON,
    "Events.Attributes"     JSON,
    "Links.TraceId"         JSON,
    "Links.SpanId"          JSON,
    "Links.TraceState"      JSON,
    "Links.Attributes"      JSON
);
`

const schemaLogs = `
CREATE TABLE IF NOT EXISTS otel_logs (
    Timestamp               TIMESTAMP NOT NULL,
    TraceId                 VARCHAR,
    SpanId                  VARCHAR,
    TraceFlags              UINTEGER,
    SeverityText            VARCHAR,
    SeverityNumber          INTEGER,
    ServiceName             VARCHAR NOT NULL,
    Body                    VARCHAR,
    ResourceSchemaUrl       VARCHAR,
    ResourceAttributes      JSON,
    ScopeSchemaUrl          VARCHAR,
    ScopeName               VARCHAR,
    ScopeVersion            VARCHAR,
    ScopeAttributes         JSON,
    LogAttributes           JSON
);
`

const schemaMetrics = `
CREATE TABLE IF NOT EXISTS otel_metrics (
    Timestamp               TIMESTAMP NOT NULL,
    ServiceName             VARCHAR NOT NULL,
    MetricName              VARCHAR NOT NULL,
    MetricDescription       VARCHAR,
    MetricUnit              VARCHAR,
    ResourceAttributes      JSON,
    ScopeName               VARCHAR,
    ScopeVersion            VARCHAR,
    Attributes              JSON,
    MetricType              VARCHAR NOT NULL,
    Value                   DOUBLE,
    AggregationTemporality  INTEGER,
    IsMonotonic             BOOLEAN,
    Count                   UBIGINT,
    Sum                     DOUBLE,
    BucketCounts            JSON,
    ExplicitBounds          JSON,
    Scale                   INTEGER,
    ZeroCount               UBIGINT,
    PositiveOffset          INTEGER,
    PositiveBucketCounts    JSON,
    NegativeOffset          INTEGER,
    NegativeBucketCounts    JSON,
    QuantileValues          JSON,
    QuantileQuantiles       JSON,
    Min                     DOUBLE,
    Max                     DOUBLE
);
`

// Telemetry tables only index the IDs used for selective point lookups.
// Time-range and service filters are served by DuckDB's automatic min-max
// zonemaps; ART indexes on those columns gave no measurable query speedup but
// slowed inserts by ~25% and made the database ~13x larger (260 MB vs 20 MB for
// 950k rows). Indexes that earlier versions created are dropped on startup.
const indexTraces = `
DROP INDEX IF EXISTS idx_traces_trace_id;
DROP INDEX IF EXISTS idx_traces_trace_timestamp;
DROP INDEX IF EXISTS idx_traces_service_trace_span;
DROP INDEX IF EXISTS idx_traces_trace_span;
DROP INDEX IF EXISTS idx_traces_timestamp;
DROP INDEX IF EXISTS idx_traces_service_name;
DROP INDEX IF EXISTS idx_traces_service_timestamp;
DROP INDEX IF EXISTS idx_traces_service_span_name;
DROP INDEX IF EXISTS idx_traces_trace_timestamp_v2;
DROP INDEX IF EXISTS idx_traces_service_trace_span_v2;
DROP INDEX IF EXISTS idx_traces_trace_span_v2;

CREATE INDEX IF NOT EXISTS idx_traces_trace_id_v2 ON otel_traces(TraceId);
CREATE INDEX IF NOT EXISTS idx_traces_span_id ON otel_traces(SpanId);
`

const indexLogs = `
DROP INDEX IF EXISTS idx_logs_timestamp;
DROP INDEX IF EXISTS idx_logs_severity;
DROP INDEX IF EXISTS idx_logs_service_name;
DROP INDEX IF EXISTS idx_logs_service_timestamp;
DROP INDEX IF EXISTS idx_logs_trace_timestamp;
DROP INDEX IF EXISTS idx_logs_severity_timestamp;

CREATE INDEX IF NOT EXISTS idx_logs_trace_id ON otel_logs(TraceId);
`

const indexMetrics = `
DROP INDEX IF EXISTS idx_metrics_timestamp;
DROP INDEX IF EXISTS idx_metrics_name;
DROP INDEX IF EXISTS idx_metrics_type;
DROP INDEX IF EXISTS idx_metrics_service_name;
DROP INDEX IF EXISTS idx_metrics_name_service_timestamp;
DROP INDEX IF EXISTS idx_metrics_service_timestamp;
DROP INDEX IF EXISTS idx_metrics_name_timestamp;
`

// metric_names lists the stored metrics (addMetricNames). It is part of the
// base schema because metric inserts write it, including those of migrations.
const schemaMetricNames = `
CREATE TABLE IF NOT EXISTS metric_names (
    ServiceName VARCHAR NOT NULL,
    MetricName  VARCHAR NOT NULL,
    PRIMARY KEY (ServiceName, MetricName)
);
`

const schemaDashboards = `
CREATE TABLE IF NOT EXISTS dashboards (
    id              VARCHAR PRIMARY KEY,
    name            VARCHAR NOT NULL,
    description     VARCHAR,
    is_default      BOOLEAN DEFAULT FALSE,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`

const schemaDashboardWidgets = `
CREATE TABLE IF NOT EXISTS dashboard_widgets (
    id              VARCHAR PRIMARY KEY,
    dashboard_id    VARCHAR NOT NULL,
    widget_type     VARCHAR NOT NULL,
    title           VARCHAR NOT NULL,
    grid_column     INTEGER NOT NULL,
    grid_row        INTEGER NOT NULL,
    col_span        INTEGER NOT NULL DEFAULT 1,
    row_span        INTEGER NOT NULL DEFAULT 1,
    config          JSON,
    created_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`

const indexDashboards = `
CREATE INDEX IF NOT EXISTS idx_dashboards_is_default ON dashboards(is_default);

DROP INDEX IF EXISTS idx_dashboard_widgets_dashboard_id;

CREATE INDEX IF NOT EXISTS idx_dashboard_widgets_dashboard_id_v2 ON dashboard_widgets(dashboard_id);
`

const schemaImportState = `
CREATE TABLE IF NOT EXISTS import_state (
    source          VARCHAR NOT NULL,
    file_path       VARCHAR NOT NULL,
    file_hash       VARCHAR NOT NULL,
    imported_at     TIMESTAMP NOT NULL,
    record_count    INTEGER NOT NULL,
    PRIMARY KEY (source, file_path)
);
`

const indexImportState = `
CREATE INDEX IF NOT EXISTS idx_import_state_source ON import_state(source);
`

const migrateImportStateForWatch = `
ALTER TABLE import_state ADD COLUMN IF NOT EXISTS byte_offset BIGINT DEFAULT 0;
ALTER TABLE import_state ADD COLUMN IF NOT EXISTS message_count INTEGER DEFAULT 0;
ALTER TABLE import_state ADD COLUMN IF NOT EXISTS parser_state VARCHAR;
`

// The statements above are re-run on every startup and must stay idempotent.
// Changes that are not, or that rewrite existing data, are migrations below.

const schemaMigrations = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     INTEGER PRIMARY KEY,
    name        VARCHAR NOT NULL,
    applied_at  TIMESTAMP NOT NULL
);
`

// migration is a one-time schema or data change. Migrations run in version
// order, each in its own transaction, and are recorded in schema_migrations so
// that every database applies each of them exactly once. Never edit or reorder
// a released migration; add a new one instead.
type migration struct {
	version int
	name    string
	stmts   []string
	// run, if set, runs after stmts in the same transaction, for changes
	// that need Go code
	run func(ctx context.Context, w *writeTx) error
}

// migration1SessionIDKeys are the session ID attribute keys migration 1
// backfilled from. They are a literal copy of tools.SessionIDKeys at the time,
// so that the released migration does not change when that list does.
var migration1SessionIDKeys = []string{
	"session.id",
	"conversation.id",
	"gen_ai.conversation.id",
	"copilot_chat.session_id",
	"copilot_chat.chat_session_id",
}

// migration1BackfillSQL fills SessionId and SessionIds of rows stored before
// the columns existed from the attribute keys, in priority order. It is part
// of migration 1 and must not change: it deliberately does not share
// attributeValueExpr with the queries, which may change.
func migration1BackfillSQL(table, attrsColumn string, keys []string) string {
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, fmt.Sprintf(`NULLIF(json_extract_string(%s, '$."%s"'), '')`, attrsColumn, key))
	}
	primary := "COALESCE(" + strings.Join(values, ", ") + ")"
	return fmt.Sprintf(`UPDATE %s SET SessionId = %s, SessionIds = list_distinct([%s]) WHERE json_valid(%s) AND %s IS NOT NULL`,
		table, primary, strings.Join(values, ", "), attrsColumn, primary)
}

var migrations = []migration{
	{
		version: 1,
		name:    "session id columns",
		stmts: []string{
			`ALTER TABLE otel_logs ADD COLUMN IF NOT EXISTS SessionId VARCHAR`,
			`ALTER TABLE otel_logs ADD COLUMN IF NOT EXISTS SessionIds VARCHAR[]`,
			`ALTER TABLE otel_traces ADD COLUMN IF NOT EXISTS SessionId VARCHAR`,
			`ALTER TABLE otel_traces ADD COLUMN IF NOT EXISTS SessionIds VARCHAR[]`,
			migration1BackfillSQL("otel_logs", "LogAttributes", migration1SessionIDKeys),
			migration1BackfillSQL("otel_traces", "SpanAttributes", migration1SessionIDKeys),
		},
	},
	{
		// Inserts skip spans that are already stored. Remove the duplicates
		// that earlier versions stored, so queries need not deduplicate.
		version: 2,
		name:    "deduplicate spans",
		stmts: []string{`
			DELETE FROM otel_traces WHERE rowid IN (
				SELECT rowid FROM (
					SELECT rowid, ROW_NUMBER() OVER (
						PARTITION BY ServiceName, TraceId, SpanId
						ORDER BY Timestamp DESC
					) AS rn
					FROM otel_traces
				)
				WHERE rn > 1
			)`,
		},
	},
	{
		// SourceFile links imported and watched records to their session
		// file, so a re-import replaces them instead of adding them again.
		// Records stored earlier have none: import_state.source_tracked is
		// false for their files. file_size and file_mtime_ns let imports skip
		// hashing unchanged files.
		version: 3,
		name:    "source file columns",
		stmts: []string{
			`ALTER TABLE otel_logs ADD COLUMN IF NOT EXISTS SourceFile VARCHAR`,
			`ALTER TABLE otel_metrics ADD COLUMN IF NOT EXISTS SourceFile VARCHAR`,
			`ALTER TABLE otel_traces ADD COLUMN IF NOT EXISTS SourceFile VARCHAR`,
			`ALTER TABLE import_state ADD COLUMN IF NOT EXISTS file_size BIGINT DEFAULT 0`,
			`ALTER TABLE import_state ADD COLUMN IF NOT EXISTS file_mtime_ns BIGINT DEFAULT 0`,
			`ALTER TABLE import_state ADD COLUMN IF NOT EXISTS source_tracked BOOLEAN DEFAULT false`,
		},
	},
	{
		// Gemini CLI reports cumulative token counters. Earlier versions
		// derived ".delta" series from them at ingestion, which queries now
		// compute from the counters (storage.deltaAliases). The cost of every
		// running total was stored as a per-interval amount, multiplying it
		// by the number of exports; migration 5 recomputes it.
		version: 4,
		name:    "gemini cost from cumulative tokens",
		stmts: []string{`
			DELETE FROM otel_metrics WHERE MetricName IN (
				'gemini_cli.token.usage.delta',
				'gemini_cli.api.request.count.delta',
				'gemini_cli.file.operation.count.delta',
				'gemini_cli.session.count.delta'
			)`,
		},
	},
	{
		// Gemini's input tokens include the cached ones, which earlier
		// versions charged at the input rate on top of the cache rate.
		// Imports named two token types differently from Gemini CLI's own
		// telemetry ("cached", "thoughts"). The costs are recomputed from the
		// token points with the pricing tables of this version.
		version: 5,
		name:    "gemini cost from uncached input",
		stmts: []string{
			`UPDATE otel_metrics SET Attributes = json_merge_patch(Attributes, '{"type": "cache"}')
			WHERE MetricName = 'gemini_cli.token.usage' AND json_extract_string(Attributes, '$.type') = 'cached'`,
			`UPDATE otel_metrics SET Attributes = json_merge_patch(Attributes, '{"type": "thought"}')
			WHERE MetricName = 'gemini_cli.token.usage' AND json_extract_string(Attributes, '$.type') = 'thoughts'`,
		},
		run: rebuildGeminiCost,
	},
	{
		// import_state.partial marks files of which an import stored only the
		// records in a date range (ImportState.Partial)
		version: 6,
		name:    "partial imports",
		stmts: []string{
			`ALTER TABLE import_state ADD COLUMN IF NOT EXISTS partial BOOLEAN DEFAULT false`,
		},
	},
	{
		// metric_baselines keeps the last deleted point of each cumulative
		// series, so the next stored point is not counted as an increase of
		// its whole running total (deleteCumulativeBaselines)
		version: 7,
		name:    "cumulative metric baselines",
		stmts: []string{`
			CREATE TABLE IF NOT EXISTS metric_baselines (
				ServiceName        VARCHAR NOT NULL,
				MetricName         VARCHAR NOT NULL,
				Attributes         VARCHAR NOT NULL,
				ResourceAttributes VARCHAR NOT NULL,
				Timestamp          TIMESTAMP NOT NULL,
				Value              DOUBLE NOT NULL,
				PRIMARY KEY (ServiceName, MetricName, Attributes, ResourceAttributes)
			)`,
		},
	},
	{
		// metric_names (base schema) lists the stored metrics, so listing
		// them does not scan all metric points; fill it with the metrics
		// stored before
		version: 8,
		name:    "metric names",
		stmts: []string{
			`INSERT OR IGNORE INTO metric_names SELECT DISTINCT ServiceName, MetricName FROM otel_metrics`,
		},
	},
}
