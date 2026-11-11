package compression

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tobilg/ai-observer/internal/middleware"
)

func TestDecompressMiddleware_WithGzipContent(t *testing.T) {
	// Create gzip-compressed content
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	originalContent := "hello world"
	gw.Write([]byte(originalContent))
	gw.Close()

	// Create handler that reads the body
	var receivedBody string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	})

	// Wrap with middleware
	wrapped := DecompressMiddleware(handler)

	// Create request with gzip content
	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if receivedBody != originalContent {
		t.Errorf("body = %q, want %q", receivedBody, originalContent)
	}
	if rr.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestDecompressMiddleware_WithoutGzipContent(t *testing.T) {
	originalContent := "plain text content"

	var receivedBody string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	})

	wrapped := DecompressMiddleware(handler)

	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(originalContent))
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if receivedBody != originalContent {
		t.Errorf("body = %q, want %q", receivedBody, originalContent)
	}
}

func TestDecompressMiddleware_InvalidGzip(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := DecompressMiddleware(handler)

	// Send invalid gzip data
	req := httptest.NewRequest("POST", "/", bytes.NewBufferString("not valid gzip"))
	req.Header.Set("Content-Encoding", "gzip")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d for invalid gzip", rr.Code, http.StatusBadRequest)
	}
	// The same JSON error format as the other OTLP errors
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestDecompressMiddleware_RemovesContentEncodingHeader(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte("test"))
	gw.Close()

	var contentEncodingAfter string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentEncodingAfter = r.Header.Get("Content-Encoding")
		w.WriteHeader(http.StatusOK)
	})

	wrapped := DecompressMiddleware(handler)

	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if contentEncodingAfter != "" {
		t.Errorf("Content-Encoding header = %q, want empty", contentEncodingAfter)
	}
}

func compressed(t *testing.T, coding, content string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	var w io.WriteCloser
	switch coding {
	case "gzip":
		w = gzip.NewWriter(&buf)
	case "zlib":
		w = zlib.NewWriter(&buf)
	case "flate":
		fw, err := flate.NewWriter(&buf, flate.DefaultCompression)
		if err != nil {
			t.Fatal(err)
		}
		w = fw
	default:
		t.Fatalf("unknown coding %s", coding)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf
}

// echo runs a request through the middleware and returns the status and the
// body the handler read
func echo(t *testing.T, body io.Reader, encodings ...string) (int, string, http.Header) {
	t.Helper()
	var received string
	handler := DecompressMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		received = string(data)
	}))
	req := httptest.NewRequest("POST", "/v1/logs", body)
	for _, e := range encodings {
		req.Header.Add("Content-Encoding", e)
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	return rr.Code, received, rr.Header()
}

func TestDecompressMiddlewareCodings(t *testing.T) {
	const content = "hello world, hello world"
	tests := []struct {
		name      string
		body      func() io.Reader
		encodings []string
	}{
		{"gzip", func() io.Reader { return compressed(t, "gzip", content) }, []string{"gzip"}},
		{"gzip upper case with spaces", func() io.Reader { return compressed(t, "gzip", content) }, []string{" GZIP "}},
		{"x-gzip alias", func() io.Reader { return compressed(t, "gzip", content) }, []string{"x-gzip"}},
		{"deflate as zlib stream", func() io.Reader { return compressed(t, "zlib", content) }, []string{"deflate"}},
		{"raw deflate", func() io.Reader { return compressed(t, "flate", content) }, []string{"deflate"}},
		{"identity", func() io.Reader { return strings.NewReader(content) }, []string{"identity"}},
		{"identity listed with gzip", func() io.Reader { return compressed(t, "gzip", content) }, []string{"identity, gzip"}},
		{"no encoding", func() io.Reader { return strings.NewReader(content) }, nil},
		{"gzip then deflate, in one header", func() io.Reader {
			return compressed(t, "zlib", compressed(t, "gzip", content).String())
		}, []string{"gzip, deflate"}},
		{"gzip then deflate, in two headers", func() io.Reader {
			return compressed(t, "zlib", compressed(t, "gzip", content).String())
		}, []string{"gzip", "deflate"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, received, _ := echo(t, tt.body(), tt.encodings...)
			if code != http.StatusOK || received != content {
				t.Errorf("status %d, body %q; want 200 and %q", code, received, content)
			}
		})
	}
}

// Unsupported codings are a 415 that names the supported ones, so exporters
// can fall back (RFC 7694); zstd would need a new dependency
func TestDecompressMiddlewareRejectsUnsupportedCoding(t *testing.T) {
	for _, encoding := range []string{"zstd", "br", "gzip, zstd", "compress"} {
		code, _, header := echo(t, strings.NewReader("x"), encoding)
		if code != http.StatusUnsupportedMediaType {
			t.Errorf("%s: status = %d, want 415", encoding, code)
		}
		if got := header.Get("Accept-Encoding"); got != SupportedEncodings {
			t.Errorf("%s: Accept-Encoding = %q, want %q", encoding, got, SupportedEncodings)
		}
	}
}

// strings.Contains matching took "gzipped" or "x-gzip-not" for gzip
func TestDecompressMiddlewareMatchesWholeCodings(t *testing.T) {
	if code, _, _ := echo(t, strings.NewReader("x"), "gzipped"); code != http.StatusUnsupportedMediaType {
		t.Errorf("status = %d, want 415 for a coding that only contains gzip", code)
	}
}

func TestDecompressMiddlewareInvalidDeflate(t *testing.T) {
	if code, _, _ := echo(t, strings.NewReader("not deflate data at all"), "deflate"); code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
}

// The OTLP payload limit wraps the decompressed body: a small compressed
// body that expands beyond the limit is rejected while it is read
func TestPayloadLimitAppliesToDecompressedBytes(t *testing.T) {
	const limit = 1024
	body := compressed(t, "gzip", strings.Repeat("a", 10*limit))
	if body.Len() >= limit {
		t.Fatalf("compressed body has %d bytes, the test needs fewer than %d", body.Len(), limit)
	}
	var readErr error
	handler := DecompressMiddleware(middleware.PayloadLimitMiddleware(limit)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	})))
	req := httptest.NewRequest("POST", "/v1/logs", body)
	req.Header.Set("Content-Encoding", "gzip")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	var tooLarge *http.MaxBytesError
	if !errors.As(readErr, &tooLarge) {
		t.Errorf("read error = %v, want the payload limit's MaxBytesError", readErr)
	}
}
