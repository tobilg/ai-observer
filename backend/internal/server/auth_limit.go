package server

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/tobilg/ai-observer/internal/api"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/selfmetrics"
)

// Limits of failed authentication attempts: an address that presents a
// wrong token, ?token= or cookie authFailureLimit times within
// authFailureWindow is answered 429 for authBlockDuration, whatever it
// sends. With a long random token guessing is hopeless anyway; the limit
// protects short tokens.
const (
	authFailureLimit  = 10
	authFailureWindow = time.Minute
	authBlockDuration = 5 * time.Minute
	// authLimiterAddrs bounds the addresses the limiter remembers
	authLimiterAddrs = 10_000
)

// authLimiter counts failed authentication attempts per remote address: the
// connection's address (r.RemoteAddr), never forwarded headers, which a
// client could set to anything. Loopback is not exempt: on a shared machine
// a local process is the likely attacker. A blocked address is refused
// without its credentials being checked, so it cannot keep guessing; other
// addresses are not affected. Successes do not reset the count, since a
// process sharing the address (the dashboard on loopback) would otherwise
// reset it for the attacker. Requests without credentials are not attempts,
// and a wrong credential counts once per window: a browser with the cookie of
// a replaced token, or an exporter with an old token, repeats the same one
// and is not blocked for it.
type authLimiter struct {
	limit    int
	window   time.Duration
	block    time.Duration
	maxAddrs int
	now      func() time.Time

	mu    sync.Mutex
	addrs map[string]*authFailures
}

type authFailures struct {
	count        int
	since        time.Time // start of the current window
	blockedUntil time.Time
	// tried holds hashes of the wrong credentials of the window
	tried map[uint64]bool
}

func newAuthLimiter() *authLimiter {
	return &authLimiter{
		limit: authFailureLimit, window: authFailureWindow, block: authBlockDuration, maxAddrs: authLimiterAddrs,
		now: time.Now, addrs: make(map[string]*authFailures),
	}
}

// blocked returns how long addr is still blocked, 0 when it is not
func (l *authLimiter) blocked(addr string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.addrs[addr]
	if f == nil {
		return 0
	}
	return max(f.blockedUntil.Sub(l.now()), 0)
}

// fail records a failed attempt of addr with credential and reports whether
// addr is blocked now
func (l *authLimiter) fail(addr, credential string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	f := l.addrs[addr]
	if f == nil {
		if len(l.addrs) >= l.maxAddrs {
			l.pruneLocked(now)
		}
		f = &authFailures{since: now}
		l.addrs[addr] = f
	}
	if now.Sub(f.since) >= l.window {
		f.count, f.since, f.tried = 0, now, nil
	}
	sum := sha256.Sum256([]byte(credential))
	key := binary.BigEndian.Uint64(sum[:8])
	if f.tried[key] {
		return false
	}
	if f.tried == nil {
		f.tried = make(map[uint64]bool)
	}
	f.tried[key] = true
	f.count++
	if f.count >= l.limit && !now.Before(f.blockedUntil) {
		f.blockedUntil = now.Add(l.block)
		f.count, f.since, f.tried = 0, f.blockedUntil, nil
		logger.Warn("Blocking an address after failed authentication attempts",
			"remote_addr", addr, "attempts", l.limit, "within", l.window, "for", l.block)
		return true
	}
	return false
}

// pruneLocked forgets addresses that are neither blocked nor within a window
// of failures; when all are, it forgets an arbitrary one, so the map stays
// bounded
func (l *authLimiter) pruneLocked(now time.Time) {
	for addr, f := range l.addrs {
		if !now.Before(f.blockedUntil) && now.Sub(f.since) >= l.window {
			delete(l.addrs, addr)
		}
	}
	for addr := range l.addrs {
		if len(l.addrs) < l.maxAddrs {
			break
		}
		delete(l.addrs, addr)
	}
}

// remoteHost is the address of the connection of r, without port
func remoteHost(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// tooManyAttempts answers a request of an address blocked for wait
func tooManyAttempts(w http.ResponseWriter, wait time.Duration) {
	selfmetrics.AuthFailures.With(selfmetrics.TransportHTTP, "blocked").Inc()
	seconds := int(math.Ceil(wait.Seconds()))
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	api.WriteError(w, http.StatusTooManyRequests,
		fmt.Sprintf("too many failed authentication attempts from this address; retry in %d seconds", seconds))
}
