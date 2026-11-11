package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// clock is a settable time for the limiter
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func TestAuthLimiterBlocksAfterFailures(t *testing.T) {
	auth := NewTokenAuth("s3cret")
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	auth.limiter.now = c.now
	h := auth.APIMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))

	request := func(addr, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/services", nil)
		req.RemoteAddr = addr
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	// Requests without credentials and a repeated wrong token are no guesses
	for range 2 * authFailureLimit {
		if rec := request("10.0.0.1:1000", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("no token: %d", rec.Code)
		}
		if rec := request("10.0.0.1:1000", "old-token"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("old token: %d", rec.Code)
		}
	}
	// Distinct wrong tokens are (old-token counted once): the limit-th one
	// blocks the address
	for i := range authFailureLimit - 2 {
		if rec := request("10.0.0.1:1001", fmt.Sprintf("guess-%d", i)); rec.Code != http.StatusUnauthorized {
			t.Fatalf("guess %d: %d", i, rec.Code)
		}
	}
	if rec := request("10.0.0.1:1002", "s3cret"); rec.Code != http.StatusOK {
		t.Fatalf("valid token before the block: %d", rec.Code)
	}
	if rec := request("10.0.0.1:1003", "last-guess"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("last guess: %d", rec.Code)
	}

	// Blocked: even the valid token gets 429, with Retry-After
	rec := request("10.0.0.1:1004", "s3cret")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked address: %d, want 429", rec.Code)
	}
	if retry, _ := strconv.Atoi(rec.Header().Get("Retry-After")); retry != int(authBlockDuration.Seconds()) {
		t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
	// Other addresses are not affected
	if rec := request("10.0.0.2:1000", "s3cret"); rec.Code != http.StatusOK {
		t.Errorf("other address: %d", rec.Code)
	}
	// The block ends
	c.t = c.t.Add(authBlockDuration - time.Second)
	if rec := request("10.0.0.1:1005", "s3cret"); rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "1" {
		t.Errorf("almost unblocked: %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
	c.t = c.t.Add(time.Second)
	if rec := request("10.0.0.1:1005", "s3cret"); rec.Code != http.StatusOK {
		t.Errorf("after the block: %d", rec.Code)
	}
}

func TestAuthLimiterCountsWithinWindow(t *testing.T) {
	l := newAuthLimiter()
	c := &clock{t: time.Unix(1_700_000_000, 0)}
	l.now = c.now
	for i := range authFailureLimit - 1 {
		if l.fail("a", strconv.Itoa(i)) {
			t.Fatal("blocked early")
		}
	}
	// The window passed: counting starts again
	c.t = c.t.Add(authFailureWindow)
	for i := range authFailureLimit - 1 {
		if l.fail("a", "next-"+strconv.Itoa(i)) {
			t.Fatal("failures of an earlier window counted")
		}
	}
	if !l.fail("a", "last") || l.blocked("a") != authBlockDuration {
		t.Error("not blocked at the limit")
	}
}

func TestAuthLimiterStaysBounded(t *testing.T) {
	l := newAuthLimiter()
	l.maxAddrs = 100
	for i := range 1000 {
		l.fail(fmt.Sprintf("10.0.%d.%d", i/256, i%256), "x")
	}
	if n := len(l.addrs); n > l.maxAddrs {
		t.Errorf("remembers %d addresses, at most %d", n, l.maxAddrs)
	}
}

func TestAuthLimiterOnTokenLinksAndOTLP(t *testing.T) {
	server := newAuthServer(t)
	// Wrong ?token= links count
	for i := range authFailureLimit {
		req := httptest.NewRequest(http.MethodGet, "/?token=wrong"+strconv.Itoa(i), nil)
		req.RemoteAddr = "192.0.2.7:5000"
		serve(server.apiRouter, req)
	}
	req := httptest.NewRequest(http.MethodGet, "/?token=s3cret", nil)
	req.RemoteAddr = "192.0.2.7:5000"
	if rec := serve(server.apiRouter, req); rec.Code != http.StatusTooManyRequests {
		t.Errorf("token link from a blocked address: %d, want 429", rec.Code)
	}
	// The OTLP router shares the limiter
	req = httptest.NewRequest(http.MethodPost, "/v1/logs", nil)
	req.RemoteAddr = "192.0.2.7:5001"
	req.Header.Set("Authorization", "Bearer s3cret")
	if rec := serve(server.otlpRouter, req); rec.Code != http.StatusTooManyRequests {
		t.Errorf("OTLP from a blocked address: %d, want 429", rec.Code)
	}
	// Health and readiness need no token and are never blocked
	for _, path := range []string{"/health", "/ready"} {
		req = httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "192.0.2.7:5002"
		if rec := serve(server.otlpRouter, req); rec.Code != http.StatusOK {
			t.Errorf("OTLP %s: %d", path, rec.Code)
		}
		if rec := serve(server.apiRouter, req); rec.Code != http.StatusOK {
			t.Errorf("API %s: %d", path, rec.Code)
		}
	}
}
