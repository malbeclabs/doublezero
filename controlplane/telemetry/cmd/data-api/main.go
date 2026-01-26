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
	devicedata "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/device"
	inetdata "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/internet"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/epoch"
)

func main() {
	verbose := flag.Bool("verbose", false, "verbose logging")
	listenAddr := flag.String("listen-addr", ":8080", "address to listen on")
	flag.Parse()

	log := newLogger(*verbose)

	mainnetDeviceProvider, err := newDeviceProvider(log, config.EnvMainnetBeta)
	if err != nil {
		log.Error("failed to create mainnet provider", "error", err)
		os.Exit(1)
	}

	testnetDeviceProvider, err := newDeviceProvider(log, config.EnvTestnet)
	if err != nil {
		log.Error("failed to create testnet provider", "error", err)
		os.Exit(1)
	}

	devnetDeviceProvider, err := newDeviceProvider(log, config.EnvDevnet)
	if err != nil {
		log.Error("failed to create devnet provider", "error", err)
		os.Exit(1)
	}

	mainnetInternetProvider, err := newInternetProvider(log, config.EnvMainnetBeta)
	if err != nil {
		log.Error("failed to create mainnet internet provider", "error", err)
		os.Exit(1)
	}

	testnetInternetProvider, err := newInternetProvider(log, config.EnvTestnet)
	if err != nil {
		log.Error("failed to create testnet internet provider", "error", err)
		os.Exit(1)
	}

	devnetInternetProvider, err := newInternetProvider(log, config.EnvDevnet)
	if err != nil {
		log.Error("failed to create devnet internet provider", "error", err)
		os.Exit(1)
	}

	cfg := data.ServerConfig{
		Logger:                      log,
		MainnetDeviceDataProvider:   mainnetDeviceProvider,
		MainnetInternetDataProvider: mainnetInternetProvider,
		TestnetDeviceDataProvider:   testnetDeviceProvider,
		DevnetDeviceDataProvider:    devnetDeviceProvider,
		TestnetInternetDataProvider: testnetInternetProvider,
		DevnetInternetDataProvider:  devnetInternetProvider,
	}

	server, err := data.NewServer(&cfg)
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

func newDeviceProvider(log *slog.Logger, env string) (devicedata.Provider, error) {
	networkConfig, err := config.NetworkConfigForEnv(env)
	if err != nil {
		return nil, fmt.Errorf("failed to get network config: %w", err)
	}

	rpcClient := solanarpc.New(networkConfig.LedgerPublicRPCURL)

	epochFinder, err := epoch.NewFinder(log, rpcClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create epoch finder: %w", err)
	}

	return devicedata.NewProvider(&devicedata.ProviderConfig{
		Logger:               log,
		ServiceabilityClient: serviceability.New(rpcClient, networkConfig.ServiceabilityProgramID),
		TelemetryClient:      telemetry.New(log, rpcClient, nil, networkConfig.TelemetryProgramID),
		EpochFinder:          epochFinder,
	})
}

func newInternetProvider(log *slog.Logger, env string) (inetdata.Provider, error) {
	networkConfig, err := config.NetworkConfigForEnv(env)
	if err != nil {
		return nil, fmt.Errorf("failed to get network config: %w", err)
	}

	rpcClient := solanarpc.New(networkConfig.LedgerPublicRPCURL)

	epochFinder, err := epoch.NewFinder(log, rpcClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create epoch finder: %w", err)
	}

	return inetdata.NewProvider(&inetdata.ProviderConfig{
		Logger:               log,
		ServiceabilityClient: serviceability.New(rpcClient, networkConfig.ServiceabilityProgramID),
		TelemetryClient:      telemetry.New(log, rpcClient, nil, networkConfig.TelemetryProgramID),
		EpochFinder:          epochFinder,
		AgentPK:              networkConfig.InternetLatencyCollectorPK,
	})
}
