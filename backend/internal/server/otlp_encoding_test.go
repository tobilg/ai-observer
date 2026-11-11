package server

import (
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"testing"

	appMiddleware "github.com/tobilg/ai-observer/internal/middleware"
)

// The OTLP router decodes request bodies before the 10 MB payload limit
// applies, so the limit counts decompressed bytes: a small gzip body that
// expands beyond it is rejected (413), unsupported encodings are a 415
func TestOTLPRouterEncodingsAndDecompressedLimit(t *testing.T) {
	srv, err := New(getTestConfig(t))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown(t.Context())

	var body bytes.Buffer
	zw := gzip.NewWriter(&body)
	zw.Write([]byte(`{"resourceLogs":[{"scopeLogs":[{"logRecords":[{"body":{"stringValue":"`))
	zw.Write(bytes.Repeat([]byte("a"), int(appMiddleware.MaxPayloadBytes)))
	zw.Write([]byte(`"}}]}]}]}`))
	zw.Close()
	if body.Len() >= int(appMiddleware.MaxPayloadBytes)/100 {
		t.Fatalf("compressed body has %d bytes", body.Len())
	}

	post := func(encoding string, data []byte) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/logs", bytes.NewReader(data))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Encoding", encoding)
		rec := httptest.NewRecorder()
		srv.otlpRouter.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post("gzip", body.Bytes()); code != http.StatusRequestEntityTooLarge {
		t.Errorf("gzip body over the limit after decompression: status %d, want 413", code)
	}
	if code := post("zstd", []byte("x")); code != http.StatusUnsupportedMediaType {
		t.Errorf("zstd: status %d, want 415", code)
	}
	if code := post("identity", []byte(`{}`)); code != http.StatusOK {
		t.Errorf("identity: status %d, want 200", code)
	}
}
