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

	"github.com/malbeclabs/doublezero/telemetry/enricher/internal/enricher"
)

var (
	clickhouseAddr = flag.String("clickhouse-addr", "localhost:9440", "clickhouse address")
	clickhouseUser = flag.String("clickhouse-user", "default", "clickhouse user")

	kafkaUser = flag.String("kafka-user", "enricher", "kafka username")
	useTls    = flag.Bool("use-tls", false, "use TLS for kafka and clickhouse connections")

	version = flag.Bool("version", false, "version info")
	Build   string
)

func main() {
	flag.Parse()

	if *version {
		fmt.Printf("build: %s\n", Build)
		os.Exit(0)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	chWriter, err := enricher.NewClickhouseWriter(
		enricher.WithClickhouseAddr(os.Getenv("CLICKHOUSE_ADDR")),
		enricher.WithClickhouseDB("default"),
		enricher.WithClickhouseUser(*clickhouseUser),
		enricher.WithClickhousePassword(os.Getenv("CLICKHOUSE_PASS")),
		enricher.WithTLS(*useTls),
		enricher.WithClickhouseLogger(logger),
	)
	if err != nil {
		logger.Error("error creating clickhouse writer", "error", err)
		os.Exit(1)
	}

	brokers := strings.Split(os.Getenv("KAFKA_BROKERS"), ",")
	flowConsumer, err := enricher.NewKafkaFlowConsumer(
		enricher.WithKafkaBroker(brokers),
		enricher.WithKafkaUser(*kafkaUser),
		enricher.WithKafkaPassword(os.Getenv("KAFKA_PASS")),
		enricher.WithKafkaConsumerTopic(os.Getenv("KAFKA_TOPIC")),
		enricher.WithKafkaConsumerGroup(os.Getenv("KAFKA_CONSUMER_GROUP")),
		enricher.WithKafkaTLS(*useTls),
		enricher.WithKafkaLogger(logger),
	)
	if err != nil {
		logger.Error("error creating kafka flow consumer", "error", err)
		os.Exit(1)
	}

	enricher := enricher.NewEnricher(
		enricher.WithClickhouseWriter(chWriter),
		enricher.WithFlowConsumer(flowConsumer),
		enricher.WithLogger(logger),
	)
	logger.Info("starting enricher...")
	if err := enricher.Run(ctx); err != nil {
		logger.Error("error while running enricher", "error", err)
		os.Exit(1)
	}
	logger.Info("enricher stopped")
}
