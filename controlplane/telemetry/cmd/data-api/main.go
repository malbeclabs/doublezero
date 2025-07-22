package main

import (
	"context"
	"flag"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/lmittmann/tint"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data"
	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

func main() {
	verbose := flag.Bool("verbose", false, "verbose logging")
	listenAddr := flag.String("listen-addr", ":8080", "address to listen on")
	flag.Parse()

	log := newLogger(*verbose)

	testnetProvider, err := testnetProvider(log)
	if err != nil {
		log.Error("failed to create testnet provider", "error", err)
		os.Exit(1)
	}

	devnetProvider, err := devnetProvider(log)
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

func testnetProvider(log *slog.Logger) (data.Provider, error) {
	serviceabilityProgramID := solana.MustPublicKeyFromBase58(serviceability.SERVICEABILITY_PROGRAM_ID_TESTNET)
	telemetryProgramID := solana.MustPublicKeyFromBase58(telemetry.TELEMETRY_PROGRAM_ID_TESTNET)

	rpcClient := solanarpc.New(dzsdk.DZ_LEDGER_RPC_URL)

	return data.NewProvider(&data.ProviderConfig{
		Logger:               log,
		ServiceabilityClient: serviceability.New(rpcClient, serviceabilityProgramID),
		TelemetryClient:      telemetry.New(log, rpcClient, nil, telemetryProgramID),
	})
}

func devnetProvider(log *slog.Logger) (data.Provider, error) {
	serviceabilityProgramID := solana.MustPublicKeyFromBase58(serviceability.SERVICEABILITY_PROGRAM_ID_DEVNET)
	telemetryProgramID := solana.MustPublicKeyFromBase58(telemetry.TELEMETRY_PROGRAM_ID_DEVNET)

	rpcClient := solanarpc.New(dzsdk.DZ_LEDGER_RPC_URL)

	return data.NewProvider(&data.ProviderConfig{
		Logger:               log,
		ServiceabilityClient: serviceability.New(rpcClient, serviceabilityProgramID),
		TelemetryClient:      telemetry.New(log, rpcClient, nil, telemetryProgramID),
	})
}
