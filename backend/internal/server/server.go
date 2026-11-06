package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/handlers"
	"github.com/tobilg/ai-observer/internal/logger"
	"github.com/tobilg/ai-observer/internal/mcpserver"
	appMiddleware "github.com/tobilg/ai-observer/internal/middleware"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/watcher"
	"github.com/tobilg/ai-observer/internal/websocket"
	"github.com/tobilg/ai-observer/pkg/compression"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

// apiMaxPayloadBytes bounds API request bodies
const apiMaxPayloadBytes = 1 << 20 // 1 MB

type Server struct {
	otlpRouter chi.Router // OTLP ingestion endpoints (port 4318)
	apiRouter  chi.Router // API and WebSocket endpoints (port 8080)
	storage    *storage.DuckDBStore
	wsHub      *websocket.Hub
	config     *config.Config

	// HTTP servers for graceful shutdown
	otlpServer    *http.Server
	apiServer     *http.Server
	fileWatcher   *watcher.Watcher // nil if watch mode not enabled
	stopRetention func()           // nil if retention is not running
	closed        bool             // set by Shutdown; servers must not start afterwards
	mu            sync.Mutex

	// stopping is cancelled when Shutdown starts, ending requests that have
	// no timeout (MCP); stop cancels it
	stopping context.Context
	stop     context.CancelFunc
}

func New(cfg *config.Config) (*Server, error) {
	store, err := storage.NewDuckDBStore(cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("initializing storage: %w", err)
	}
	store.SetRedactor(NewRedactor(cfg.Redaction))

	hub := websocket.NewHub()
	go hub.Run()

	s := &Server{
		otlpRouter: chi.NewRouter(),
		apiRouter:  chi.NewRouter(),
		storage:    store,
		wsHub:      hub,
		config:     cfg,
	}
	s.stopping, s.stop = context.WithCancel(context.Background())

	s.setupMiddleware()

	h := handlers.New(store, hub)
	if err := s.setupRoutes(h); err != nil {
		s.stop()
		hub.Stop()
		store.Close()
		return nil, fmt.Errorf("setting up routes: %w", err)
	}

	return s, nil
}

// corsOrigins are the browser origins the API answers CORS requests for
func (s *Server) corsOrigins() []string {
	return []string{s.config.FrontendURL, "http://localhost:5173", "http://localhost:8080"}
}

func (s *Server) setupMiddleware() {
	origins := NewOriginPolicy(s.corsOrigins(), s.allowedHosts())
	s.wsHub.SetOriginCheck(origins.Allowed)

	// Common middleware for both routers
	for _, router := range []chi.Router{s.otlpRouter, s.apiRouter} {
		router.Use(middleware.RequestID)
		router.Use(RequestLogger)
		router.Use(middleware.Recoverer)
		// Reject cross-site requests (CSRF), including CORS "simple" requests
		router.Use(origins.Middleware)
	}

	// Optional shared token (AI_OBSERVER_AUTH_TOKEN)
	auth := NewTokenAuth(s.config.AuthToken)
	if auth != nil {
		s.otlpRouter.Use(auth.OTLPMiddleware)
		logger.Info("Token authentication enabled; open the dashboard once with ?token=<AI_OBSERVER_AUTH_TOKEN>")
	} else if !isLoopbackHost(normalizeHost(s.config.Host)) {
		logger.Warn("Listening beyond localhost without authentication: anyone who can reach the ports can read "+
			"the stored prompts and code and send telemetry. Set AI_OBSERVER_AUTH_TOKEN, or publish container ports "+
			"to 127.0.0.1 only", "host", s.config.Host)
	}

	// OTLP router needs gzip decompression for clients that compress payloads
	s.otlpRouter.Use(compression.GzipDecompressMiddleware)

	// OTLP router has 10MB payload size limit
	s.otlpRouter.Use(appMiddleware.DefaultPayloadLimitMiddleware)

	// Reject API requests for unknown host names (DNS rebinding protection)
	s.apiRouter.Use(HostCheck(s.allowedHosts()))
	if auth != nil {
		s.apiRouter.Use(auth.APIMiddleware)
	}

	// API request bodies are small JSON documents (dashboards, batch queries)
	s.apiRouter.Use(appMiddleware.PayloadLimitMiddleware(apiMaxPayloadBytes))

	// CORS only needed for API router (frontend access). Cross-origin requests
	// are not credentialed: the auth cookie is SameSite=Strict and only sent
	// by the dashboard itself.
	s.apiRouter.Use(cors.Handler(cors.Options{
		AllowedOrigins: s.corsOrigins(),
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "Content-Encoding", "X-Requested-With"},
		ExposedHeaders: []string{"Link"},
		MaxAge:         300,
	}))

	// Add context timeout for API requests (skips WebSocket upgrade requests)
	// Handlers should check context.Done() to respect timeout. MCP tool calls
	// run without it: an agent's search over a large database may take
	// longer, and the client cancels requests it no longer waits for.
	s.apiRouter.Use(exceptPath(mcpserver.Path, appMiddleware.DefaultContextTimeoutMiddleware))
}

// allowedHosts returns the host names accepted by HostCheck in addition to IP
// addresses and localhost: the configured ones plus the frontend URL's host and
// the bind host when it is a name.
func (s *Server) allowedHosts() []string {
	hosts := append([]string{}, s.config.AllowedHosts...)
	if u, err := url.Parse(s.config.FrontendURL); err == nil && u.Hostname() != "" {
		hosts = append(hosts, u.Hostname())
	}
	if s.config.Host != "" {
		hosts = append(hosts, s.config.Host)
	}
	return hosts
}

