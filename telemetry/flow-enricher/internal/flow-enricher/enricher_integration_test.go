package enricher

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/prometheus/client_golang/prometheus"
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
	chTable    = "flows_integration"

	rpBroker        string
	rpUser          = "enricher"
	rpPassword      = "redpanda"
	rpTopicEnriched = "flows_raw_devnet"
	rpConsumerGroup = "enricher"
	rpClient        *kgo.Client

	err    error
	logger *slog.Logger
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
	const maxAttempts = 5
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		redpandaCtr, err = redpanda.Run(ctx,
			"docker.redpanda.com/redpandadata/redpanda:v24.2.6",
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
			lastErr = err
			if isRetryableContainerStartErr(err) && attempt < maxAttempts {
				logger.Warn("redpanda container start attempt failed, retrying", "attempt", attempt, "error", err)
				time.Sleep(time.Duration(attempt) * time.Second)
				continue
			}
			return fmt.Errorf("error creating redpanda container: %v", err)
		}

		rpBroker, err = redpandaCtr.KafkaSeedBroker(context.Background())
		if err != nil {
			lastErr = err
			if isRetryableContainerStartErr(err) && attempt < maxAttempts {
				logger.Warn("redpanda broker fetch attempt failed, retrying", "attempt", attempt, "error", err)
				_ = redpandaCtr.Terminate(context.Background())
				time.Sleep(time.Duration(attempt) * time.Second)
				continue
			}
			return fmt.Errorf("unable to get redpanda seed broker: %v", err)
		}

		// Success - break out of retry loop
		break
	}

	if redpandaCtr == nil {
		return fmt.Errorf("failed to start redpanda after %d attempts: %v", maxAttempts, lastErr)
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
			logger.Info("created topic", "topic", ctr.Topic)
		}
	}
	return nil
}

func isRetryableContainerStartErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "wait until ready") ||
		strings.Contains(s, "mapped port") ||
		strings.Contains(s, "timeout") ||
		strings.Contains(s, "context deadline exceeded") ||
		strings.Contains(s, "TLS handshake") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "/containers/") && strings.Contains(s, "json") ||
		strings.Contains(s, "Get \"http")
}

