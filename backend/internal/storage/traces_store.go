package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func (s *DuckDBStore) InsertSpans(ctx context.Context, spans []api.Span) error {
	if len(spans) == 0 {
		return nil
	}

	return s.inWriteTx(ctx, func(w *writeTx) error {
		return insertSpansTx(ctx, w, spans, "")
	})
}

var spanColumns = []string{
	"Timestamp", "TraceId", "SpanId", "ParentSpanId", "TraceState",
	"SpanName", "SpanKind", "ServiceName", "ResourceAttributes",
	"ScopeName", "ScopeVersion", "SpanAttributes", "Duration",
	"StatusCode", "StatusMessage",
	"Events.Timestamp", "Events.Name", "Events.Attributes",
	"Links.TraceId", "Links.SpanId", "Links.TraceState", "Links.Attributes",
	"SessionId", "SessionIds", "SourceFile",
}

// storedSpanKey identifies a span for deduplication
type storedSpanKey struct {
	serviceName, traceID, spanID string
}

// spanLookupBatchSize bounds the number of span IDs per duplicate lookup query
const spanLookupBatchSize = 500

// insertSpansTx inserts spans that are not stored yet. Exporters retry and
// importers re-read files, so the same span can arrive more than once.
func insertSpansTx(ctx context.Context, w *writeTx, spans []api.Span, sourceFile string) error {
	_, err := insertNewSpansTx(ctx, w, spans, sourceFile)
	return err
}

// insertNewSpansTx inserts the spans that are not stored yet and returns them
func insertNewSpansTx(ctx context.Context, w *writeTx, spans []api.Span, sourceFile string) ([]api.Span, error) {
	if len(spans) == 0 {
		return nil, nil
	}

	seen, err := existingSpanKeys(ctx, w.tx, spans)
	if err != nil {
		return nil, err
	}
	newSpans := make([]api.Span, 0, len(spans))
	for _, span := range spans {
		key := storedSpanKey{span.ServiceName, span.TraceID, span.SpanID}
		if seen[key] {
			continue
		}
		seen[key] = true // also drops duplicates within the batch
		newSpans = append(newSpans, span)
	}

	err = w.appendRows(ctx, "otel_traces", spanColumns, len(newSpans), func(i int) []driver.Value {
		return append(spanRow(newSpans[i], w), nullableString(sourceFile))
	})
	if err != nil {
		return nil, err
	}
	return newSpans, nil
}

// existingSpanKeys returns the keys of already stored spans among spans. It
// looks spans up by their IDs (idx_traces_span_id), one query per batch,
// rather than reading whole traces: Codex CLI sends one trace per session, so
// every export would read the session's spans again.
func existingSpanKeys(ctx context.Context, tx *sql.Tx, spans []api.Span) (map[storedSpanKey]bool, error) {
	var spanIDs []any
	seenSpans := make(map[string]bool)
	for _, span := range spans {
		if !seenSpans[span.SpanID] {
			seenSpans[span.SpanID] = true
			spanIDs = append(spanIDs, span.SpanID)
		}
	}

	existing := make(map[storedSpanKey]bool)
	for start := 0; start < len(spanIDs); start += spanLookupBatchSize {
		batch := spanIDs[start:min(start+spanLookupBatchSize, len(spanIDs))]
		query := "SELECT ServiceName, TraceId, SpanId FROM otel_traces WHERE SpanId IN (?" +
			strings.Repeat(", ?", len(batch)-1) + ")"

		rows, err := tx.QueryContext(ctx, query, batch...)
		if err != nil {
			return nil, fmt.Errorf("checking duplicate spans: %w", err)
		}
		for rows.Next() {
			var key storedSpanKey
			if err := rows.Scan(&key.serviceName, &key.traceID, &key.spanID); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scanning duplicate span: %w", err)
			}
			existing[key] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("checking duplicate spans: %w", err)
		}
		rows.Close()
	}
	return existing, nil
}

