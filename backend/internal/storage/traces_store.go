package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/tools"
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

// TraceFilter selects the traces of a trace list
type TraceFilter struct {
	// Service is a service filter (serviceCondition): a service or tool name
	Service string
	// Search is text that a span of the trace contains
	Search string
	// Background includes the background traces of services that send
	// internal requests as traces (tools.BackgroundTraceServices, Codex's
	// app and exec servers): traces of only those services in which no span
	// belongs to a session, and traces whose root span is one of
	// tools.BackgroundTraceRoots (Codex's approval reviews and the CLI's
	// startup spans). Lists leave them out by default; they are most of
	// these services' traces.
	Background bool
}

// QueryTraces returns a page of the traces with spans in [from, to],
// newest first. Like sessions, traces are listed whole: the range selects
// the traces (those with a span in it, matching the filter) and orders them
// by their first span in it, and each listed trace is described by all of
// its spans, also those outside the range (traceOverviews), as its detail
// view shows it. A trace that began before from therefore shows its true
// start, earlier than the time it is sorted by.
func (s *DuckDBStore) QueryTraces(ctx context.Context, filter TraceFilter, from, to time.Time, page Page) (*api.TracesResponse, error) {
	return s.queryRawTraceOverviews(ctx, filter, from, to, page, true)
}