// TestFlowEnrichement does the following:
//   - sets up a clickhouse container and populates dependent tables,
//   - sets up a redpanda cluster and creates a flows_raw_devnet topic
//   - produces a test flow onto the flows_raw_devnet topic which is consumed by the enricher, enriched
//     and written to clickhous
//   - queries clickhouse to verify that the enriched flow is present
func TestFlowEnrichment(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger = slog.New(slog.NewTextHandler(os.Stderr, nil))

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

	reg := prometheus.NewRegistry()

	// Start enricher
	chConn, err := clickhouseCtr.ConnectionHost(context.Background())
	if err != nil {
		t.Fatalf("unable to get clickhouse host addr: %v", err)
	}
	chWriter, err := NewClickhouseWriter(
		WithClickhouseAddr(chConn),
		WithClickhouseDB(chDbname),
		WithClickhouseUser(chUser),
		WithClickhousePassword(chPassword),
		WithClickhouseTable("flows_integration"),
		WithTLSDisabled(true),
		WithClickhouseLogger(logger),
		WithClickhouseMetrics(NewClickhouseMetrics(reg)),
	)
	if err != nil {
		logger.Error("error creating clickhouse writer", "error", err)
		os.Exit(1)
	}

	flowConsumer, err := NewKafkaFlowConsumer(
		WithKafkaBroker([]string{rpBroker}),
		WithKafkaUser(rpUser),
		WithKafkaPassword(rpPassword),
		WithKafkaConsumerTopic(rpTopicEnriched),
		WithKafkaConsumerGroup(rpConsumerGroup),
		WithKafkaTLSDisabled(true),
		WithKafkaLogger(logger),
		WithFlowConsumerMetrics(NewFlowConsumerMetrics(reg)),
	)
	if err != nil {
		logger.Error("error creating kafka flow consumer", "error", err)
		os.Exit(1)
	}

	// Mock serviceability data with two users
	// User 1: 137.174.145.144 on device "test-device-1" in location "TEST-LOC1" and exchange "tst1"
	// User 2: 137.174.145.145 on device "test-device-2" in location "TEST-LOC2" and exchange "tst2"
	device1PK := [32]byte{1}
	device2PK := [32]byte{2}
	location1PK := [32]byte{3}
	location2PK := [32]byte{4}
	exchange1PK := [32]byte{5}
	exchange2PK := [32]byte{6}

	mockServiceability := &MockServiceabilityFetcher{}
	mockServiceability.SetProgramData(&serviceability.ProgramData{
		Users: []serviceability.User{
			{
				DzIp:         [4]uint8{137, 174, 145, 144},
				DevicePubKey: device1PK,
			},
			{
				DzIp:         [4]uint8{137, 174, 145, 145},
				DevicePubKey: device2PK,
			},
		},
		Devices: []serviceability.Device{
			{
				PubKey:         device1PK,
				Code:           "test-device-1",
				LocationPubKey: location1PK,
				ExchangePubKey: exchange1PK,
			},
			{
				PubKey:         device2PK,
				Code:           "test-device-2",
				LocationPubKey: location2PK,
				ExchangePubKey: exchange2PK,
			},
		},
		Locations: []serviceability.Location{
			{
				PubKey: location1PK,
				Code:   "TEST-LOC1",
				Name:   "Test Location 1",
			},
			{
				PubKey: location2PK,
				Code:   "TEST-LOC2",
				Name:   "Test Location 2",
			},
		},
		Exchanges: []serviceability.Exchange{
			{
				PubKey: exchange1PK,
				Code:   "tst1",
				Name:   "Test Exchange 1",
			},
			{
				PubKey: exchange2PK,
				Code:   "tst2",
				Name:   "Test Exchange 2",
			},
		},
	})

	enricher := NewEnricher(
		WithFlowConsumer(flowConsumer),
		WithClickhouseWriter(chWriter),
		WithLogger(logger),
		WithEnricherMetrics(NewEnricherMetrics(reg)),
		WithServiceabilityFetcher(mockServiceability),
	)
	enricher.AddAnnotator(NewServiceabilityAnnotator(enricher.ServiceabilityData))

	go func() {
		if err := enricher.Run(ctx); err != nil {
			logger.Error("error during enrichment", "error", err)
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

	type flowRow struct {
		SrcAddr       string `db:"src_addr"`
		DstAddr       string `db:"dst_addr"`
		SrcPort       uint16 `db:"src_port"`
		DstPort       uint16 `db:"dst_port"`
		Proto         string `db:"proto"`
		EType         string `db:"etype"`
		SrcDeviceCode string `db:"src_device_code"`
		DstDeviceCode string `db:"dst_device_code"`
		SrcLocation   string `db:"src_location"`
		DstLocation   string `db:"dst_location"`
		SrcExchange   string `db:"src_exchange"`
		DstExchange   string `db:"dst_exchange"`
	}

	var rows []flowRow
	require.Eventually(t, func() bool {
		rows = nil // Reset at the start of each attempt
		dbRows, err := conn.Query(fmt.Sprintf(`
			SELECT src_addr, dst_addr, src_port, dst_port, proto, etype,
			       src_device_code, dst_device_code, src_location, dst_location, src_exchange, dst_exchange
			FROM %s.%s
		`, chDbname, chTable))
		if err != nil {
			t.Logf("error querying flows table: %v", err)
			return false
		}
		defer dbRows.Close()

		for dbRows.Next() {
			var row flowRow
			if err := dbRows.Scan(
				&row.SrcAddr, &row.DstAddr, &row.SrcPort, &row.DstPort, &row.Proto, &row.EType,
				&row.SrcDeviceCode, &row.DstDeviceCode, &row.SrcLocation, &row.DstLocation, &row.SrcExchange, &row.DstExchange,
			); err != nil {
				t.Logf("error scanning row: %v", err)
				return false
			}
			rows = append(rows, row)
		}
		return len(rows) >= 2
	}, 20*time.Second, 1*time.Second, "expected at least 2 rows in flows table")

	t.Logf("found %d rows in flows table", len(rows))

	// The pcap contains flows from 137.174.145.145 (test-device-2) to 137.174.145.147 (not in serviceability)
	for _, row := range rows {
		require.Equal(t, "137.174.145.145", row.SrcAddr, "unexpected src_addr")
		require.Equal(t, "137.174.145.147", row.DstAddr, "unexpected dst_addr")
		require.Equal(t, uint16(47252), row.SrcPort, "unexpected src_port")
		require.Equal(t, uint16(5001), row.DstPort, "unexpected dst_port")
		require.Equal(t, "UDP", row.Proto, "unexpected proto")
		require.Equal(t, "IPv4", row.EType, "unexpected etype")

		// Validate enriched serviceability fields
		// Source IP 137.174.145.145 maps to test-device-2, TEST-LOC2, tst2
		require.Equal(t, "test-device-2", row.SrcDeviceCode, "unexpected src_device_code")
		require.Equal(t, "TEST-LOC2", row.SrcLocation, "unexpected src_location")
		require.Equal(t, "tst2", row.SrcExchange, "unexpected src_exchange")

		// Destination IP 137.174.145.147 is not in serviceability data, so fields should be empty
		require.Equal(t, "", row.DstDeviceCode, "unexpected dst_device_code")
		require.Equal(t, "", row.DstLocation, "unexpected dst_location")
		require.Equal(t, "", row.DstExchange, "unexpected dst_exchange")
	}
}
