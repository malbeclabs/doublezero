package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/malbeclabs/doublezero/config"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/metrics"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netns"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netutil"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	sdktelemetry "github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

var (
	env                     = flag.String("env", "", "The network environment to use (devnet, testnet).")
	ledgerRPCURL            = flag.String("ledger-rpc-url", defaultLedgerRPCURL, "The url of the ledger rpc. If env is provided, this flag is ignored.")
	serviceabilityProgramID = flag.String("serviceability-program-id", defaultProgramId, "The id of the serviceability program. If env is provided, this flag is ignored.")
	telemetryProgramID      = flag.String("telemetry-program-id", defaultProgramId, "The id of the telemetry program. If env is provided, this flag is ignored.")
	keypairPath             = flag.String("keypair", "", "The path to the metrics publisher keypair.")
	localDevicePK           = flag.String("local-device-pubkey", defaultLocalDevicePubkey, "The pubkey of the local device.")
	twampListenPort         = flag.Uint("twamp-listen-port", uint(defaultTWAMPListenPort), "The port to listen for twamp probes.")
	probeInterval           = flag.Duration("probe-interval", defaultProbeInterval, "The interval to probe peers.")
	submissionInterval      = flag.Duration("submission-interval", defaultSubmissionInterval, "The interval to submit samples.")
	twampSenderTimeout      = flag.Duration("twamp-sender-timeout", defaultTWAMPSenderTimeout, "The timeout for sending twamp probes.")
	twampReflectorTimeout   = flag.Duration("twamp-reflector-timeout", defaultTWAMPReflectorTimeout, "The timeout for the twamp reflector.")
	peersRefreshInterval    = flag.Duration("peers-refresh-interval", defaultPeersRefreshInterval, "The interval to refresh the peer discovery.")
	managementNamespace     = flag.String("management-namespace", "", "The name of the management namespace to use for ledger communication. If not provided, the default namespace will be used. (default: '')")
	verbose                 = flag.Bool("verbose", false, "Enable verbose logging.")
	showVersion             = flag.Bool("version", false, "Print the version of the doublezero-agent and exit.")
	metricsEnable           = flag.Bool("metrics-enable", false, "Enable prometheus metrics.")
	metricsAddr             = flag.String("metrics-addr", ":8080", "Address to listen on for prometheus metrics.")

	// Set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	flag.Parse()

	if *probeInterval >= *submissionInterval {
		fmt.Println("probe-interval must be less than submission-interval")
		os.Exit(1)
	}

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
	if *env == "" {
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
	} else {
		networkConfig, err := config.NetworkConfigForEnv(*env)
		if err != nil {
			log.Error("Failed to get network config", "error", err)
			flag.Usage()
			os.Exit(1)
		}
		*ledgerRPCURL = networkConfig.LedgerRPCURL
		*serviceabilityProgramID = networkConfig.ServiceabilityProgramID.String()
		*telemetryProgramID = networkConfig.TelemetryProgramID.String()
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

	// If using a management namespace, wait for it to be ready.
	if *managementNamespace != "" {
		_, err := netns.WaitForNamespace(log, *managementNamespace, waitForNamespaceTimeout)
		if err != nil {
			log.Error("failed to wait for namespace", "error", err)
			os.Exit(1)
		}
	}

	// Set up prometheus metrics server if enabled.
	if *metricsEnable {
		metrics.BuildInfo.WithLabelValues(version, commit, date).Set(1)
		go func() {
			var listener net.Listener
			if *managementNamespace != "" {
				// If the management namespace is provided, we need to run the metrics server in that namespace.
				listener, err = netns.RunInNamespace(*managementNamespace, func() (net.Listener, error) {
					return net.Listen("tcp", *metricsAddr)
				})
				if err != nil {
					log.Error("Failed to start prometheus metrics server listener in namespace", "error", err, "namespace", *managementNamespace)
					return
				}
				log.Info("Prometheus metrics server listening", "namespace", *managementNamespace, "address", listener.Addr())
			} else {
				listener, err = net.Listen("tcp", *metricsAddr)
				if err != nil {
					log.Error("Failed to start prometheus metrics server listener", "error", err)
					return
				}
				log.Info("Prometheus metrics server listening", "address", listener.Addr())
			}
			http.Handle("/metrics", promhttp.Handler())
			if err := http.Serve(listener, nil); err != nil {
				log.Error("Failed to start prometheus metrics server", "error", err)
			}
		}()
	}

	// Set up TWAMP reflector
	reflector, err := twamplight.NewReflector(log, fmt.Sprintf("0.0.0.0:%d", *twampListenPort), *twampReflectorTimeout)
	if err != nil {
		log.Error("failed to create TWAMP reflector", "error", err)
		os.Exit(1)
	}

	// Build solana RPC client.
	var rpcClient *solanarpc.Client
	if *managementNamespace != "" {
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
		GetCurrentEpochFunc: func(ctx context.Context) (uint64, error) {
			epochInfo, err := rpcClient.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
			if err != nil {
				return 0, err
			}
			return epochInfo.Epoch, nil
		},
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
