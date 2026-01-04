package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
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

	for _, span := range spans {
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

	return tx.Commit()
}

func (s *DuckDBStore) QueryTraces(ctx context.Context, service, search string, from, to time.Time, limit, offset int) (*api.TracesResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// For Codex CLI, we treat first-level spans (those whose parent doesn't exist) as virtual traces.
	// For other services, we use traditional GROUP BY TraceId.
	// When service filter is empty, we combine both approaches.

	const codexService = "codex_cli_rs"

	// Check if we need Codex query, non-Codex query, or both
	includeCodex := service == "" || service == codexService
	includeOther := service == "" || service != codexService

	var allTraces []api.TraceOverview
	var total int

	// Query non-Codex traces (traditional GROUP BY TraceId)
	if includeOther {
		traces, count, err := s.queryNonCodexTraces(ctx, service, search, from, to, limit, offset)
		if err != nil {
			return nil, err
		}
		allTraces = append(allTraces, traces...)
		total += count
	}

	// Query Codex virtual traces (first-level spans as trace roots)
	if includeCodex {
		traces, count, err := s.queryCodexVirtualTraces(ctx, search, from, to, limit, offset)
		if err != nil {
			return nil, err
		}
		allTraces = append(allTraces, traces...)
		total += count
	}

	// Sort combined results by StartTime DESC and apply pagination
	// (We fetched more than needed to handle combined pagination properly)
	sortTracesByStartTime(allTraces)

	// Apply offset and limit to combined results
	if offset >= len(allTraces) {
		allTraces = nil
	} else {
		end := offset + limit
		if end > len(allTraces) {
			end = len(allTraces)
		}
		allTraces = allTraces[offset:end]
	}

	return &api.TracesResponse{
		Traces:  allTraces,
		Total:   total,
		HasMore: offset+len(allTraces) < total,
	}, nil
}

// queryNonCodexTraces queries traces for non-Codex services using GROUP BY TraceId
func (s *DuckDBStore) queryNonCodexTraces(ctx context.Context, service, search string, from, to time.Time, limit, offset int) ([]api.TraceOverview, int, error) {
	const codexService = "codex_cli_rs"

	// Format times as strings to avoid timezone issues with DuckDB's TIMESTAMP type
	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	timeFilter := "Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP"
	serviceFilter := " AND ServiceName != '" + codexService + "'"
	if service != "" && service != codexService {
		serviceFilter = " AND ServiceName = ?"
	}

	searchFilter := ""
	if search != "" {
		searchFilter = " AND (SpanName ILIKE ? OR ServiceName ILIKE ? OR StatusMessage ILIKE ? OR CAST(SpanAttributes AS VARCHAR) ILIKE ?)"
	}

	var args []interface{}
	args = append(args, fromStr, toStr)
	if service != "" && service != codexService {
		args = append(args, service)
	}

	query := `
		SELECT
			TraceId,
			FIRST(SpanName ORDER BY Timestamp ASC) as RootSpan,
			FIRST(ServiceName ORDER BY Timestamp ASC) as ServiceName,
			MIN(Timestamp) as StartTime,
			CAST((MAX(epoch_ms(Timestamp) + Duration/1000000) - MIN(epoch_ms(Timestamp))) * 1000000 AS BIGINT) as Duration,
			COUNT(*) as SpanCount,
			CASE WHEN SUM(CASE WHEN StatusCode = 'ERROR' THEN 1 ELSE 0 END) > 0 THEN 'ERROR'
			     WHEN SUM(CASE WHEN StatusCode = 'OK' THEN 1 ELSE 0 END) > 0 THEN 'OK'
			     ELSE 'UNSET' END as Status
		FROM otel_traces
		WHERE ` + timeFilter + serviceFilter + searchFilter + `
		GROUP BY TraceId
		ORDER BY StartTime DESC
		LIMIT ? OFFSET ?
	`

	if search != "" {
		pattern := "%" + search + "%"
		args = append(args, pattern, pattern, pattern, pattern)
	}
	args = append(args, limit+offset, 0) // Fetch enough for combined pagination

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("querying non-codex traces: %w", err)
	}
	defer rows.Close()

	var traces []api.TraceOverview
	for rows.Next() {
		var t api.TraceOverview
		if err := rows.Scan(&t.TraceID, &t.RootSpan, &t.ServiceName, &t.StartTime, &t.Duration, &t.SpanCount, &t.Status); err != nil {
			return nil, 0, fmt.Errorf("scanning trace: %w", err)
		}
		traces = append(traces, t)
	}

	// Count query
	var countArgs []interface{}
	countArgs = append(countArgs, fromStr, toStr)
	if service != "" && service != codexService {
		countArgs = append(countArgs, service)
	}
	if search != "" {
		pattern := "%" + search + "%"
		countArgs = append(countArgs, pattern, pattern, pattern, pattern)
	}

	countQuery := `SELECT COUNT(DISTINCT TraceId) FROM otel_traces WHERE ` + timeFilter + serviceFilter + searchFilter
	var count int
	if err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&count); err != nil {
		return nil, 0, fmt.Errorf("counting non-codex traces: %w", err)
	}

	return traces, count, nil
}

