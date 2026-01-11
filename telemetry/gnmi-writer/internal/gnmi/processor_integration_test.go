package gnmi

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	ch "github.com/ClickHouse/clickhouse-go/v2"
	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/clickhouse"
	"github.com/testcontainers/testcontainers-go/modules/redpanda"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/scram"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/proto"
)

const (
	chUser     = "default"
	chPassword = ""
	chDbname   = "default"
	rpUser     = "testuser"
	rpPassword = "testpassword"
	rpTopic    = "gnmi-notifications"
)

// testHarness manages test infrastructure for integration tests.
type testHarness struct {
	t             *testing.T
	ctx           context.Context
	cancel        context.CancelFunc
	clickhouseCtr *clickhouse.ClickHouseContainer
	redpandaCtr   *redpanda.Container
	chConn        string
	rpBroker      string
	rpClient      *kgo.Client
	chDB          *sql.DB
	logger        *slog.Logger
}

// newTestHarness creates a new test harness with containers and connections.
func newTestHarness(t *testing.T) *testHarness {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	h := &testHarness{
		t:      t,
		ctx:    ctx,
		cancel: cancel,
		logger: logger,
	}

	// Setup ClickHouse with production schemas
	schemaDir := filepath.Join("..", "..", "clickhouse")
	var err error
	h.clickhouseCtr, err = clickhouse.Run(ctx,
		"clickhouse/clickhouse-server:23.3.8.21-alpine",
		clickhouse.WithUsername(chUser),
		clickhouse.WithPassword(chPassword),
		clickhouse.WithDatabase(chDbname),
		clickhouse.WithInitScripts(
			filepath.Join(schemaDir, "isis_adjacencies.sql"),
			filepath.Join(schemaDir, "system_state.sql"),
			filepath.Join(schemaDir, "bgp_neighbors.sql"),
			filepath.Join(schemaDir, "interface_ifindex.sql"),
		),
	)
	require.NoError(t, err, "error setting up clickhouse container")
	testcontainers.CleanupContainer(t, h.clickhouseCtr)

	h.chConn, err = h.clickhouseCtr.ConnectionHost(ctx)
	require.NoError(t, err, "error getting clickhouse connection")

	// Setup Redpanda
	h.redpandaCtr, err = redpanda.Run(ctx,
		"docker.redpanda.com/redpandadata/redpanda:v24.2.6",
		redpanda.WithEnableSASL(),
		redpanda.WithAutoCreateTopics(),
		redpanda.WithEnableKafkaAuthorization(),
		redpanda.WithNewServiceAccount(rpUser, rpPassword),
		redpanda.WithSuperusers(rpUser),
	)
	require.NoError(t, err, "error setting up redpanda container")
	testcontainers.CleanupContainer(t, h.redpandaCtr)

	h.rpBroker, err = h.redpandaCtr.KafkaSeedBroker(ctx)
	require.NoError(t, err, "error getting redpanda seed broker")

	// Create Kafka client
	h.rpClient, err = kgo.NewClient(
		kgo.SeedBrokers(h.rpBroker),
		kgo.SASL(scram.Auth{User: rpUser, Pass: rpPassword}.AsSha256Mechanism()),
	)
	require.NoError(t, err, "error creating kafka client")

	err = h.rpClient.Ping(ctx)
	require.NoError(t, err, "error pinging kafka broker")

	// Create topic
	admin := kadm.NewClient(h.rpClient)
	resp, err := admin.CreateTopics(ctx, 1, -1, nil, rpTopic)
	require.NoError(t, err, "error creating topic")
	for _, ctr := range resp {
		require.NoError(t, ctr.Err, "error creating topic %s", ctr.Topic)
		logger.Info("created topic", "topic", ctr.Topic)
	}

	// Open ClickHouse DB for queries
	h.chDB = ch.OpenDB(&ch.Options{
		Addr: []string{h.chConn},
		Auth: ch.Auth{
			Database: chDbname,
			Username: chUser,
			Password: chPassword,
		},
	})

	return h
}

// publishPrototext loads a prototext file and publishes it as binary protobuf to Redpanda.
func (h *testHarness) publishPrototext(filename string) {
	h.t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", filename))
	require.NoError(h.t, err, "error reading prototext file")

	var resp gpb.SubscribeResponse
	err = prototext.Unmarshal(data, &resp)
	require.NoError(h.t, err, "error unmarshaling prototext")

	binary, err := proto.Marshal(&resp)
	require.NoError(h.t, err, "error marshaling protobuf")

	record := &kgo.Record{
		Topic: rpTopic,
		Value: binary,
	}

	results := h.rpClient.ProduceSync(h.ctx, record)
	require.NoError(h.t, results.FirstErr(), "error producing to Redpanda")
	h.t.Logf("published %d bytes to topic %s", len(binary), rpTopic)
}

