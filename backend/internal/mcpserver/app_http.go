package mcpserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Modern clients include capabilities on each request. A legacy Apps client
// sends them only on initialize, so use an SDK session for that handshake
// and its subsequent requests. Text-only clients can continue without one.
func appHTTPHandler(stateless, legacyApps http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Header.Get("Mcp-Session-Id") != "" {
			legacyApps.ServeHTTP(w, r)
			return
		}
		method := r.Header.Get("Mcp-Method")
		if r.Method == http.MethodPost && r.Header.Get("Mcp-Protocol-Version") < "2026-07-28" && (method == "" || method == "initialize") {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mcp.DefaultMaxRequestBodyBytes))
			if err != nil {
				status := http.StatusBadRequest
				var tooLarge *http.MaxBytesError
				if errors.As(err, &tooLarge) {
					status = http.StatusRequestEntityTooLarge
				}
				http.Error(w, err.Error(), status)
				return
			}
			r.Body.Close()
			r.Body = io.NopCloser(bytes.NewReader(body))
			var handshake struct {
				Method string
				Params mcp.InitializeParams
			}
			if json.Unmarshal(body, &handshake) == nil && handshake.Method == "initialize" && supportsApp(handshake.Params.Capabilities) {
				legacyApps.ServeHTTP(w, r)
				return
			}
		}
		stateless.ServeHTTP(w, r)
	})
}
