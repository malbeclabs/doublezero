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
	"github.com/malbeclabs/doublezero/controlplane/device-health-oracle/internal/worker"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultInterval = 1 * time.Minute

	// Default burn-in slot counts for devices/links.
	// Provisioning: ~20 hours (200,000 slots * 400ms per slot)
	// Drained: ~30 minutes (5,000 slots * 400ms per slot)
	defaultProvisioningSlotCount = 200_000
	defaultDrainedSlotCount      = 5_000
)

var (
	env                     = flag.String("env", "", "the environment to run the component in (devnet, testnet, mainnet)")
	interval                = flag.Duration("interval", defaultInterval, "default interval to execute watcher ticks")
	verbose                 = flag.Bool("verbose", false, "enable verbose logging")
	showVersion             = flag.Bool("version", false, "Print the version and exit")
	metricsAddr             = flag.String("metrics-addr", ":8080", "Address to listen on for prometheus metrics")
	ledgerRPCURL            = flag.String("ledger-rpc-url", "", "the url of the ledger rpc")
	serviceabilityProgramID = flag.String("serviceability-program-id", "", "the id of the serviceability program")
	telemetryProgramID      = flag.String("telemetry-program-id", "", "the id of the telemetry program")
	slackWebhookURL         = flag.String("slack-webhook-url", "", "The Slack webhook URL to send alerts")
	provisioningSlotCount   = flag.Uint64("provisioning-slot-count", defaultProvisioningSlotCount, "Burn-in slot count for new devices/links (~20 hours at 200000)")
	drainedSlotCount        = flag.Uint64("drained-slot-count", defaultDrainedSlotCount, "Burn-in slot count for reactivated devices/links (~30 min at 5000)")
	version                 = "dev"
	commit                  = "none"
	date                    = "unknown"
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
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))

	// Initialize program IDs and ledger RPC URL.
	var networkConfig *config.NetworkConfig
	if *env == "" {
		if *ledgerRPCURL == "" {
			log.Error("Missing required flag", "flag", "ledger-rpc-url|env")
			flag.Usage()
			os.Exit(1)
		}
		if *serviceabilityProgramID == "" {
			log.Error("Missing required flag", "flag", "serviceability-program-id|env")
			flag.Usage()
			os.Exit(1)
		}
		if *telemetryProgramID == "" {
			log.Error("Missing required flag", "flag", "telemetry-program-id|env")
			flag.Usage()
			os.Exit(1)
		}
		serviceabilityProgramID, err := solana.PublicKeyFromBase58(*serviceabilityProgramID)
		if err != nil {
			log.Error("Failed to parse serviceability program id", "error", err)
			flag.Usage()
			os.Exit(1)
		}
		telemetryProgramID, err := solana.PublicKeyFromBase58(*telemetryProgramID)
		if err != nil {
			log.Error("Failed to parse telemetry program id", "error", err)
			flag.Usage()
			os.Exit(1)
		}
		networkConfig = &config.NetworkConfig{
			LedgerPublicRPCURL:      *ledgerRPCURL,
			ServiceabilityProgramID: serviceabilityProgramID,
			TelemetryProgramID:      telemetryProgramID,
		}
	} else {
		var err error
		networkConfig, err = config.NetworkConfigForEnv(*env)
		if err != nil {
			log.Error("Failed to get network config", "error", err)
			flag.Usage()
			os.Exit(1)
		}
	}

	// Initialize ledger clients.
	rpcClient := solanarpc.New(networkConfig.LedgerPublicRPCURL)
	serviceabilityClient := serviceability.New(rpcClient, networkConfig.ServiceabilityProgramID)
	telemetryClient := telemetry.New(log, rpcClient, nil, networkConfig.TelemetryProgramID)

	worker.MetricBuildInfo.WithLabelValues(version, commit, date).Set(1)
	go func() {
		listener, err := net.Listen("tcp", *metricsAddr)
		if err != nil {
			log.Error("Failed to start prometheus metrics server listener", "error", err)
			return
		}
		log.Info("Prometheus metrics server listening", "address", listener.Addr().String())
		http.Handle("/metrics", promhttp.Handler())
		if err := http.Serve(listener, nil); err != nil {
			log.Error("Failed to start prometheus metrics server", "error", err)
		}
	}()

	w, err := worker.New(&worker.Config{
		Logger:                log,
		LedgerRPCClient:       rpcClient,
		Serviceability:        serviceabilityClient,
		Telemetry:             telemetryClient,
		Interval:              *interval,
		SlackWebhookURL:       *slackWebhookURL,
		Env:                   *env,
		ProvisioningSlotCount: *provisioningSlotCount,
		DrainedSlotCount:      *drainedSlotCount,
	})
	if err != nil {
		log.Error("Failed to create worker", "error", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	err = w.Run(ctx)
	if err != nil {
		log.Error("Failed to run worker", "error", err)
		os.Exit(1)
	}
}
