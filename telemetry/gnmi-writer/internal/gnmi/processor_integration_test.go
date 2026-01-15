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
	gnmiPath      string // gNMI subscription path that generated this testdata
	consumerGroup string
	table         string
	minRows       int
	verify        func(t *testing.T, h *testHarness)
}

var integrationTests = []integrationTestCase{
	{
		name:          "IsisAdjacency",
		prototext:     "isis_adjacency.prototext",
		gnmiPath:      "/network-instances/network-instance[name=default]/protocols/protocol[identifier=ISIS][name=*]/isis/interfaces/interface[interface-id=*]/levels/level/adjacencies/",
		consumerGroup: "test-isis",
		table:         "isis_adjacencies",
		minRows:       2,
		verify:        verifyIsisAdjacencies,
	},
	{
		name:          "SystemState",
		prototext:     "system_hostname.prototext",
		gnmiPath:      "/system/state/hostname",
		consumerGroup: "test-system",
		table:         "system_state",
		minRows:       1,
		verify:        verifySystemState,
	},
	{
		name:          "BgpNeighbors",
		prototext:     "bgp_neighbors.prototext",
		gnmiPath:      "/network-instances/.../protocols/protocol[identifier=BGP][name=*]/bgp/neighbors/neighbor/state",
		consumerGroup: "test-bgp",
		table:         "bgp_neighbors",
		minRows:       2,
		verify:        verifyBgpNeighbors,
	},
	{
		name:          "InterfaceIfindex",
		prototext:     "interfaces_ifindex.prototext",
		gnmiPath:      "/interfaces/interface/subinterfaces/subinterface/state/ifindex",
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
		DevicePubkey   string
		InterfaceID    string
		Level          uint8
		SystemID       string
		AdjacencyState string
		NeighborIPv4   string
	}

	rows := queryRows(h, fmt.Sprintf(`
		SELECT device_pubkey, interface_id, level, system_id, adjacency_state, neighbor_ipv4
		FROM %s.isis_adjacencies
	`, chDbname), func(r *sql.Rows) (isisRow, error) {
		var row isisRow
		err := r.Scan(&row.DevicePubkey, &row.InterfaceID, &row.Level,
			&row.SystemID, &row.AdjacencyState, &row.NeighborIPv4)
		return row, err
	})

	t.Logf("found %d ISIS adjacency records", len(rows))
	require.GreaterOrEqual(t, len(rows), 2)

	for _, row := range rows {
		require.Equal(t, "CHiDN1111111111111111111111111111111111111111", row.DevicePubkey, "unexpected device_pubkey")
		require.Equal(t, uint8(2), row.Level, "unexpected level")
		require.Equal(t, "UP", row.AdjacencyState, "unexpected adjacency_state")
		require.Contains(t, []string{"Switch1/11/2", "Switch1/11/4"}, row.InterfaceID, "unexpected interface_id")
		require.NotEmpty(t, row.SystemID, "system_id should be populated")
		require.NotEmpty(t, row.NeighborIPv4, "neighbor_ipv4 should be populated")
	}
}

func verifySystemState(t *testing.T, h *testHarness) {
	type systemRow struct {
		DevicePubkey string
		Hostname     string
	}

	rows := queryRows(h, fmt.Sprintf(`
		SELECT device_pubkey, hostname FROM %s.system_state
	`, chDbname), func(r *sql.Rows) (systemRow, error) {
		var row systemRow
		err := r.Scan(&row.DevicePubkey, &row.Hostname)
		return row, err
	})

	t.Logf("found %d system state records", len(rows))
	require.GreaterOrEqual(t, len(rows), 1)

	require.Equal(t, "DZd011111111111111111111111111111111111111111", rows[0].DevicePubkey, "unexpected device_pubkey")
	require.Equal(t, "e76554a34f51", rows[0].Hostname, "unexpected hostname")
}

