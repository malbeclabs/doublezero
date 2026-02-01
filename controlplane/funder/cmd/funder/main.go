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
	"github.com/malbeclabs/doublezero/controlplane/funder/internal/funder"
	"github.com/malbeclabs/doublezero/controlplane/funder/internal/metrics"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultInterval      = 1 * time.Minute
	defaultMinBalanceSOL = 3
	defaultTopUpSOL      = 5
)

var (
	env                     = flag.String("env", "", "the environment to run the funder in (devnet, testnet, mainnet-beta)")
	ledgerRPCURL            = flag.String("ledger-rpc-url", "", "the url of the ledger rpc")
	serviceabilityProgramID = flag.String("serviceability-program-id", "", "the id of the serviceability program")
	keypairPath             = flag.String("keypair", "", "the path to the metrics publisher keypair")
	interval                = flag.Duration("interval", defaultInterval, "the interval to check balances")
	minBalanceSOL           = flag.Float64("min-balance-sol", defaultMinBalanceSOL, "the minimum balance of the funder in SOL")
	topUpSOL                = flag.Float64("top-up-sol", defaultTopUpSOL, "the amount of SOL to top up the funder with")
	verbose                 = flag.Bool("verbose", false, "enable verbose logging")
	showVersion             = flag.Bool("version", false, "Print the version of the doublezero-agent and exit")
	metricsEnable           = flag.Bool("metrics-enable", false, "Enable prometheus metrics")
	metricsAddr             = flag.String("metrics-addr", ":8080", "Address to listen on for prometheus metrics")
	recipientsPath          = flag.String("recipients", "", "the path to the recipients JSON file")

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
	var internetLatencyCollectorPK solana.PublicKey
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
	} else {
		networkConfig, err := config.NetworkConfigForEnv(*env)
		if err != nil {
			log.Error("Failed to get network config", "error", err)
			flag.Usage()
			os.Exit(1)
		}
		*ledgerRPCURL = networkConfig.LedgerPublicRPCURL
		*serviceabilityProgramID = networkConfig.ServiceabilityProgramID.String()
		internetLatencyCollectorPK = networkConfig.InternetLatencyCollectorPK
	}
	if *keypairPath == "" {
		log.Error("Missing required flag", "flag", "keypair")
		flag.Usage()
		os.Exit(1)
	}

	// Set up prometheus metrics server if enabled.
	if *metricsEnable {
		metrics.BuildInfo.WithLabelValues(version, commit, date).Set(1)
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
	}

	// Check that keypair path exists.
	if _, err := os.Stat(*keypairPath); os.IsNotExist(err) {
		log.Error("Funder keypair does not exist", "path", *keypairPath)
		os.Exit(1)
	}

	// Check that the funder keypair is valid.
	keypair, err := solana.PrivateKeyFromSolanaKeygenFile(*keypairPath)
	if err != nil {
		log.Error("Failed to load funder keypair", "error", err)
		os.Exit(1)
	}

	// Check that the recipients path exists.
	var recipients []funder.Recipient
	if *recipientsPath != "" {
		if _, err := os.Stat(*recipientsPath); os.IsNotExist(err) {
			log.Error("Recipients file does not exist", "path", *recipientsPath)
			os.Exit(1)
		}
		recipientsMap, err := funder.LoadRecipientsFromJSONFile(*recipientsPath)
		if err != nil {
			log.Error("Failed to load recipients", "error", err)
			os.Exit(1)
		}
		for name, pubKey := range recipientsMap {
			recipients = append(recipients, funder.NewRecipient(name, pubKey))
		}
		log.Info("Loaded recipients", "count", len(recipients))
	}

	log.Info("Starting funder",
		"version", version,
		"ledgerRPCURL", *ledgerRPCURL,
		"serviceabilityProgramID", *serviceabilityProgramID,
		"keypairPath", *keypairPath,
		"interval", *interval,
		"internetLatencyCollectorPK", internetLatencyCollectorPK,
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Build solana RPC client.
	rpcClient := solanarpc.New(*ledgerRPCURL)

	// Set up serviceability program client.
	serviceabilityProgramID, err := solana.PublicKeyFromBase58(*serviceabilityProgramID)
	if err != nil {
		log.Error("failed to parse program ID", "error", err)
		os.Exit(1)
	}
	serviceabilityClient := serviceability.New(rpcClient, serviceabilityProgramID)

	// Initialize funder.
	collector, err := funder.New(funder.Config{
		Logger: log,
		GetRecipientsFunc: func(ctx context.Context) ([]funder.Recipient, error) {
			return funder.GetRecipients(ctx, serviceabilityClient, recipients, internetLatencyCollectorPK)
		},
		Solana:        rpcClient,
		Signer:        keypair,
		MinBalanceSOL: *minBalanceSOL,
		TopUpSOL:      *topUpSOL,
		Interval:      *interval,
	})
	if err != nil {
		log.Error("failed to create funder", "error", err)
		os.Exit(1)
	}

	// Run the funder.
	if err := collector.Run(ctx); err != nil {
		log.Error("funder exited with error", "error", err)
		os.Exit(1)
	}
}
