package storage

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/tobilg/ai-observer/internal/api"
)

// spanPageOrder is the order of the spans of a trace detail page. Parents
// start before their children, so each page and all before it form a tree
// without missing parents (but for spans starting in the same microsecond).
const spanPageOrder = "Timestamp, SpanId, ServiceName"

// TraceSpansPage returns a page of the spans of a trace, oldest first, with
// the overview of the whole trace, or nil when there is no such trace. An ID
// that is no trace ID is looked up as a span ID, and that span's trace is
// returned. A turn of Codex can have over 100,000 spans, so a trace is never
// read whole.
func (s *DuckDBStore) TraceSpansPage(ctx context.Context, traceID string, page Page) (*api.SpansResponse, error) {
	limit, offset, err := pageLimit(page)
	if err != nil {
		return nil, err
	}
	overview, err := s.traceOverview(ctx, traceID)
	if err != nil {
		return nil, err
	}
	if overview == nil {
		var resolved string
		err := s.db.QueryRowContext(ctx,
			"SELECT TraceId FROM otel_traces WHERE SpanId = ? ORDER BY Timestamp LIMIT 1", traceID).Scan(&resolved)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("resolving trace from span id: %w", err)
		}
		if overview, err = s.traceOverview(ctx, resolved); err != nil || overview == nil {
			return nil, err
		}
	}

	var where conditions
	where.add("TraceId = ?", overview.TraceID)
	if page.Cursor != "" {
		c, err := decodeCursor(page.Cursor, 2)
		if err != nil {
			return nil, err
		}
		where.add(`(Timestamp > ?::TIMESTAMP OR (Timestamp = ?::TIMESTAMP
			AND (SpanId > ? OR (SpanId = ? AND ServiceName > ?))))`, c.Time, c.Time, c.Keys[0], c.Keys[0], c.Keys[1])
	}
	query := "SELECT" + spanSelectColumns + " FROM otel_traces" + where.clause("WHERE") +
		" ORDER BY " + spanPageOrder + " LIMIT ? OFFSET ?"
	spans, err := s.scanSpans(ctx, query, concatArgs(where.args, []any{limit + 1, offset})...)
	if err != nil {
		return nil, err
	}

	resp := &api.SpansResponse{Spans: spans, Trace: overview, Total: overview.SpanCount}
	cutSpanPage(resp, limit)
	return resp, nil
}

// traceOverview returns the overview of all spans of a trace, as trace lists
// show it, or nil when the trace has no spans
func (s *DuckDBStore) traceOverview(ctx context.Context, traceID string) (*api.TraceOverview, error) {
	overviews, err := s.traceOverviews(ctx, []string{traceID})
	if err != nil {
		return nil, err
	}
	return overviews[traceID], nil
}

// traceOverviews returns the overviews of the traces with the given IDs,
// each over all of its spans (looked up by idx_traces_trace_id_v2), by trace
// ID; traces without spans are missing
func (s *DuckDBStore) traceOverviews(ctx context.Context, traceIDs []string) (map[string]*api.TraceOverview, error) {
	overviews := make(map[string]*api.TraceOverview, len(traceIDs))
	if len(traceIDs) == 0 {
		return overviews, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT TraceId, COUNT(*), MIN(Timestamp),
			MAX(epoch_ns(Timestamp) + COALESCE(Duration, 0)) - MIN(epoch_ns(Timestamp)),
			COUNT(*) FILTER (WHERE StatusCode = 'ERROR'), COUNT(*) FILTER (WHERE StatusCode = 'OK'),
			arg_min(SpanId, Timestamp), arg_min(SpanName, Timestamp), arg_min(ServiceName, Timestamp),
			`+traceSessionExpr("SessionId")+`
		FROM otel_traces
		WHERE TraceId IN (?`+strings.Repeat(", ?", len(traceIDs)-1)+`)
		GROUP BY TraceId`, stringArgs(traceIDs)...)
	if err != nil {
		return nil, fmt.Errorf("querying trace overviews: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		o := api.TraceOverview{Kind: api.TraceKindOTelTrace}
		var duration sql.NullInt64
		var rootSpanID, rootSpan, service, sessionID sql.NullString
		var failed, ok int
		if err := rows.Scan(&o.TraceID, &o.SpanCount, &o.StartTime, &duration, &failed, &ok,
			&rootSpanID, &rootSpan, &service, &sessionID); err != nil {
			return nil, fmt.Errorf("scanning trace overview: %w", err)
		}
		o.ID, o.Duration = o.TraceID, duration.Int64
		o.RootSpanID, o.RootSpan, o.ServiceName = rootSpanID.String, rootSpan.String, service.String
		o.SessionID = sessionID.String
		o.Status = TraceStatus(failed, ok)
		overviews[o.TraceID] = &o
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating trace overviews: %w", err)
	}
	return overviews, nil
}

// TraceStatus returns the status of a trace with this many failed and
// successful spans
func TraceStatus(failed, ok int) string {
	switch {
	case failed > 0:
		return api.TraceStatusError
	case ok > 0:
		return api.TraceStatusOK
	default:
		return api.TraceStatusUnset
	}
}

// PageOfSpans returns a page of spans that are held in memory, in the order
// and with the cursors of TraceSpansPage. It sorts spans.
func PageOfSpans(spans []api.Span, page Page) (*api.SpansResponse, error) {
	limit, offset, err := pageLimit(page)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(spans, func(i, j int) bool { return spanPageLess(spanPageKey(spans[i]), spanPageKey(spans[j])) })

	resp := &api.SpansResponse{Total: len(spans)}
	if page.Cursor != "" {
		c, err := decodeCursor(page.Cursor, 2)
		if err != nil {
			return nil, err
		}
		after := [3]string{c.Time, c.Keys[0], c.Keys[1]}
		offset = sort.Search(len(spans), func(i int) bool { return spanPageLess(after, spanPageKey(spans[i])) })
	}
	resp.Spans = spans[min(offset, len(spans)):]
	cutSpanPage(resp, limit)
	return resp, nil
}

// spanPageKey is the sort key of a span in spanPageOrder. Its time has the
// microsecond precision of the stored timestamps, zero-padded so that keys
// compare as strings.
func spanPageKey(span api.Span) [3]string {
	return [3]string{span.Timestamp.UTC().Format("2006-01-02 15:04:05.000000"), span.SpanID, span.ServiceName}
}

func spanPageLess(a, b [3]string) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// cutSpanPage cuts the spans of resp, read with one more than limit, to the
// page and sets the cursor of the next one
func cutSpanPage(resp *api.SpansResponse, limit int) {
	if len(resp.Spans) > limit {
		resp.Spans = resp.Spans[:limit]
		resp.HasMore = true
		key := spanPageKey(resp.Spans[limit-1])
		resp.NextCursor = encodeCursor(pageCursor{Time: key[0], Keys: key[1:]})
	}
	if resp.Spans == nil {
		resp.Spans = []api.Span{}
	}
}