// queryCodexVirtualTraces queries Codex CLI "virtual traces" - first-level spans treated as trace roots
func (s *DuckDBStore) queryCodexVirtualTraces(ctx context.Context, search string, from, to time.Time, limit, offset int) ([]api.TraceOverview, int, error) {
	const codexService = "codex_cli_rs"

	// Format times as strings to avoid timezone issues with DuckDB's TIMESTAMP type
	fromStr := formatTimeForDB(from)
	toStr := formatTimeForDB(to)

	// First, check if there are any Codex spans at all
	var codexCount int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM otel_traces WHERE ServiceName = ?`,
		codexService).Scan(&codexCount)
	if err != nil || codexCount == 0 {
		return nil, 0, nil // No Codex spans, return empty
	}

	searchFilter := ""
	searchArgs := []interface{}{}
	if search != "" {
		searchFilter = " AND (SpanName ILIKE ? OR StatusMessage ILIKE ? OR CAST(SpanAttributes AS VARCHAR) ILIKE ?)"
		pattern := "%" + search + "%"
		searchArgs = append(searchArgs, pattern, pattern, pattern)
	}

	// Query first-level spans (those whose parent doesn't exist)
	// Use string interpolation for service name since it's a constant
	query := `
		SELECT
			t.SpanId as TraceId,
			t.SpanName as RootSpan,
			t.ServiceName,
			t.Timestamp as StartTime,
			t.Duration,
			1 as SpanCount,
			COALESCE(t.StatusCode, 'UNSET') as Status
		FROM otel_traces t
		WHERE t.ServiceName = '` + codexService + `'
		  AND t.Timestamp >= ?::TIMESTAMP AND t.Timestamp <= ?::TIMESTAMP
		  AND NOT EXISTS (
			SELECT 1 FROM otel_traces p
			WHERE p.SpanId = t.ParentSpanId AND p.ServiceName = '` + codexService + `'
		  )
		` + searchFilter + `
		ORDER BY t.Timestamp DESC
		LIMIT ? OFFSET ?
	`

	var args []interface{}
	args = append(args, fromStr, toStr)
	args = append(args, searchArgs...)
	args = append(args, limit+offset, 0)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		// Log the error but don't fail - just return empty results
		fmt.Printf("Warning: Codex query failed: %v\n", err)
		return nil, 0, nil
	}
	defer rows.Close()

	var traces []api.TraceOverview
	for rows.Next() {
		var t api.TraceOverview
		if err := rows.Scan(&t.TraceID, &t.RootSpan, &t.ServiceName, &t.StartTime, &t.Duration, &t.SpanCount, &t.Status); err != nil {
			return nil, 0, fmt.Errorf("scanning codex trace: %w", err)
		}
		traces = append(traces, t)
	}

	// Get accurate span counts for each trace
	for i := range traces {
		var count int
		err := s.db.QueryRowContext(ctx, `
			WITH RECURSIVE subtree AS (
				SELECT SpanId FROM otel_traces WHERE SpanId = ?
				UNION ALL
				SELECT t.SpanId FROM otel_traces t
				JOIN subtree s ON t.ParentSpanId = s.SpanId
				WHERE t.ServiceName = '`+codexService+`'
			)
			SELECT COUNT(*) FROM subtree
		`, traces[i].TraceID).Scan(&count)
		if err == nil {
			traces[i].SpanCount = count
		}
	}

	// Count total first-level spans
	countQuery := `
		SELECT COUNT(*) FROM otel_traces t
		WHERE t.ServiceName = '` + codexService + `'
		  AND t.Timestamp >= ?::TIMESTAMP AND t.Timestamp <= ?::TIMESTAMP
		  AND NOT EXISTS (
			SELECT 1 FROM otel_traces p
			WHERE p.SpanId = t.ParentSpanId AND p.ServiceName = '` + codexService + `'
		  )
		` + searchFilter

	var countArgs []interface{}
	countArgs = append(countArgs, fromStr, toStr)
	countArgs = append(countArgs, searchArgs...)

	var count int
	if err := s.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&count); err != nil {
		// Return what we have without count
		return traces, len(traces), nil
	}

	return traces, count, nil
}

// sortTracesByStartTime sorts traces by StartTime in descending order
func sortTracesByStartTime(traces []api.TraceOverview) {
	for i := 0; i < len(traces)-1; i++ {
		for j := i + 1; j < len(traces); j++ {
			if traces[j].StartTime.After(traces[i].StartTime) {
				traces[i], traces[j] = traces[j], traces[i]
			}
		}
	}
}

func (s *DuckDBStore) GetTraceSpans(ctx context.Context, traceID string) ([]api.Span, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	const codexService = "codex_cli_rs"

	// Check if this is a Codex first-level span (virtual trace root)
	// For Codex, the "traceID" is actually the SpanId of the first-level span
	var isCodexSpan bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM otel_traces WHERE SpanId = ? AND ServiceName = ?)`,
		traceID, codexService).Scan(&isCodexSpan)
	if err != nil {
		return nil, fmt.Errorf("checking codex span: %w", err)
	}

	if isCodexSpan {
		// Use recursive CTE to get the span and all its descendants
		return s.getCodexSpanSubtree(ctx, traceID)
	}

	// Standard query by TraceId for non-Codex services
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

	return s.scanSpans(ctx, query, traceID)
}

