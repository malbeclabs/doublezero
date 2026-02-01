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
	influxdb2 "github.com/influxdata/influxdb-client-go/v2"
	"github.com/malbeclabs/doublezero/config"
	twozoracle "github.com/malbeclabs/doublezero/controlplane/monitor/internal/2z-oracle"
	"github.com/malbeclabs/doublezero/controlplane/monitor/internal/worker"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultInterval           = 1 * time.Minute
	defaultTwoZOracleInterval = 5 * time.Second
)

var (
	env                        = flag.String("env", "", "the environment to run the component in (devnet, testnet, mainnet)")
	ledgerRPCURL               = flag.String("ledger-rpc-url", "", "the url of the ledger rpc")
	serviceabilityProgramID    = flag.String("serviceability-program-id", "", "the id of the serviceability program")
	telemetryProgramID         = flag.String("telemetry-program-id", "", "the id of the telemetry program")
	internetLatencyCollectorPK = flag.String("internet-latency-collector-pk", "", "the public key of the internet latency collector")
	interval                   = flag.Duration("interval", defaultInterval, "default interval to execute watcher ticks")
	verbose                    = flag.Bool("verbose", false, "enable verbose logging")
	showVersion                = flag.Bool("version", false, "Print the version of the doublezero-agent and exit")
	metricsAddr                = flag.String("metrics-addr", ":8080", "Address to listen on for prometheus metrics")
	slackWebhookURL            = flag.String("slack-webhook-url", "", "The Slack webhook URL to send alerts")
	twoZOracleInterval         = flag.Duration("twoz-oracle-interval", defaultTwoZOracleInterval, "interval to execute twoz oracle watcher ticks")

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

	// Initialize logger.
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
		if *internetLatencyCollectorPK == "" {
			log.Error("Missing required flag", "flag", "internet-latency-collector-pk")
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
		internetLatencyCollectorPK, err := solana.PublicKeyFromBase58(*internetLatencyCollectorPK)
		if err != nil {
			log.Error("Failed to parse internet latency collector pk", "error", err)
			flag.Usage()
			os.Exit(1)
		}
		networkConfig = &config.NetworkConfig{
			LedgerPublicRPCURL:         *ledgerRPCURL,
			ServiceabilityProgramID:    serviceabilityProgramID,
			TelemetryProgramID:         telemetryProgramID,
			InternetLatencyCollectorPK: internetLatencyCollectorPK,
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

	var solanaRPCClient *solanarpc.Client
	if networkConfig.SolanaRPCURL != "" {
		solanaRPCClient = solanarpc.New(networkConfig.SolanaRPCURL)
	}

	// Initialize prometheus metrics server.
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

	var twoZOracleClient twozoracle.TwoZOracleClient
	if networkConfig.TwoZOracleURL != "" {
		// 2ZOracle is not configured in some environments.
		twoZOracleClient = twozoracle.NewTwoZOracleClient(http.DefaultClient, networkConfig.TwoZOracleURL)
	}

	// Initialize InfluxDB writer
	var influxClient influxdb2.Client
	var influxWriter worker.InfluxWriter
	var influxUrl, influxToken, influxBucket string

	// Check whether writing to InfluxDB should be enabled.
	enableInflux := func() bool {
		influxUrl = os.Getenv("INFLUX_URL")
		influxToken = os.Getenv("INFLUX_TOKEN")
		if influxUrl == "" {
			log.Info("INFLUX_URL not set, not enabling writes to InfluxDB")
			return false
		}
		if influxToken == "" {
			log.Info("INFLUX_TOKEN not set, not enabling writes to InfluxDB")
			return false
		}
		influxBucket = "doublezero-" + *env
		return true
	}

	if enableInflux() {
		influxClient = influxdb2.NewClient(influxUrl, influxToken)
		influxWriter = influxClient.WriteAPI("rd", influxBucket)
		defer influxClient.Close()
	}

	// Initialize worker.
	worker, err := worker.New(&worker.Config{
		Logger:                     log,
		LedgerRPCClient:            rpcClient,
		SolanaRPCClient:            solanaRPCClient,
		Serviceability:             serviceabilityClient,
		Telemetry:                  telemetryClient,
		InternetLatencyCollectorPK: networkConfig.InternetLatencyCollectorPK,
		Interval:                   *interval,
		SlackWebhookURL:            *slackWebhookURL,
		TwoZOracleClient:           twoZOracleClient,
		TwoZOracleInterval:         *twoZOracleInterval,
		InfluxWriter:               influxWriter,
		Env:                        *env,
	})
	if err != nil {
		log.Error("Failed to create worker", "error", err)
		os.Exit(1)
	}

	// Start the worker.
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	err = worker.Run(ctx)
	if err != nil {
		log.Error("Failed to run worker", "error", err)
		os.Exit(1)
	}
}