// createProcessor creates a new processor with the harness's infrastructure.
func (h *testHarness) createProcessor(consumerGroup string) *Processor {
	h.t.Helper()

	reg := prometheus.NewRegistry()

	consumer, err := NewKafkaConsumer(
		WithKafkaBrokers([]string{h.rpBroker}),
		WithKafkaUser(rpUser),
		WithKafkaPassword(rpPassword),
		WithKafkaTopic(rpTopic),
		WithKafkaGroup(consumerGroup),
		WithKafkaTLSDisabled(true),
		WithKafkaLogger(h.logger),
		WithConsumerMetrics(NewConsumerMetrics(reg)),
	)
	require.NoError(h.t, err, "error creating kafka consumer")

	writer, err := NewClickhouseRecordWriter(
		WithClickhouseAddr(h.chConn),
		WithClickhouseDB(chDbname),
		WithClickhouseUser(chUser),
		WithClickhousePassword(chPassword),
		WithClickhouseTLSDisabled(true),
		WithClickhouseLogger(h.logger),
		WithClickhouseMetrics(NewClickhouseMetrics(reg)),
	)
	require.NoError(h.t, err, "error creating clickhouse writer")
	h.t.Cleanup(func() { writer.Close() })

	processor, err := NewProcessor(
		WithConsumer(consumer),
		WithRecordWriter(writer),
		WithProcessorLogger(h.logger),
		WithProcessorMetrics(NewProcessorMetrics(reg)),
	)
	require.NoError(h.t, err, "error creating processor")

	return processor
}

// runProcessorUntilRows runs the processor and waits until the specified table has at least minRows.
func (h *testHarness) runProcessorUntilRows(processor *Processor, table string, minRows int) {
	h.t.Helper()

	processorCtx, processorCancel := context.WithTimeout(h.ctx, 10*time.Second)
	h.t.Cleanup(processorCancel)

	go func() {
		_ = processor.Run(processorCtx)
	}()

	h.waitForRows(table, minRows)
}

// waitForRows waits until the specified table has at least minRows.
func (h *testHarness) waitForRows(table string, minRows int) {
	h.t.Helper()

	require.Eventually(h.t, func() bool {
		var count int
		err := h.chDB.QueryRowContext(h.ctx, fmt.Sprintf("SELECT count() FROM %s.%s", chDbname, table)).Scan(&count)
		if err != nil {
			h.t.Logf("error querying %s: %v", table, err)
			return false
		}
		h.t.Logf("table %s has %d rows", table, count)
		return count >= minRows
	}, 15*time.Second, 500*time.Millisecond, "expected at least %d rows in %s", minRows, table)
}

// queryRows queries a table and returns all rows using the provided scan function.
func queryRows[T any](h *testHarness, query string, scan func(*sql.Rows) (T, error)) []T {
	h.t.Helper()

	rows, err := h.chDB.QueryContext(h.ctx, query)
	require.NoError(h.t, err, "error querying database")
	defer rows.Close()

	var results []T
	for rows.Next() {
		row, err := scan(rows)
		require.NoError(h.t, err, "error scanning row")
		results = append(results, row)
	}
	return results
}

// Test case definitions for table-driven tests.
type integrationTestCase struct {
	name          string
	prototext     string
	consumerGroup string
	table         string
	minRows       int
	verify        func(t *testing.T, h *testHarness)
}

var integrationTests = []integrationTestCase{
	{
		name:          "IsisAdjacency",
		prototext:     "isis_adjacency.prototext",
		consumerGroup: "test-isis",
		table:         "isis_adjacencies",
		minRows:       2,
		verify:        verifyIsisAdjacencies,
	},
	{
		name:          "SystemState",
		prototext:     "system_hostname.prototext",
		consumerGroup: "test-system",
		table:         "system_state",
		minRows:       1,
		verify:        verifySystemState,
	},
	{
		name:          "BgpNeighbors",
		prototext:     "bgp_neighbors.prototext",
		consumerGroup: "test-bgp",
		table:         "bgp_neighbors",
		minRows:       2,
		verify:        verifyBgpNeighbors,
	},
	{
		name:          "InterfaceIfindex",
		prototext:     "interfaces_ifindex.prototext",
		consumerGroup: "test-ifindex",
		table:         "interface_ifindex",
		minRows:       2,
		verify:        verifyInterfaceIfindex,
	},
}

func TestIntegration(t *testing.T) {
	for _, tc := range integrationTests {
		t.Run(tc.name, func(t *testing.T) {
			h := newTestHarness(t)

			h.publishPrototext(tc.prototext)

			processor := h.createProcessor(tc.consumerGroup)
			h.runProcessorUntilRows(processor, tc.table, tc.minRows)

			tc.verify(t, h)
		})
	}
}

// Verification functions for each test case.

