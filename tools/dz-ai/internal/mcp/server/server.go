package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/server/metrics"
)

type Server struct {
	log  *slog.Logger
	cfg  Config
	mcp  *mcp.Server
	http *http.Server
}

func New(ctx context.Context, cfg Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "DoubleZero MCP Server",
		Version: cfg.Version,
	}, nil)

	s := &Server{
		log: cfg.Logger,
		cfg: cfg,
		mcp: mcpServer,
	}

	// Register the new schema tools (list-tables and get-table-schema)
	// These are always available and don't depend on which schemas are enabled
	if err := RegisterListDatasetsTool(s.log, mcpServer, cfg.Querier); err != nil {
		return nil, fmt.Errorf("failed to register list-datasets tool: %w", err)
	}

	if err := RegisterDescribeDatasetsTool(s.log, mcpServer, cfg.Querier); err != nil {
		return nil, fmt.Errorf("failed to register describe-datasets tool: %w", err)
	}

	err := RegisterQueryTool(s.log, mcpServer, cfg.Querier)
	if err != nil {
		return nil, fmt.Errorf("failed to create query tool: %w", err)
	}

	mux := http.NewServeMux()
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return s.mcp
	}, &mcp.StreamableHTTPOptions{
		Stateless: true, // Auto-initialize sessions, no manual initialize required
	})

	// Apply metrics middleware first, then authentication if needed
	metricsHandler := s.metricsMiddleware(handler)
	if len(cfg.AllowedTokens) > 0 {
		authHandler := s.authMiddleware(metricsHandler)
		mux.Handle("/", authHandler)
	} else {
		mux.Handle("/", metricsHandler)
	}

	mux.Handle("/healthz", s.metricsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("ok\n")); err != nil {
			s.log.Error("failed to write healthz response", "error", err)
		}
	})))
	mux.Handle("/readyz", s.metricsMiddleware(http.HandlerFunc(s.readyzHandler)))

	s.http = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		// Add timeouts to prevent connection issues from affecting the server
		// Increased from 30s to 60s to avoid readiness probe timeouts
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
		// Set MaxHeaderBytes to prevent abuse
		MaxHeaderBytes: 1 << 20, // 1MB
	}

	return s, nil
}

func (s *Server) Run(ctx context.Context) error {
	if s.cfg.Indexer != nil {
		s.cfg.Indexer.Start(ctx)
	}

	serveErrCh := make(chan error, 1)
	go func() {
		if err := s.http.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log the error but don't immediately exit - this could be a transient network issue
			s.log.Error("server: http server error", "error", err)
			serveErrCh <- fmt.Errorf("failed to listen and serve: %w", err)
		}
	}()

	s.log.Info("server: mcp streamable http listening",
		"listenAddr", s.cfg.ListenAddr,
	)

	select {
	case <-ctx.Done():
		s.log.Info("server: stopping",
			"reason", ctx.Err(),
			"listenAddr", s.cfg.ListenAddr,
		)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer shutdownCancel()
		if err := s.http.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to shutdown server: %w", err)
		}
		s.log.Info("server: HTTP server shutdown complete")
		return nil
	case err := <-serveErrCh:
		s.log.Error("server: http server error causing shutdown",
			"error", err,
			"listenAddr", s.cfg.ListenAddr,
		)
		return err
	}
}

func (s *Server) readyzHandler(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Indexer != nil && !s.cfg.Indexer.Ready() {
		s.log.Debug("readyz: indexer not ready")
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := w.Write([]byte("indexer not ready\n")); err != nil {
			s.log.Error("failed to write readyz response", "error", err)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok\n")); err != nil {
		s.log.Error("failed to write readyz response", "error", err)
	}
}

// authMiddleware wraps an HTTP handler with Bearer token authentication
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			metrics.AuthFailuresTotal.WithLabelValues("missing_header").Inc()
			w.Header().Set("WWW-Authenticate", `Bearer`)
			w.WriteHeader(http.StatusUnauthorized)
			if _, err := w.Write([]byte("unauthorized: missing authorization header\n")); err != nil {
				s.log.Error("failed to write auth error response", "error", err)
			}
			return
		}

		// Extract token from "Bearer <token>" format
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			metrics.AuthFailuresTotal.WithLabelValues("invalid_format").Inc()
			w.Header().Set("WWW-Authenticate", `Bearer`)
			w.WriteHeader(http.StatusUnauthorized)
			if _, err := w.Write([]byte("unauthorized: invalid authorization header format\n")); err != nil {
				s.log.Error("failed to write auth error response", "error", err)
			}
			return
		}

		token := strings.TrimSpace(parts[1])
		if token == "" {
			metrics.AuthFailuresTotal.WithLabelValues("empty_token").Inc()
			w.Header().Set("WWW-Authenticate", `Bearer`)
			w.WriteHeader(http.StatusUnauthorized)
			if _, err := w.Write([]byte("unauthorized: empty token\n")); err != nil {
				s.log.Error("failed to write auth error response", "error", err)
			}
			return
		}

		// Check if token is in the allowed list
		allowed := false
		for _, allowedToken := range s.cfg.AllowedTokens {
			if token == allowedToken {
				allowed = true
				break
			}
		}

		if !allowed {
			metrics.AuthFailuresTotal.WithLabelValues("invalid_token").Inc()
			w.Header().Set("WWW-Authenticate", `Bearer`)
			w.WriteHeader(http.StatusUnauthorized)
			if _, err := w.Write([]byte("unauthorized: invalid token\n")); err != nil {
				s.log.Error("failed to write auth error response", "error", err)
			}
			return
		}

		// Token is valid, proceed with the request
		next.ServeHTTP(w, r)
	})
}

// metricsMiddleware wraps an HTTP handler with metrics collection
func (s *Server) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startTime := time.Now()
		method := r.Method
		endpoint := r.URL.Path

		// Create a response writer wrapper to capture status code
		wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		duration := time.Since(startTime).Seconds()
		status := fmt.Sprintf("%d", wrapped.statusCode)

		metrics.HTTPRequestsTotal.WithLabelValues(method, endpoint, status).Inc()
		metrics.HTTPRequestDuration.Observe(duration)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}
