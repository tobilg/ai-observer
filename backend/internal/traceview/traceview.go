// Package traceview assembles the spans shown on the trace detail page from
// the spans storage reads: a whole OTel trace, or one Codex operation. Codex
// CLI records a whole session as a single trace, so each of its operations is
// also shown as a trace of its own (kind codex_operation, identified by the
// operation's root span ID).
package traceview

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/tools"
)

// codexTurnIDAttribute groups the spans of one Codex operation (a turn)
const codexTurnIDAttribute = "turn_id"

// ErrUnsupportedKind is returned for a trace kind other than api.TraceKind*
var ErrUnsupportedKind = errors.New("unsupported trace kind")

// Store reads spans (implemented by storage.DuckDBStore)
type Store interface {
	// TraceSpansPage returns a page of the spans of a trace, oldest first,
	// with the trace's overview, or nil when there is no such trace; an ID
	// that is no trace ID is looked up as a span ID
	TraceSpansPage(ctx context.Context, traceID string, page storage.Page) (*api.SpansResponse, error)
	// SpanTraceID returns the trace ID of a service's span, or "" when the
	// service has no such span
	SpanTraceID(ctx context.Context, service, spanID string) (string, error)
	// ServiceTraceSpans returns the spans of a service's trace, oldest first
	ServiceTraceSpans(ctx context.Context, service, traceID string) ([]api.Span, error)
}

// Load returns a page of the spans of the trace view id of the given kind,
// oldest first, with the overview of all of its spans, or nil when it does
// not exist. Traces are read a page at a time: a turn of Codex's app server
// can have over 100,000 spans.
func Load(ctx context.Context, store Store, id, kind string, page storage.Page) (*api.SpansResponse, error) {
	switch kind {
	case api.TraceKindOTelTrace:
		return store.TraceSpansPage(ctx, id, page)
	case api.TraceKindCodexOperation:
		spans, err := loadCodexOperation(ctx, store, id)
		if err != nil || len(spans) == 0 {
			return nil, err
		}
		overview := codexOperationOverview(id, spans)
		resp, err := storage.PageOfSpans(spans, page)
		if err != nil {
			return nil, err
		}
		resp.Trace = overview
		return resp, nil
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedKind, kind)
	}
}

// codexOperationOverview describes the spans of the Codex operation with
// the given root span
func codexOperationOverview(rootSpanID string, spans []api.Span) *api.TraceOverview {
	root := spans[0]
	o := &api.TraceOverview{
		ID: rootSpanID, Kind: api.TraceKindCodexOperation, GroupLevel: api.TraceGroupLevelCodexOperation,
		SpanCount: len(spans), StartTime: spans[0].Timestamp,
	}
	end := spans[0].Timestamp
	var failed, ok int
	// The session of the operation: of its spans that have one, when they
	// have the same (as trace lists show it)
	sessions := make(map[string]bool)
	for _, span := range spans {
		if span.SpanID == rootSpanID {
			root = span
		}
		if span.SessionID != "" {
			sessions[span.SessionID] = true
		}
		if codexTurnID(span) != "" {
			o.GroupLevel = api.TraceGroupLevelCodexTurn
		}
		if span.Timestamp.Before(o.StartTime) {
			o.StartTime = span.Timestamp
		}
		if spanEnd := span.Timestamp.Add(time.Duration(span.Duration)); spanEnd.After(end) {
			end = spanEnd
		}
		switch span.StatusCode {
		case "ERROR":
			failed++
		case "OK":
			ok++
		}
	}
	o.TraceID, o.RootSpanID, o.RootSpan, o.ServiceName = root.TraceID, root.SpanID, root.SpanName, root.ServiceName
	o.Duration = int64(end.Sub(o.StartTime))
	o.Status = storage.TraceStatus(failed, ok)
	if len(sessions) == 1 {
		for id := range sessions {
			o.SessionID = id
		}
	}
	return o
}

// loadCodexOperation returns a Codex operation's root span and the spans
// grouped with it
func loadCodexOperation(ctx context.Context, store Store, rootSpanID string) ([]api.Span, error) {
	service := tools.Codex.ServiceName()
	traceID, err := store.SpanTraceID(ctx, service, rootSpanID)
	if err != nil {
		return nil, fmt.Errorf("finding codex operation root: %w", err)
	}
	if traceID == "" {
		return nil, nil
	}
	traceSpans, err := store.ServiceTraceSpans(ctx, service, traceID)
	if err != nil {
		return nil, err
	}
	for _, span := range traceSpans {
		if span.SpanID == rootSpanID {
			return codexOperationSpans(span, traceSpans), nil
		}
	}
	return nil, nil
}

func codexTurnID(span api.Span) string {
	return strings.TrimSpace(span.SpanAttributes[codexTurnIDAttribute])
}

// codexOperationSpans returns the spans of root's operation: the spans of
// its turn (same turn_id) and their descendants, or without a turn ID root's
// subtree. They are ordered oldest first.
func codexOperationSpans(root api.Span, traceSpans []api.Span) []api.Span {
	children := childSpans(traceSpans)
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

	if turnID := codexTurnID(root); turnID != "" {
		for _, span := range traceSpans {
			if span.TraceID == root.TraceID && codexTurnID(span) == turnID {
				visit(span)
			}
		}
	}
	visit(root) // no-op when the turn included it

	sort.SliceStable(grouped, func(i, j int) bool {
		return grouped[i].Timestamp.Before(grouped[j].Timestamp)
	})
	return grouped
}

// childSpans maps span IDs to their child spans, oldest first
func childSpans(spans []api.Span) map[string][]api.Span {
	children := make(map[string][]api.Span)
	for _, span := range spans {
		if span.ParentSpanID != "" {
			children[span.ParentSpanID] = append(children[span.ParentSpanID], span)
		}
	}
	for _, list := range children {
		sort.SliceStable(list, func(i, j int) bool {
			return list[i].Timestamp.Before(list[j].Timestamp)
		})
	}
	return children
}
