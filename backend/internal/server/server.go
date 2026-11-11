package server

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
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
	"github.com/tobilg/ai-observer/internal/otlpgrpc"
	"github.com/tobilg/ai-observer/internal/redact"
	"github.com/tobilg/ai-observer/internal/selfmetrics"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/tools"
	"github.com/tobilg/ai-observer/internal/transcript"
	"github.com/tobilg/ai-observer/internal/watcher"
	"github.com/tobilg/ai-observer/internal/websocket"
	"github.com/tobilg/ai-observer/pkg/compression"
	"google.golang.org/grpc"
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
	grpcServer    *grpc.Server      // OTLP/gRPC; nil when disabled or not started
	ingest        otlpgrpc.Ingester // the OTLP ingestion pipeline, for gRPC
	fileWatcher   *watcher.Watcher  // nil if watch mode not enabled
	watchedTools  []string          // tools the file watcher watches
	stopRetention func()            // nil if retention is not running
	closed        bool              // set by Shutdown; servers must not start afterwards
	mu            sync.Mutex

	// stopping is cancelled when Shutdown starts, ending requests that have
	// no timeout (MCP); stop cancels it
	stopping context.Context
	stop     context.CancelFunc

	startedAt time.Time // for GET /api/status

	// transcripts is shared by the REST API and the MCP endpoint
	transcripts *transcript.Cache

	// tls is the HTTP servers' TLS configuration (AI_OBSERVER_TLS_CERT,
	// AI_OBSERVER_TLS_KEY); nil serves plain HTTP
	tls *tls.Config
	// auth checks the token on OTLP (HTTP and gRPC) and API requests; nil
	// without AI_OBSERVER_AUTH_TOKEN. One instance, so failed attempts are
	// counted across all of them.
	auth *TokenAuth
}

func New(cfg *config.Config) (*Server, error) {
	tlsConfig, err := loadTLS(cfg.TLSCert, cfg.TLSKey)
	if err != nil {
		return nil, err
	}
	store, err := storage.NewDuckDBStoreWithOptions(cfg.DatabasePath, cfg.StorageOptions())
	if err != nil {
		return nil, fmt.Errorf("initializing storage: %w", err)
	}
	store.SetRedactor(NewRedactor(cfg.Redaction))
	store.SetStoreAllSpans(cfg.StoreAllSpans)

	hub := websocket.NewHub()
	go hub.Run()
	selfmetrics.SetWebSocketClients(hub.ClientCount)

	s := &Server{
		otlpRouter: chi.NewRouter(),
		apiRouter:  chi.NewRouter(),
		storage:    store,
		wsHub:      hub,
		config:     cfg,
		startedAt:  time.Now(),
		tls:        tlsConfig,
	}
	s.transcripts = transcript.NewCache(store)
	s.stopping, s.stop = context.WithCancel(context.Background())

	s.setupMiddleware()

	h := handlers.New(store, hub)
	s.ingest = h
	h.SetServerInfo(s.serverInfo)
	h.SetTranscriptCache(s.transcripts)
	if err := s.setupRoutes(h); err != nil {
		s.stop()
		hub.Stop()
		store.Close()
		return nil, fmt.Errorf("setting up routes: %w", err)
	}

	return s, nil
}

