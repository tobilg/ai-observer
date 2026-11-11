package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

// A trace is read a page at a time, oldest first, each page with the
// overview of the whole trace
func TestTraceSpansPage(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ts := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)

	// 25 spans, several of them starting at the same time
	var spans []api.Span
	for i := 0; i < 25; i++ {
		span := api.Span{
			Timestamp: ts.Add(time.Duration(i/3) * time.Second), TraceID: "trace", SpanID: fmt.Sprintf("span-%02d", i),
			ParentSpanID: "span-00", ServiceName: "codex-app-server", SpanName: fmt.Sprintf("name-%02d", i),
			Duration: int64(time.Second), StatusCode: "OK",
		}
		if i == 0 {
			span.ParentSpanID, span.Duration = "", int64(time.Minute)
		}
		if i == 20 {
			span.StatusCode = "ERROR"
		}
		spans = append(spans, span)
	}
	other := api.Span{Timestamp: ts, TraceID: "other", SpanID: "other-root", ServiceName: "claude-code", SpanName: "other"}
	if err := store.InsertSpans(ctx, append(spans, other)); err != nil {
		t.Fatal(err)
	}

	var got []string
	page := Page{Limit: 10}
	for pages := 1; ; pages++ {
		resp, err := store.TraceSpansPage(ctx, "trace", page)
		if err != nil {
			t.Fatal(err)
		}
		o := resp.Trace
		if resp.Total != 25 || o == nil || o.SpanCount != 25 || o.TraceID != "trace" || o.RootSpanID != "span-00" ||
			o.RootSpan != "name-00" || o.ServiceName != "codex-app-server" || o.Status != "ERROR" ||
			!o.StartTime.Equal(ts) || o.Duration != int64(time.Minute) || o.Kind != api.TraceKindOTelTrace {
			t.Fatalf("page %d: total %d, overview %+v", pages, resp.Total, o)
		}
		for _, span := range resp.Spans {
			got = append(got, span.SpanID)
		}
		if !resp.HasMore {
			if pages != 3 || resp.NextCursor != "" {
				t.Errorf("%d pages, last cursor %q; want 3 pages", pages, resp.NextCursor)
			}
			break
		}
		page.Cursor = resp.NextCursor
	}
	if len(got) != 25 {
		t.Fatalf("pages hold %d spans, want 25: %v", len(got), got)
	}
	for i, id := range got {
		if id != spans[i].SpanID {
			t.Fatalf("span %d = %s, want %s (oldest first, no span twice): %v", i, id, spans[i].SpanID, got)
		}
	}

	// The spans held in memory page alike
	memory, err := PageOfSpans(append([]api.Span(nil), spans...), Page{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	stored, _ := store.TraceSpansPage(ctx, "trace", Page{Limit: 10})
	if memory.NextCursor != stored.NextCursor || memory.Total != 25 || len(memory.Spans) != 10 {
		t.Errorf("in-memory page: cursor %q, total %d; stored page: cursor %q", memory.NextCursor, memory.Total, stored.NextCursor)
	}
	next, err := PageOfSpans(append([]api.Span(nil), spans...), Page{Limit: 10, Cursor: stored.NextCursor})
	if err != nil || len(next.Spans) != 10 || next.Spans[0].SpanID != "span-10" {
		t.Errorf("in-memory page after the stored page's cursor starts at %v, %v", next, err)
	}

	// A span ID finds its trace; unknown IDs find nothing
	bySpan, err := store.TraceSpansPage(ctx, "other-root", Page{})
	if err != nil || bySpan == nil || bySpan.Trace.TraceID != "other" || len(bySpan.Spans) != 1 || bySpan.Trace.Status != "UNSET" {
		t.Errorf("trace by span ID = %+v, %v", bySpan, err)
	}
	if missing, err := store.TraceSpansPage(ctx, "missing", Page{}); err != nil || missing != nil {
		t.Errorf("unknown trace = %v, %v; want none", missing, err)
	}
	if _, err := store.TraceSpansPage(ctx, "trace", Page{Cursor: "bogus"}); !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("invalid cursor: error = %v, want ErrInvalidCursor", err)
	}
}
