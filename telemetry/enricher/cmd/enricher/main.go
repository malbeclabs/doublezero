package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/malbeclabs/doublezero/telemetry/enricher/internal/enricher"
)

var (
	clickhouseAddr = flag.String("clickhouse-addr", "localhost:9440", "clickhouse address")
	clickhouseUser = flag.String("clickhouse-user", "default", "clickhouse user")

	kafkaBroker        = flag.String("kafka-broker", "localhost:9000", "kafka broker")
	kafkaUser          = flag.String("kafka-user", "enricher", "kafka username")
	kafkaTopicRaw      = flag.String("kafka-topic-raw", "flows_raw", "kafka topic to read raw flows")
	kafkaTopicEnriched = flag.String("kafka-topic-enriched", "flows_enriched", "kafka topic to produce enriched flows")
	kafkaConsumerGroup = flag.String("kafka-consumer-group", "enricher", "kafka consumer group")
	useTls             = flag.Bool("use-tls", false, "use TLS for kafka and clickhouse connections")

	version = flag.Bool("version", false, "version info")
	Build   string
)

func main() {
	flag.Parse()

	if *version {
		fmt.Printf("build: %s\n", Build)
		os.Exit(0)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// if os.Getenv("CLICKHOUSE_PASS") == "" {
	// 	log.Fatalf("CLICKHOUSE_PASS env var not set")
	// }

	// if os.Getenv("REDPANDA_PASS") == "" {
	// 	log.Fatalf("REDPANDA_PASS env var not set")
	// }

	opts := []enricher.EnricherOption{
		enricher.WithClickhouseAddr(os.Getenv("CLICKHOUSE_ADDR")),
		enricher.WithClickhouseCreds(*clickhouseUser, os.Getenv("CLICKHOUSE_PASS")),
		enricher.WithKafkaBroker(os.Getenv("KAFKA_BROKERS")),
		enricher.WithKafkaCreds(*kafkaUser, os.Getenv("KAFKA_PASS")),
		enricher.WithKafkaConsumerTopic(os.Getenv("KAFKA_TOPIC")),
		enricher.WithKafkaConsumerGroup(os.Getenv("KAFKA_CONSUMER_GROUP")),
		enricher.WithKafkaMetrics(true),
	}
	enricher := enricher.NewEnricher(opts...)
	log.Println("starting enricher...")
	if err := enricher.Run(ctx); err != nil {
		log.Fatalf("error while running enricher: %v", err)
	}
	log.Println("enricher stopped")
}
