package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netns"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/state"
	stateingest "github.com/malbeclabs/doublezero/telemetry/state-ingest/pkg/client"
)

const (
	defaultStateInterval     = 60 * time.Second
	defaultLocalDevicePubkey = ""

	waitForNamespaceTimeout = 30 * time.Second
)

var (
	env                 = flag.String("env", "", "The network environment to use (devnet, testnet, mainnet-beta).")
	keypairPath         = flag.String("keypair", "", "The path to the metrics publisher keypair.")
	localDevicePK       = flag.String("local-device-pubkey", defaultLocalDevicePubkey, "The pubkey of the local device.")
	managementNamespace = flag.String("management-namespace", "", "The name of the management namespace to use for ledger communication. If not provided, the default namespace will be used. (default: '')")
	verbose             = flag.Bool("verbose", false, "Enable verbose logging.")
	showVersion         = flag.Bool("version", false, "Print the version of the doublezero-agent and exit.")
	stateInterval       = flag.Duration("state-interval", defaultStateInterval, "The interval to collect and submit state snapshots.")

	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("version: %s, commit: %s, date: %s\n", version, commit, date)
		os.Exit(0)
	}

	logLevel := slog.LevelInfo
	if *verbose {
		logLevel = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level:     logLevel,
		AddSource: true,
	}))

	// Validate required flags.
	networkConfig, err := config.NetworkConfigForEnv(*env)
	if err != nil {
		log.Error("Failed to get network config", "error", err)
		flag.Usage()
		os.Exit(1)
	}

	if *localDevicePK == "" {
		log.Error("Missing required flag", "flag", "local-device-pubkey")
		flag.Usage()
		os.Exit(1)
	}
	if *keypairPath == "" {
		log.Error("Missing required flag", "flag", "keypair")
		flag.Usage()
		os.Exit(1)
	}

	// Check that local device pubkey is valid.
	localDevicePK, err := solana.PublicKeyFromBase58(*localDevicePK)
	if err != nil {
		log.Error("Failed to parse local device pubkey", "error", err)
		os.Exit(1)
	}

	// Check that metrics publisher keypair path exists.
	if _, err := os.Stat(*keypairPath); os.IsNotExist(err) {
		log.Error("Metrics publisher keypair does not exist", "path", *keypairPath)
		os.Exit(1)
	}

	// Check that the metrics publisher keypair is valid.
	keypair, err := solana.PrivateKeyFromSolanaKeygenFile(*keypairPath)
	if err != nil {
		log.Error("Failed to load metrics publisher keypair", "error", err)
		os.Exit(1)
	}

	log.Info("Starting telemetry collector",
		"version", version,
		"ledgerRPCURL", networkConfig.LedgerPublicRPCURL,
		"serviceabilityProgramID", networkConfig.ServiceabilityProgramID.String(),
		"devicePubkey", localDevicePK,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// If using a management namespace, wait for it to be ready.
	if *managementNamespace != "" {
		_, err := netns.WaitForNamespace(log, *managementNamespace, waitForNamespaceTimeout)
		if err != nil {
			log.Error("failed to wait for namespace", "error", err)
			os.Exit(1)
		}
	}

	// Build HTTP client.
	var httpClient *http.Client
	if *managementNamespace != "" {
		httpClient, err = netns.NewNamespacedHTTPClient(*managementNamespace, nil)
		if err != nil {
			log.Error("failed to create namespace-safe HTTP client", "error", err)
			os.Exit(1)
		}
	} else {
		httpClient = &http.Client{
			Timeout:   10 * time.Second,
			Transport: http.DefaultTransport,
		}
	}

	// Initialize state ingest client.
	signer, err := newSigner(keypair)
	if err != nil {
		log.Error("failed to create signer", "error", err)
		os.Exit(1)
	}
	stateIngestClient, err := stateingest.NewClient(networkConfig.TelemetryStateIngestURL, signer, stateingest.WithHTTPClient(httpClient))
	if err != nil {
		log.Error("failed to create state ingest client", "error", err)
		os.Exit(1)
	}

	// Initialize telemetry state collector.
	stateCollector, err := state.NewCollector(&state.CollectorConfig{
		Logger:      log,
		StateIngest: stateIngestClient,
		Interval:    *stateInterval,
		DevicePK:    localDevicePK,
	})
	if err != nil {
		log.Error("failed to create telemetry state collector", "error", err)
		os.Exit(1)
	}
	stateCollectorErrCh := stateCollector.Start(ctx, cancel)

	// Wait for the context to be done or an error to be returned.
	select {
	case <-ctx.Done():
		log.Info("context done, stopping")
		return
	case err := <-stateCollectorErrCh:
		log.Error("state collector exited with error", "error", err)
		os.Exit(1)
	}
}

func newSigner(keypair solana.PrivateKey) (stateingest.Signer, error) {
	return &signer{keypair: keypair}, nil
}

type signer struct {
	keypair solana.PrivateKey
}

func (s *signer) PublicKey() solana.PublicKey {
	return s.keypair.PublicKey()
}

func (s *signer) Sign(ctx context.Context, data []byte) ([]byte, error) {
	signature, err := s.keypair.Sign(data)
	if err != nil {
		return nil, err
	}
	return signature[:], nil
}
