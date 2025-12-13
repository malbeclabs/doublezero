//go:build integration

package enricher

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"testing"

	"time"

	"github.com/stretchr/testify/require"

	flow "github.com/malbeclabs/doublezero/telemetry/proto/flow/gen/pb-go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	ch "github.com/ClickHouse/clickhouse-go/v2"
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
	rpTopicEnriched = "flows_raw_devnet"
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
			filepath.Join("fixtures", "create_table_flows.sql"),
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
	resp, err := admin.CreateTopics(ctx, 1, -1, nil, rpTopicEnriched)
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

	// Start enricher
	chConn, err := clickhouseCtr.ConnectionHost(context.Background())
	if err != nil {
		t.Fatalf("unable to get clickhouse host addr: %v", err)
	}
	opts := []EnricherOption{
		WithClickhouseAddr(chConn),
		WithClickhouseCreds(chUser, chPassword),
		WithClickhouseTLSEnabled(false),
		WithKafkaBroker(rpBroker),
		WithKafkaTLSEnabled(false),
		WithKafkaCreds(rpUser, rpPassword),
		WithKafkaConsumerTopic(rpTopicEnriched),
		WithKafkaConsumerGroup(rpConsumerGroup),
	}
	enricher := NewEnricher(opts...)
	go func() {
		if err := enricher.Run(ctx); err != nil {
			log.Printf("error during enrichment: %v", err)
		}
	}()

	payload := readPcap(t, "./fixtures/sflow_ingress_user_traffic.pcap")

	f := &flow.FlowSample{
		ReceiveTimestamp: &timestamppb.Timestamp{Seconds: 1625243456, Nanos: 0},
		FlowPayload:      payload,
	}

	body, err := proto.Marshal(f)
	if err != nil {
		t.Fatalf("error marshaling test flow: %v", err)
	}
	// Produce test flow onto flow_unenriched topic
	record := &kgo.Record{
		Topic: rpTopicEnriched,
		Value: body,
	}
	rpClient.Produce(ctx, record, func(record *kgo.Record, err error) {
		if err != nil {
			t.Fatalf("Error sending message: %v\n", err)
		}
	})

	t.Log("produced record")

	conn := ch.OpenDB(&ch.Options{
		Addr: []string{chConn},
		Auth: ch.Auth{
			Database: chDbname,
			Username: chUser,
			Password: chPassword,
		},
	})

	var count int
	require.Eventually(t, func() bool {
		count = 0 // Reset count at the start of each attempt
		rows, err := conn.Query("select * from default.flows")
		if err != nil {
			t.Logf("error querying flows table: %v", err)
			return false
		}
		defer rows.Close()

		for rows.Next() {
			t.Log("found row in flows table")
			count++
		}
		return count > 0
	}, 20*time.Second, 1*time.Second, "no rows found in flows table")

	t.Logf("found %d rows in flows table", count)
}
