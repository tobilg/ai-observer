package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/storage"
)

// List page sizes. Every list endpoint takes limit as a positive integer;
// larger values than the endpoint's maximum are clamped to it (as storage
// always did), while non-numbers, zero and negative values are rejected.
const (
	defaultPageSize = 50
	maxPageSize     = 1000
	// maxOffset is storage's limit for the deprecated offset parameter
	maxOffset = 10_000
)

// params reads the query parameters of a request and collects every invalid
// one, so a handler answers a single 400 naming all of them:
//
//	p := newParams(r)
//	limit := p.Limit("limit", 50, 100)
//	from, to := p.TimeRange()
//	if !p.Valid(w) {
//		return
//	}
//
// A getter returns its default when the parameter is missing or empty and
// when it is invalid (after recording the error). Unknown parameters are
// ignored.
type params struct {
	q    url.Values
	errs []string
}

func newParams(r *http.Request) *params {
	return &params{q: r.URL.Query()}
}

// Str returns the parameter as given ("" when missing)
func (p *params) Str(name string) string {
	return p.q.Get(name)
}

// Required returns the parameter, recording an error when it is missing or
// empty
func (p *params) Required(name string) string {
	v := p.q.Get(name)
	if v == "" {
		p.Fail(name + " is required")
	}
	return v
}

// Int returns an integer in [min, max]; other values are errors
func (p *params) Int(name string, def, min, max int) int {
	v := p.q.Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < min || n > max {
		p.Invalid(name, fmt.Sprintf("an integer from %d to %d", min, max))
		return def
	}
	return n
}

// Int64 returns an integer in [min, max]; other values are errors
func (p *params) Int64(name string, def, min, max int64) int64 {
	v := p.q.Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < min || n > max {
		p.Invalid(name, fmt.Sprintf("an integer from %d to %d", min, max))
		return def
	}
	return n
}

// Limit returns a page size: a positive integer, clamped to max
func (p *params) Limit(name string, def, max int) int {
	v := p.q.Get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		p.Invalid(name, fmt.Sprintf("a positive integer (values above %d are clamped to it)", max))
		return def
	}
	return min(n, max)
}

// Bool returns true for "true" and false for "false"; other values are errors
func (p *params) Bool(name string, def bool) bool {
	switch v := p.q.Get(name); v {
	case "":
		return def
	case "true":
		return true
	case "false":
		return false
	default:
		p.Invalid(name, "true or false")
		return def
	}
}

// Enum returns one of allowed; other values are errors
func (p *params) Enum(name, def string, allowed ...string) string {
	v := p.q.Get(name)
	if v == "" {
		return def
	}
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	p.Invalid(name, "one of "+strings.Join(allowed, ", "))
	return def
}

// TimeRange returns the range of from and to (RFC 3339): to defaults to now
// and from to 24 hours before to; from after to is an error
func (p *params) TimeRange() (from, to time.Time) {
	from, to, err := parseTimeRange(p.q.Get("from"), p.q.Get("to"))
	if err != nil {
		p.Fail(err.Error())
	}
	return from, to
}

// OptionalTimeRange returns from and to (RFC 3339), leaving a missing side
// open (zero); from after to is an error
func (p *params) OptionalTimeRange() storage.TimeRange {
	var tr storage.TimeRange
	tr.From = p.time("from")
	tr.To = p.time("to")
	if !tr.From.IsZero() && !tr.To.IsZero() && tr.From.After(tr.To) {
		p.Fail("from must not be after to")
	}
	return tr
}

func (p *params) time(name string) time.Time {
	v := p.q.Get(name)
	if v == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		p.Invalid(name, "an RFC 3339 time")
		return time.Time{}
	}
	return t
}

// Page returns the page of a list request: limit (default
// defaultPageSize, clamped to maxPageSize), cursor, and the deprecated
// offset (0 to maxOffset)
func (p *params) Page() storage.Page {
	return p.PageOf(defaultPageSize, maxPageSize)
}

// PageOf is Page with another default and maximum page size
func (p *params) PageOf(def, max int) storage.Page {
	page := storage.Page{
		Limit:  p.Limit("limit", def, max),
		Cursor: p.q.Get("cursor"),
	}
	if v := p.q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		switch {
		case err != nil || n < 0:
			p.Invalid("offset", fmt.Sprintf("an integer from 0 to %d", maxOffset))
		case n > maxOffset:
			p.Fail(fmt.Sprintf("offset exceeds %d: page with cursor (the nextCursor of the previous page) instead", maxOffset))
		default:
			page.Offset = n
		}
	}
	return page
}

// SearchTerms returns the terms of the search parameter (storage.SearchTerms:
// words and "quoted phrases", at most storage.MaxSearchTerms), nil when it
// is missing. Its errors start with "invalid search".
func (p *params) SearchTerms() []string {
	v := p.q.Get("search")
	if v == "" {
		return nil
	}
	terms, err := storage.SearchTerms(v)
	if err != nil {
		p.Fail(err.Error())
		return nil
	}
	return terms
}

// Invalid records that name has an invalid value, saying what is accepted
func (p *params) Invalid(name, accepted string) {
	p.Fail(fmt.Sprintf("invalid %s %q: expected %s", name, p.q.Get(name), accepted))
}

// Fail records an error message
func (p *params) Fail(message string) {
	p.errs = append(p.errs, message)
}

// Err returns the recorded errors as one error, or nil
func (p *params) Err() error {
	if len(p.errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(p.errs, "; "))
}

// Valid reports whether all parameters read so far were valid. Otherwise it
// writes a 400 response listing the errors.
func (p *params) Valid(w http.ResponseWriter) bool {
	if err := p.Err(); err != nil {
		api.WriteError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

// parseTimeRange parses from and to (RFC 3339). to defaults to now and from
// to 24 hours before to. Invalid values and from after to are errors, rather
// than silently showing a different range.
func parseTimeRange(fromStr, toStr string) (from, to time.Time, err error) {
	to = time.Now()
	if toStr != "" {
		if to, err = time.Parse(time.RFC3339, toStr); err != nil {
			return from, to, fmt.Errorf("invalid to %q: expected an RFC 3339 time", toStr)
		}
	}
	from = to.Add(-24 * time.Hour)
	if fromStr != "" {
		if from, err = time.Parse(time.RFC3339, fromStr); err != nil {
			return from, to, fmt.Errorf("invalid from %q: expected an RFC 3339 time", fromStr)
		}
	}
	if from.After(to) {
		return from, to, fmt.Errorf("from must not be after to")
	}
	return from, to, nil
}
