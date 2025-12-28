package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/mcastrelay/internal/multicast"
	"github.com/malbeclabs/doublezero/mcastrelay/internal/server"
	flag "github.com/spf13/pflag"
)

var (
	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

type config struct {
	MulticastIP       string
	MulticastPort     int
	SocketBufferSize  int
	GRPCAddr          string
	Interface         string
	MulticastLoopback bool
	Verbose           bool
	ShowVersion       bool
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cfg := parseFlags()

	if cfg.ShowVersion {
		fmt.Printf("mcastrelay version: %s, commit: %s, date: %s\n", version, commit, date)
		return nil
	}

	log := newLogger(cfg.Verbose)

	// Create multicast listener
	mcastCfg := &multicast.Config{
		Logger:            log.With("component", "multicast"),
		MulticastIP:       cfg.MulticastIP,
		Port:              cfg.MulticastPort,
		InterfaceName:     cfg.Interface,
		BufferSize:        65535,
		SocketBufferSize:  cfg.SocketBufferSize,
		ReadTimeout:       250 * time.Millisecond,
		MulticastLoopback: cfg.MulticastLoopback,
	}

	mcastListener, err := multicast.NewListener(mcastCfg)
	if err != nil {
		return fmt.Errorf("failed to create multicast listener: %w", err)
	}

	// Create gRPC listener
	grpcLis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", cfg.GRPCAddr, err)
	}
	log.Info("gRPC listener created", "address", grpcLis.Addr().String())

	// Create gRPC server
	srvCfg := &server.Config{
		Logger:        log.With("component", "grpc"),
		Listener:      mcastListener,
		ChannelBuffer: 256,
	}

	srv, err := server.New(srvCfg)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Setup context for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Start multicast listener
	go func() {
		if err := mcastListener.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("multicast listener error", "error", err)
			cancel()
		}
	}()

	// Start gRPC server
	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(grpcLis); err != nil {
			errCh <- err
		}
		close(errCh)
	}()

	// Wait for shutdown signal or error
	select {
	case sig := <-sigCh:
		log.Info("received shutdown signal", "signal", sig)
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
	}

	// Graceful shutdown
	cancel()
	srv.Stop()

	log.Info("server shutdown complete")
	return nil
}

func parseFlags() *config {
	cfg := &config{}

	flag.StringVar(&cfg.MulticastIP, "multicast-ip", "239.0.0.1", "Multicast group IP address")
	flag.IntVar(&cfg.MulticastPort, "multicast-port", 5000, "Multicast port")
	flag.IntVar(&cfg.SocketBufferSize, "socket-buffer-size", multicast.DefaultSocketBufferSize,
		"UDP socket receive buffer size (SO_RCVBUF) for high-throughput streams")
	flag.StringVar(&cfg.GRPCAddr, "grpc-addr", ":50051", "gRPC server address")
	flag.StringVar(&cfg.Interface, "interface", "", "Network interface for multicast (optional)")
	flag.BoolVar(&cfg.MulticastLoopback, "loopback", false, "Enable multicast loopback (receive own packets, for testing)")
	flag.BoolVarP(&cfg.Verbose, "verbose", "v", false, "Enable verbose logging")
	flag.BoolVar(&cfg.ShowVersion, "version", false, "Show version and exit")

	flag.Parse()
	return cfg
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}

	return slog.New(tint.NewHandler(os.Stderr, &tint.Options{
		Level:      level,
		TimeFormat: time.RFC3339,
	}))
}