// queryRawTraceOverviews queries all services as normal OTLP traces,
// including Codex. Without includeCount, Total is the number of traces
// returned.
func (s *DuckDBStore) queryRawTraceOverviews(ctx context.Context, filter TraceFilter, from, to time.Time, page Page, includeCount bool) (*api.TracesResponse, error) {
	var spanWhere conditions
	spanWhere.add("Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP", formatTimeForDB(from), formatTimeForDB(to))
	spanWhere.addService(filter.Service)

	// Spans are unique per (ServiceName, TraceId, SpanId): inserts skip stored
	// spans and migration 2 removed older duplicates. The attribute columns are
	// only read when searching.
	columns := "TraceId, Timestamp, SpanName, ServiceName, SessionId"
	ctes := conditions{}
	var joins []string
	if background := tools.BackgroundTraceServices(); !filter.Background && len(background) > 0 {
		// A trace is kept when one of its spans is not a background span:
		// one of another service, or one that belongs to a session
		placeholders := func(n int) string { return "?" + strings.Repeat(", ?", n-1) }
		args := stringArgs(background)
		foreground := `ServiceName NOT IN (` + placeholders(len(background)) + `) OR COALESCE(SessionId, '') <> ''`
		if services, roots := tools.BackgroundTraceRoots(); len(roots) > 0 {
			// ... unless it is work the tool does on its own, which can
			// have sessions too: recognized by the name of its root span
			columns += ", ParentSpanId"
			foreground = `(` + foreground + `) AND TraceId NOT IN (
				SELECT TraceId FROM spans
				WHERE ServiceName IN (` + placeholders(len(services)) + `)
				  AND SpanName IN (` + placeholders(len(roots)) + `)
				  AND COALESCE(ParentSpanId, '') = ''
			)`
			args = concatArgs(args, stringArgs(services), stringArgs(roots))
		}
		ctes.add(`foreground_traces AS (
			SELECT DISTINCT TraceId
			FROM spans
			WHERE `+foreground+`
		)`, args...)
		joins = append(joins, "JOIN foreground_traces ft ON ft.TraceId = spans.TraceId")
	}
	if search := filter.Search; search != "" {
		columns += ", StatusMessage, SpanAttributes, ResourceAttributes"
		cond, args := searchCondition([]string{"SpanName", "ServiceName", "COALESCE(StatusMessage, '')"},
			[]string{"SpanAttributes", "ResourceAttributes"}, search)
		ctes.add(`matching_traces AS (
			SELECT DISTINCT TraceId
			FROM spans
			WHERE `+cond+`
		)`, args...)
		joins = append(joins, "JOIN matching_traces mt ON mt.TraceId = spans.TraceId")
	}
	joinMatching := strings.Join(joins, "\n\t\t")
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

	// The page: trace IDs and their first span in the range, the sort key
	query := with + `
		SELECT spans.TraceId, MIN(spans.Timestamp) AS FirstInRange
		FROM spans
		` + joinMatching + `
		GROUP BY spans.TraceId` + having.clause("HAVING") + `
		ORDER BY FirstInRange DESC, spans.TraceId DESC
		LIMIT ? OFFSET ?`
	queryArgs := concatArgs(withArgs, having.args, []any{limit + 1, offset})

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying traces: %w", err)
	}
	type pageRow struct {
		traceID string
		first   time.Time
	}
	var pageRows []pageRow
	for rows.Next() {
		var r pageRow
		if err := rows.Scan(&r.traceID, &r.first); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning trace: %w", err)
		}
		pageRows = append(pageRows, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, fmt.Errorf("iterating traces: %w", err)
	}
	rows.Close()

	resp := &api.TracesResponse{Traces: []api.TraceOverview{}}
	if len(pageRows) > limit {
		pageRows = pageRows[:limit]
		resp.HasMore = true
		last := pageRows[limit-1]
		resp.NextCursor = encodeCursor(pageCursor{Time: formatTimeForDB(last.first), Keys: []string{last.traceID}})
	}
	ids := make([]string, len(pageRows))
	for i, r := range pageRows {
		ids[i] = r.traceID
	}
	overviews, err := s.traceOverviews(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, r := range pageRows {
		// A trace deleted meanwhile is left out
		if o := overviews[r.traceID]; o != nil {
			resp.Traces = append(resp.Traces, *o)
		}
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

// traceSessionExpr is the aggregate of a trace's spans' session column that
// is the session of the trace: the session ID of those spans that have one
// when they all have the same, else NULL. MIN and MAX are cheaper than
// COUNT(DISTINCT ...) in the per-trace aggregation of trace lists.
func traceSessionExpr(column string) string {
	return "CASE WHEN MIN(" + column + ") = MAX(" + column + ") THEN MIN(" + column + ") END"
}

// spanSelectColumns are the columns scanSpans reads
const spanSelectColumns = `
	Timestamp, TraceId, SpanId, ParentSpanId, TraceState,
	SpanName, SpanKind, ServiceName, ResourceAttributes,
	ScopeName, ScopeVersion, SpanAttributes, Duration,
	StatusCode, StatusMessage, SessionId`

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

// scanSpans executes a query selecting spanSelectColumns and scans the
// results into api.Span slice
func (s *DuckDBStore) scanSpans(ctx context.Context, query string, args ...interface{}) ([]api.Span, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying spans: %w", err)
	}
	defer rows.Close()

	spans := []api.Span{} // clients read a list
	for rows.Next() {
		var span api.Span
		var parentSpanID, traceState, spanKind, scopeName, scopeVersion, statusCode, statusMessage, sessionID sql.NullString
		var resourceAttrs, spanAttrs interface{}

		if err := rows.Scan(
			&span.Timestamp, &span.TraceID, &span.SpanID, &parentSpanID, &traceState,
			&span.SpanName, &spanKind, &span.ServiceName, &resourceAttrs,
			&scopeName, &scopeVersion, &spanAttrs, &span.Duration,
			&statusCode, &statusMessage, &sessionID,
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
		span.SessionID = sessionID.String
		span.ResourceAttributes = scanJSONToMap(resourceAttrs)
		span.SpanAttributes = scanJSONToMap(spanAttrs)

		spans = append(spans, span)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating spans: %w", err)
	}

	return spans, nil
}

// GetRecentTraces returns the newest traces in [from, to], without paging
// and without background traces (TraceFilter.Background)
func (s *DuckDBStore) GetRecentTraces(ctx context.Context, limit int, from, to time.Time) (*api.TracesResponse, error) {
	resp, err := s.queryRawTraceOverviews(ctx, TraceFilter{}, from, to, Page{Limit: limit}, false)
	if err != nil {
		return nil, err
	}
	resp.HasMore, resp.NextCursor = false, ""
	return resp, nil
}
