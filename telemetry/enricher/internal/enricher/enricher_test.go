package enricher

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/clickhouse"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
)

var (
	clickhouseCtr *clickhouse.ClickHouseContainer
	redpandaCtr   *redpanda.Container

	chUser     = "enricher"
	chPassword = "clickhouse"
	chDbname   = "default"

	rpBroker        string
	rpUser          = "enricher"
	rpPassword      = "redpanda"
	rpTopicRaw      = "flows_raw"
	rpTopicEnriched = "flows_enriched"
	rpConsumerGroup = "enricher"
	rpClient        *kgo.Client

	err error
)

func setupClickhouseContainer(ctx context.Context) error {
	clickhouseCtr, err = clickhouse.Run(ctx,
		"clickhouse/clickhouse-server:23.3.8.21-alpine",
		clickhouse.WithUsername(chUser),
		clickhouse.WithPassword(chPassword),
		clickhouse.WithDatabase(chDbname),
		clickhouse.WithInitScripts(
			filepath.Join("fixtures", "create_table_device_ifindex.sql"),
			filepath.Join("fixtures", "insert_device_ifindex.sql"),
		),
	)
	return err
}

func setupRedpandaContainer(ctx context.Context) error {
	redpandaCtr, err = redpanda.Run(ctx,
		"docker.redpanda.com/redpandadata/redpanda:v23.3.3",
		redpanda.WithEnableSASL(),
		redpanda.WithAutoCreateTopics(),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithEnableWasmTransform(),
		redpanda.WithBootstrapConfig("data_transforms_per_core_memory_reservation", 33554432),
		redpanda.WithBootstrapConfig("data_transforms_per_function_memory_limit", 16777216),
		redpanda.WithNewServiceAccount(rpUser, rpPassword),
		redpanda.WithSuperusers(rpUser),
		redpanda.WithEnableSchemaRegistryHTTPBasicAuth(),
	)
	if err != nil {
		return fmt.Errorf("error creating redpanda container: %v", err)
	}

	rpBroker, err = redpandaCtr.KafkaSeedBroker(context.Background())
	if err != nil {
		return fmt.Errorf("unable to get redpanda seed broker: %v", err)
	}

	rpOpts := []kgo.Opt{}
	rpOpts = append(rpOpts,
		kgo.SeedBrokers(rpBroker),
		kgo.SASL(scram.Auth{User: rpUser, Pass: rpPassword}.AsSha256Mechanism()),
		kgo.SeedBrokers(rpBroker),
		kgo.ConsumeTopics(rpTopicEnriched),
		kgo.ConsumerGroup("flow_test"),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)

	rpClient, err = kgo.NewClient(rpOpts...)
	if err != nil {
		return fmt.Errorf("error creating redpanda client: %v", err)
	}

	err = rpClient.Ping(ctx)
	if err != nil {
		return fmt.Errorf("error pinging broker: %v", err)
	}
	admin := kadm.NewClient(rpClient)

	// Create a topic with a single partition and single replica
	resp, err := admin.CreateTopics(ctx, 1, -1, nil, rpTopicRaw, rpTopicEnriched)
	if err != nil {
		return fmt.Errorf("error creating topics: %v", err)
	}
	for _, ctr := range resp {
		if ctr.Err != nil {
			return fmt.Errorf("unable to create topic '%s': %s\n", ctr.Topic, ctr.Err)
		} else {
			log.Printf("created topic '%s'\n", ctr.Topic)
		}
	}
	return nil
}

// TestFlowEnrichement does the following:
//   - sets up a clickhouse container and populates dependent tables,
//   - sets up a redpanda cluster and creates a flows_raw topic and a flows_unenriched topic
//   - produces a test flow onto the flows_raw topic which is consumed by the enricher, enriched
//     and placed on the flow_enriched topic
//   - consumes the enriched flow from the flows_enriched topic and compared against a golden file
func TestFlowEnrichment(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup clickhouse container
	if err := setupClickhouseContainer(ctx); err != nil {
		t.Fatalf("error setting up clickhouse container: %v", err)
	}
	testcontainers.CleanupContainer(t, clickhouseCtr)

	// Setup redpanda container
	if err := setupRedpandaContainer(ctx); err != nil {
		t.Fatalf("error setting up redpanda container: %v", err)
	}
	testcontainers.CleanupContainer(t, redpandaCtr)

	// Read unenriched flow from file
	file, err := os.ReadFile("fixtures/unenriched_flow.json")
	if err != nil {
		t.Fatalf("cannot read test message from file: %v", err)
	}

	// make sure unenriched flow isn't malformed before writing to topic
	var testFlow FlowSample
	err = json.Unmarshal(file, &testFlow)
	if err != nil {
		t.Fatalf("error unmarshaling test message: %v", err)
	}
	t.Logf("unenriched flow: %v", testFlow)

	body, err := json.Marshal(testFlow)
	if err != nil {
		t.Fatalf("error marshaling test flow: %v\n", err)
	}

	// Produce test flow onto flow_unenriched topic
	record := &kgo.Record{
		Topic: rpTopicRaw,
		Key:   []byte(`test`),
		Value: body,
	}
	rpClient.Produce(ctx, record, func(record *kgo.Record, err error) {
		if err != nil {
			t.Fatalf("Error sending message: %v\n", err)
		}
	})

	// Start enricher
	chConn, err := clickhouseCtr.ConnectionHost(context.Background())
	if err != nil {
		t.Fatalf("unable to get clickhouse host addr: %v", err)
	}
	opts := []EnricherOption{
		WithClickhouseAddr(chConn),
		WithClickhouseCreds(chUser, chPassword),
		WithClickhouseTLSEnabled(false),
		WithRedpandaBroker(rpBroker),
		WithRedpandaCreds(rpUser, rpPassword),
		WithRedpandaConsumerTopic(rpTopicRaw),
		WithRedpandaConsumerGroup(rpConsumerGroup),
		WithRedpandaProducerTopic(rpTopicEnriched),
	}
	enricher := NewEnricher(opts...)
	go func() {
		if err := enricher.Run(ctx); err != nil {
			log.Printf("error during enrichment: %v", err)
		}
	}()

	// Poll flows_enriched topic until we get the enriched flow from the enricher
	var enrichedflow FlowSample
	fetches := rpClient.PollRecords(ctx, 1)
	if errs := fetches.Errors(); len(errs) > 0 {
		t.Fatalf("error during polling: %v", errs)
	}
	iter := fetches.RecordIter()
	for !iter.Done() {
		record := iter.Next()

		err := json.Unmarshal(record.Value, &enrichedflow)
		if err != nil {
			t.Logf("error unmarshalling flow record: %v", err)
		}
		t.Logf("flow record consumed: %v", enrichedflow)
	}

	// Compare enriched record to golden file
	f, err := os.ReadFile("fixtures/enriched_flow.json")
	if err != nil {
		t.Fatalf("error reading enriched flow: %v", err)
	}

	var goldenFlow FlowSample
	err = json.Unmarshal(f, &goldenFlow)
	if err != nil {
		t.Fatalf("error unmarshaling golden flow: %v", err)
	}
	if diff := cmp.Diff(goldenFlow, enrichedflow); diff != "" {
		t.Fatalf("mismatched flowoutput: +(want), -(got): %s\n", diff)
	}
}
