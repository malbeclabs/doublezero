package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/indexer"
)

type Server struct {
	log     *slog.Logger
	cfg     Config
	indexer *indexer.Indexer
	httpSrv *http.Server
}

func New(ctx context.Context, cfg Config) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	indexer, err := indexer.New(ctx, cfg.IndexerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create indexer: %w", err)
	}

	mux := http.NewServeMux()

	s := &Server{
		log:     cfg.IndexerConfig.Logger,
		cfg:     cfg,
		indexer: indexer,
	}

	mux.Handle("/healthz", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write([]byte("ok\n")); err != nil {
			s.log.Error("failed to write healthz response", "error", err)
		}
	}))
	mux.Handle("/readyz", http.HandlerFunc(s.readyzHandler))
	mux.Handle("/version", http.HandlerFunc(s.versionHandler))

	s.httpSrv = &http.Server{
		Addr:              cfg.ListenAddr,
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

	return s, nil
}

func (s *Server) Run(ctx context.Context) error {
	s.indexer.Start(ctx)

	serveErrCh := make(chan error, 1)
	go func() {
		if err := s.httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			// Log the error but don't immediately exit - this could be a transient network issue
			s.log.Error("server: http server error", "error", err)
			serveErrCh <- fmt.Errorf("failed to listen and serve: %w", err)
		}
	}()

	s.log.Info("server: http listening", "address", s.cfg.ListenAddr)

	select {
	case <-ctx.Done():
		s.log.Info("server: stopping", "reason", ctx.Err(), "address", s.cfg.ListenAddr)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer shutdownCancel()
		if err := s.httpSrv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("failed to shutdown server: %w", err)
		}
		s.log.Info("server: http server shutdown complete")
		return nil
	case err := <-serveErrCh:
		s.log.Error("server: http server error causing shutdown", "error", err, "address", s.cfg.ListenAddr)
		return err
	}
}

func (s *Server) readyzHandler(w http.ResponseWriter, r *http.Request) {
	if !s.indexer.Ready() {
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

func (s *Server) versionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(s.cfg.VersionInfo); err != nil {
		s.log.Error("failed to write version response", "error", err)
	}
}