// serverInfo describes the server for GET /api/status. The mode is "watch"
// while the file watcher runs without OTLP (the watch command),
// "serve+watch" while it runs beside the OTLP servers (serve --watch), and
// "serve" otherwise. OTLP is enabled while the OTLP/HTTP server runs, gRPC
// while the OTLP/gRPC server does.
func (s *Server) serverInfo() handlers.ServerInfo {
	s.mu.Lock()
	watching, otlp, grpcOn := s.fileWatcher != nil, s.otlpServer != nil, s.grpcServer != nil
	watchedTools := append([]string(nil), s.watchedTools...)
	s.mu.Unlock()
	mode := ModeServe
	switch {
	case watching && otlp:
		mode = ModeServeWatch
	case watching:
		mode = ModeWatch
	}
	grpcPort := s.config.OTLPGRPCPort
	// The mode in effect: a redactor without rules redacts nothing
	redaction := redact.Off
	if r := s.storage.Redactor(); r.Enabled() {
		redaction = r.Mode()
	}
	dbPath := s.config.DatabasePath
	if abs, err := filepath.Abs(dbPath); err == nil {
		dbPath = abs
	}
	return handlers.ServerInfo{
		Mode:        mode,
		StartedAt:   s.startedAt,
		Host:        s.config.Host,
		APIPort:     s.config.APIPort,
		OTLPPort:    s.config.OTLPPort,
		OTLPEnabled: otlp,
		// The configured port (0 when disabled) and whether it listens
		OTLPGRPCPort:    grpcPort,
		OTLPGRPCEnabled: grpcOn,
		WatchedTools:    watchedTools,
		DatabasePath:    dbPath,
		DBMemoryLimit:   s.config.DBMemoryLimit,
		DBThreads:       s.config.DBThreads,
		RetentionDays:   s.config.RetentionDays,
		Redaction:       string(redaction),
		AuthEnabled:     s.config.AuthToken != "",
		MCPEnabled:      s.config.MCPEnabled,
		StoreAllSpans:   s.config.StoreAllSpans,
		MaxDBSize:       s.config.MaxDBSize,
		AdminAPI:        s.config.AdminAPI,
		BackupDir:       absPath(s.config.AdminBackupDir()),

		TLSEnabled:          s.tls != nil,
		LogLevel:            s.config.LogLevel,
		LogFormat:           s.config.LogFormat,
		CORSOrigin:          s.config.FrontendURL,
		AllowedHosts:        s.config.AllowedHosts,
		SkipMigrationBackup: s.config.SkipMigrationBackup,
	}
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
		router.Use(requestMetrics(router == s.otlpRouter))
		// Reject cross-site requests (CSRF), including CORS "simple" requests
		router.Use(origins.Middleware)
	}

	// Optional shared token (AI_OBSERVER_AUTH_TOKEN)
	auth := NewTokenAuth(s.config.AuthToken)
	s.auth = auth
	if auth != nil {
		s.otlpRouter.Use(auth.OTLPMiddleware)
		logger.Info("Token authentication enabled; open the dashboard once with ?token=<AI_OBSERVER_AUTH_TOKEN>")
	}
	s.warnInsecureListening(auth != nil)

	// OTLP router decodes compressed payloads (gzip, deflate; 415 for others)
	s.otlpRouter.Use(compression.DecompressMiddleware)

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

	// Compress the dashboard's scripts and styles (1.3 MB uncompressed) and
	// API responses for clients that accept it, which matters when AI
	// Observer is reached over a network (Docker, a remote machine).
	// Responses of other types, such as the WebSocket upgrade, pass through.
	s.apiRouter.Use(middleware.Compress(5,
		"application/json", "text/html", "text/css", "text/javascript", "application/javascript", "image/svg+xml", "text/plain"))

	// Add context timeout for API requests (skips WebSocket upgrade requests)
	// Handlers should check context.Done() to respect timeout. MCP tool calls
	// run without it: an agent's search over a large database may take
	// longer, and the client cancels requests it no longer waits for.
	// The admin API's operations run without it too (adminRoutes).
	s.apiRouter.Use(exceptPath(mcpserver.Path, exceptPrefix(adminPathPrefix, appMiddleware.DefaultContextTimeoutMiddleware)))
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

// Server modes reported by GET /api/status
const (
	ModeServe      = "serve"       // OTLP and API
	ModeWatch      = "watch"       // session files and API, no OTLP
	ModeServeWatch = "serve+watch" // OTLP, session files and API (serve --watch)
)

// grpcProtocol describes the OTLP/gRPC server's protocol for the log
func (s *Server) grpcProtocol() string {
	if s.tls != nil {
		return "gRPC (TLS)"
	}
	return "gRPC (HTTP/2 cleartext)"
}

