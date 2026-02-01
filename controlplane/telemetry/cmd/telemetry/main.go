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
	"github.com/malbeclabs/doublezero/controlplane/agent/pkg/arista"
	aristapb "github.com/malbeclabs/doublezero/controlplane/proto/arista/gen/pb-go/arista/EosSdkRpc"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/geoprobe"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/gnmitunnel"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/metrics"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netns"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/netutil"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/state"
	"github.com/malbeclabs/doublezero/controlplane/telemetry/internal/telemetry"
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	sdktelemetry "github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	stateingest "github.com/malbeclabs/doublezero/telemetry/state-ingest/pkg/client"
	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultProbeInterval              = 10 * time.Second
	defaultSubmissionInterval         = 60 * time.Second
	defaultTWAMPListenPort            = telemetryconfig.TWAMPListenPort
	defaultTWAMPReflectorTimeout      = 1 * time.Second
	defaultPeersRefreshInterval       = 10 * time.Second
	defaultTWAMPSenderTimeout         = 1 * time.Second
	defaultSenderTTL                  = 5 * time.Minute
	defaultMaxConsecutiveSenderLosses = 30
	defaultLedgerRPCURL               = ""
	defaultProgramId                  = ""
	defaultLocalDevicePubkey          = ""
	defaultSubmitterMaxConcurrency    = 10
	defaultStateCollectInterval       = 60 * time.Second

	waitForNamespaceTimeout             = 30 * time.Second
	defaultStateIngestHTTPClientTimeout = 10 * time.Second
)

