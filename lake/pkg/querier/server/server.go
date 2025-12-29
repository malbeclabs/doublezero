package server

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	wire "github.com/jeroenrinzema/psql-wire"
	"github.com/malbeclabs/doublezero/lake/pkg/querier"
)

type Server struct {
	log              *slog.Logger
	cfg              Config
	querier          *querier.Querier
	httpSrv          *http.Server
	httpListener     net.Listener
	psqlSrv          *wire.Server
	postgresListener net.Listener
}

func New(ctx context.Context, cfg Config) (*Server, error) {
	// Load configuration from environment variables if not already set
	if err := cfg.LoadFromEnv(); err != nil {
		return nil, fmt.Errorf("failed to load config from environment: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	querier, err := querier.New(cfg.QuerierConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create querier: %w", err)
	}

	mux := http.NewServeMux()

	s := &Server{
		log:     cfg.QuerierConfig.Logger,
		cfg:     cfg,
		querier: querier,
	}

	mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("ok\n")); err != nil {
			s.log.Error("failed to write healthz response", "error", err)
		}
	}))
	mux.Handle("/readyz", http.HandlerFunc(s.readyzHandler))

	s.httpSrv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		// Add timeouts to prevent connection issues from affecting the server
		// Increased from 30s to 60s to avoid readiness probe timeouts
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
		// Set MaxHeaderBytes to prevent abuse
		MaxHeaderBytes: 1 << 20, // 1MB
	}
	s.httpListener = cfg.HTTPListener

	// Set up PostgreSQL wire protocol server if listener is configured
	if cfg.PostgresListener != nil {
		// Log authentication status
		authEnabled := len(cfg.PostgresAccounts) > 0
		if authEnabled {
			s.log.Info("server: postgres authentication enabled", "account_count", len(cfg.PostgresAccounts))
		} else {
			s.log.Info("server: postgres authentication disabled (no accounts configured)")
		}

		// Create auth strategy that checks accounts dynamically at runtime
		authStrategy := createAuthStrategy(s.log, cfg.PostgresAccounts)

		psqlSrv, err := wire.NewServer(
			s.queryHandler,
			wire.Logger(s.log),
			wire.SessionAuthStrategy(authStrategy),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create PostgreSQL wire server: %w", err)
		}
		s.psqlSrv = psqlSrv
		s.postgresListener = cfg.PostgresListener
	}

	return s, nil
}

func (s *Server) Run(ctx context.Context) error {
	serveErrCh := make(chan error, 2)

	// Start HTTP server
	go func() {
		if err := s.httpSrv.Serve(s.httpListener); err != nil && err != http.ErrServerClosed {
			s.log.Error("server: http server error", "error", err)
			serveErrCh <- fmt.Errorf("failed to serve HTTP: %w", err)
		}
	}()
	s.log.Info("server: http listening", "address", s.httpListener.Addr())

	// Start PostgreSQL wire protocol server if configured
	if s.psqlSrv != nil && s.postgresListener != nil {
		go func() {
			if err := s.psqlSrv.Serve(s.postgresListener); err != nil {
				s.log.Error("server: postgres wire server error", "error", err)
				serveErrCh <- fmt.Errorf("failed to serve PostgreSQL: %w", err)
			}
		}()
		s.log.Info("server: postgres wire protocol listening", "address", s.postgresListener.Addr())
	}

	select {
	case <-ctx.Done():
		s.log.Info("server: stopping", "reason", ctx.Err())
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer shutdownCancel()

		// Shutdown HTTP server
		if err := s.httpSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to shutdown HTTP server: %w", err)
		}
		s.log.Info("server: http server shutdown complete")

		// Shutdown PostgreSQL wire protocol server
		if s.psqlSrv != nil {
			if err := s.psqlSrv.Shutdown(shutdownCtx); err != nil {
				return fmt.Errorf("failed to shutdown PostgreSQL wire server: %w", err)
			}
			s.log.Info("server: postgres wire server shutdown complete")
		}

		return nil
	case err := <-serveErrCh:
		s.log.Error("server: server error causing shutdown", "error", err)
		return err
	}
}

func (s *Server) readyzHandler(w http.ResponseWriter, r *http.Request) {
	if !s.querier.Ready() {
		s.log.Debug("readyz: querier not ready")
		w.WriteHeader(http.StatusServiceUnavailable)
		if _, err := w.Write([]byte("querier not ready\n")); err != nil {
			s.log.Error("failed to write readyz response", "error", err)
		}
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write([]byte("ok\n")); err != nil {
		s.log.Error("failed to write readyz response", "error", err)
	}
}