// spanRow converts a span into otel_traces column values (spanColumns
// without the trailing SourceFile). Its text is redacted with w's redactor;
// session IDs are read before.
func spanRow(span api.Span, w *writeTx) []driver.Value {
	r := w.redact
	sessionID, sessionIDs := sessionIDColumns(span.SpanAttributes)
	span.SpanAttributes = r.Map(span.SpanAttributes)
	span.ResourceAttributes = w.redactShared(span.ResourceAttributes)
	span.StatusMessage = r.String(span.StatusMessage)

	eventTimestamps := make([]time.Time, len(span.Events))
	eventNames := make([]string, len(span.Events))
	eventAttributes := make([]map[string]string, len(span.Events))
	for i, e := range span.Events {
		eventTimestamps[i] = e.Timestamp
		eventNames[i] = e.Name
		eventAttributes[i] = r.Map(e.Attributes)
	}

	linkTraceIDs := make([]string, len(span.Links))
	linkSpanIDs := make([]string, len(span.Links))
	linkTraceStates := make([]string, len(span.Links))
	linkAttributes := make([]map[string]string, len(span.Links))
	for i, l := range span.Links {
		linkTraceIDs[i] = l.TraceID
		linkSpanIDs[i] = l.SpanID
		linkTraceStates[i] = l.TraceState
		linkAttributes[i] = r.Map(l.Attributes)
	}

	return []driver.Value{
		span.Timestamp,
		span.TraceID,
		span.SpanID,
		nullableString(span.ParentSpanID),
		nullableString(span.TraceState),
		span.SpanName,
		nullableString(span.SpanKind),
		span.ServiceName,
		rawJSON(mapToString(span.ResourceAttributes)),
		nullableString(span.ScopeName),
		nullableString(span.ScopeVersion),
		rawJSON(mapToString(span.SpanAttributes)),
		span.Duration,
		nullableString(span.StatusCode),
		nullableString(span.StatusMessage),
		rawJSON(timestampArrayToString(eventTimestamps)),
		rawJSON(stringArrayToString(eventNames)),
		rawJSON(mapArrayToString(eventAttributes)),
		rawJSON(stringArrayToString(linkTraceIDs)),
		rawJSON(stringArrayToString(linkSpanIDs)),
		rawJSON(stringArrayToString(linkTraceStates)),
		rawJSON(mapArrayToString(linkAttributes)),
		sessionID,
		sessionIDs,
	}
}

// QueryTraces returns a page of the traces with spans in [from, to],
// newest first
func (s *DuckDBStore) QueryTraces(ctx context.Context, service, search string, from, to time.Time, page Page) (*api.TracesResponse, error) {
	return s.queryRawTraceOverviews(ctx, service, search, from, to, page, true)
}

