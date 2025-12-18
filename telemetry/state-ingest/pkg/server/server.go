package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"sync"
)

type Server struct {
	log *slog.Logger
	cfg Config

	svc     *ServiceabilityView
	handler *Handler

	httpSrv      *http.Server
	shutdownOnce sync.Once
}

func New(log *slog.Logger, cfg Config) (*Server, error) {
	if log == nil {
		return nil, errors.New("logger is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	svc := NewServiceabilityView(log, cfg.Clock, cfg.ServiceabilityRefreshInterval, cfg.ServiceabilityRPC)
	h, err := NewHandler(log, cfg, svc)
	if err != nil {
		return nil, err
	}

	return &Server{
		log:     log,
		cfg:     cfg,
		svc:     svc,
		handler: h,
	}, nil
}

func (s *Server) Start(ctx context.Context, cancel context.CancelFunc, listener net.Listener) <-chan error {
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		defer cancel()
		if err := s.svc.Run(ctx); err != nil {
			s.log.Error("failed to run serviceability view", "error", err)
			errCh <- err
		}
	}()

	go func() {
		defer wg.Done()
		defer cancel()
		if err := s.Serve(ctx, listener); err != nil {
			s.log.Error("server exited with error", "error", err)
			errCh <- err
		} else {
			s.log.Info("server stopped")
		}
	}()

	go func() {
		wg.Wait()
		close(errCh)
	}()

	return errCh
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	mux := http.NewServeMux()
	s.handler.Register(mux)

	s.httpSrv = &http.Server{Handler: mux}

	go func() {
		<-ctx.Done()
		s.shutdown()
	}()

	err := s.httpSrv.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) shutdown() {
	s.shutdownOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer cancel()
		if s.httpSrv != nil {
			_ = s.httpSrv.Shutdown(ctx)
		}
	})
}
