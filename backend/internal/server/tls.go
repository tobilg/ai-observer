package server

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/tobilg/ai-observer/internal/logger"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// loadTLS returns the TLS configuration of the HTTP servers from a PEM
// certificate (chain) and key file (AI_OBSERVER_TLS_CERT,
// AI_OBSERVER_TLS_KEY), nil when neither is set. Both or neither must be
// set, and they must form a valid pair: otherwise the server does not start.
// The files are read once at startup; a renewed certificate needs a restart.
func loadTLS(certFile, keyFile string) (*tls.Config, error) {
	if certFile == "" && keyFile == "" {
		return nil, nil
	}
	if certFile == "" || keyFile == "" {
		return nil, errors.New("TLS needs both AI_OBSERVER_TLS_CERT (certificate) and AI_OBSERVER_TLS_KEY (private key)")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("loading the TLS certificate %s and key %s: %w", certFile, keyFile, err)
	}
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
}

// scheme is the URL scheme of the HTTP servers
func (s *Server) scheme() string {
	if s.tls != nil {
		return "https"
	}
	return "http"
}

// otlpHandler is the handler of the OTLP/HTTP server: HTTP/1.1 and h2c
// without TLS; with TLS HTTP/2 is negotiated by ALPN (serveOTLP), and h2c
// is not accepted
func (s *Server) otlpHandler() http.Handler {
	if s.tls != nil {
		return s.otlpRouter
	}
	return h2c.NewHandler(s.otlpRouter, &http2.Server{})
}

// otlpProtocol describes the OTLP/HTTP server's protocols for the log
func (s *Server) otlpProtocol() string {
	if s.tls != nil {
		return "HTTPS (HTTP/1.1 + HTTP/2)"
	}
	return "HTTP/1.1 + h2c"
}

// serveOTLP serves the OTLP/HTTP server on listener, over TLS when it is
// configured
func (s *Server) serveOTLP(srv *http.Server, listener net.Listener) error {
	if s.tls == nil {
		return srv.Serve(listener)
	}
	srv.TLSConfig = s.tls.Clone()
	// ServeTLS adds h2 to the ALPN protocols; the certificate is in TLSConfig
	return srv.ServeTLS(listener, "", "")
}

// apiListener returns the listener the API server serves on: listener, or
// with TLS a TLS listener offering HTTP/1.1 only, like the cleartext API
// (WebSocket upgrades need HTTP/1.1)
func (s *Server) apiListener(listener net.Listener) net.Listener {
	if s.tls == nil {
		return listener
	}
	cfg := s.tls.Clone()
	cfg.NextProtos = []string{"http/1.1"}
	return tls.NewListener(listener, cfg)
}

// apiProtocol describes the API server's protocol for the log
func (s *Server) apiProtocol() string {
	if s.tls != nil {
		return "HTTPS (HTTP/1.1)"
	}
	return "HTTP/1.1"
}

// warnInsecureListening warns when the servers listen beyond loopback
// without a token, or with a token but without TLS
func (s *Server) warnInsecureListening(authEnabled bool) {
	if isLoopbackHost(normalizeHost(s.config.Host)) {
		return
	}
	switch {
	case !authEnabled:
		logger.Warn("Listening beyond localhost without authentication: anyone who can reach the ports can read "+
			"the stored prompts and code and send telemetry. Set AI_OBSERVER_AUTH_TOKEN (and AI_OBSERVER_TLS_CERT/"+
			"AI_OBSERVER_TLS_KEY), or publish container ports to 127.0.0.1 only", "host", s.config.Host)
	case s.tls == nil:
		logger.Warn("Listening beyond localhost without TLS: the token, prompts and code travel in clear text. "+
			"Set AI_OBSERVER_TLS_CERT and AI_OBSERVER_TLS_KEY, or terminate TLS in a reverse proxy", "host", s.config.Host)
	}
}
