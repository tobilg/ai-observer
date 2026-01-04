package storage

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

const indexTraces = `
CREATE INDEX IF NOT EXISTS idx_traces_timestamp ON otel_traces(Timestamp);
CREATE INDEX IF NOT EXISTS idx_traces_trace_id ON otel_traces(TraceId);
CREATE INDEX IF NOT EXISTS idx_traces_service_name ON otel_traces(ServiceName);
`

const indexLogs = `
CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON otel_logs(Timestamp);
CREATE INDEX IF NOT EXISTS idx_logs_severity ON otel_logs(SeverityNumber);
CREATE INDEX IF NOT EXISTS idx_logs_trace_id ON otel_logs(TraceId);
CREATE INDEX IF NOT EXISTS idx_logs_service_name ON otel_logs(ServiceName);
`

const indexMetrics = `
CREATE INDEX IF NOT EXISTS idx_metrics_timestamp ON otel_metrics(Timestamp);
CREATE INDEX IF NOT EXISTS idx_metrics_name ON otel_metrics(MetricName);
CREATE INDEX IF NOT EXISTS idx_metrics_type ON otel_metrics(MetricType);
CREATE INDEX IF NOT EXISTS idx_metrics_service_name ON otel_metrics(ServiceName);
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
CREATE INDEX IF NOT EXISTS idx_dashboard_widgets_dashboard_id ON dashboard_widgets(dashboard_id);
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
