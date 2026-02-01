package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/malbeclabs/doublezero/config"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	enricher "github.com/malbeclabs/doublezero/telemetry/flow-enricher/internal/flow-enricher"
	"github.com/malbeclabs/doublezero/tools/solana/pkg/rpc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

var (
	// set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"

	showVersion  = flag.Bool("version", false, "print version information and exit")
	metricsAddr  = flag.String("metrics-addr", "127.0.0.1:2112", "The address the metric endpoint binds to.")
	pcapInput    = flag.String("pcap-input", "", "Path to pcap file to read sFlow packets from (instead of Kafka)")
	stdoutOutput = flag.Bool("stdout-output", false, "Output enriched flows as JSON to stdout (instead of ClickHouse)")
)

func main() {
	flag.Parse()

	if *showVersion {
		fmt.Printf("version: %s\ncommit: %s\ndate: %s\n", version, commit, date)
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	reg := prometheus.WrapRegistererWithPrefix("enricher_", prometheus.DefaultRegisterer)

	// setup output writer (ClickHouse or stdout)
	var chWriter enricher.Clicker
	if *stdoutOutput {
		chWriter = enricher.NewStdoutWriter()
	} else {
		chOpts := []enricher.ClickhouseOption{}
		if os.Getenv("CLICKHOUSE_TLS_DISABLED") == "true" {
			chOpts = append(chOpts, enricher.WithTLSDisabled(true))
		}

		chOpts = append(chOpts, enricher.WithClickhouseAddr(os.Getenv("CLICKHOUSE_ADDR")),
			enricher.WithClickhouseDB(getEnvOrDefault("CLICKHOUSE_DB", "default")),
			enricher.WithClickhouseTable(getEnvOrDefault("CLICKHOUSE_TABLE", "flows")),
			enricher.WithClickhouseUser(os.Getenv("CLICKHOUSE_USER")),
			enricher.WithClickhousePassword(os.Getenv("CLICKHOUSE_PASS")),
			enricher.WithClickhouseLogger(logger),
			enricher.WithClickhouseMetrics(enricher.NewClickhouseMetrics(reg)),
		)
		var err error
		chWriter, err = enricher.NewClickhouseWriter(chOpts...)
		if err != nil {
			logger.Error("error creating clickhouse writer", "error", err)
			os.Exit(1)
		}
	}

	// setup input consumer (Kafka or pcap)
	var flowConsumer enricher.FlowConsumer
	if *pcapInput != "" {
		flowConsumer = enricher.NewPcapFlowConsumer(*pcapInput)
	} else {
		kOpts := []enricher.KafkaOption{}
		if os.Getenv("KAFKA_TLS_DISABLED") == "true" {
			kOpts = append(kOpts, enricher.WithKafkaTLSDisabled(true))
		}

		if os.Getenv("KAFKA_AUTH_TYPE") == "SCRAM" && os.Getenv("KAFKA_USER") != "" && os.Getenv("KAFKA_PASS") != "" {
			kOpts = append(kOpts, enricher.WithKafkaUser(os.Getenv("KAFKA_USER")),
				enricher.WithKafkaPassword(os.Getenv("KAFKA_PASS")),
				enricher.WithKafkaAuthType(enricher.KafkaAuthTypeSCRAM),
			)
		} else {
			kOpts = append(kOpts, enricher.WithKafkaAuthType(enricher.KafkaAuthTypeAWSMSK))
		}

		brokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
		kOpts = append(kOpts, enricher.WithKafkaBroker(brokers),
			enricher.WithKafkaConsumerTopic(os.Getenv("KAFKA_TOPIC")),
			enricher.WithKafkaConsumerGroup(os.Getenv("KAFKA_CONSUMER_GROUP")),
			enricher.WithKafkaLogger(logger),
			enricher.WithFlowConsumerMetrics(enricher.NewFlowConsumerMetrics(reg)),
		)
		var err error
		flowConsumer, err = enricher.NewKafkaFlowConsumer(kOpts...)
		if err != nil {
			logger.Error("error creating kafka flow consumer", "error", err)
			os.Exit(1)
		}
	}

	networkConfig, err := config.NetworkConfigForEnv(os.Getenv("DZ_ENV"))
	if err != nil {
		logger.Error("error getting network config", "error", err)
		os.Exit(1)
	}

	rpcClient := rpc.NewWithRetries(networkConfig.LedgerPublicRPCURL, nil)
	serviceabilityClient := serviceability.New(rpcClient, networkConfig.ServiceabilityProgramID)

	e := enricher.NewEnricher(
		enricher.WithClickhouseWriter(chWriter),
		enricher.WithFlowConsumer(flowConsumer),
		enricher.WithLogger(logger),
		enricher.WithEnricherMetrics(enricher.NewEnricherMetrics(reg)),
		enricher.WithServiceabilityFetcher(serviceabilityClient),
		enricher.WithServiceabilityFetchInterval(10*time.Second),
	)

	e.AddAnnotator(enricher.NewServiceabilityAnnotator(e.ServiceabilityData))

	// start prometheus
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe(*metricsAddr, mux) //nolint
	}()

	logger.Info("starting enricher...")
	if err := e.Run(ctx); err != nil {
		logger.Error("error while running enricher", "error", err)
		os.Exit(1)
	}
	logger.Info("enricher stopped")
}
