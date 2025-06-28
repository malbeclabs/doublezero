package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

const (
	defaultProbeInterval         = 10 * time.Second
	defaultSubmissionInterval    = 60 * time.Second
	defaultTWAMPListenPort       = telemetry.DefaultTWAMPListenPort
	defaultTWAMPReflectorTimeout = 1 * time.Second
	defaultPeersRefreshInterval  = 10 * time.Second
	defaultTWAMPSenderTimeout    = 1 * time.Second
	defaultLedgerRPCURL          = ""
	defaultProgramId             = ""
	defaultLocalDevicePubkey     = ""
)

var version = "dev"

var (
	ledgerRPCURL          = flag.String("ledger-rpc-url", defaultLedgerRPCURL, "the url of the ledger rpc")
	programId             = flag.String("program-id", defaultProgramId, "the id of the program")
	localDevicePubkey     = flag.String("local-device-pubkey", defaultLocalDevicePubkey, "the pubkey of the local device")
	twampListenPort       = flag.Uint("twamp-listen-port", uint(defaultTWAMPListenPort), "the port to listen for twamp probes")
	probeInterval         = flag.Duration("probe-interval", defaultProbeInterval, "the interval to probe peers")
	submissionInterval    = flag.Duration("submission-interval", defaultSubmissionInterval, "the interval to submit samples")
	twampSenderTimeout    = flag.Duration("twamp-sender-timeout", defaultTWAMPSenderTimeout, "the timeout for sending twamp probes")
	twampReflectorTimeout = flag.Duration("twamp-reflector-timeout", defaultTWAMPReflectorTimeout, "the timeout for the twamp reflector")
	peersRefreshInterval  = flag.Duration("peers-refresh-interval", defaultPeersRefreshInterval, "the interval to refresh the peer discovery")
	verbose               = flag.Bool("verbose", false, "enable verbose logging")
	showVersion           = flag.Bool("version", false, "print version and exit")
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
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

	if *ledgerRPCURL == "" {
		log.Error("Missing required flag", "flag", "ledger-rpc-url")
		flag.Usage()
		os.Exit(1)
	}
	if *programId == "" {
		log.Error("Missing required flag", "flag", "program-id")
		flag.Usage()
		os.Exit(1)
	}
	if *localDevicePubkey == "" {
		log.Error("Missing required flag", "flag", "local-device-pubkey")
		flag.Usage()
		os.Exit(1)
	}

	log.Info("Starting telemetry collector",
		"ledgerRPCURL", *ledgerRPCURL,
		"programId", *programId,
		"devicePubkey", *localDevicePubkey,
		"probeInterval", *probeInterval,
		"submissionInterval", *submissionInterval,
		"twampListenPort", *twampListenPort,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Set up TWAMP reflector
	reflector, err := twamplight.NewReflector(log, uint16(*twampListenPort), *twampReflectorTimeout)
	if err != nil {
		log.Error("failed to create TWAMP reflector", "error", err)
		os.Exit(1)
	}

	// Set up real peer discovery.
	programID, err := solana.PublicKeyFromBase58(*programId)
	if err != nil {
		log.Error("failed to parse program ID", "error", err)
		os.Exit(1)
	}
	rpcClient := rpc.New(*ledgerRPCURL)
	serviceabilityClient := serviceability.New(rpcClient, programID)
	if err != nil {
		log.Error("failed to create serviceability client", "error", err)
		os.Exit(1)
	}
	peerDiscovery, err := telemetry.NewLedgerPeerDiscovery(
		&telemetry.LedgerPeerDiscoveryConfig{
			Logger:            log,
			LocalDevicePubKey: *localDevicePubkey,
			ProgramClient:     serviceabilityClient,
			TWAMPPort:         uint16(*twampListenPort),
			RefreshInterval:   *peersRefreshInterval,
		},
	)
	if err != nil {
		log.Error("failed to create ledger peer discovery", "error", err)
		os.Exit(1)
	}

	// Initialize telemetry program client.
	// TODO(snormore): Replace with actual implementation.
	telemetryProgramClient := newLoggingTelemetryClient(log)

	// Initialize collector.
	collector, err := telemetry.New(log, telemetry.Config{
		LocalDevicePubkey:      *localDevicePubkey,
		ProbeInterval:          *probeInterval,
		SubmissionInterval:     *submissionInterval,
		TWAMPSenderTimeout:     *twampSenderTimeout,
		TWAMPReflector:         reflector,
		PeerDiscovery:          peerDiscovery,
		TelemetryProgramClient: telemetryProgramClient,
	})
	if err != nil {
		log.Error("failed to create telemetry collector", "error", err)
		os.Exit(1)
	}

	// Run the collector.
	if err := collector.Run(ctx); err != nil {
		log.Error("collector exited with error", "error", err)
		os.Exit(1)
	}
}

// newLoggingTelemetryClient prints submitted samples to log.
func newLoggingTelemetryClient(log *slog.Logger) telemetry.TelemetryProgramClient {
	return &loggingClient{log}
}

type loggingClient struct {
	log *slog.Logger
}

func (l *loggingClient) AddSamples(ctx context.Context, samples []telemetry.Sample) error {
	for _, s := range samples {
		l.log.Info("[MOCK LEDGER LOG] telemetry sample", "device_a_pk", *localDevicePubkey, "device_z_pk", s.Device, "link_pk", s.Link, "rtt", s.RTT, "loss", s.Loss)
	}
	return nil
}