// queryRawTraceOverviews queries all services as normal OTLP traces,
// including Codex. Without includeCount, Total is the number of traces
// returned.
func (s *DuckDBStore) queryRawTraceOverviews(ctx context.Context, service, search string, from, to time.Time, page Page, includeCount bool) (*api.TracesResponse, error) {
	var spanWhere conditions
	spanWhere.add("Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP", formatTimeForDB(from), formatTimeForDB(to))
	if service != "" {
		spanWhere.add("ServiceName = ?", service)
	}

	// Spans are unique per (ServiceName, TraceId, SpanId): inserts skip stored
	// spans and migration 2 removed older duplicates. The attribute columns are
	// only read when searching.
	columns := "TraceId, SpanId, SpanName, ServiceName, Timestamp, Duration, StatusCode"
	ctes := conditions{}
	joinMatching := ""
	if search != "" {
		columns += ", StatusMessage, SpanAttributes, ResourceAttributes"
		cond, args := containsCondition([]string{
			"SpanName", "ServiceName", "COALESCE(StatusMessage, '')",
			"COALESCE(CAST(SpanAttributes AS VARCHAR), '')", "COALESCE(CAST(ResourceAttributes AS VARCHAR), '')",
		}, search)
		ctes.add(`matching_traces AS (
			SELECT DISTINCT TraceId
			FROM spans
			WHERE `+cond+`
		)`, args...)
		joinMatching = "JOIN matching_traces mt ON mt.TraceId = spans.TraceId"
	}
	with := "WITH spans AS (SELECT " + columns + " FROM otel_traces" + spanWhere.clause("WHERE") + ")"
	for _, cte := range ctes.sql {
		with += ",\n" + cte
	}
	withArgs := concatArgs(spanWhere.args, ctes.args)

	limit, offset, err := pageLimit(page)
	if err != nil {
		return nil, err
	}
	var having conditions
	if page.Cursor != "" {
		c, err := decodeCursor(page.Cursor, 1)
		if err != nil {
			return nil, err
		}
		cond, args := newestFirstAfter("MIN(spans.Timestamp)", []string{"spans.TraceId"}, c.Time, []any{c.Keys[0]})
		having.add(cond, args...)
	}

	query := with + `
		SELECT
			spans.TraceId as ID,
			'` + api.TraceKindOTelTrace + `' as Kind,
			spans.TraceId,
			FIRST(spans.SpanId ORDER BY spans.Timestamp ASC) as RootSpanId,
			FIRST(spans.SpanName ORDER BY spans.Timestamp ASC) as RootSpan,
			FIRST(spans.ServiceName ORDER BY spans.Timestamp ASC) as ServiceName,
			MIN(spans.Timestamp) as StartTime,
			CAST((MAX(epoch_ms(spans.Timestamp) + COALESCE(spans.Duration, 0)/1000000) - MIN(epoch_ms(spans.Timestamp))) * 1000000 AS BIGINT) as Duration,
			COUNT(*) as SpanCount,
			CASE WHEN SUM(CASE WHEN spans.StatusCode = 'ERROR' THEN 1 ELSE 0 END) > 0 THEN 'ERROR'
			     WHEN SUM(CASE WHEN spans.StatusCode = 'OK' THEN 1 ELSE 0 END) > 0 THEN 'OK'
			     ELSE 'UNSET' END as Status
		FROM spans
		` + joinMatching + `
		GROUP BY spans.TraceId` + having.clause("HAVING") + `
		ORDER BY StartTime DESC, spans.TraceId DESC
		LIMIT ? OFFSET ?`
	queryArgs := concatArgs(withArgs, having.args, []any{limit + 1, offset})

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying traces: %w", err)
	}
	defer rows.Close()

	var traces []api.TraceOverview
	for rows.Next() {
		var t api.TraceOverview
		if err := rows.Scan(&t.ID, &t.Kind, &t.TraceID, &t.RootSpanID, &t.RootSpan, &t.ServiceName, &t.StartTime, &t.Duration, &t.SpanCount, &t.Status); err != nil {
			return nil, fmt.Errorf("scanning trace: %w", err)
		}
		traces = append(traces, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating traces: %w", err)
	}

	resp := &api.TracesResponse{Traces: traces}
	if len(traces) > limit {
		resp.Traces = traces[:limit]
		resp.HasMore = true
		last := traces[limit-1]
		resp.NextCursor = encodeCursor(pageCursor{Time: formatTimeForDB(last.StartTime), Keys: []string{last.TraceID}})
	}
	if !includeCount {
		resp.Total = len(resp.Traces)
		return resp, nil
	}

	// Counted separately: a window count in the page query was not faster,
	// since the page query aggregates every trace in the range anyway
	countQuery := with + `
		SELECT COUNT(DISTINCT spans.TraceId)
		FROM spans
		` + joinMatching
	if err := s.db.QueryRowContext(ctx, countQuery, withArgs...).Scan(&resp.Total); err != nil {
		return nil, fmt.Errorf("counting traces: %w", err)
	}
	return resp, nil
}

// spanSelectColumns are the columns scanSpans reads
const spanSelectColumns = `
	Timestamp, TraceId, SpanId, ParentSpanId, TraceState,
	SpanName, SpanKind, ServiceName, ResourceAttributes,
	ScopeName, ScopeVersion, SpanAttributes, Duration,
	StatusCode, StatusMessage`

