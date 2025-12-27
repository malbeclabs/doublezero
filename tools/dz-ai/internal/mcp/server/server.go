package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	schematypes "github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/indexer/schema"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/server/metrics"
)

type Server struct {
	log  *slog.Logger
	cfg  Config
	mcp  *mcp.Server
	http *http.Server

	registeredSchemas   map[string]struct{}
	registeredSchemasMu sync.Mutex
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

		registeredSchemas: make(map[string]struct{}),
	}

	err := RegisterQueryTool(s.log, mcpServer, cfg.Querier, "query", `
			PURPOSE:
			Execute DuckDB SQL queries across all DoubleZero datasets (serviceability, telemetry latency, telemetry usage, and Solana).

			USAGE RULES:
			- Consult the appropriate schema tool before writing any SQL. Do not guess column names.
			- Prefer single, well-constructed queries that return summarized results.
			- Aggregate data using 'GROUP BY' and apply 'LIMIT' to keep result sets small.
			- Use multiple queries only when the question requires distinct, independent results.

			SUPPORTED SQL:
			- 'SELECT', 'JOIN', 'WHERE', 'GROUP BY', aggregations ('COUNT', 'SUM', 'AVG', percentiles), 'ORDER BY', 'LIMIT'

			IMPORTANT CONSTRAINTS:
			1. When performing arithmetic on 'BIGINT' columns (e.g. 'rtt_us'), explicitly cast operands to 'BIGINT' to avoid overflow:
				CAST(rtt_usASBIGINT) * CAST(rtt_usASBIGINT)
			2. Do not return large volumes of raw rows. Summarize whenever possible.

			For general information about DoubleZero, see https://doublezero.xyz/
		`,
	)
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

	if err := s.registerSchemas(ctx); err != nil {
		s.log.Error("failed to register schemas", "error", err)
	}

	// Peirodically check if the schemas are enabled and register the tools if they are.
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.registerSchemas(ctx); err != nil {
					s.log.Error("failed to register schemas", "error", err)
				}
			}
		}
	}()

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

func (s *Server) registerSchemas(ctx context.Context) error {
	candidateSchemas := map[string]*schematypes.Schema{}
	for _, schema := range s.cfg.Querier.CandidateSchemas(ctx) {
		candidateSchemas[schema.Name] = schema
	}

	schemas, err := s.cfg.Querier.EnabledSchemas(ctx)
	if err != nil {
		return fmt.Errorf("failed to get enabled schemas: %w", err)
	}
	enabledSchemas := map[string]*schematypes.Schema{}
	for _, schema := range schemas {
		enabledSchemas[schema.Name] = schema
	}

	s.registeredSchemasMu.Lock()
	defer s.registeredSchemasMu.Unlock()

	// Register the tools for the enabled schemas.
	for _, schema := range enabledSchemas {
		if _, ok := s.registeredSchemas[schema.Name]; ok {
			continue
		}
		s.log.Info("mcp/server: registering schema tool", "schema", schema.Name)
		if err := RegisterSchemaTool(s.log, s.mcp, schema); err != nil {
			s.log.Error("failed to register schema", "error", err)
		}
		s.registeredSchemas[schema.Name] = struct{}{}
	}

	// Unregister the tools for the disabled schemas.
	for _, schema := range candidateSchemas {
		if _, ok := s.registeredSchemas[schema.Name]; !ok {
			continue
		}
		if _, ok := enabledSchemas[schema.Name]; !ok {
			s.log.Info("mcp/server: unregistering schema tool", "schema", schema.Name)
			s.mcp.RemoveTools(schema.Name)
			delete(s.registeredSchemas, schema.Name)
		}
	}
	return nil
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
