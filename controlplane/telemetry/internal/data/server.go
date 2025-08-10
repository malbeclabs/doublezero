package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/malbeclabs/doublezero/config"
	datadevice "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/device"
	datainet "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/internet"
)

var (
	ErrInvalidEnvironment = fmt.Errorf("invalid environment")
)

const (
	DefaultMaxPoints = 1000
)

type ServerConfig struct {
	Logger *slog.Logger

	TestnetInternetDataProvider datainet.Provider
	TestnetDeviceDataProvider   datadevice.Provider
	DevnetInternetDataProvider  datainet.Provider
	DevnetDeviceDataProvider    datadevice.Provider
}

func (c *ServerConfig) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.TestnetInternetDataProvider == nil {
		return errors.New("testnet internet data provider is required")
	}
	if c.TestnetDeviceDataProvider == nil {
		return errors.New("testnet device data provider is required")
	}
	if c.DevnetInternetDataProvider == nil {
		return errors.New("devnet internet data provider is required")
	}
	if c.DevnetDeviceDataProvider == nil {
		return errors.New("devnet device data provider is required")
	}
	return nil
}

type Server struct {
	log *slog.Logger

	deviceServer *datadevice.Server
	inetServer   *datainet.Server
}

func NewServer(cfg *ServerConfig) (*Server, error) {
	deviceServer, err := datadevice.NewServer(cfg.Logger, cfg.TestnetDeviceDataProvider, cfg.DevnetDeviceDataProvider)
	if err != nil {
		return nil, err
	}
	inetServer, err := datainet.NewServer(cfg.Logger, cfg.TestnetInternetDataProvider, cfg.DevnetInternetDataProvider)
	if err != nil {
		return nil, err
	}
	s := &Server{
		log:          cfg.Logger,
		deviceServer: deviceServer,
		inetServer:   inetServer,
	}
	return s, nil
}

func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	combinedMux := http.NewServeMux()
	combinedMux.HandleFunc("/envs", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode([]string{config.EnvMainnet, config.EnvTestnet, config.EnvDevnet}); err != nil {
			s.log.Error("failed to encode envs", "error", err)
			http.Error(w, fmt.Sprintf("failed to encode envs: %v", err), http.StatusInternalServerError)
			return
		}
	})
	combinedMux.Handle("/device-link/", s.deviceServer.Mux)
	combinedMux.Handle("/location-internet/", s.inetServer.Mux)

	srv := &http.Server{
		Handler: combinedMux,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Serve(listener)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			s.log.Warn("server shutdown error", "error", err)
		} else {
			s.log.Info("server shutdown via context")
		}
		return nil
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			s.log.Info("server closed")
			return nil
		}
		return err
	}
}