// TraceSpans returns the spans of a trace, oldest first. An ID that is no
// trace ID is looked up as a span ID, and that span's trace is returned.
func (s *DuckDBStore) TraceSpans(ctx context.Context, traceID string) ([]api.Span, error) {
	query := `SELECT` + spanSelectColumns + ` FROM otel_traces WHERE TraceId = ? ORDER BY Timestamp`
	spans, err := s.scanSpans(ctx, query, traceID)
	if err != nil || len(spans) > 0 {
		return spans, err
	}

	var resolvedTraceID string
	err = s.db.QueryRowContext(ctx, `
		SELECT TraceId
		FROM otel_traces
		WHERE SpanId = ?
		ORDER BY Timestamp
		LIMIT 1
	`, traceID).Scan(&resolvedTraceID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("resolving trace from span id: %w", err)
	}
	return s.scanSpans(ctx, query, resolvedTraceID)
}

// SpanTraceID returns the trace ID of a service's span, or "" when the
// service has no such span
func (s *DuckDBStore) SpanTraceID(ctx context.Context, service, spanID string) (string, error) {
	var traceID string
	err := s.db.QueryRowContext(ctx, `
		SELECT TraceId
		FROM otel_traces
		WHERE SpanId = ? AND ServiceName = ?
		LIMIT 1
	`, spanID, service).Scan(&traceID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return traceID, err
}

// ServiceTraceSpans returns the spans of a service's trace, oldest first
func (s *DuckDBStore) ServiceTraceSpans(ctx context.Context, service, traceID string) ([]api.Span, error) {
	query := `SELECT` + spanSelectColumns + ` FROM otel_traces WHERE ServiceName = ? AND TraceId = ? ORDER BY Timestamp`
	return s.scanSpans(ctx, query, service, traceID)
}

// scanSpans executes a query and scans the results into api.Span slice
func (s *DuckDBStore) scanSpans(ctx context.Context, query string, args ...interface{}) ([]api.Span, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying spans: %w", err)
	}
	defer rows.Close()

	var spans []api.Span
	for rows.Next() {
		var span api.Span
		var parentSpanID, traceState, spanKind, scopeName, scopeVersion, statusCode, statusMessage sql.NullString
		var resourceAttrs, spanAttrs interface{}

		if err := rows.Scan(
			&span.Timestamp, &span.TraceID, &span.SpanID, &parentSpanID, &traceState,
			&span.SpanName, &spanKind, &span.ServiceName, &resourceAttrs,
			&scopeName, &scopeVersion, &spanAttrs, &span.Duration,
			&statusCode, &statusMessage,
		); err != nil {
			return nil, fmt.Errorf("scanning span: %w", err)
		}

		span.ParentSpanID = parentSpanID.String
		span.TraceState = traceState.String
		span.SpanKind = spanKind.String
		span.ScopeName = scopeName.String
		span.ScopeVersion = scopeVersion.String
		span.StatusCode = statusCode.String
		span.StatusMessage = statusMessage.String
		span.ResourceAttributes = scanJSONToMap(resourceAttrs)
		span.SpanAttributes = scanJSONToMap(spanAttrs)

		spans = append(spans, span)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating spans: %w", err)
	}

	return spans, nil
}

