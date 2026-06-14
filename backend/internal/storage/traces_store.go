package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

const (
	codexServiceName     = "codex_cli_rs"
	codexTurnIDAttribute = "turn_id"
)

func (s *DuckDBStore) InsertSpans(ctx context.Context, spans []api.Span) error {
	if len(spans) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	if err := insertSpansTx(ctx, tx, spans); err != nil {
		return err
	}

	return tx.Commit()
}

func insertSpansTx(ctx context.Context, tx *sql.Tx, spans []api.Span) error {
	if len(spans) == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO otel_traces (
			Timestamp, TraceId, SpanId, ParentSpanId, TraceState,
			SpanName, SpanKind, ServiceName, ResourceAttributes,
			ScopeName, ScopeVersion, SpanAttributes, Duration,
			StatusCode, StatusMessage,
			"Events.Timestamp", "Events.Name", "Events.Attributes",
			"Links.TraceId", "Links.SpanId", "Links.TraceState", "Links.Attributes"
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing statement: %w", err)
	}
	defer stmt.Close()

	existsStmt, err := tx.PrepareContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM otel_traces
			WHERE ServiceName = ? AND TraceId = ? AND SpanId = ?
		)
	`)
	if err != nil {
		return fmt.Errorf("preparing dedupe statement: %w", err)
	}
	defer existsStmt.Close()

	for _, span := range spans {
		var exists bool
		if err := existsStmt.QueryRowContext(ctx, span.ServiceName, span.TraceID, span.SpanID).Scan(&exists); err != nil {
			return fmt.Errorf("checking duplicate span: %w", err)
		}
		if exists {
			continue
		}

		eventTimestamps := make([]time.Time, len(span.Events))
		eventNames := make([]string, len(span.Events))
		eventAttributes := make([]map[string]string, len(span.Events))
		for i, e := range span.Events {
			eventTimestamps[i] = e.Timestamp
			eventNames[i] = e.Name
			eventAttributes[i] = e.Attributes
		}

		linkTraceIDs := make([]string, len(span.Links))
		linkSpanIDs := make([]string, len(span.Links))
		linkTraceStates := make([]string, len(span.Links))
		linkAttributes := make([]map[string]string, len(span.Links))
		for i, l := range span.Links {
			linkTraceIDs[i] = l.TraceID
			linkSpanIDs[i] = l.SpanID
			linkTraceStates[i] = l.TraceState
			linkAttributes[i] = l.Attributes
		}

		_, err := stmt.ExecContext(ctx,
			span.Timestamp,
			span.TraceID,
			span.SpanID,
			nullString(span.ParentSpanID),
			nullString(span.TraceState),
			span.SpanName,
			nullString(span.SpanKind),
			span.ServiceName,
			mapToString(span.ResourceAttributes),
			nullString(span.ScopeName),
			nullString(span.ScopeVersion),
			mapToString(span.SpanAttributes),
			span.Duration,
			nullString(span.StatusCode),
			nullString(span.StatusMessage),
			timestampArrayToString(eventTimestamps),
			stringArrayToString(eventNames),
			mapArrayToString(eventAttributes),
			stringArrayToString(linkTraceIDs),
			stringArrayToString(linkSpanIDs),
			stringArrayToString(linkTraceStates),
			mapArrayToString(linkAttributes),
		)
		if err != nil {
			return fmt.Errorf("inserting span: %w", err)
		}
	}

	return nil
}

func (s *DuckDBStore) QueryTraces(ctx context.Context, service, search string, from, to time.Time, limit, offset int) (*api.TracesResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit, offset = normalizePagination(limit, offset)
	traces, total, err := s.queryRawTraceOverviews(ctx, service, search, from, to, limit, offset, true)
	if err != nil {
		return nil, err
	}

	return &api.TracesResponse{
		Traces:  traces,
		Total:   total,
		HasMore: offset+len(traces) < total,
	}, nil
}

// queryRawTraceOverviews queries all services as normal OTLP traces, including Codex.
func (s *DuckDBStore) queryRawTraceOverviews(ctx context.Context, service, search string, from, to time.Time, limit, offset int, includeCount bool) ([]api.TraceOverview, int, error) {
	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)
	limit, offset = normalizePagination(limit, offset)

	serviceFilter := ""
	if service != "" {
		serviceFilter = " AND ServiceName = ?"
	}

	searchFilter := ""
	if search != "" {
		searchFilter = `
		,
		matching_traces AS (
			SELECT DISTINCT TraceId
			FROM spans
			WHERE SpanName ILIKE ?
			   OR ServiceName ILIKE ?
			   OR COALESCE(StatusMessage, '') ILIKE ?
			   OR COALESCE(CAST(SpanAttributes AS VARCHAR), '') ILIKE ?
			   OR COALESCE(CAST(ResourceAttributes AS VARCHAR), '') ILIKE ?
		)
	`
	}

	args := []interface{}{fromStr, toStr}
	if service != "" {
		args = append(args, service)
	}
	if search != "" {
		pattern := "%" + search + "%"
		args = append(args, pattern, pattern, pattern, pattern, pattern)
	}

	joinMatching := ""
	if search != "" {
		joinMatching = "JOIN matching_traces mt ON mt.TraceId = spans.TraceId"
	}

	baseQuery := `
		WITH spans AS (
			SELECT * FROM (
				SELECT *,
					ROW_NUMBER() OVER (
						PARTITION BY ServiceName, TraceId, SpanId
						ORDER BY Timestamp DESC
					) AS rn
				FROM otel_traces
				WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
				` + serviceFilter + `
			)
			WHERE rn = 1
		)
		` + searchFilter + `
	`

	query := baseQuery + `
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
		GROUP BY spans.TraceId
		ORDER BY StartTime DESC
	`
	query, queryArgs := appendLimitOffset(query, append([]interface{}{}, args...), limit, offset)

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying traces: %w", err)
	}
	defer rows.Close()

	var traces []api.TraceOverview
	for rows.Next() {
		var t api.TraceOverview
		if err := rows.Scan(&t.ID, &t.Kind, &t.TraceID, &t.RootSpanID, &t.RootSpan, &t.ServiceName, &t.StartTime, &t.Duration, &t.SpanCount, &t.Status); err != nil {
			return nil, 0, fmt.Errorf("scanning trace: %w", err)
		}
		traces = append(traces, t)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterating traces: %w", err)
	}

	if !includeCount {
		return traces, len(traces), nil
	}

	countQuery := baseQuery + `
		SELECT COUNT(DISTINCT spans.TraceId)
		FROM spans
		` + joinMatching + `
	`
	var count int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&count); err != nil {
		return nil, 0, fmt.Errorf("counting traces: %w", err)
	}

	return traces, count, nil
}

func codexTurnID(span api.Span) string {
	if span.SpanAttributes == nil {
		return ""
	}
	return strings.TrimSpace(span.SpanAttributes[codexTurnIDAttribute])
}

func collectCodexOperationSpans(root api.Span, traceSpans []api.Span, children map[string][]api.Span) []api.Span {
	turnID := codexTurnID(root)
	if turnID == "" {
		return collectCodexSubtree(root, children)
	}

	var grouped []api.Span
	visited := make(map[string]bool)

	var visit func(api.Span)
	visit = func(span api.Span) {
		if visited[span.SpanID] {
			return
		}
		visited[span.SpanID] = true
		grouped = append(grouped, span)
		for _, child := range children[span.SpanID] {
			visit(child)
		}
	}

	for _, span := range traceSpans {
		if span.TraceID == root.TraceID && codexTurnID(span) == turnID {
			visit(span)
		}
	}
	if len(grouped) == 0 {
		visit(root)
	}

	sort.Slice(grouped, func(i, j int) bool {
		return grouped[i].Timestamp.Before(grouped[j].Timestamp)
	})
	return grouped
}

func collectCodexSubtree(root api.Span, children map[string][]api.Span) []api.Span {
	var subtree []api.Span
	visited := make(map[string]bool)

	var visit func(api.Span)
	visit = func(span api.Span) {
		if visited[span.SpanID] {
			return
		}
		visited[span.SpanID] = true
		subtree = append(subtree, span)
		for _, child := range children[span.SpanID] {
			visit(child)
		}
	}

	visit(root)
	sort.Slice(subtree, func(i, j int) bool {
		return subtree[i].Timestamp.Before(subtree[j].Timestamp)
	})
	return subtree
}

func (s *DuckDBStore) GetTraceSpans(ctx context.Context, id, kind string) ([]api.Span, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	switch kind {
	case api.TraceKindCodexOperation:
		return s.getCodexSpanSubtree(ctx, id)
	case api.TraceKindOTelTrace:
		return s.getOTelTraceSpans(ctx, id)
	default:
		return nil, fmt.Errorf("unsupported trace kind: %s", kind)
	}
}

func (s *DuckDBStore) getOTelTraceSpans(ctx context.Context, traceID string) ([]api.Span, error) {
	query := `
		SELECT
			Timestamp, TraceId, SpanId, ParentSpanId, TraceState,
			SpanName, SpanKind, ServiceName, ResourceAttributes,
			ScopeName, ScopeVersion, SpanAttributes, Duration,
			StatusCode, StatusMessage
		FROM otel_traces
		WHERE TraceId = ?
		ORDER BY Timestamp
	`

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

func (s *DuckDBStore) loadDedupedCodexTraceSpans(ctx context.Context, traceID string) ([]api.Span, error) {
	query := `
		SELECT
			Timestamp, TraceId, SpanId, ParentSpanId, TraceState,
			SpanName, SpanKind, ServiceName, ResourceAttributes,
			ScopeName, ScopeVersion, SpanAttributes, Duration,
			StatusCode, StatusMessage
		FROM (
			SELECT *,
				ROW_NUMBER() OVER (
					PARTITION BY ServiceName, TraceId, SpanId
					ORDER BY Timestamp DESC
				) AS rn
			FROM otel_traces
			WHERE ServiceName = ? AND TraceId = ?
		)
		WHERE rn = 1
		ORDER BY Timestamp
	`

	return s.scanSpans(ctx, query, codexServiceName, traceID)
}

func buildCodexChildren(spans []api.Span) map[string][]api.Span {
	children := make(map[string][]api.Span)
	for _, span := range spans {
		if span.ParentSpanID == "" {
			continue
		}
		children[span.ParentSpanID] = append(children[span.ParentSpanID], span)
	}
	for parentID := range children {
		sort.Slice(children[parentID], func(i, j int) bool {
			return children[parentID][i].Timestamp.Before(children[parentID][j].Timestamp)
		})
	}
	return children
}

// getCodexSpanSubtree returns a Codex row root and its grouped descendant spans.
func (s *DuckDBStore) getCodexSpanSubtree(ctx context.Context, rootSpanID string) ([]api.Span, error) {
	var rootTraceID string
	err := s.db.QueryRowContext(ctx, `
		SELECT TraceId
		FROM (
			SELECT TraceId,
				ROW_NUMBER() OVER (
					PARTITION BY ServiceName, TraceId, SpanId
					ORDER BY Timestamp DESC
				) AS rn
			FROM otel_traces
			WHERE SpanId = ? AND ServiceName = ?
		)
		WHERE rn = 1
		LIMIT 1
	`, rootSpanID, codexServiceName).Scan(&rootTraceID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("finding codex operation root: %w", err)
	}

	traceSpans, err := s.loadDedupedCodexTraceSpans(ctx, rootTraceID)
	if err != nil {
		return nil, err
	}

	var root api.Span
	rootFound := false
	for _, span := range traceSpans {
		if span.SpanID == rootSpanID {
			root = span
			rootFound = true
			break
		}
	}
	if !rootFound {
		return nil, nil
	}

	return collectCodexOperationSpans(root, traceSpans, buildCodexChildren(traceSpans)), nil
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.getServicesLocked(ctx)
}

func (s *DuckDBStore) getServicesLocked(ctx context.Context) ([]string, error) {
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

func (s *DuckDBStore) getServicesInRangeLocked(ctx context.Context, from, to time.Time) ([]string, error) {
	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	query := `
		SELECT DISTINCT ServiceName
		FROM (
			SELECT ServiceName FROM otel_traces WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
			UNION
			SELECT ServiceName FROM otel_logs WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
			UNION
			SELECT ServiceName FROM otel_metrics WHERE Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP
		)
		ORDER BY ServiceName
	`

	rows, err := s.db.QueryContext(ctx, query, fromStr, toStr, fromStr, toStr, fromStr, toStr)
	if err != nil {
		return nil, fmt.Errorf("querying services in range: %w", err)
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

func (s *DuckDBStore) GetRecentTraces(ctx context.Context, limit int, from, to time.Time) (*api.TracesResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	traces, _, err := s.queryRawTraceOverviews(ctx, "", "", from, to, limit, 0, false)
	if err != nil {
		return nil, err
	}

	return &api.TracesResponse{
		Traces:  traces,
		Total:   len(traces),
		HasMore: false,
	}, nil
}

func (s *DuckDBStore) GetStats(ctx context.Context) (*api.StatsResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := &api.StatsResponse{}

	statsQuery := `
		SELECT
			(SELECT COUNT(*) FROM otel_traces) as span_count,
			(SELECT COUNT(DISTINCT TraceId) FROM otel_traces) as raw_trace_count,
			(SELECT COUNT(*) FROM otel_logs) as log_count,
			(SELECT COUNT(*) FROM otel_metrics) as metric_count,
			(SELECT COUNT(*) FROM otel_traces WHERE StatusCode = 'ERROR') as error_count
	`

	var errorCount int64
	if err := s.db.QueryRowContext(ctx, statsQuery).Scan(
		&stats.SpanCount,
		&stats.RawTraceCount,
		&stats.LogCount,
		&stats.MetricCount,
		&errorCount,
	); err != nil {
		return nil, fmt.Errorf("getting stats: %w", err)
	}

	stats.TraceCount = stats.RawTraceCount
	stats.CodexOperationCount = 0

	// Get services (still needs separate query due to multiple rows)
	services, err := s.getServicesLocked(ctx)
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
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

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

func parseMapString(s string) (map[string]string, error) {
	result := make(map[string]string)
	if s == "" || s == "{}" {
		return result, nil
	}
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return result, fmt.Errorf("parsing JSON map: %w", err)
	}
	return result, nil
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
