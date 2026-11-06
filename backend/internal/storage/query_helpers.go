package storage

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrInvalidAttributeKey indicates a user-provided JSON attribute key cannot
	// be safely used to build a DuckDB JSONPath.
	ErrInvalidAttributeKey = errors.New("invalid attribute key")

	// ErrInvalidCursor indicates a page cursor that no list query returned
	ErrInvalidCursor = errors.New("invalid cursor")

	jsonAttributeKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
)

// maxPageSize bounds the rows of a list page
const maxPageSize = 1000

// maxOffset bounds Page.Offset. An offset page sorts every row before it, so
// a deep offset costs as much as reading its whole range; cursor pages have
// no such limit.
const maxOffset = 10_000

// ErrOffsetTooLarge indicates a Page.Offset beyond maxOffset
var ErrOffsetTooLarge = fmt.Errorf("offset exceeds %d", maxOffset)

func normalizePagination(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 50
	}
	if limit > maxPageSize {
		limit = maxPageSize
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func normalizeIntervalSeconds(intervalSeconds int64) int64 {
	if intervalSeconds <= 0 {
		return 60
	}
	if intervalSeconds > 86400 {
		return 86400
	}
	return intervalSeconds
}

// conditions collects the conditions of a WHERE or HAVING clause together
// with their arguments, so that a condition and its placeholders stay in step
type conditions struct {
	sql  []string
	args []any
}

// add appends a condition and the arguments of its placeholders
func (c *conditions) add(cond string, args ...any) {
	c.sql = append(c.sql, cond)
	c.args = append(c.args, args...)
}

// and returns a copy of c with another condition
func (c conditions) and(cond string, args ...any) conditions {
	out := conditions{sql: append([]string(nil), c.sql...), args: append([]any(nil), c.args...)}
	out.add(cond, args...)
	return out
}

// clause returns " <keyword> a AND b ...", or "" without conditions
func (c conditions) clause(keyword string) string {
	if len(c.sql) == 0 {
		return ""
	}
	return " " + keyword + " " + strings.Join(c.sql, " AND ")
}

// concatArgs joins query arguments in the order of their placeholders
func concatArgs(parts ...[]any) []any {
	var args []any
	for _, part := range parts {
		args = append(args, part...)
	}
	return args
}

// Page selects a page of a list query. The first page has no Cursor; the
// next one passes the NextCursor of the previous page. Cursor pages are
// stable while new data arrives, unlike Offset pages, which shift. Offset is
// kept for API clients that still use it and is ignored with a Cursor.
type Page struct {
	Limit  int
	Offset int
	Cursor string
}

// pageCursor is the sort key of the last row of a page: rows of the next
// page sort after it. Time is the primary key (newest first), RowID or Keys
// break ties.
type pageCursor struct {
	Time  string   `json:"t"` // formatTimeForDB, microsecond precision like the TIMESTAMP columns
	RowID int64    `json:"r,omitempty"`
	Keys  []string `json:"k,omitempty"`
}

func encodeCursor(c pageCursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeCursor parses a cursor with the given number of tie-breaking keys
func decodeCursor(s string, keys int) (pageCursor, error) {
	var c pageCursor
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil || json.Unmarshal(b, &c) != nil || len(c.Keys) != keys {
		return c, ErrInvalidCursor
	}
	if _, err := time.Parse("2006-01-02 15:04:05.999999", c.Time); err != nil {
		return c, ErrInvalidCursor
	}
	return c, nil
}

// pageLimit returns the normalized page size and the offset of page, which
// is 0 for cursor pages: their rows are selected by the cursor condition
func pageLimit(page Page) (limit, offset int, err error) {
	limit, offset = normalizePagination(page.Limit, page.Offset)
	if page.Cursor != "" {
		return limit, 0, nil
	}
	if offset > maxOffset {
		return 0, 0, ErrOffsetTooLarge
	}
	return limit, offset, nil
}

// newestFirstAfter is the condition for rows that sort after the cursor in
// "ORDER BY timeCol DESC, tieCols... DESC" order: older, or as old with
// smaller tie-breaking values. The arguments are the cursor's time and then
// tieArgs, matching tieCols.
func newestFirstAfter(timeCol string, tieCols []string, cursorTime string, tieArgs []any) (string, []any) {
	cond := timeCol + " = ?::TIMESTAMP"
	args := []any{cursorTime}
	var ties []string
	var tieArgList []any
	for i := len(tieCols) - 1; i >= 0; i-- {
		// (a < x) OR (a = x AND <rest>), built from the last column outwards
		if i == len(tieCols)-1 {
			ties = []string{tieCols[i] + " < ?"}
			tieArgList = []any{tieArgs[i]}
			continue
		}
		ties = []string{"(" + tieCols[i] + " < ? OR (" + tieCols[i] + " = ? AND " + ties[0] + "))"}
		tieArgList = append([]any{tieArgs[i], tieArgs[i]}, tieArgList...)
	}
	sql := "(" + timeCol + " < ?::TIMESTAMP OR (" + cond + " AND " + ties[0] + "))"
	return sql, append(append([]any{cursorTime}, args...), tieArgList...)
}

// TimeRange bounds a query by time. A zero From or To leaves that side open.
type TimeRange struct {
	From, To time.Time
}

// filter returns the SQL conditions (each starting with " AND ") limiting
// column to r, and their arguments
func (r TimeRange) filter(column string) (string, []any) {
	var sql string
	var args []any
	if !r.From.IsZero() {
		sql += " AND " + column + " >= ?::TIMESTAMP"
		args = append(args, formatTimeForDB(r.From))
	}
	if !r.To.IsZero() {
		sql += " AND " + column + " <= ?::TIMESTAMP"
		args = append(args, formatTimeForDB(r.To))
	}
	return sql, args
}

// likeEscaper escapes the LIKE wildcards for patterns with ESCAPE '\'
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// containsCondition returns a condition matching rows where any of the
// expressions contains search literally (case-insensitive), and its
// arguments: % and _ in a search are not wildcards. ESCAPE is only added for
// searches that need it, since it makes DuckDB use a slower matcher (2x in a
// benchmark).
func containsCondition(exprs []string, search string) (string, []any) {
	pattern, escape := containsPattern(search)
	conds := make([]string, len(exprs))
	args := make([]any, len(exprs))
	for i, expr := range exprs {
		conds[i] = expr + " ILIKE ?" + escape
		args[i] = pattern
	}
	return "(" + strings.Join(conds, " OR ") + ")", args
}

// containsPattern returns the ILIKE pattern matching text that contains
// search literally, and the ESCAPE clause it needs (empty when it needs none)
func containsPattern(search string) (pattern, escape string) {
	if strings.ContainsAny(search, `%_\`) {
		return "%" + likeEscaper.Replace(search) + "%", ` ESCAPE '\'`
	}
	return "%" + search + "%", ""
}

func metricAttributeJSONPath(attribute string) (string, error) {
	attribute = strings.TrimSpace(attribute)
	if attribute == "" || !jsonAttributeKeyPattern.MatchString(attribute) {
		return "", ErrInvalidAttributeKey
	}
	return `$."` + attribute + `"`, nil
}
