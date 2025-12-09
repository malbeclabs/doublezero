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

	redpandaBroker        = flag.String("redpanda-broker", "localhost:9000", "redpanda broker")
	redpandaUser          = flag.String("redpanda-user", "enricher", "redpanda username")
	redpandaTopicRaw      = flag.String("redpanda-topic-raw", "flows_raw", "redpanda topic to read raw flows")
	redpandaTopicEnriched = flag.String("redpnada-topic-enriched", "flows_enriched", "redpanda topic to produce enriched flows")
	redpandaConsumerGroup = flag.String("redpanda-consumer-group", "enricher", "redpanda consumer group")

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
		enricher.WithClickhouseAddr(*clickhouseAddr),
		enricher.WithClickhouseCreds(*clickhouseUser, os.Getenv("CLICKHOUSE_PASS")),
		enricher.WithRedpandaBroker(os.Getenv("KAFKA_BROKERS")),
		enricher.WithRedpandaCreds(*redpandaUser, os.Getenv("REDPANDA_PASS")),
		enricher.WithRedpandaConsumerTopic(os.Getenv("KAFKA_TOPIC")),
		enricher.WithRedpandaConsumerGroup(os.Getenv("KAFKA_CONSUMER_GROUP")),
		enricher.WithRedpandaMetrics(true),
	}
	enricher := enricher.NewEnricher(opts...)
	log.Println("starting enricher...")
	if err := enricher.Run(ctx); err != nil {
		log.Fatalf("error while running enricher: %v", err)
	}
	log.Println("enricher stopped")
}
