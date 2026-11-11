// Package compression decodes compressed OTLP/HTTP request bodies
package compression

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/tobilg/ai-observer/internal/api"
)

// SupportedEncodings are the request content codings DecompressMiddleware
// accepts, as announced in Accept-Encoding when it rejects one. zstd, which
// some exporters offer, is not among them: it would need a new dependency.
const SupportedEncodings = "gzip, deflate, identity"

// DecompressMiddleware decodes request bodies sent with a Content-Encoding.
// The header is parsed as a list of codings in the order they were applied
// (RFC 9110, section 8.4), matched case-insensitively: gzip (and its alias
// x-gzip), deflate (zlib-wrapped as HTTP specifies, or raw deflate as some
// clients send it) and identity. A request with any other coding is answered
// 415 Unsupported Media Type with Accept-Encoding listing the supported ones
// (RFC 7694); a body that is not valid in its coding is a 400.
//
// The decoded body replaces the request's body, so a limit applied by a later
// middleware (the 10 MB OTLP payload limit) applies to the decompressed bytes.
func DecompressMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		codings, err := parseContentEncoding(r.Header.Values("Content-Encoding"))
		if err != nil {
			w.Header().Set("Accept-Encoding", SupportedEncodings)
			api.WriteError(w, http.StatusUnsupportedMediaType, err.Error())
			return
		}
		if len(codings) == 0 {
			next.ServeHTTP(w, r)
			return
		}
		body := io.Reader(r.Body)
		var closers []io.Closer
		defer func() {
			for _, c := range closers {
				c.Close()
			}
		}()
		// Undo the codings in reverse order of application
		for i := len(codings) - 1; i >= 0; i-- {
			reader, err := decoder(codings[i], body)
			if err != nil {
				api.WriteError(w, http.StatusBadRequest, "failed to decompress request body: "+err.Error())
				return
			}
			closers = append(closers, reader)
			body = reader
		}
		r.Body = readCloser{Reader: body, Closer: r.Body}
		r.Header.Del("Content-Encoding")
		next.ServeHTTP(w, r)
	})
}

// parseContentEncoding returns the codings of Content-Encoding header values
// to undo, normalized (gzip, deflate) and without identity, in the order they
// were applied. An unsupported coding is an error.
func parseContentEncoding(values []string) ([]string, error) {
	var codings []string
	for _, value := range values {
		for _, coding := range strings.Split(value, ",") {
			switch c := strings.ToLower(strings.TrimSpace(coding)); c {
			case "", "identity":
			case "gzip", "x-gzip":
				codings = append(codings, "gzip")
			case "deflate":
				codings = append(codings, "deflate")
			default:
				return nil, fmt.Errorf("unsupported Content-Encoding %q (supported: %s)", strings.TrimSpace(coding), SupportedEncodings)
			}
		}
	}
	return codings, nil
}

// decoder returns a reader that decodes r in coding
func decoder(coding string, r io.Reader) (io.ReadCloser, error) {
	switch coding {
	case "gzip":
		return gzip.NewReader(r)
	case "deflate":
		// HTTP's deflate is a zlib stream (RFC 9110, section 8.4.1.2), but
		// some clients send raw deflate data: a zlib stream starts with a
		// header whose compression method is 8 and whose first two bytes
		// are a multiple of 31
		br := bufio.NewReader(r)
		header, err := br.Peek(2)
		if err != nil {
			return nil, fmt.Errorf("reading deflate header: %w", err)
		}
		if header[0]&0x0f == 8 && (uint16(header[0])<<8|uint16(header[1]))%31 == 0 {
			return zlib.NewReader(br)
		}
		return flate.NewReader(br), nil
	default:
		return nil, fmt.Errorf("unsupported coding %q", coding)
	}
}

// readCloser reads the decoded body and closes the original one
type readCloser struct {
	io.Reader
	io.Closer
}