// listen opens a TCP listener for port on the configured host
func (s *Server) listen(port int) (net.Listener, error) {
	return net.Listen("tcp", net.JoinHostPort(s.config.Host, strconv.Itoa(port)))
}

// ListenAndServe starts the OTLP and API servers. Both listeners are opened
// before serving, so an unavailable port fails startup instead of leaving the
// process running without OTLP ingestion. It returns when the API server stops.
func (s *Server) ListenAndServe() error {
	log := logger.Logger()

	otlpListener, err := s.listen(s.config.OTLPPort)
	if err != nil {
		return fmt.Errorf("starting OTLP server: %w", err)
	}
	apiListener, err := s.listen(s.config.APIPort)
	if err != nil {
		otlpListener.Close()
		return fmt.Errorf("starting API server: %w", err)
	}

	otlpServer := &http.Server{
		Handler:      h2c.NewHandler(s.otlpRouter, &http2.Server{}),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		otlpListener.Close()
		apiListener.Close()
		return http.ErrServerClosed
	}
	s.otlpServer = otlpServer
	s.mu.Unlock()

	go func() {
		log.Info("OTLP server starting",
			"addr", otlpListener.Addr().String(),
			"protocol", "HTTP/1.1 + h2c",
			"endpoints", "POST /v1/traces, /v1/metrics, /v1/logs",
		)

		if err := otlpServer.Serve(otlpListener); err != nil && err != http.ErrServerClosed {
			log.Error("OTLP server error", "error", err)
		}
	}()

	return s.serveAPI(apiListener)
}

// StartWatcher starts the file watcher with the given configuration
func (s *Server) StartWatcher(cfg watcher.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return http.ErrServerClosed
	}
	w := watcher.New(s.storage, s.wsHub, cfg)
	if err := w.Start(context.Background()); err != nil {
		w.Stop()
		return err
	}
	s.fileWatcher = w
	return nil
}

// ListenAndServeAPIOnly starts only the API server (no OTLP server)
func (s *Server) ListenAndServeAPIOnly() error {
	apiListener, err := s.listen(s.config.APIPort)
	if err != nil {
		return fmt.Errorf("starting API server: %w", err)
	}
	return s.serveAPI(apiListener)
}

// serveAPI serves the API router on listener until the server is shut down.
// It speaks HTTP/1.1 only: browsers never use h2c, and accepting h2c upgrades
// behind a reverse proxy would let clients tunnel past the proxy's rules.
func (s *Server) serveAPI(listener net.Listener) error {
	// Note: WriteTimeout is disabled to accommodate WebSocket connections
	// which need to stay open for real-time updates
	apiServer := &http.Server{
		Handler:      s.apiRouter,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		listener.Close()
		return http.ErrServerClosed
	}
	s.apiServer = apiServer
	s.startRetention()
	s.mu.Unlock()

	logger.Info("API server starting",
		"addr", listener.Addr().String(),
		"protocol", "HTTP/1.1",
		"endpoints", "GET /api/*, /ws, /health",
	)
	if s.config.MCPEnabled {
		logger.Info("MCP endpoint for agents enabled", "url", localURL(listener.Addr(), mcpserver.Path))
	}

	return apiServer.Serve(listener)
}

func (s *Server) Shutdown(ctx context.Context) error {
	logger.Info("Shutting down server")
	// http.Server.Shutdown waits for running requests without cancelling
	// them: end those without a timeout, or they would hold up closing the
	// database
	s.stop()

	var wg sync.WaitGroup
	var errs []error
	var errMu sync.Mutex

	// Shutdown OTLP server
	s.mu.Lock()
	s.closed = true
	otlpServer := s.otlpServer
	apiServer := s.apiServer
	fileWatcher := s.fileWatcher
	s.mu.Unlock()

	if otlpServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("Shutting down OTLP server")
			if err := otlpServer.Shutdown(ctx); err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("shutting down OTLP server: %w", err))
				errMu.Unlock()
			}
		}()
	}

	// Shutdown API server
	if apiServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("Shutting down API server")
			if err := apiServer.Shutdown(ctx); err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("shutting down API server: %w", err))
				errMu.Unlock()
			}
		}()
	}

	// Wait for servers to shutdown
	wg.Wait()

	// Stop file watcher and retention before closing storage
	if fileWatcher != nil {
		fileWatcher.Stop()
	}
	s.mu.Lock()
	stopRetention := s.stopRetention
	s.mu.Unlock()
	if stopRetention != nil {
		stopRetention()
	}

	// Close WebSocket connections; the API server's Shutdown does not wait
	// for hijacked connections
	s.wsHub.Stop()

	// Close storage
	if err := s.storage.Close(); err != nil {
		errs = append(errs, fmt.Errorf("closing storage: %w", err))
	}

	return errors.Join(errs...)
}

// localURL returns the URL of path on addr for clients on this machine: a
// server listening on all interfaces (0.0.0.0, ::) is reached via localhost
func localURL(addr net.Addr, path string) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "http://" + addr.String() + path
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, port) + path
}

// endOnStop cancels the context of each request to h when stopping is done
func endOnStop(stopping context.Context, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		defer context.AfterFunc(stopping, cancel)()
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// exceptPath applies middleware mw to every request but those for path
func exceptPath(path string, mw func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		wrapped := mw(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == path {
				next.ServeHTTP(w, r)
				return
			}
			wrapped.ServeHTTP(w, r)
		})
	}
}
