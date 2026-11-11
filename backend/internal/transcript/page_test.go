package transcript

import (
	"errors"
	"testing"

	"github.com/tobilg/ai-observer/internal/api"
)

func TestPage(t *testing.T) {
	whole := &api.TranscriptResponse{SessionID: "s1", ServiceName: "claude-code"}
	for i := 0; i < 5; i++ {
		whole.Messages = append(whole.Messages, api.TranscriptMessage{Index: i})
	}

	var got []int
	cursor := ""
	for pages := 0; ; pages++ {
		page, err := Page(whole, 2, cursor)
		if err != nil {
			t.Fatal(err)
		}
		if page.Total != 5 || page.SessionID != "s1" {
			t.Errorf("page %d: total %d, session %q; want 5 of s1", pages, page.Total, page.SessionID)
		}
		for _, m := range page.Messages {
			got = append(got, m.Index)
		}
		if !page.HasMore {
			if page.NextCursor != "" || pages != 2 {
				t.Errorf("last page %d has cursor %q, want the third page without one", pages, page.NextCursor)
			}
			break
		}
		cursor = page.NextCursor
	}
	if len(got) != 5 || got[0] != 0 || got[4] != 4 {
		t.Errorf("paged messages = %v, want 0 to 4 once each", got)
	}
	if len(whole.Messages) != 5 {
		t.Error("Page changed the transcript it pages")
	}

	// A cursor past the end, e.g. after records were deleted, is an empty page
	if page, err := Page(whole, 2, "9"); err != nil || page.Messages == nil || len(page.Messages) != 0 || page.HasMore {
		t.Errorf("page past the end = %+v, %v; want an empty list", page, err)
	}
	for _, cursor := range []string{"x", "-1"} {
		if _, err := Page(whole, 2, cursor); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("cursor %q: %v, want ErrInvalidCursor", cursor, err)
		}
	}
}
