package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/tools"
)

// SessionMatch is a session with logs or spans that contain the search terms
type SessionMatch struct {
	api.Session
	// Matches counts the session's records in the range containing all terms
	Matches int
	// MatchedAt is the time of the newest match
	MatchedAt time.Time
	// Snippet is the text around the terms in the newest matching record
	Snippet string
}

// snippetRadius is the number of characters kept on each side of a match
const snippetRadius = 120

// SearchSessions returns the sessions with logs or spans in [from, to] that
// contain every one of terms (case-insensitive substrings, in any order, in
// the log body or attribute values, not attribute keys), the most recently
// matched first. Terms must appear in the same record, not just somewhere in
// the session. Logs carry the conversations of most tools; GitHub Copilot's
// live in GenAI span attributes. Each session shows its whole extent, like
// the session list. Snippets are taken from redacted text, so a cut cannot
// split a secret into parts the redactor no longer recognizes.
func (s *DuckDBStore) SearchSessions(ctx context.Context, terms []string, service string, from, to time.Time, limit int) ([]SessionMatch, error) {
	terms = slices.DeleteFunc(slices.Clone(terms), func(t string) bool { return strings.TrimSpace(t) == "" })
	if len(terms) == 0 {
		return nil, fmt.Errorf("a search term is required")
	}
	limit, _ = normalizePagination(limit, 0)
	// DuckDB checks the terms in the order of the conditions, each only in
	// the records that contained the ones before, so a rare term first saves
	// most of the work (2.4x for three terms in a benchmark). Longer terms
	// are usually rarer.
	byLength := slices.Clone(terms)
	slices.SortStableFunc(byLength, func(a, b string) int { return len(b) - len(a) })

	type key struct{ id, service string }
	matches := make(map[key]*SessionMatch)
	collect := func(table, textColumns string, plainColumns, jsonColumns []string) error {
		var where conditions
		where.add("SessionId IS NOT NULL")
		where.add("Timestamp >= ?::TIMESTAMP AND Timestamp <= ?::TIMESTAMP", formatTimeForDB(from), formatTimeForDB(to))
		if service != "" {
			where.add("ServiceName = ?", service)
		}
		for _, term := range byLength {
			cond, args := termCondition(plainColumns, jsonColumns, term)
			where.add(cond, args...)
		}
		query := `
			SELECT SessionId, ServiceName, COUNT(*), MAX(Timestamp), CAST(arg_max(` + textColumns + `, Timestamp) AS VARCHAR)
			FROM ` + table + where.clause("WHERE") + `
			GROUP BY SessionId, ServiceName
			ORDER BY MAX(Timestamp) DESC
			LIMIT ?`
		rows, err := s.db.QueryContext(ctx, query, append(where.args, limit)...)
		if err != nil {
			return fmt.Errorf("searching %s: %w", table, err)
		}
		defer rows.Close()
		for rows.Next() {
			var k key
			var count int
			var at time.Time
			var texts sql.NullString
			if err := rows.Scan(&k.id, &k.service, &count, &at, &texts); err != nil {
				return fmt.Errorf("scanning %s match: %w", table, err)
			}
			m := matches[k]
			if m == nil {
				m = &SessionMatch{Session: api.Session{SessionID: k.id, ServiceName: k.service, StartTime: at, LastTime: at}}
				matches[k] = m
			}
			m.Matches += count
			if at.After(m.MatchedAt) {
				m.MatchedAt = at
				m.Snippet = snippetFrom(texts.String, terms, s.redactor.String)
			}
		}
		return rows.Err()
	}

	// The newest matching record's texts come back as one JSON array, so
	// the snippet can be taken from the field that matched
	if err := collect("otel_logs", "to_json([COALESCE(Body, ''), CAST(LogAttributes AS VARCHAR)])",
		[]string{"Body"}, []string{"LogAttributes"}); err != nil {
		return nil, err
	}
	if err := collect("otel_traces", "to_json([CAST(SpanAttributes AS VARCHAR)])",
		nil, []string{"SpanAttributes"}); err != nil {
		return nil, err
	}

	result := make([]SessionMatch, 0, len(matches))
	for _, m := range matches {
		result = append(result, *m)
	}
	sort.Slice(result, func(i, j int) bool {
		if !result[i].MatchedAt.Equal(result[j].MatchedAt) {
			return result[i].MatchedAt.After(result[j].MatchedAt)
		}
		return result[i].SessionID < result[j].SessionID
	})
	if len(result) > limit {
		result = result[:limit]
	}

	sessions := make([]api.Session, len(result))
	for i, m := range result {
		sessions[i] = m.Session
	}
	if err := s.fillSessionExtents(ctx, sessions); err != nil {
		return nil, err
	}
	if err := s.fillSpanSessionExtents(ctx, sessions); err != nil {
		return nil, err
	}
	for i := range result {
		result[i].Session = sessions[i]
	}
	return result, nil
}