// getCodexSpanSubtree returns a Codex span and all its descendants using recursive CTE
func (s *DuckDBStore) getCodexSpanSubtree(ctx context.Context, rootSpanID string) ([]api.Span, error) {
	const codexService = "codex_cli_rs"

	query := `
		WITH RECURSIVE subtree AS (
			-- Base case: the root span
			SELECT
				Timestamp, TraceId, SpanId, ParentSpanId, TraceState,
				SpanName, SpanKind, ServiceName, ResourceAttributes,
				ScopeName, ScopeVersion, SpanAttributes, Duration,
				StatusCode, StatusMessage
			FROM otel_traces
			WHERE SpanId = ?

			UNION ALL

			-- Recursive case: children of spans in the subtree
			SELECT
				t.Timestamp, t.TraceId, t.SpanId, t.ParentSpanId, t.TraceState,
				t.SpanName, t.SpanKind, t.ServiceName, t.ResourceAttributes,
				t.ScopeName, t.ScopeVersion, t.SpanAttributes, t.Duration,
				t.StatusCode, t.StatusMessage
			FROM otel_traces t
			JOIN subtree s ON t.ParentSpanId = s.SpanId
			WHERE t.ServiceName = '` + codexService + `'
		)
		SELECT * FROM subtree ORDER BY Timestamp
	`

	return s.scanSpans(ctx, query, rootSpanID)
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

func (s *DuckDBStore) GetRecentTraces(ctx context.Context, limit int) (*api.TracesResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	const codexService = "codex_cli_rs"

	// Query non-Codex traces
	nonCodexQuery := `
		SELECT
			TraceId,
			FIRST(SpanName ORDER BY Timestamp ASC) as RootSpan,
			FIRST(ServiceName ORDER BY Timestamp ASC) as ServiceName,
			MIN(Timestamp) as StartTime,
			CAST((MAX(epoch_ms(Timestamp) + Duration/1000000) - MIN(epoch_ms(Timestamp))) * 1000000 AS BIGINT) as Duration,
			COUNT(*) as SpanCount,
			CASE WHEN SUM(CASE WHEN StatusCode = 'ERROR' THEN 1 ELSE 0 END) > 0 THEN 'ERROR'
			     WHEN SUM(CASE WHEN StatusCode = 'OK' THEN 1 ELSE 0 END) > 0 THEN 'OK'
			     ELSE 'UNSET' END as Status
		FROM otel_traces
		WHERE ServiceName != '` + codexService + `'
		GROUP BY TraceId
		ORDER BY StartTime DESC
		LIMIT ?
	`

	rows, err := s.db.QueryContext(ctx, nonCodexQuery, limit)
	if err != nil {
		return nil, fmt.Errorf("querying recent non-codex traces: %w", err)
	}

	var traces []api.TraceOverview
	for rows.Next() {
		var t api.TraceOverview
		if err := rows.Scan(&t.TraceID, &t.RootSpan, &t.ServiceName, &t.StartTime, &t.Duration, &t.SpanCount, &t.Status); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning trace: %w", err)
		}
		traces = append(traces, t)
	}
	rows.Close()

	// Query Codex virtual traces (first-level spans) - simplified query
	codexQuery := `
		SELECT
			t.SpanId as TraceId,
			t.SpanName as RootSpan,
			t.ServiceName,
			t.Timestamp as StartTime,
			t.Duration,
			1 as SpanCount,
			COALESCE(t.StatusCode, 'UNSET') as Status
		FROM otel_traces t
		WHERE t.ServiceName = '` + codexService + `'
		  AND NOT EXISTS (
			SELECT 1 FROM otel_traces p
			WHERE p.SpanId = t.ParentSpanId AND p.ServiceName = '` + codexService + `'
		  )
		ORDER BY t.Timestamp DESC
		LIMIT ?
	`

	rows, err = s.db.QueryContext(ctx, codexQuery, limit)
	if err == nil {
		for rows.Next() {
			var t api.TraceOverview
			if err := rows.Scan(&t.TraceID, &t.RootSpan, &t.ServiceName, &t.StartTime, &t.Duration, &t.SpanCount, &t.Status); err != nil {
				break
			}
			traces = append(traces, t)
		}
		rows.Close()
	}

	// Sort combined results and limit
	sortTracesByStartTime(traces)
	if len(traces) > limit {
		traces = traces[:limit]
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

	// Combined query to get all counts in a single round-trip
	// This reduces 5 queries to 1
	statsQuery := `
		SELECT
			(SELECT COUNT(*) FROM otel_traces) as span_count,
			(SELECT COUNT(DISTINCT TraceId) FROM otel_traces) as trace_count,
			(SELECT COUNT(*) FROM otel_logs) as log_count,
			(SELECT COUNT(*) FROM otel_metrics) as metric_count,
			(SELECT COUNT(*) FROM otel_traces WHERE StatusCode = 'ERROR') as error_count
	`

	var errorCount int64
	if err := s.db.QueryRowContext(ctx, statsQuery).Scan(
		&stats.SpanCount,
		&stats.TraceCount,
		&stats.LogCount,
		&stats.MetricCount,
		&errorCount,
	); err != nil {
		return nil, fmt.Errorf("getting stats: %w", err)
	}

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
