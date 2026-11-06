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

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/tools"
)

// codexTurnIDAttribute groups the spans of one Codex operation (a turn)
const codexTurnIDAttribute = "turn_id"

// ErrUnsupportedKind is returned for a trace kind other than api.TraceKind*
var ErrUnsupportedKind = errors.New("unsupported trace kind")

// Store reads spans (implemented by storage.DuckDBStore)
type Store interface {
	// TraceSpans returns the spans of a trace, oldest first; an ID that is no
	// trace ID is looked up as a span ID
	TraceSpans(ctx context.Context, traceID string) ([]api.Span, error)
	// SpanTraceID returns the trace ID of a service's span, or "" when the
	// service has no such span
	SpanTraceID(ctx context.Context, service, spanID string) (string, error)
	// ServiceTraceSpans returns the spans of a service's trace, oldest first
	ServiceTraceSpans(ctx context.Context, service, traceID string) ([]api.Span, error)
}

// Load returns the spans of the trace view id of the given kind, oldest
// first, or none when it does not exist
func Load(ctx context.Context, store Store, id, kind string) ([]api.Span, error) {
	switch kind {
	case api.TraceKindOTelTrace:
		return store.TraceSpans(ctx, id)
	case api.TraceKindCodexOperation:
		return loadCodexOperation(ctx, store, id)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedKind, kind)
	}
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
