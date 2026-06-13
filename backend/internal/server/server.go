package server

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/tobilg/ai-observer/internal/config"
	"github.com/tobilg/ai-observer/internal/handlers"
	"github.com/tobilg/ai-observer/internal/logger"
	appMiddleware "github.com/tobilg/ai-observer/internal/middleware"
	"github.com/tobilg/ai-observer/internal/storage"
	"github.com/tobilg/ai-observer/internal/watcher"
	"github.com/tobilg/ai-observer/internal/websocket"
	"github.com/tobilg/ai-observer/pkg/compression"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type Server struct {
	otlpRouter chi.Router // OTLP ingestion endpoints (port 4318)
	apiRouter  chi.Router // API and WebSocket endpoints (port 8080)
	storage    *storage.DuckDBStore
	wsHub      *websocket.Hub
	config     *config.Config

	// HTTP servers for graceful shutdown
	otlpServer *http.Server
	apiServer  *http.Server
	fileWatcher *watcher.Watcher // nil if watch mode not enabled
	mu         sync.Mutex
}

func New(cfg *config.Config) (*Server, error) {
	store, err := storage.NewDuckDBStore(cfg.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("initializing storage: %w", err)
	}

	hub := websocket.NewHub()
	go hub.Run()

	// Configure WebSocket allowed origins
	websocket.SetAllowedOrigins([]string{cfg.FrontendURL, "http://localhost:5173", "http://localhost:8080"})

	s := &Server{
		otlpRouter: chi.NewRouter(),
		apiRouter:  chi.NewRouter(),
		storage:    store,
		wsHub:      hub,
		config:     cfg,
	}

	s.setupMiddleware()

	h := handlers.New(store, hub)
	if err := s.setupRoutes(h); err != nil {
		return nil, fmt.Errorf("setting up routes: %w", err)
	}

	return s, nil
}

func (s *Server) setupMiddleware() {
	// Common middleware for both routers
	for _, router := range []chi.Router{s.otlpRouter, s.apiRouter} {
		router.Use(middleware.RequestID)
		router.Use(middleware.RealIP)
		router.Use(RequestLogger)
		router.Use(middleware.Recoverer)
	}

	// OTLP router needs gzip decompression for clients that compress payloads
	s.otlpRouter.Use(compression.GzipDecompressMiddleware)

	// OTLP router has 10MB payload size limit
	s.otlpRouter.Use(appMiddleware.DefaultPayloadLimitMiddleware)

	// CORS only needed for API router (frontend access)
	s.apiRouter.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{s.config.FrontendURL, "http://localhost:5173", "http://localhost:8080"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Content-Type", "Content-Encoding", "X-Requested-With"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Add context timeout for API requests (skips WebSocket upgrade requests)
	// Handlers should check context.Done() to respect timeout
	s.apiRouter.Use(appMiddleware.DefaultContextTimeoutMiddleware)
}

func (s *Server) ListenAndServe() error {
	log := logger.Logger()

	// Create OTLP server
	otlpAddr := fmt.Sprintf(":%d", s.config.OTLPPort)
	h2sOTLP := &http2.Server{}
	handlerOTLP := h2c.NewHandler(s.otlpRouter, h2sOTLP)

	s.mu.Lock()
	s.otlpServer = &http.Server{
		Addr:         otlpAddr,
		Handler:      handlerOTLP,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	s.mu.Unlock()

	// Start OTLP server in a goroutine
	go func() {
		log.Info("OTLP server starting",
			"addr", otlpAddr,
			"protocol", "HTTP/1.1 + h2c",
			"endpoints", "POST /v1/traces, /v1/metrics, /v1/logs",
		)

		if err := s.otlpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("OTLP server error", "error", err)
		}
	}()

	// Create API server
	apiAddr := fmt.Sprintf(":%d", s.config.APIPort)
	h2sAPI := &http2.Server{}
	handlerAPI := h2c.NewHandler(s.apiRouter, h2sAPI)

	s.mu.Lock()
	// Note: WriteTimeout is set longer to accommodate WebSocket connections
	// which need to stay open for real-time updates
	s.apiServer = &http.Server{
		Addr:         apiAddr,
		Handler:      handlerAPI,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // Disabled for WebSocket support
		IdleTimeout:  120 * time.Second,
	}
	s.mu.Unlock()

	log.Info("API server starting",
		"addr", apiAddr,
		"protocol", "HTTP/1.1 + h2c",
		"endpoints", "GET /api/*, /ws, /health",
	)

	return s.apiServer.ListenAndServe()
}

// StartWatcher starts the file watcher with the given configuration
func (s *Server) StartWatcher(cfg watcher.Config) error {
	w := watcher.New(s.storage, s.wsHub, cfg)
	s.fileWatcher = w
	return s.fileWatcher.Start(context.Background())
}

// ListenAndServeAPIOnly starts only the API server (no OTLP server)
func (s *Server) ListenAndServeAPIOnly() error {
	log := logger.Logger()

	apiAddr := fmt.Sprintf(":%d", s.config.APIPort)
	h2sAPI := &http2.Server{}
	handlerAPI := h2c.NewHandler(s.apiRouter, h2sAPI)

	s.mu.Lock()
	s.apiServer = &http.Server{
		Addr:         apiAddr,
		Handler:      handlerAPI,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // Disabled for WebSocket support
		IdleTimeout:  120 * time.Second,
	}
	s.mu.Unlock()

	log.Info("API server starting",
		"addr", apiAddr,
		"protocol", "HTTP/1.1 + h2c",
		"endpoints", "GET /api/*, /ws, /health",
	)

	return s.apiServer.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	logger.Info("Shutting down server")

	var wg sync.WaitGroup
	var errs []error
	var errMu sync.Mutex

	// Shutdown OTLP server
	s.mu.Lock()
	otlpServer := s.otlpServer
	apiServer := s.apiServer
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

	// Stop file watcher before closing storage
	if s.fileWatcher != nil {
		s.fileWatcher.Stop()
	}

	// Close storage
	if err := s.storage.Close(); err != nil {
		errs = append(errs, fmt.Errorf("closing storage: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("shutdown errors: %v", errs)
	}
	return nil
}
