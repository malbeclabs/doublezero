package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	enricher "github.com/malbeclabs/doublezero/telemetry/flow-enricher/internal/flow-enricher"
)

var (
	// set by LDFLAGS
	version = "dev"
	commit  = "none"
	date    = "unknown"

	showVersion = flag.Bool("version", false, "print version information and exit")
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

	chOpts := []enricher.ClickhouseOption{}
	if os.Getenv("CLICKHOUSE_TLS_DISABLED") == "true" {
		chOpts = append(chOpts, enricher.WithTLSDisabled(true))
	}

	chOpts = append(chOpts, enricher.WithClickhouseAddr(os.Getenv("CLICKHOUSE_ADDR")),
		enricher.WithClickhouseDB("default"),
		enricher.WithClickhouseUser(os.Getenv("CLICKHOUSE_USER")),
		enricher.WithClickhousePassword(os.Getenv("CLICKHOUSE_PASS")),
		enricher.WithClickhouseLogger(logger),
	)
	chWriter, err := enricher.NewClickhouseWriter(chOpts...)
	if err != nil {
		logger.Error("error creating clickhouse writer", "error", err)
		os.Exit(1)
	}

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
	)
	flowConsumer, err := enricher.NewKafkaFlowConsumer(kOpts...)
	if err != nil {
		logger.Error("error creating kafka flow consumer", "error", err)
		os.Exit(1)
	}

	enricherOpts := []enricher.EnricherOption{
		enricher.WithClickhouseWriter(chWriter),
		enricher.WithFlowConsumer(flowConsumer),
		enricher.WithLogger(logger),
	}
	enricher := enricher.NewEnricher(enricherOpts...)
	logger.Info("starting enricher...")
	if err := enricher.Run(ctx); err != nil {
		logger.Error("error while running enricher", "error", err)
		os.Exit(1)
	}
	logger.Info("enricher stopped")
}
