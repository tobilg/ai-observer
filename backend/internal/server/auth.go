package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/mcpserver"
	"github.com/tobilg/ai-observer/internal/selfmetrics"
)

// authCookieName is the cookie that keeps the dashboard signed in
const authCookieName = "ai_observer_token"

// authCookieMaxAge is how long the dashboard stays signed in
const authCookieMaxAge = 30 * 24 * time.Hour

// TokenAuth requires a shared token, configured with AI_OBSERVER_AUTH_TOKEN.
// Exporters and scripts send it as "Authorization: Bearer <token>". The
// dashboard is opened once with ?token=<token>, which sets an HttpOnly,
// SameSite=Strict cookie that the browser sends with API and WebSocket
// requests. The cookie holds a value derived from the token, not the token,
// so a leaked cookie does not let anyone send telemetry. /health, /ready and
// the static dashboard files need no token.
type TokenAuth struct {
	token   []byte
	session string // the cookie value
	// limiter blocks addresses with many failed attempts (auth_limit.go)
	limiter *authLimiter
}

// NewTokenAuth returns nil when token is empty: authentication is off
func NewTokenAuth(token string) *TokenAuth {
	if token == "" {
		return nil
	}
	mac := hmac.New(sha256.New, []byte(token))
	mac.Write([]byte("ai-observer dashboard session"))
	return &TokenAuth{token: []byte(token), session: hex.EncodeToString(mac.Sum(nil)), limiter: newAuthLimiter()}
}

func (a *TokenAuth) matches(candidate string) bool {
	return candidate != "" && subtle.ConstantTimeCompare([]byte(candidate), a.token) == 1
}

func (a *TokenAuth) validSession(candidate string) bool {
	return candidate != "" && subtle.ConstantTimeCompare([]byte(candidate), []byte(a.session)) == 1
}

// bearer returns the token of an "Authorization: Bearer" header
func bearer(r *http.Request) string {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

func (a *TokenAuth) authorized(r *http.Request, allowCookie bool) bool {
	if a.matches(bearer(r)) {
		return true
	}
	if !allowCookie {
		return false
	}
	cookie, err := r.Cookie(authCookieName)
	return err == nil && a.validSession(cookie.Value)
}

// credential returns the credential r presents (bearer token, or with
// allowCookie the session cookie), "" when none
func credential(r *http.Request, allowCookie bool) string {
	if token := bearer(r); token != "" {
		return token
	}
	if allowCookie {
		if cookie, err := r.Cookie(authCookieName); err == nil {
			return cookie.Value
		}
	}
	return ""
}

// check answers whether r may pass. Otherwise it has answered r: 429 for an
// address blocked after failed attempts, 401 for missing or wrong
// credentials (counted by the limiter when wrong).
func (a *TokenAuth) check(w http.ResponseWriter, r *http.Request, allowCookie bool) bool {
	addr := remoteHost(r)
	if wait := a.limiter.blocked(addr); wait > 0 {
		tooManyAttempts(w, wait)
		return false
	}
	if a.authorized(r, allowCookie) {
		return true
	}
	if c := credential(r, allowCookie); c != "" {
		a.limiter.fail(addr, c)
	}
	unauthorized(w, r)
	return false
}

func unauthorized(w http.ResponseWriter, r *http.Request) {
	selfmetrics.AuthFailures.With(selfmetrics.TransportHTTP, "unauthorized").Inc()
	logger.Warn("Rejected unauthenticated request", "path", r.URL.Path)
	w.Header().Set("WWW-Authenticate", `Bearer realm="ai-observer"`)
	api.WriteError(w, http.StatusUnauthorized, "authentication required; see AI_OBSERVER_AUTH_TOKEN")
}

// OTLPMiddleware requires the bearer token on OTLP requests
func (a *TokenAuth) OTLPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" && r.URL.Path != "/ready" && !a.check(w, r, false) {
			return
		}
		next.ServeHTTP(w, r)
	})
}

// APIMiddleware requires the token on API and WebSocket requests, and turns a
// valid ?token= on a dashboard page into the session cookie
func (a *TokenAuth) APIMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if path == "/api" || strings.HasPrefix(path, "/api/") || path == "/ws" || path == mcpserver.Path || path == "/metrics" {
			// CORS preflights carry no credentials; the request that follows does
			if r.Method != http.MethodOptions && !a.check(w, r, true) {
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		query := r.URL.Query()
		if token := query.Get("token"); token != "" && r.Method == http.MethodGet {
			addr := remoteHost(r)
			if wait := a.limiter.blocked(addr); wait > 0 {
				tooManyAttempts(w, wait)
				return
			}
			if !a.matches(token) {
				a.limiter.fail(addr, token)
				unauthorized(w, r)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     authCookieName,
				Value:    a.session,
				Path:     "/",
				MaxAge:   int(authCookieMaxAge.Seconds()),
				HttpOnly: true,
				Secure:   r.TLS != nil,
				SameSite: http.SameSiteStrictMode,
			})
			// Drop the token from the address bar and browser history
			query.Del("token")
			target := *r.URL
			target.RawQuery = query.Encode()
			http.Redirect(w, r, target.RequestURI(), http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}
