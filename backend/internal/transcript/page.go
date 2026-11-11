package transcript

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/tobilg/ai-observer/internal/api"
)

// Search returns the positions (0-based, ascending) of the messages of a
// transcript that contain all terms (case-insensitive substrings, in any
// order), each in its content, tool name, tool input or tool output: the
// session search's semantics (storage.SearchTerms) for one message
func Search(resp *api.TranscriptResponse, terms []string) []int {
	lowerTerms := make([]string, len(terms))
	for i, term := range terms {
		lowerTerms[i] = strings.ToLower(term)
	}
	matches := []int{}
	for i, m := range resp.Messages {
		fields := []string{m.Content, m.ToolName, m.ToolInput, m.ToolOutput}
		for j, field := range fields {
			fields[j] = strings.ToLower(field)
		}
		if containsAll(fields, lowerTerms) {
			matches = append(matches, i)
		}
	}
	return matches
}

// containsAll reports whether each of terms is in one of fields
func containsAll(fields, terms []string) bool {
	for _, term := range terms {
		if !slices.ContainsFunc(fields, func(f string) bool { return strings.Contains(f, term) }) {
			return false
		}
	}
	return true
}

// ErrInvalidCursor indicates a cursor that Page did not return
var ErrInvalidCursor = errors.New("invalid cursor")

// Page returns the limit messages of a transcript that follow cursor, the
// NextCursor of the previous page (empty for the first). The transcript is
// assembled whole from the session's records, so pages bound what is sent
// and shown, not what is read.
func Page(resp *api.TranscriptResponse, limit int, cursor string) (*api.TranscriptResponse, error) {
	offset := 0
	if cursor != "" {
		var err error
		if offset, err = strconv.Atoi(cursor); err != nil || offset < 0 {
			return nil, fmt.Errorf("%w: %q", ErrInvalidCursor, cursor)
		}
	}
	page := *resp
	page.Total = len(resp.Messages)
	start := min(offset, len(resp.Messages))
	end := min(start+max(limit, 1), len(resp.Messages))
	// Never nil: clients read the messages as a list
	page.Messages = append([]api.TranscriptMessage{}, resp.Messages[start:end]...)
	page.HasMore = end < len(resp.Messages)
	page.NextCursor = ""
	if page.HasMore {
		page.NextCursor = strconv.Itoa(end)
	}
	return &page, nil
}