// ListenAndServe starts the OTLP servers (HTTP, and gRPC unless its port is
// 0) and the API server. All listeners are opened before serving, so an
// unavailable port fails startup instead of leaving the process running
// without OTLP ingestion. It returns when the API server stops.
func (s *Server) ListenAndServe() error {
	log := logger.Logger()

	otlpListener, err := s.listen(s.config.OTLPPort)
	if err != nil {
		return fmt.Errorf("starting OTLP server: %w", err)
	}
	var grpcListener net.Listener
	if s.config.OTLPGRPCPort != 0 {
		if grpcListener, err = s.listen(s.config.OTLPGRPCPort); err != nil {
			otlpListener.Close()
			return fmt.Errorf("starting OTLP/gRPC server (set AI_OBSERVER_OTLP_GRPC_PORT to another port, or off): %w", err)
		}
	}
	apiListener, err := s.listen(s.config.APIPort)
	if err != nil {
		otlpListener.Close()
		if grpcListener != nil {
			grpcListener.Close()
		}
		return fmt.Errorf("starting API server: %w", err)
	}

	otlpServer := &http.Server{
		Handler:      s.otlpHandler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	var grpcServer *grpc.Server
	if grpcListener != nil {
		grpcServer = otlpgrpc.NewServer(s.ingest, otlpgrpc.Options{Authorize: s.auth.GRPCAuthorizer(), TLS: s.tls.Clone()})
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		otlpListener.Close()
		if grpcListener != nil {
			grpcListener.Close()
		}
		apiListener.Close()
		return http.ErrServerClosed
	}
	s.otlpServer = otlpServer
	s.grpcServer = grpcServer
	s.mu.Unlock()

	go func() {
		log.Info("OTLP server starting",
			"addr", otlpListener.Addr().String(),
			"protocol", s.otlpProtocol(),
			"endpoints", "POST /v1/traces, /v1/metrics, /v1/logs",
		)

		if err := s.serveOTLP(otlpServer, otlpListener); err != nil && err != http.ErrServerClosed {
			log.Error("OTLP server error", "error", err)
		}
	}()
	if grpcServer != nil {
		go func() {
			log.Info("OTLP/gRPC server starting",
				"addr", grpcListener.Addr().String(),
				"protocol", s.grpcProtocol(),
				"services", "TraceService, MetricsService, LogsService",
			)
			if err := grpcServer.Serve(grpcListener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				log.Error("OTLP/gRPC server error", "error", err)
			}
		}()
	}

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
	s.watchedTools = watchedTools(cfg.Tools)
	return nil
}

// watchedTools returns the tools a watcher configured with names watches:
// those named, or all tools with session files when none are
func watchedTools(names []string) []string {
	if len(names) > 0 {
		return append([]string(nil), names...)
	}
	var all []string
	for _, p := range tools.Providers() {
		if p.SessionFiles {
			all = append(all, string(p.Tool))
		}
	}
	return all
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
	// Message keys of logs stored by another version; Close stops it
	s.storage.StartMessageKeyRefill()
	s.mu.Unlock()

	logger.Info("API server starting",
		"addr", listener.Addr().String(),
		"protocol", s.apiProtocol(),
		"endpoints", "GET /api/*, /ws, /health, /ready, /metrics",
	)
	logger.Info("Dashboard available", "url", localURL(s.scheme(), listener.Addr(), "/"))
	if s.config.MCPEnabled {
		logger.Info("MCP endpoint for agents enabled", "url", localURL(s.scheme(), listener.Addr(), mcpserver.Path))
	}

	return apiServer.Serve(s.apiListener(listener))
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

	s.mu.Lock()
	s.closed = true
	otlpServer := s.otlpServer
	apiServer := s.apiServer
	grpcServer := s.grpcServer
	fileWatcher := s.fileWatcher
	s.mu.Unlock()

	// Shutdown OTLP/gRPC server: finish running exports, unless ctx ends first
	if grpcServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logger.Info("Shutting down OTLP/gRPC server")
			stopped := make(chan struct{})
			go func() {
				grpcServer.GracefulStop()
				close(stopped)
			}()
			select {
			case <-stopped:
			case <-ctx.Done():
				grpcServer.Stop()
				<-stopped
				errMu.Lock()
				errs = append(errs, fmt.Errorf("shutting down OTLP/gRPC server: %w", ctx.Err()))
				errMu.Unlock()
			}
		}()
	}

	// Shutdown OTLP/HTTP server
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

// localURL returns the URL (scheme http or https) of path on addr for
// clients on this machine: a server listening on all interfaces (0.0.0.0,
// ::) is reached via localhost
func localURL(scheme string, addr net.Addr, path string) string {
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return scheme + "://" + addr.String() + path
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		host = "localhost"
	}
	return scheme + "://" + net.JoinHostPort(host, port) + path
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
