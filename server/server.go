package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/kumarlokesh/contextd/config"
)

// BuildInfo holds version metadata injected at build time.
type BuildInfo struct {
	Version   string
	Commit    string
	BuildDate string
}

// Server is the contextd HTTP daemon.
type Server struct {
	cfg       *config.Config
	router    *chi.Mux
	http      *http.Server
	logger    *slog.Logger
	build     BuildInfo
	startTime time.Time
	healthy   atomic.Bool
}

// New creates a Server. Call Routes() before Start().
func New(cfg *config.Config, logger *slog.Logger, build BuildInfo) *Server {
	s := &Server{
		cfg:       cfg,
		logger:    logger,
		build:     build,
		startTime: time.Now(),
	}
	s.healthy.Store(true)

	s.router = chi.NewRouter()
	s.http = &http.Server{
		Addr:              cfg.Addr(),
		Handler:           s.router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return s
}

// Routes mounts the base middleware stack and the /health and /version
// endpoints. API routes are mounted separately via MountAPI.
func (s *Server) Routes() {
	s.router.Use(middleware.RequestID)
	s.router.Use(middleware.RealIP)
	s.router.Use(s.slogMiddleware())
	s.router.Use(middleware.Recoverer)
	s.router.Use(middleware.Timeout(30 * time.Second))

	s.router.Get("/health", s.handleHealth)
	s.router.Get("/version", s.handleVersion)
}

// MountAPI attaches a sub-router under a prefix. Used by the api package to
// register its handlers without creating an import cycle.
func (s *Server) MountAPI(prefix string, r http.Handler) {
	s.router.Mount(prefix, r)
}

// Start begins listening and blocks until ctx is cancelled.
func (s *Server) Start(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		s.healthy.Store(false)
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("graceful shutdown error", "err", err)
		}
	}()

	s.logger.Info("starting contextd", "addr", s.http.Addr, "version", s.build.Version)
	if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("http server: %w", err)
	}
	return nil
}

// Shutdown gracefully drains in-flight requests.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down")
	return s.http.Shutdown(ctx)
}

// handleHealth returns 200 with server uptime.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if !s.healthy.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	uptime := int64(math.Round(time.Since(s.startTime).Seconds()))
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ok",
		"uptime_seconds":  uptime,
	})
}

// handleVersion returns build metadata.
func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"version":    s.build.Version,
		"commit":     s.build.Commit,
		"build_date": s.build.BuildDate,
	})
}

// slogMiddleware returns a chi-compatible middleware that logs each request
// via slog at Info level.
func (s *Server) slogMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			s.logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", middleware.GetReqID(r.Context()),
			)
		})
	}
}

// writeJSON encodes v as JSON and writes it with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
