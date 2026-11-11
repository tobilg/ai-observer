package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
)

func paramsFor(query string) *params {
	return newParams(httptest.NewRequest(http.MethodGet, "/x?"+query, nil))
}

func TestParamsGetters(t *testing.T) {
	p := paramsFor("n=7&b=true&e=b&limit=5000&offset=20&cursor=abc&s=hi")
	if got := p.Int("n", 1, 1, 10); got != 7 {
		t.Errorf("Int = %d", got)
	}
	if got := p.Int("missing", 3, 1, 10); got != 3 {
		t.Errorf("Int default = %d", got)
	}
	if !p.Bool("b", false) || p.Bool("missing", false) {
		t.Error("Bool")
	}
	if got := p.Enum("e", "a", "a", "b"); got != "b" {
		t.Errorf("Enum = %q", got)
	}
	page := p.Page()
	if page.Limit != maxPageSize || page.Offset != 20 || page.Cursor != "abc" {
		t.Errorf("Page = %+v, want limit clamped to %d", page, maxPageSize)
	}
	if p.Str("s") != "hi" {
		t.Error("Str")
	}
	if err := p.Err(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParamsCollectsErrors(t *testing.T) {
	p := paramsFor("n=11&b=yes&e=c&limit=0&offset=-1&from=yesterday")
	p.Int("n", 1, 1, 10)
	p.Bool("b", false)
	p.Enum("e", "a", "a", "b")
	p.Page()
	p.TimeRange()
	p.Required("name")
	err := p.Err()
	if err == nil {
		t.Fatal("expected errors")
	}
	for _, want := range []string{
		`invalid n "11": expected an integer from 1 to 10`,
		`invalid b "yes": expected true or false`,
		`invalid e "c": expected one of a, b`,
		`invalid limit "0": expected a positive integer`,
		`invalid offset "-1"`,
		`invalid from "yesterday"`,
		`name is required`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}

	rec := httptest.NewRecorder()
	if p.Valid(rec) {
		t.Fatal("Valid = true")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
	var body api.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil || !strings.Contains(body.Message, "invalid n") {
		t.Errorf("body = %+v, %v", body, err)
	}
}

func TestParamsOffsetTooLarge(t *testing.T) {
	p := paramsFor("offset=10001")
	p.Page()
	if err := p.Err(); err == nil || !strings.Contains(err.Error(), "offset exceeds 10000") {
		t.Errorf("err = %v", err)
	}
}

func TestParamsOptionalTimeRange(t *testing.T) {
	p := paramsFor("from=2025-01-02T00:00:00Z")
	tr := p.OptionalTimeRange()
	if !tr.To.IsZero() || !tr.From.Equal(time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC)) || p.Err() != nil {
		t.Errorf("tr = %+v, err = %v", tr, p.Err())
	}
	p = paramsFor("from=2025-01-02T00:00:00Z&to=2025-01-01T00:00:00Z")
	p.OptionalTimeRange()
	if err := p.Err(); err == nil || !strings.Contains(err.Error(), "from must not be after to") {
		t.Errorf("err = %v", err)
	}
}