// termCondition returns the condition that a record contains term in one of
// the plain text columns or in a value of one of the JSON attribute columns.
// Keys are not searched: "id" would match every record with a session.id.
// Extracting the values is about 3x slower than reading the JSON text, so
// the JSON text is checked first, which only records containing the term
// somewhere pass (only when the term reads the same in JSON, as most do).
func termCondition(plainColumns, jsonColumns []string, term string) (string, []any) {
	var conds []string
	var args []any
	pattern, escape := containsPattern(term)
	for _, column := range plainColumns {
		conds = append(conds, column+" ILIKE ?"+escape)
		args = append(args, pattern)
	}
	prefilter := jsonUnchanged(term)
	for _, column := range jsonColumns {
		values := "array_to_string(json_extract_string(" + column + ", '$.*'), chr(10)) ILIKE ?" + escape
		if prefilter {
			conds = append(conds, "(CAST("+column+" AS VARCHAR) ILIKE ?"+escape+" AND "+values+")")
			args = append(args, pattern, pattern)
		} else {
			conds = append(conds, values)
			args = append(args, pattern)
		}
	}
	return "(" + strings.Join(conds, " OR ") + ")", args
}

// jsonUnchanged reports whether term is written the same inside a JSON
// string: it has no characters JSON escapes (quotes, backslashes, control
// characters, and <, > and & which Go's encoder escapes too)
func jsonUnchanged(term string) bool {
	for _, r := range term {
		if r < 0x20 || r == '"' || r == '\\' || r == '<' || r == '>' || r == '&' || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// fillSpanSessionExtents fills the extent of sessions that have no logs
// (fillSessionExtents found none) from their spans: GitHub Copilot sessions
// can consist of spans only. MessageCount is then the number of spans.
func (s *DuckDBStore) fillSpanSessionExtents(ctx context.Context, sessions []api.Session) error {
	type sessionKey struct{ id, service string }
	index := make(map[sessionKey]int)
	var where conditions
	var ids []any
	for i, session := range sessions {
		if session.MessageCount > 0 {
			continue
		}
		index[sessionKey{session.SessionID, session.ServiceName}] = i
		ids = append(ids, session.SessionID)
	}
	if len(ids) == 0 {
		return nil
	}
	where.add("SessionId IN ("+strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")+")", ids...)
	query := `
		SELECT SessionId, ServiceName, MIN(Timestamp), MAX(Timestamp + to_microseconds(COALESCE(Duration, 0) // 1000)), COUNT(*),
			arg_max(` + spanModelExpr + `, Timestamp) FILTER (WHERE ` + spanModelExpr + ` IS NOT NULL)
		FROM otel_traces` + where.clause("WHERE") + `
		GROUP BY SessionId, ServiceName`
	rows, err := s.db.QueryContext(ctx, query, where.args...)
	if err != nil {
		return fmt.Errorf("querying span session extents: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var key sessionKey
		var start, last time.Time
		var count int
		var model sql.NullString
		if err := rows.Scan(&key.id, &key.service, &start, &last, &count, &model); err != nil {
			return fmt.Errorf("scanning span session extent: %w", err)
		}
		if i, ok := index[key]; ok {
			sessions[i].StartTime, sessions[i].LastTime = start, last
			sessions[i].MessageCount = count
			sessions[i].Model = model.String
		}
	}
	return rows.Err()
}

// spanModelExpr is the model of a span: the first of the model attributes
var spanModelExpr = attributeValueExpr("SpanAttributes", tools.ModelKeys)

// snippetFrom returns the text around the terms in the texts of a record: a
// JSON array of strings, which may themselves be JSON objects of attributes,
// whose values are searched one by one. Each text is redacted first. The
// text containing the most terms is used (the first of equals), since terms
// may be spread over the body and the attributes.
func snippetFrom(texts string, terms []string, redact func(string) string) string {
	var fields []string
	if err := json.Unmarshal([]byte(texts), &fields); err != nil {
		return ""
	}
	best, bestCount := "", 0
	consider := func(text string) {
		if countTerms(text, terms) <= bestCount {
			return // skips redacting texts that cannot win
		}
		text = redact(text)
		if n := countTerms(text, terms); n > bestCount {
			best, bestCount = text, n
		}
	}
	for _, field := range fields {
		var attrs map[string]any
		if strings.HasPrefix(field, "{") && json.Unmarshal([]byte(field), &attrs) == nil {
			keys := make([]string, 0, len(attrs))
			for k := range attrs {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if v, ok := attrs[k].(string); ok {
					consider(v)
				}
			}
			continue
		}
		consider(field)
	}
	snippet, _ := snippetAround(best, terms)
	return snippet
}

// countTerms returns how many of terms text contains (case-insensitive)
func countTerms(text string, terms []string) int {
	lower, n := strings.ToLower(text), 0
	for _, term := range terms {
		if strings.Contains(lower, strings.ToLower(term)) {
			n++
		}
	}
	return n
}

// snippetAround returns the text from the first to the last of the terms'
// first case-insensitive occurrences, with up to snippetRadius bytes on each
// side. When the terms lie further apart than snippetRadius, only the first
// occurrence is shown.
func snippetAround(text string, terms []string) (string, bool) {
	lowerText, offsets := lowerWithOffsets(text)
	first, firstEnd, last := -1, -1, -1
	for _, term := range terms {
		lowerTerm, _ := lowerWithOffsets(term)
		i := strings.Index(lowerText, lowerTerm)
		if i < 0 || lowerTerm == "" {
			continue
		}
		// Back to the byte offsets of text
		start, end := offsets[i], offsets[i+len(lowerTerm)]
		if first < 0 || start < first {
			first, firstEnd = start, end
		}
		last = max(last, end)
	}
	if first < 0 {
		return "", false
	}
	if last-first > snippetRadius {
		last = firstEnd
	}
	start, end := first-snippetRadius, last+snippetRadius
	prefix, suffix := "…", "…"
	if start <= 0 {
		start, prefix = 0, ""
	}
	if end >= len(text) {
		end, suffix = len(text), ""
	}
	for start > 0 && !utf8.RuneStart(text[start]) {
		start--
	}
	for end < len(text) && !utf8.RuneStart(text[end]) {
		end++
	}
	return prefix + strings.Join(strings.Fields(text[start:end]), " ") + suffix, true
}

// lowerWithOffsets lowercases text rune by rune and returns, for each byte
// of the result (and its end), the offset of the rune in text it came from:
// lowercasing can change a rune's length in bytes
func lowerWithOffsets(text string) (string, []int) {
	var b strings.Builder
	b.Grow(len(text))
	offsets := make([]int, 0, len(text)+1)
	for i, r := range text {
		n := b.Len()
		b.WriteRune(unicode.ToLower(r))
		for range b.Len() - n {
			offsets = append(offsets, i)
		}
	}
	offsets = append(offsets, len(text))
	return b.String(), offsets
}