var (
	env                        = flag.String("env", "", "The network environment to use (devnet, testnet, mainnet-beta).")
	ledgerRPCURL               = flag.String("ledger-rpc-url", defaultLedgerRPCURL, "The url of the ledger rpc. If env is provided, this flag is ignored.")
	serviceabilityProgramID    = flag.String("serviceability-program-id", defaultProgramId, "The id of the serviceability program. If env is provided, this flag is ignored.")
	telemetryProgramID         = flag.String("telemetry-program-id", defaultProgramId, "The id of the telemetry program. If env is provided, this flag is ignored.")
	keypairPath                = flag.String("keypair", "", "The path to the metrics publisher keypair.")
	localDevicePK              = flag.String("local-device-pubkey", defaultLocalDevicePubkey, "The pubkey of the local device.")
	twampListenPort            = flag.Uint("twamp-listen-port", uint(defaultTWAMPListenPort), "The port to listen for twamp probes.")
	probeInterval              = flag.Duration("probe-interval", defaultProbeInterval, "The interval to probe peers.")
	submissionInterval         = flag.Duration("submission-interval", defaultSubmissionInterval, "The interval to submit samples.")
	twampSenderTimeout         = flag.Duration("twamp-sender-timeout", defaultTWAMPSenderTimeout, "The timeout for sending twamp probes.")
	twampReflectorTimeout      = flag.Duration("twamp-reflector-timeout", defaultTWAMPReflectorTimeout, "The timeout for the twamp reflector.")
	peersRefreshInterval       = flag.Duration("peers-refresh-interval", defaultPeersRefreshInterval, "The interval to refresh the peer discovery.")
	senderTTL                  = flag.Duration("sender-ttl", defaultSenderTTL, "The time to live for a sender instance until it's recreated.")
	submitterMaxConcurrency    = flag.Int("submitter-max-concurrency", defaultSubmitterMaxConcurrency, "The maximum number of concurrent submissions.")
	maxConsecutiveSenderLosses = flag.Int("max-consecutive-sender-losses", defaultMaxConsecutiveSenderLosses, "The number of consecutive probe losses before a sender is evicted and recreated.")
	managementNamespace        = flag.String("management-namespace", "", "The name of the management namespace to use for communication over the internet. If not provided, the default namespace will be used. (default: '')")
	bgpNamespace               = flag.String("bgp-namespace", "ns-vrf1", "The name of the ns-vrf1 namespace to use for BGP state collection. (default: 'ns-vrf1')")
	stateCollectEnable         = flag.Bool("state-collect-enable", false, "Enable state collection (unstable)")
	stateCollectInterval       = flag.Duration("state-collect-interval", defaultStateCollectInterval, "The interval to collect and submit state snapshots.")
	stateIngestURL             = flag.String("state-ingest-url", "", "The URL of the state ingest server.")
	eapiAddr                   = flag.String("eapi-addr", "127.0.0.1:9543", "IP Address and port of the Arist EOS API. Should always be the local switch at 127.0.0.1:9543.")
	verbose                    = flag.Bool("verbose", false, "Enable verbose logging.")
	showVersion                = flag.Bool("version", false, "Print the version of the doublezero-agent and exit.")
	metricsEnable              = flag.Bool("metrics-enable", false, "Enable prometheus metrics.")
	metricsAddr                = flag.String("metrics-addr", ":8080", "Address to listen on for prometheus metrics.")

	// gNMI tunnel flags
	gnmiTunnelEnable     = flag.Bool("gnmi-tunnel-enable", false, "Enable gNMI tunnel client for remote access.")
	gnmiTunnelServerAddr = flag.String("gnmi-tunnel-server-addr", "", "Address of the gNMI tunnel server (defaults to env config, e.g., gnmic-devnet.doublezero.xyz:443).")

	// geoprobe flags
	additionalChildProbes = flag.String("additional-child-probes", "", "Comma-separated list of child geoProbe addresses (host:port) to measure RTT and send location offsets.")

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
		if *stateCollectEnable && *stateIngestURL == "" {
			log.Error("Missing required flag", "flag", "state-ingest-url")
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
		*ledgerRPCURL = networkConfig.LedgerPublicRPCURL
		*serviceabilityProgramID = networkConfig.ServiceabilityProgramID.String()
		*telemetryProgramID = networkConfig.TelemetryProgramID.String()
		*stateIngestURL = networkConfig.TelemetryStateIngestURL
		if *gnmiTunnelServerAddr == "" {
			*gnmiTunnelServerAddr = networkConfig.TelemetryGNMITunnelServerAddr
		}
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

	// Parse additional child probes if provided.
	childProbes, err := geoprobe.ParseProbeAddresses(*additionalChildProbes)
	if err != nil {
		log.Error("Failed to parse additional-child-probes", "error", err)
		os.Exit(1)
	}
	if len(childProbes) > 0 {
		log.Info("Configured child probes for geolocation measurement", "count", len(childProbes), "probes", childProbes)
	}

	log.Info("Starting telemetry collector",
		"version", version,
		"ledgerRPCURL", *ledgerRPCURL,
		"serviceabilityProgramID", *serviceabilityProgramID,
		"telemetryProgramID", *telemetryProgramID,
		"devicePubkey", localDevicePK,
		"probeInterval", *probeInterval,
		"submissionInterval", *submissionInterval,
		"twampListenPort", *twampListenPort,
		"senderTTL", *senderTTL,
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
				log.Info("Prometheus metrics server listening", "namespace", *managementNamespace, "address", listener.Addr().String())
			} else {
				listener, err = net.Listen("tcp", *metricsAddr)
				if err != nil {
					log.Error("Failed to start prometheus metrics server listener", "error", err)
					return
				}
				log.Info("Prometheus metrics server listening", "address", listener.Addr().String())
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
		LocalDevicePK:               localDevicePK,
		MetricsPublisherPK:          keypair.PublicKey(),
		ProbeInterval:               *probeInterval,
		SubmissionInterval:          *submissionInterval,
		TWAMPSenderTimeout:          *twampSenderTimeout,
		TWAMPReflector:              reflector,
		PeerDiscovery:               peerDiscovery,
		TelemetryProgramClient:      sdktelemetry.New(log, rpcClient, &keypair, telemetryProgramID),
		ServiceabilityProgramClient: serviceability.New(rpcClient, serviceabilityProgramID),
		RPCClient:                   rpcClient,
		Keypair:                     keypair,
		GetCurrentEpochFunc: func(ctx context.Context) (uint64, error) {
			epochInfo, err := rpcClient.GetEpochInfo(ctx, solanarpc.CommitmentFinalized)
			if err != nil {
				return 0, err
			}
			return epochInfo.Epoch, nil
		},
		SenderTTL:                  *senderTTL,
		SubmitterMaxConcurrency:    *submitterMaxConcurrency,
		InitialChildGeoProbes:      childProbes,
		MaxConsecutiveSenderLosses: *maxConsecutiveSenderLosses,
	})
	if err != nil {
		log.Error("failed to create telemetry collector", "error", err)
		os.Exit(1)
	}

	errCh := make(chan error, 2)

	// Run the onchain device-link latency collector.
	go func() {
		if err := collector.Run(ctx); err != nil {
			errCh <- err
		}
	}()

	// Run state collector if enabled.
	var stateCollectorErrCh <-chan error
	if *stateCollectEnable {
		stateCollectorErrCh = startStateCollector(ctx, cancel, log, keypair, localDevicePK, *bgpNamespace)
	}

	// Run gNMI tunnel client if enabled.
	var gnmiTunnelClientErrCh <-chan error
	if *gnmiTunnelEnable {
		gnmiTunnelClientErrCh = startGNMITunnelClient(ctx, cancel, log, localDevicePK)
	}

	// Wait for the context to be done or an error to be returned.
	select {
	case <-ctx.Done():
		log.Info("telemetry collector shutting down")
	case err := <-errCh:
		log.Error("telemetry collector exited with error", "error", err)
		cancel()
		os.Exit(1)
	case err := <-stateCollectorErrCh:
		log.Error("state collector exited with error", "error", err)
		cancel()
		os.Exit(1)
	case err := <-gnmiTunnelClientErrCh:
		log.Error("gnmi tunnel client exited with error", "error", err)
		cancel()
		os.Exit(1)
	}
}

