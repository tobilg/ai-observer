package frontend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSPAHandlerCacheHeaders(t *testing.T) {
	h, err := NewSPAHandler()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := distFS.ReadDir("dist/assets")
	if err != nil || len(entries) == 0 {
		t.Skip("the frontend is not built")
	}

	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if got := get("/assets/" + entries[0].Name()).Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("asset Cache-Control = %q, want immutable", got)
	}
	// The page, SPA routes and missing assets (which answer with the page)
	// must not be kept
	for _, path := range []string{"/", "/traces", "/assets/missing-abc123.js"} {
		rec := get(path)
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("%s: Cache-Control = %q, want no-cache", path, got)
		}
		if !strings.Contains(rec.Body.String(), "<html") {
			t.Errorf("%s did not serve the page", path)
		}
	}
}
