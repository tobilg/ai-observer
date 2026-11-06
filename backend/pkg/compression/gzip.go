package compression

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"github.com/tobilg/ai-observer/internal/api"
)

// GzipDecompressMiddleware decompresses gzip-encoded request bodies
func GzipDecompressMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Header.Get("Content-Encoding"), "gzip") {
			reader, err := gzip.NewReader(r.Body)
			if err != nil {
				api.WriteError(w, http.StatusBadRequest, "failed to decompress request body: "+err.Error())
				return
			}
			defer reader.Close()
			r.Body = io.NopCloser(reader)
			r.Header.Del("Content-Encoding")
		}
		next.ServeHTTP(w, r)
	})
}