func startStateCollector(ctx context.Context, cancel context.CancelFunc, log *slog.Logger, keypair solana.PrivateKey, localDevicePK solana.PublicKey, bgpNamespace string) <-chan error {
	// Build state ingest HTTP client.
	var stateIngestHTTPClient *http.Client
	if *managementNamespace != "" {
		var err error
		stateIngestHTTPClient, err = netns.NewNamespacedHTTPClient(*managementNamespace, nil)
		if err != nil {
			log.Error("failed to create namespace-safe state ingest HTTP client", "error", err)
			os.Exit(1)
		}
	} else {
		stateIngestHTTPClient = &http.Client{
			Timeout:   defaultStateIngestHTTPClientTimeout,
			Transport: http.DefaultTransport,
		}
	}

	// Initialize state ingest client.
	signer := state.NewKeypairSigner(keypair)
	stateIngestClient, err := stateingest.NewClient(
		*stateIngestURL,
		localDevicePK,
		signer,
		stateingest.WithHTTPClient(stateIngestHTTPClient),
	)
	if err != nil {
		log.Error("failed to create state ingest client", "error", err)
		os.Exit(1)
	}

	// Build EAPI client.
	var clientConn *grpc.ClientConn
	if *managementNamespace != "" {
		clientConn, err = netns.NewNamespacedGRPCConn(ctx, *managementNamespace, *eapiAddr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		)
		if err != nil {
			log.Error("failed to create namespace-safe EAPI client", "error", err)
			os.Exit(1)
		}
	} else {
		clientConn, err = arista.NewClientConn(*eapiAddr)
		if err != nil {
			log.Error("failed to create EAPI client", "error", err)
			os.Exit(1)
		}
	}
	eapiMgrServiceClient := aristapb.NewEapiMgrServiceClient(clientConn)

	// Initialize the state collector.
	if err != nil {
		log.Error("failed to create state ingest client", "error", err)
		os.Exit(1)
	}
	stateCollector, err := state.NewCollector(&state.CollectorConfig{
		Logger:       log,
		StateIngest:  stateIngestClient,
		Interval:     *stateCollectInterval,
		DevicePK:     localDevicePK,
		EAPI:         eapiMgrServiceClient,
		BGPNamespace: bgpNamespace,
	})
	if err != nil {
		log.Error("failed to create state collector", "error", err)
		os.Exit(1)
	}

	return stateCollector.Start(ctx, cancel)
}

func startGNMITunnelClient(ctx context.Context, cancel context.CancelFunc, log *slog.Logger, localDevicePK solana.PublicKey) <-chan error {
	// Validate required config.
	if *gnmiTunnelServerAddr == "" {
		log.Error("gNMI tunnel server address not configured (set --env or --gnmi-tunnel-server-addr)")
		os.Exit(1)
	}

	// Build gNMI tunnel configuration.
	// - TargetID: uses device pubkey so the tunnel server can route to this device
	// - TargetType: GNMI_GNOI for gNMI/gNOI services
	// - LocalDialAddr: standard Arista gNMI socket path
	cfg := &gnmitunnel.Config{
		Logger:           log,
		TargetID:         localDevicePK.String(),
		TargetType:       gnmitunnel.TargetTypeGNMIGNOI,
		LocalDialAddr:    "/var/run/gnmiServer.sock",
		TunnelServerAddr: *gnmiTunnelServerAddr,
	}

	// If using a management namespace, configure namespace-aware dialers.
	if *managementNamespace != "" {
		cfg.LocalDialer = func(ctx context.Context, network, address string) (net.Conn, error) {
			return netns.RunInNamespace(*managementNamespace, func() (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, address)
			})
		}
		cfg.GRPCClientConnFactory = func(ctx context.Context, target string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
			return netns.NewNamespacedGRPCConn(ctx, *managementNamespace, target, opts...)
		}
	}

	gnmiTunnelClient, err := gnmitunnel.NewClient(cfg)
	if err != nil {
		log.Error("failed to create gNMI tunnel client", "error", err)
		os.Exit(1)
	}

	return gnmiTunnelClient.Start(ctx, cancel)
}