func (s *DuckDBStore) GetServices(ctx context.Context) ([]string, error) {
	query := `
		SELECT DISTINCT ServiceName
		FROM (
			SELECT ServiceName FROM otel_traces
			UNION
			SELECT ServiceName FROM otel_logs
			UNION
			SELECT ServiceName FROM otel_metrics
		)
		ORDER BY ServiceName
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying services: %w", err)
	}
	defer rows.Close()

	var services []string
	for rows.Next() {
		var service string
		if err := rows.Scan(&service); err != nil {
			return nil, fmt.Errorf("scanning service: %w", err)
		}
		services = append(services, service)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating services: %w", err)
	}

	return services, nil
}

// GetRecentTraces returns the newest traces in [from, to], without paging
func (s *DuckDBStore) GetRecentTraces(ctx context.Context, limit int, from, to time.Time) (*api.TracesResponse, error) {
	resp, err := s.queryRawTraceOverviews(ctx, "", "", from, to, Page{Limit: limit}, false)
	if err != nil {
		return nil, err
	}
	resp.HasMore, resp.NextCursor = false, ""
	return resp, nil
}

// GetStats returns record counts over all stored telemetry
func (s *DuckDBStore) GetStats(ctx context.Context) (*api.StatsResponse, error) {
	return s.GetStatsInRange(ctx, TimeRange{})
}

// GetStatsInRange returns the record counts of the records in r. Services
// lists every service that sent telemetry, also outside r.
func (s *DuckDBStore) GetStatsInRange(ctx context.Context, r TimeRange) (*api.StatsResponse, error) {
	stats := &api.StatsResponse{}

	filter, filterArgs := r.filter("Timestamp")
	where := " WHERE true" + filter
	statsQuery := `
		SELECT
			(SELECT COUNT(*) FROM otel_traces` + where + `) as span_count,
			(SELECT COUNT(DISTINCT TraceId) FROM otel_traces` + where + `) as trace_count,
			(SELECT COUNT(*) FROM otel_logs` + where + `) as log_count,
			(SELECT COUNT(*) FROM otel_metrics` + where + `) as metric_count,
			(SELECT COUNT(*) FROM otel_traces` + where + ` AND StatusCode = 'ERROR') as error_count
	`
	var args []any
	for range 5 {
		args = append(args, filterArgs...)
	}

	var errorCount int64
	if err := s.db.QueryRowContext(ctx, statsQuery, args...).Scan(
		&stats.SpanCount,
		&stats.TraceCount,
		&stats.LogCount,
		&stats.MetricCount,
		&errorCount,
	); err != nil {
		return nil, fmt.Errorf("getting stats: %w", err)
	}

	// Get services (still needs separate query due to multiple rows)
	services, err := s.GetServices(ctx)
	if err != nil {
		return nil, err
	}
	stats.Services = services
	stats.ServiceCount = len(services)

	// Calculate error rate
	if stats.SpanCount > 0 {
		stats.ErrorRate = float64(errorCount) / float64(stats.SpanCount) * 100
	}

	return stats, nil
}

// Helper functions
func mapToString(m map[string]string) string {
	if len(m) == 0 {
		return "{}"
	}
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func stringArrayToString(arr []string) string {
	if len(arr) == 0 {
		return "[]"
	}
	b, err := json.Marshal(arr)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func timestampArrayToString(arr []time.Time) string {
	if len(arr) == 0 {
		return "[]"
	}
	strs := make([]string, len(arr))
	for i, t := range arr {
		strs[i] = t.Format(time.RFC3339Nano)
	}
	b, err := json.Marshal(strs)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func mapArrayToString(arr []map[string]string) string {
	if len(arr) == 0 {
		return "[]"
	}
	b, err := json.Marshal(arr)
	if err != nil {
		return "[]"
	}
	return string(b)
}

// scanJSONToMap scans a JSON column that DuckDB returns as map[string]interface{}
// and converts it to map[string]string. Errors are logged but not returned since
// partial results may still be useful for display purposes.
func scanJSONToMap(v interface{}) map[string]string {
	result := make(map[string]string)
	if v == nil {
		return result
	}

	switch val := v.(type) {
	case map[string]interface{}:
		for k, v := range val {
			if s, ok := v.(string); ok {
				result[k] = s
			} else if v != nil {
				// Convert non-string values to JSON
				b, err := json.Marshal(v)
				if err != nil {
					// Log but continue - use empty string for this key
					result[k] = fmt.Sprintf("<error: %v>", err)
					continue
				}
				result[k] = string(b)
			}
		}
	case string:
		// If it's a string, try to parse it as JSON
		if err := json.Unmarshal([]byte(val), &result); err != nil {
			// Log error but return empty map - caller can handle missing data
			return result
		}
	}
	return result
}
