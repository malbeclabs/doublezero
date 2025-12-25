package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	dzsvc "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/dz/serviceability"
	dztelem "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/dz/telemetry"
	mcpgeoip "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/geoip"
	mcpmetrics "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/metrics"
	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/sol"
	sqltools "github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/tools/sql"
)

type Server struct {
	cfg                Config
	serviceabilityView *dzsvc.View
	telemetryView      *dztelem.View
	solanaView         *sol.View
	mcpServer          *mcp.Server
	httpServer         *http.Server
}

func New(cfg Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Initialize GeoIP store
	geoIPStore, err := mcpgeoip.NewStore(mcpgeoip.StoreConfig{
		Logger: cfg.Logger,
		DB:     cfg.DB,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create GeoIP store: %w", err)
	}
	if err := geoIPStore.CreateTablesIfNotExists(); err != nil {
		return nil, fmt.Errorf("failed to create GeoIP tables: %w", err)
	}

	svcView, err := dzsvc.NewView(dzsvc.ViewConfig{
		Logger:            cfg.Logger,
		Clock:             cfg.Clock,
		ServiceabilityRPC: cfg.ServiceabilityRPC,
		RefreshInterval:   cfg.RefreshInterval,
		DB:                cfg.DB,
		GeoIPStore:        geoIPStore,
		GeoIPResolver:     cfg.GeoIPResolver,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create serviceability view: %w", err)
	}

	telemView, err := dztelem.NewView(dztelem.ViewConfig{
		Logger:                 cfg.Logger,
		Clock:                  cfg.Clock,
		TelemetryRPC:           cfg.TelemetryRPC,
		EpochRPC:               cfg.DZEpochRPC,
		MaxConcurrency:         cfg.MaxConcurrency,
		InternetLatencyAgentPK: cfg.InternetLatencyAgentPK,
		InternetDataProviders:  cfg.InternetDataProviders,
		DB:                     cfg.DB,
		Serviceability:         svcView,
		RefreshInterval:        cfg.RefreshInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create telemetry view: %w", err)
	}

	solanaView, err := sol.NewView(sol.ViewConfig{
		Logger:          cfg.Logger,
		Clock:           cfg.Clock,
		RPC:             cfg.SolanaRPC,
		DB:              cfg.DB,
		RefreshInterval: cfg.RefreshInterval,
		GeoIPStore:      *geoIPStore,
		GeoIPResolver:   cfg.GeoIPResolver,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create solana view: %w", err)
	}

	mcpServer := mcp.NewServer(&mcp.Implementation{
		Name:    "DoubleZero MCP Server",
		Version: cfg.Version,
	}, nil)

	svcSchemaTool, err := svcView.SchemaTool()
	if err != nil {
		return nil, fmt.Errorf("failed to create serviceability schema tool: %w", err)
	}
	if err := svcSchemaTool.Register(mcpServer); err != nil {
		return nil, fmt.Errorf("failed to register serviceability schema tool: %w", err)
	}

	telemSchemaTool, err := telemView.SchemaTool()
	if err != nil {
		return nil, fmt.Errorf("failed to create telemetry schema tool: %w", err)
	}
	if err := telemSchemaTool.Register(mcpServer); err != nil {
		return nil, fmt.Errorf("failed to register telemetry schema tool: %w", err)
	}

	solanaSchemaTool, err := solanaView.SchemaTool()
	if err != nil {
		return nil, fmt.Errorf("failed to create solana schema tool: %w", err)
	}
	if err := solanaSchemaTool.Register(mcpServer); err != nil {
		return nil, fmt.Errorf("failed to register solana schema tool: %w", err)
	}

	geoipSchemaTool, err := geoIPStore.SchemaTool()
	if err != nil {
		return nil, fmt.Errorf("failed to create geoip schema tool: %w", err)
	}
	if err := geoipSchemaTool.Register(mcpServer); err != nil {
		return nil, fmt.Errorf("failed to register geoip schema tool: %w", err)
	}

	queryTool, err := sqltools.NewQueryTool(sqltools.QueryToolConfig{
		Logger: cfg.Logger,
		DB:     cfg.DB,
		Name:   "query",
		Description: `
			PURPOSE:
			Execute DuckDB SQL queries across all DoubleZero datasets (serviceability, telemetry, and Solana).

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
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create query tool: %w", err)
	}
	if err := queryTool.Register(mcpServer); err != nil {
		return nil, fmt.Errorf("failed to register query tool: %w", err)
	}

	s := &Server{
		cfg:                cfg,
		serviceabilityView: svcView,
		telemetryView:      telemView,
		solanaView:         solanaView,
		mcpServer:          mcpServer,
	}

	mux := http.NewServeMux()
	handler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return mcpServer
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
			s.cfg.Logger.Error("failed to write healthz response", "error", err)
		}
	})))
	mux.Handle("/readyz", s.metricsMiddleware(http.HandlerFunc(s.readyzHandler)))

	s.httpServer = &http.Server{
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
	s.serviceabilityView.Start(ctx)
	s.telemetryView.Start(ctx)
	s.solanaView.Start(ctx)

	serveErrCh := make(chan error, 1)
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log the error but don't immediately exit - this could be a transient network issue
			s.cfg.Logger.Error("server: http server error", "error", err)
			serveErrCh <- fmt.Errorf("failed to listen and serve: %w", err)
		}
	}()

	s.cfg.Logger.Info("server: mcp streamable http listening",
		"listenAddr", s.cfg.ListenAddr,
	)

	select {
	case <-ctx.Done():
		s.cfg.Logger.Info("server: Run() context cancelled",
			"reason", ctx.Err(),
			"listenAddr", s.cfg.ListenAddr,
		)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer shutdownCancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to shutdown server: %w", err)
		}
		s.cfg.Logger.Info("server: HTTP server shutdown complete")
		return nil
	case err := <-serveErrCh:
		s.cfg.Logger.Error("server: http server error causing shutdown",
			"error", err,
			"listenAddr", s.cfg.ListenAddr,
		)
		return err
	}
}

func (s *Server) readyzHandler(w http.ResponseWriter, r *http.Request) {
	if !s.serviceabilityView.Ready() {
		s.cfg.Logger.Debug("readyz: serviceability view not ready")
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := w.Write([]byte("serviceability view not ready\n")); err != nil {
			s.cfg.Logger.Error("failed to write readyz response", "error", err)
		}
		return
	}

	if !s.telemetryView.Ready() {
		s.cfg.Logger.Debug("readyz: telemetry view not ready")
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := w.Write([]byte("telemetry view not ready\n")); err != nil {
			s.cfg.Logger.Error("failed to write readyz response", "error", err)
		}
		return
	}

	if !s.solanaView.Ready() {
		s.cfg.Logger.Debug("readyz: solana view not ready")
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := w.Write([]byte("solana view not ready\n")); err != nil {
			s.cfg.Logger.Error("failed to write readyz response", "error", err)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok\n")); err != nil {
		s.cfg.Logger.Error("failed to write readyz response", "error", err)
	}
}

// authMiddleware wraps an HTTP handler with Bearer token authentication
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			mcpmetrics.AuthFailuresTotal.WithLabelValues("missing_header").Inc()
			w.Header().Set("WWW-Authenticate", `Bearer`)
			w.WriteHeader(http.StatusUnauthorized)
			if _, err := w.Write([]byte("unauthorized: missing authorization header\n")); err != nil {
				s.cfg.Logger.Error("failed to write auth error response", "error", err)
			}
			return
		}

		// Extract token from "Bearer <token>" format
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			mcpmetrics.AuthFailuresTotal.WithLabelValues("invalid_format").Inc()
			w.Header().Set("WWW-Authenticate", `Bearer`)
			w.WriteHeader(http.StatusUnauthorized)
			if _, err := w.Write([]byte("unauthorized: invalid authorization header format\n")); err != nil {
				s.cfg.Logger.Error("failed to write auth error response", "error", err)
			}
			return
		}

		token := strings.TrimSpace(parts[1])
		if token == "" {
			mcpmetrics.AuthFailuresTotal.WithLabelValues("empty_token").Inc()
			w.Header().Set("WWW-Authenticate", `Bearer`)
			w.WriteHeader(http.StatusUnauthorized)
			if _, err := w.Write([]byte("unauthorized: empty token\n")); err != nil {
				s.cfg.Logger.Error("failed to write auth error response", "error", err)
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
			mcpmetrics.AuthFailuresTotal.WithLabelValues("invalid_token").Inc()
			w.Header().Set("WWW-Authenticate", `Bearer`)
			w.WriteHeader(http.StatusUnauthorized)
			if _, err := w.Write([]byte("unauthorized: invalid token\n")); err != nil {
				s.cfg.Logger.Error("failed to write auth error response", "error", err)
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

		mcpmetrics.HTTPRequestsTotal.WithLabelValues(method, endpoint, status).Inc()
		mcpmetrics.HTTPRequestDuration.Observe(duration)
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
