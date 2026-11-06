package compression

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGzipDecompressMiddleware_WithGzipContent(t *testing.T) {
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
	wrapped := GzipDecompressMiddleware(handler)

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

func TestGzipDecompressMiddleware_WithoutGzipContent(t *testing.T) {
	originalContent := "plain text content"

	var receivedBody string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		receivedBody = string(body)
		w.WriteHeader(http.StatusOK)
	})

	wrapped := GzipDecompressMiddleware(handler)

	req := httptest.NewRequest("POST", "/", bytes.NewBufferString(originalContent))
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if receivedBody != originalContent {
		t.Errorf("body = %q, want %q", receivedBody, originalContent)
	}
}

func TestGzipDecompressMiddleware_InvalidGzip(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := GzipDecompressMiddleware(handler)

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

func TestGzipDecompressMiddleware_RemovesContentEncodingHeader(t *testing.T) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	gw.Write([]byte("test"))
	gw.Close()

	var contentEncodingAfter string
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contentEncodingAfter = r.Header.Get("Content-Encoding")
		w.WriteHeader(http.StatusOK)
	})

	wrapped := GzipDecompressMiddleware(handler)

	req := httptest.NewRequest("POST", "/", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if contentEncodingAfter != "" {
		t.Errorf("Content-Encoding header = %q, want empty", contentEncodingAfter)
	}
}