func verifyIsisAdjacencies(t *testing.T, h *testHarness) {
	type isisRow struct {
		DeviceCode     string
		InterfaceID    string
		Level          uint8
		SystemID       string
		AdjacencyState string
		NeighborIPv4   string
	}

	rows := queryRows(h, fmt.Sprintf(`
		SELECT device_code, interface_id, level, system_id, adjacency_state, neighbor_ipv4
		FROM %s.isis_adjacencies
	`, chDbname), func(r *sql.Rows) (isisRow, error) {
		var row isisRow
		err := r.Scan(&row.DeviceCode, &row.InterfaceID, &row.Level,
			&row.SystemID, &row.AdjacencyState, &row.NeighborIPv4)
		return row, err
	})

	t.Logf("found %d ISIS adjacency records", len(rows))
	require.GreaterOrEqual(t, len(rows), 2)

	for _, row := range rows {
		require.Equal(t, "chi-dn-dzd1", row.DeviceCode, "unexpected device_code")
		require.Equal(t, uint8(2), row.Level, "unexpected level")
		require.Equal(t, "UP", row.AdjacencyState, "unexpected adjacency_state")
		require.Contains(t, []string{"Switch1/11/2", "Switch1/11/4"}, row.InterfaceID, "unexpected interface_id")
		require.NotEmpty(t, row.SystemID, "system_id should be populated")
		require.NotEmpty(t, row.NeighborIPv4, "neighbor_ipv4 should be populated")
	}
}

func verifySystemState(t *testing.T, h *testHarness) {
	type systemRow struct {
		DeviceCode string
		Hostname   string
	}

	rows := queryRows(h, fmt.Sprintf(`
		SELECT device_code, hostname FROM %s.system_state
	`, chDbname), func(r *sql.Rows) (systemRow, error) {
		var row systemRow
		err := r.Scan(&row.DeviceCode, &row.Hostname)
		return row, err
	})

	t.Logf("found %d system state records", len(rows))
	require.GreaterOrEqual(t, len(rows), 1)

	require.Equal(t, "dzd01", rows[0].DeviceCode, "unexpected device_code")
	require.Equal(t, "e76554a34f51", rows[0].Hostname, "unexpected hostname")
}

func verifyBgpNeighbors(t *testing.T, h *testHarness) {
	type bgpRow struct {
		DeviceCode      string
		NeighborAddress string
		PeerAs          uint32
		SessionState    string
		Enabled         bool
	}

	rows := queryRows(h, fmt.Sprintf(`
		SELECT device_code, neighbor_address, peer_as, session_state, enabled
		FROM %s.bgp_neighbors
	`, chDbname), func(r *sql.Rows) (bgpRow, error) {
		var row bgpRow
		err := r.Scan(&row.DeviceCode, &row.NeighborAddress, &row.PeerAs,
			&row.SessionState, &row.Enabled)
		return row, err
	})

	t.Logf("found %d BGP neighbor records", len(rows))
	require.GreaterOrEqual(t, len(rows), 2)

	for _, row := range rows {
		require.Equal(t, "router1", row.DeviceCode, "unexpected device_code")
		require.Contains(t, []string{"10.0.0.1", "10.0.0.2"}, row.NeighborAddress, "unexpected neighbor_address")
		require.True(t, row.Enabled, "expected enabled to be true")

		if row.NeighborAddress == "10.0.0.1" {
			require.Equal(t, uint32(65001), row.PeerAs, "unexpected peer_as for 10.0.0.1")
			require.Equal(t, "ESTABLISHED", row.SessionState, "unexpected session_state for 10.0.0.1")
		} else {
			require.Equal(t, uint32(65002), row.PeerAs, "unexpected peer_as for 10.0.0.2")
			require.Equal(t, "ACTIVE", row.SessionState, "unexpected session_state for 10.0.0.2")
		}
	}
}

func verifyInterfaceIfindex(t *testing.T, h *testHarness) {
	type ifindexRow struct {
		DeviceCode    string
		InterfaceName string
		SubifIndex    uint32
		Ifindex       uint32
	}

	rows := queryRows(h, fmt.Sprintf(`
		SELECT device_code, interface_name, subif_index, ifindex
		FROM %s.interface_ifindex
	`, chDbname), func(r *sql.Rows) (ifindexRow, error) {
		var row ifindexRow
		err := r.Scan(&row.DeviceCode, &row.InterfaceName, &row.SubifIndex, &row.Ifindex)
		return row, err
	})

	t.Logf("found %d interface ifindex records", len(rows))
	require.GreaterOrEqual(t, len(rows), 2)

	for _, row := range rows {
		require.Equal(t, "dzd01", row.DeviceCode, "unexpected device_code")
		require.Equal(t, uint32(0), row.SubifIndex, "unexpected subif_index")
		require.Contains(t, []string{"Ethernet1", "Tunnel1"}, row.InterfaceName, "unexpected interface_name")

		if row.InterfaceName == "Ethernet1" {
			require.Equal(t, uint32(1), row.Ifindex, "unexpected ifindex for Ethernet1")
		} else {
			require.Equal(t, uint32(15000001), row.Ifindex, "unexpected ifindex for Tunnel1")
		}
	}
}
