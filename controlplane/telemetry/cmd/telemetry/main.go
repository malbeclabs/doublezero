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
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netns"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netutil"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	sdktelemetry "github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
)

const (
	defaultProbeInterval         = 10 * time.Second
	defaultSubmissionInterval    = 60 * time.Second
	defaultTWAMPListenPort       = telemetryconfig.TWAMPListenPort
	defaultTWAMPReflectorTimeout = 1 * time.Second
	defaultPeersRefreshInterval  = 10 * time.Second
	defaultTWAMPSenderTimeout    = 1 * time.Second
	defaultLedgerRPCURL          = ""
	defaultProgramId             = ""
	defaultLocalDevicePubkey     = ""
	defaultAristaEAPIGRPCAddress = "127.0.0.1:9543"

	waitForNamespaceTimeout = 30 * time.Second
)

var version = "dev"

var (
	ledgerRPCURL            = flag.String("ledger-rpc-url", defaultLedgerRPCURL, "the url of the ledger rpc")
	serviceabilityProgramID = flag.String("serviceability-program-id", defaultProgramId, "the id of the serviceability program")
	telemetryProgramID      = flag.String("telemetry-program-id", defaultProgramId, "the id of the telemetry program")
	keypairPath             = flag.String("keypair", "", "the path to the metrics publisher keypair")
	localDevicePK           = flag.String("local-device-pubkey", defaultLocalDevicePubkey, "the pubkey of the local device")
	twampListenPort         = flag.Uint("twamp-listen-port", uint(defaultTWAMPListenPort), "the port to listen for twamp probes")
	probeInterval           = flag.Duration("probe-interval", defaultProbeInterval, "the interval to probe peers")
	submissionInterval      = flag.Duration("submission-interval", defaultSubmissionInterval, "the interval to submit samples")
	twampSenderTimeout      = flag.Duration("twamp-sender-timeout", defaultTWAMPSenderTimeout, "the timeout for sending twamp probes")
	twampReflectorTimeout   = flag.Duration("twamp-reflector-timeout", defaultTWAMPReflectorTimeout, "the timeout for the twamp reflector")
	peersRefreshInterval    = flag.Duration("peers-refresh-interval", defaultPeersRefreshInterval, "the interval to refresh the peer discovery")
	managementNamespace     = flag.String("management-namespace", "", "the name of the management namespace to use for ledger communication. If not provided, the default namespace will be used. (default: '')")
	verbose                 = flag.Bool("verbose", false, "enable verbose logging")
	showVersion             = flag.Bool("version", false, "print version and exit")
)

func main() {
	flag.Parse()

	if *probeInterval >= *submissionInterval {
		fmt.Println("probe-interval must be less than submission-interval")
		os.Exit(1)
	}

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

	// Validate required flags.
	if *ledgerRPCURL == "" {
		log.Error("Missing required flag", "flag", "ledger-rpc-url")
		flag.Usage()
		os.Exit(1)
	}
	if *serviceabilityProgramID == "" {
		log.Error("Missing required flag", "flag", "serviceability-program-id")
		flag.Usage()
		os.Exit(1)
	}
	if *telemetryProgramID == "" {
		log.Error("Missing required flag", "flag", "telemetry-program-id")
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
		"ledgerRPCURL", *ledgerRPCURL,
		"serviceabilityProgramID", *serviceabilityProgramID,
		"telemetryProgramID", *telemetryProgramID,
		"keypairPath", *keypairPath,
		"devicePubkey", localDevicePK,
		"probeInterval", *probeInterval,
		"submissionInterval", *submissionInterval,
		"twampListenPort", *twampListenPort,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Set up TWAMP reflector
	reflector, err := twamplight.NewReflector(log, fmt.Sprintf("0.0.0.0:%d", *twampListenPort), *twampReflectorTimeout)
	if err != nil {
		log.Error("failed to create TWAMP reflector", "error", err)
		os.Exit(1)
	}

	// Build solana RPC client.
	var rpcClient *solanarpc.Client
	if *managementNamespace != "" {
		_, err := netns.WaitForNamespace(log, *managementNamespace, waitForNamespaceTimeout)
		if err != nil {
			log.Error("failed to wait for namespace", "error", err)
			os.Exit(1)
		}

		jsonrpcClient, err := netns.NewNamespacedJSONRPCClient(*ledgerRPCURL, *managementNamespace, nil)
		if err != nil {
			log.Error("failed to create namespace-safe solana RPC client", "error", err)
			os.Exit(1)
		}
		rpcClient = solanarpc.NewWithCustomRPCClient(jsonrpcClient)
	} else {
		rpcClient = solanarpc.New(*ledgerRPCURL)
	}

	// Set up real peer discovery.
	serviceabilityProgramID, err := solana.PublicKeyFromBase58(*serviceabilityProgramID)
	if err != nil {
		log.Error("failed to parse program ID", "error", err)
		os.Exit(1)
	}
	localNet := netutil.NewLocalNet(log)
	peerDiscovery, err := telemetry.NewLedgerPeerDiscovery(
		&telemetry.LedgerPeerDiscoveryConfig{
			Logger:          log,
			LocalDevicePK:   localDevicePK,
			ProgramClient:   serviceability.New(rpcClient, serviceabilityProgramID),
			LocalNet:        localNet,
			TWAMPPort:       uint16(*twampListenPort),
			RefreshInterval: *peersRefreshInterval,
		},
	)
	if err != nil {
		log.Error("failed to create ledger peer discovery", "error", err)
		os.Exit(1)
	}

	// Initialize telemetry program client.
	telemetryProgramID, err := solana.PublicKeyFromBase58(*telemetryProgramID)
	if err != nil {
		log.Error("failed to parse program ID", "error", err)
		os.Exit(1)
	}

	// Initialize collector.
	collector, err := telemetry.New(log, telemetry.Config{
		LocalDevicePK:          localDevicePK,
		MetricsPublisherPK:     keypair.PublicKey(),
		ProbeInterval:          *probeInterval,
		SubmissionInterval:     *submissionInterval,
		TWAMPSenderTimeout:     *twampSenderTimeout,
		TWAMPReflector:         reflector,
		PeerDiscovery:          peerDiscovery,
		TelemetryProgramClient: sdktelemetry.New(log, rpcClient, &keypair, telemetryProgramID),
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
