package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	dzsvc "github.com/malbeclabs/doublezero/tools/mcp/internal/dz/serviceability"
	dztelem "github.com/malbeclabs/doublezero/tools/mcp/internal/dz/telemetry"
	"github.com/malbeclabs/doublezero/tools/mcp/internal/sol"
	sqltools "github.com/malbeclabs/doublezero/tools/mcp/internal/tools/sql"
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

	svcView, err := dzsvc.NewView(dzsvc.ViewConfig{
		Logger:            cfg.Logger,
		Clock:             cfg.Clock,
		ServiceabilityRPC: cfg.ServiceabilityRPC,
		RefreshInterval:   cfg.RefreshInterval,
		DB:                cfg.DB,
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

	// Apply authentication middleware to MCP endpoint if tokens are configured
	if len(cfg.AllowedTokens) > 0 {
		authHandler := s.authMiddleware(handler)
		mux.Handle("/", authHandler)
	} else {
		mux.Handle("/", handler)
	}

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", s.readyzHandler)

	s.httpServer = &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: s.cfg.ReadHeaderTimeout,
		// Add timeouts to prevent connection issues from affecting the server
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
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
		s.cfg.Logger.Info("server: shutting down")
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer shutdownCancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to shutdown server: %w", err)
		}
		return nil
	case err := <-serveErrCh:
		return err
	}
}

func (s *Server) readyzHandler(w http.ResponseWriter, r *http.Request) {
	if !s.serviceabilityView.Ready() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("serviceability view not ready\n"))
		return
	}

	if !s.telemetryView.Ready() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("telemetry view not ready\n"))
		return
	}

	if !s.solanaView.Ready() {
		w.WriteHeader(http.StatusServiceUnavailable)
		w.Write([]byte("solana view not ready\n"))
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok\n"))
}

// authMiddleware wraps an HTTP handler with Bearer token authentication
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			w.Header().Set("WWW-Authenticate", `Bearer`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("unauthorized: missing authorization header\n"))
			return
		}

		// Extract token from "Bearer <token>" format
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			w.Header().Set("WWW-Authenticate", `Bearer`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("unauthorized: invalid authorization header format\n"))
			return
		}

		token := strings.TrimSpace(parts[1])
		if token == "" {
			w.Header().Set("WWW-Authenticate", `Bearer`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("unauthorized: empty token\n"))
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
			w.Header().Set("WWW-Authenticate", `Bearer`)
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("unauthorized: invalid token\n"))
			return
		}

		// Token is valid, proceed with the request
		next.ServeHTTP(w, r)
	})
}
