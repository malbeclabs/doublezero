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

	httpSrvsMu   sync.Mutex
	httpSrvs     []*http.Server
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

func (s *Server) Start(ctx context.Context, cancel context.CancelFunc, listeners ...net.Listener) <-chan error {
	errCh := make(chan error, 1+len(listeners))
	var wg sync.WaitGroup
	wg.Add(1 + len(listeners))

	go func() {
		defer wg.Done()
		defer cancel()
		if err := s.svc.Run(ctx); err != nil {
			s.log.Error("failed to run serviceability view", "error", err)
			errCh <- err
		}
	}()

	for _, l := range listeners {
		l := l
		go func() {
			defer wg.Done()
			defer cancel()
			if err := s.Serve(ctx, l); err != nil {
				s.log.Error("server exited with error", "error", err)
				errCh <- err
			} else {
				s.log.Info("server stopped")
			}
		}()
	}

	go func() {
		wg.Wait()
		close(errCh)
	}()

	return errCh
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	mux := http.NewServeMux()
	s.handler.Register(mux)

	httpSrv := &http.Server{Handler: mux}

	s.httpSrvsMu.Lock()
	s.httpSrvs = append(s.httpSrvs, httpSrv)
	s.httpSrvsMu.Unlock()

	go func() {
		<-ctx.Done()
		s.shutdown()
	}()

	err := httpSrv.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (s *Server) shutdown() {
	s.shutdownOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.cfg.ShutdownTimeout)
		defer cancel()
		s.httpSrvsMu.Lock()
		srvs := s.httpSrvs
		s.httpSrvsMu.Unlock()
		for _, srv := range srvs {
			_ = srv.Shutdown(ctx)
		}
	})
}
