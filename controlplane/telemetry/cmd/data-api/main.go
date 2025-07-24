package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

func main() {
	verbose := flag.Bool("verbose", false, "verbose logging")
	listenAddr := flag.String("listen-addr", ":8080", "address to listen on")
	flag.Parse()

	log := newLogger(*verbose)

	testnetProvider, err := newProvider(log, config.EnvTestnet)
	if err != nil {
		log.Error("failed to create testnet provider", "error", err)
		os.Exit(1)
	}

	devnetProvider, err := newProvider(log, config.EnvDevnet)
	if err != nil {
		log.Error("failed to create devnet provider", "error", err)
		os.Exit(1)
	}

	server, err := data.NewServer(log, testnetProvider, devnetProvider)
	if err != nil {
		log.Error("failed to create server", "error", err)
		os.Exit(1)
	}

	listener, err := net.Listen("tcp", *listenAddr)
	if err != nil {
		log.Error("failed to listen", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	log.Info("listening", "address", listener.Addr().String())
	if err := server.Serve(ctx, listener); err != nil {
		log.Error("failed to serve", "error", err)
		os.Exit(1)
	}
}

func newLogger(verbose bool) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level:      level,
		TimeFormat: time.Kitchen,
	}))
}

func newProvider(log *slog.Logger, env string) (data.Provider, error) {
	networkConfig, err := config.NetworkConfigForEnv(env)
	if err != nil {
		return nil, fmt.Errorf("failed to get network config: %w", err)
	}

	rpcClient := solanarpc.New(networkConfig.LedgerRPCURL)

	return data.NewProvider(&data.ProviderConfig{
		Logger:               log,
		ServiceabilityClient: serviceability.New(rpcClient, networkConfig.ServiceabilityProgramID),
		TelemetryClient:      telemetry.New(log, rpcClient, nil, networkConfig.TelemetryProgramID),
	})
}