func verifyBgpNeighbors(t *testing.T, h *testHarness) {
	type bgpRow struct {
		DevicePubkey           string
		NetworkInstance        string
		NeighborAddress        string
		Description            string
		PeerAs                 uint32
		LocalAs                uint32
		PeerType               string
		SessionState           string
		EstablishedTransitions uint64
		LastEstablished        int64
		MessagesReceivedUpdate uint64
		MessagesSentUpdate     uint64
	}

	rows := queryRows(h, fmt.Sprintf(`
		SELECT device_pubkey, network_instance, neighbor_address, description,
		       peer_as, local_as, peer_type, session_state,
		       established_transitions, last_established,
		       messages_received_update, messages_sent_update
		FROM %s.bgp_neighbors
	`, chDbname), func(r *sql.Rows) (bgpRow, error) {
		var row bgpRow
		err := r.Scan(&row.DevicePubkey, &row.NetworkInstance, &row.NeighborAddress,
			&row.Description, &row.PeerAs, &row.LocalAs, &row.PeerType,
			&row.SessionState, &row.EstablishedTransitions, &row.LastEstablished,
			&row.MessagesReceivedUpdate, &row.MessagesSentUpdate)
		return row, err
	})

	t.Logf("found %d BGP neighbor records", len(rows))
	require.GreaterOrEqual(t, len(rows), 2)

	// Build a map for easy lookup
	neighborMap := make(map[string]bgpRow)
	for _, row := range rows {
		require.Equal(t, "CHiDN1111111111111111111111111111111111111111", row.DevicePubkey, "unexpected device_pubkey")
		neighborMap[row.NeighborAddress] = row
	}

	// Verify specific neighbors from the test data
	// Note: Neighbors with supported-capabilities field fail to unmarshal due to schema limitation,
	// so we test neighbors without that field (172.16.0.4, 169.254.0.1)

	// Verify a neighbor in default network instance (172.16.0.4 has no supported-capabilities)
	require.Contains(t, neighborMap, "172.16.0.4", "expected neighbor 172.16.0.4 to exist")
	row := neighborMap["172.16.0.4"]
	require.Equal(t, "default", row.NetworkInstance, "unexpected network_instance for 172.16.0.4")
	require.Equal(t, "test123-vpnv4", row.Description, "unexpected description for 172.16.0.4")
	require.Equal(t, uint32(65342), row.PeerAs, "unexpected peer_as for 172.16.0.4")
	require.Equal(t, "INTERNAL", row.PeerType, "unexpected peer_type for 172.16.0.4")
	require.Equal(t, "CONNECT", row.SessionState, "unexpected session_state for 172.16.0.4")

	// Verify VRF neighbor (169.254.0.1 is in vrf1)
	require.Contains(t, neighborMap, "169.254.0.1", "expected neighbor 169.254.0.1 to exist")
	row = neighborMap["169.254.0.1"]
	require.Equal(t, "vrf1", row.NetworkInstance, "unexpected network_instance for 169.254.0.1")
	require.Equal(t, "USER-500", row.Description, "unexpected description for 169.254.0.1")
	require.Equal(t, uint32(65000), row.PeerAs, "unexpected peer_as for 169.254.0.1")
	require.Equal(t, uint32(21682), row.LocalAs, "unexpected local_as for 169.254.0.1")
	require.Equal(t, "EXTERNAL", row.PeerType, "unexpected peer_type for 169.254.0.1")
	require.Equal(t, "ACTIVE", row.SessionState, "unexpected session_state for 169.254.0.1")
}

func verifyInterfaceIfindex(t *testing.T, h *testHarness) {
	type ifindexRow struct {
		DevicePubkey  string
		InterfaceName string
		SubifIndex    uint32
		Ifindex       uint32
	}

	rows := queryRows(h, fmt.Sprintf(`
		SELECT device_pubkey, interface_name, subif_index, ifindex
		FROM %s.interface_ifindex
	`, chDbname), func(r *sql.Rows) (ifindexRow, error) {
		var row ifindexRow
		err := r.Scan(&row.DevicePubkey, &row.InterfaceName, &row.SubifIndex, &row.Ifindex)
		return row, err
	})

	t.Logf("found %d interface ifindex records", len(rows))
	require.GreaterOrEqual(t, len(rows), 2)

	// Build map for easy lookup - use composite key for interfaces with multiple subinterfaces
	type ifKey struct {
		name  string
		subif uint32
	}
	ifindexMap := make(map[ifKey]ifindexRow)
	for _, row := range rows {
		require.Equal(t, "DZd011111111111111111111111111111111111111111", row.DevicePubkey, "unexpected device_pubkey")
		ifindexMap[ifKey{row.InterfaceName, row.SubifIndex}] = row
	}

	// Verify specific interfaces from testdata
	eth1Key := ifKey{"Ethernet1", 0}
	require.Contains(t, ifindexMap, eth1Key, "expected Ethernet1 subif 0 to exist")
	require.Equal(t, uint32(1), ifindexMap[eth1Key].Ifindex, "unexpected ifindex for Ethernet1")

	tunnel500Key := ifKey{"Tunnel500", 0}
	require.Contains(t, ifindexMap, tunnel500Key, "expected Tunnel500 subif 0 to exist")
	require.Equal(t, uint32(15000500), ifindexMap[tunnel500Key].Ifindex, "unexpected ifindex for Tunnel500")
}
