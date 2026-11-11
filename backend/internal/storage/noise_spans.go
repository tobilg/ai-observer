package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/selfmetrics"
	"github.com/tobilg/ai-observer/internal/tools"
)

// spanRef identifies a span within its trace
type spanRef struct {
	traceID, spanID string
}

// dropNoiseSpans returns spans without the tools' noise spans
// (tools.IsNoiseSpan), in the same order. A noise span is kept when it
// failed, or when a span that is stored has it as parent, so no stored span
// loses its parent: such a child is in the same export or already stored,
// since a span is exported when it ends and its children end before it. A
// child that arrives after its dropped parent is shown as a root of its
// trace.
func dropNoiseSpans(ctx context.Context, tx *sql.Tx, spans []api.Span) ([]api.Span, error) {
	var noise []int
	for i, span := range spans {
		if tools.IsNoiseSpan(span.ServiceName, span.SpanName) && span.StatusCode != "ERROR" {
			noise = append(noise, i)
		}
	}
	if len(noise) == 0 {
		return spans, nil
	}

	candidates := make([]spanRef, len(noise))
	for n, i := range noise {
		candidates[n] = spanRef{spans[i].TraceID, spans[i].SpanID}
	}
	parents, err := storedParents(ctx, tx, candidates)
	if err != nil {
		return nil, err
	}

	// Kept spans keep their ancestors: a span's parent may be listed before
	// or after it, so this repeats until nothing changes (once per level of
	// nested noise spans)
	dropped := make(map[spanRef]bool, len(noise))
	for _, ref := range candidates {
		dropped[ref] = !parents[ref]
	}
	for changed := true; changed; {
		changed = false
		for _, span := range spans {
			parent := spanRef{span.TraceID, span.ParentSpanID}
			if dropped[parent] && !dropped[spanRef{span.TraceID, span.SpanID}] {
				dropped[parent] = false
				changed = true
			}
		}
	}

	kept := make([]api.Span, 0, len(spans))
	for _, span := range spans {
		if !dropped[spanRef{span.TraceID, span.SpanID}] {
			kept = append(kept, span)
		}
	}
	selfmetrics.NoiseSpansDropped.With().Add(len(spans) - len(kept))
	return kept, nil
}

// storedParents returns which of refs are the parent of a stored span. It
// reads the refs' traces by their IDs (idx_traces_trace_id).
func storedParents(ctx context.Context, tx *sql.Tx, refs []spanRef) (map[spanRef]bool, error) {
	parents := make(map[spanRef]bool)
	for start := 0; start < len(refs); start += spanLookupBatchSize {
		batch := refs[start:min(start+spanLookupBatchSize, len(refs))]
		var traceIDs, spanIDs []any
		seenTraces := make(map[string]bool)
		for _, ref := range batch {
			if !seenTraces[ref.traceID] {
				seenTraces[ref.traceID] = true
				traceIDs = append(traceIDs, ref.traceID)
			}
			spanIDs = append(spanIDs, ref.spanID)
		}
		query := "SELECT DISTINCT TraceId, ParentSpanId FROM otel_traces" +
			" WHERE TraceId IN (?" + strings.Repeat(", ?", len(traceIDs)-1) + ")" +
			" AND ParentSpanId IN (?" + strings.Repeat(", ?", len(spanIDs)-1) + ")"

		rows, err := tx.QueryContext(ctx, query, append(traceIDs, spanIDs...)...)
		if err != nil {
			return nil, fmt.Errorf("checking children of noise spans: %w", err)
		}
		for rows.Next() {
			var ref spanRef
			if err := rows.Scan(&ref.traceID, &ref.spanID); err != nil {
				rows.Close()
				return nil, fmt.Errorf("scanning parent span: %w", err)
			}
			parents[ref] = true
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, fmt.Errorf("checking children of noise spans: %w", err)
		}
		rows.Close()
	}
	return parents, nil
}
