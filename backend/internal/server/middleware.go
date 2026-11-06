package server

import (
	"bufio"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/logger"
)

// RequestLogger logs HTTP requests
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Create a response writer wrapper to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		// Every OTLP export and dashboard poll is a request, so successful ones
		// are only logged at DEBUG
		log := logger.Debug
		if wrapped.statusCode >= http.StatusBadRequest {
			log = logger.Warn
		}
		log("HTTP request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", wrapped.statusCode,
			"duration", time.Since(start),
		)
	})
}

// HostCheck rejects requests whose Host header names a host that is not
// allowed. This protects the unauthenticated API against DNS rebinding: a
// malicious site that rebinds its own domain to 127.0.0.1 still sends that
// domain in the Host header. IP addresses and localhost are always accepted,
// because a rebinding attack cannot produce them. "*" in allowed disables the check.
func HostCheck(allowed []string) func(http.Handler) http.Handler {
	allowedSet := make(map[string]bool)
	for _, host := range allowed {
		host = normalizeHost(host)
		if host == "*" {
			return func(next http.Handler) http.Handler { return next }
		}
		if host != "" {
			allowedSet[host] = true
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !hostAllowed(r.Host, allowedSet) {
				logger.Warn("Rejected request with disallowed Host header", "host", r.Host)
				api.WriteError(w, http.StatusForbidden, "host not allowed; add it to AI_OBSERVER_ALLOWED_HOSTS")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func hostAllowed(hostport string, allowed map[string]bool) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = normalizeHost(host)

	switch {
	case host == "":
		// HTTP/1.0 requests may omit Host; browsers always send it
		return true
	case host == "localhost" || strings.HasSuffix(host, ".localhost"):
		return true
	case net.ParseIP(host) != nil:
		return true
	default:
		return allowed[host]
	}
}

// OriginPolicy decides which browser origins may send requests. Requests
// without an Origin header (CLI tools, OTLP exporters) are always allowed.
// Browsers send Origin with every cross-origin request, including "simple"
// ones that skip the CORS preflight, such as a text/plain POST from a web page
// to the OTLP port. Rejecting them stops other sites from writing telemetry or
// changing dashboards.
type OriginPolicy struct {
	origins map[string]bool // exact origins, e.g. http://localhost:5173
	hosts   map[string]bool // host names accepted with any scheme and port
	any     bool
}

// NewOriginPolicy accepts the given exact origins, any origin whose host name
// is in hosts ("*" accepts every origin), loopback origins, and same-origin
// requests to an IP address
func NewOriginPolicy(origins, hosts []string) *OriginPolicy {
	p := &OriginPolicy{origins: make(map[string]bool), hosts: make(map[string]bool)}
	for _, origin := range origins {
		if origin = normalizeOrigin(origin); origin != "" {
			p.origins[origin] = true
		}
	}
	for _, host := range hosts {
		host = normalizeHost(host)
		if host == "*" {
			p.any = true
		}
		if host != "" {
			p.hosts[host] = true
		}
	}
	return p
}

// Allowed reports whether r's Origin may use the server
func (p *OriginPolicy) Allowed(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || p.any {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false // includes the opaque origin "null"
	}
	host := normalizeHost(u.Hostname())
	if p.origins[normalizeOrigin(origin)] || isLoopbackHost(host) || p.hosts[host] {
		return true
	}
	// A same-origin request is only trusted for host names that DNS
	// rebinding cannot produce: after rebinding evil.example to 127.0.0.1,
	// its pages are same-origin with the server, and the OTLP port has no
	// Host check of its own
	return strings.EqualFold(u.Host, r.Host) && hostAllowed(u.Host, p.hosts)
}

// Middleware rejects requests from origins that are not allowed
func (p *OriginPolicy) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !p.Allowed(r) {
			logger.Warn("Rejected request from disallowed origin", "origin", r.Header.Get("Origin"), "path", r.URL.Path)
			api.WriteError(w, http.StatusForbidden, "origin not allowed; add its host to AI_OBSERVER_ALLOWED_HOSTS")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackHost reports whether host names this machine
func isLoopbackHost(host string) bool {
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func normalizeOrigin(origin string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(origin)), "/")
}

func normalizeHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	return host
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// Hijack implements the http.Hijacker interface for WebSocket support
func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hijacker, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hijacker.Hijack()
	}
	return nil, nil, http.ErrNotSupported
}
