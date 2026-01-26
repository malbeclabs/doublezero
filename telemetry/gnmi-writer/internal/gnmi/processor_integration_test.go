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
			filepath.Join(schemaDir, "transceiver_state.sql"),
			filepath.Join(schemaDir, "transceiver_thresholds.sql"),
			filepath.Join(schemaDir, "interface_state.sql"),
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
		gnmiPath:      "/interfaces/interface/state/ifindex",
		consumerGroup: "test-ifindex",
		table:         "interface_ifindex",
		minRows:       2,
		verify:        verifyInterfaceIfindex,
	},
	{
		name:          "TransceiverState",
		prototext:     "transceiver_state.prototext",
		gnmiPath:      "/components/component/transceiver/physical-channels/channel/state",
		consumerGroup: "test-transceiver-state",
		table:         "transceiver_state",
		minRows:       1,
		verify:        verifyTransceiverState,
	},
	{
		name:          "InterfaceState",
		prototext:     "interfaces.prototext",
		gnmiPath:      "/interfaces/interface/state",
		consumerGroup: "test-interface-state",
		table:         "interface_state",
		minRows:       1,
		verify:        verifyInterfaceState,
	},
	{
		name:          "TransceiverThresholds",
		prototext:     "transceiver_thresholds.prototext",
		gnmiPath:      "/components/component/transceiver/thresholds/threshold/state",
		consumerGroup: "test-transceiver-thresholds",
		table:         "transceiver_thresholds",
		minRows:       1,
		verify:        verifyTransceiverThresholds,
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
		Ifindex       uint32
	}

	rows := queryRows(h, fmt.Sprintf(`
		SELECT device_pubkey, interface_name, ifindex
		FROM %s.interface_ifindex
	`, chDbname), func(r *sql.Rows) (ifindexRow, error) {
		var row ifindexRow
		err := r.Scan(&row.DevicePubkey, &row.InterfaceName, &row.Ifindex)
		return row, err
	})

	t.Logf("found %d interface ifindex records", len(rows))
	require.GreaterOrEqual(t, len(rows), 2)

	ifindexMap := make(map[string]ifindexRow)
	for _, row := range rows {
		require.Equal(t, "DZd011111111111111111111111111111111111111111", row.DevicePubkey, "unexpected device_pubkey")
		ifindexMap[row.InterfaceName] = row
	}

	// Verify specific interfaces from testdata
	require.Contains(t, ifindexMap, "Ethernet1", "expected Ethernet1 to exist")
	require.Equal(t, uint32(1), ifindexMap["Ethernet1"].Ifindex, "unexpected ifindex for Ethernet1")

	// Tunnel interfaces report ifindex=0 on some devices
	require.Contains(t, ifindexMap, "Tunnel500", "expected Tunnel500 to exist")
	require.Equal(t, uint32(0), ifindexMap["Tunnel500"].Ifindex, "unexpected ifindex for Tunnel500")
}

func verifyTransceiverState(t *testing.T, h *testHarness) {
	type transceiverRow struct {
		DevicePubkey     string
		InterfaceName    string
		ChannelIndex     uint16
		InputPower       float64
		OutputPower      float64
		LaserBiasCurrent float64
	}

	rows := queryRows(h, fmt.Sprintf(`
		SELECT device_pubkey, interface_name, channel_index, input_power, output_power, laser_bias_current
		FROM %s.transceiver_state
	`, chDbname), func(r *sql.Rows) (transceiverRow, error) {
		var row transceiverRow
		err := r.Scan(&row.DevicePubkey, &row.InterfaceName, &row.ChannelIndex,
			&row.InputPower, &row.OutputPower, &row.LaserBiasCurrent)
		return row, err
	})

	t.Logf("found %d transceiver state records", len(rows))

	// The test data has 52 updates for physical-channels:
	// - 26 updates with power metrics (input_power, output_power, laser_bias_current)
	// - 26 updates with only description (no power metrics)
	// The extractor skips description-only updates, so we should have exactly 26 rows.
	require.Equal(t, 26, len(rows), "expected 26 transceiver state rows (description-only updates should be skipped)")

	// Verify all records have expected device pubkey and at least one non-zero power value
	// (no all-zero rows from description-only updates)
	for _, row := range rows {
		require.Equal(t, "DZt011111111111111111111111111111111111111111", row.DevicePubkey, "unexpected device_pubkey")
		hasValue := row.InputPower != 0 || row.OutputPower != 0 || row.LaserBiasCurrent != 0
		require.True(t, hasValue, "row for %s channel %d has all-zero power values (description-only update not skipped?)",
			row.InterfaceName, row.ChannelIndex)
	}

	// Find Ethernet1 channel 0 and verify all fields are populated
	var eth1Found bool
	for _, row := range rows {
		if row.InterfaceName == "Ethernet1" && row.ChannelIndex == 0 {
			eth1Found = true
			require.InDelta(t, -1.89, row.InputPower, 0.01, "unexpected input_power for Ethernet1")
			require.InDelta(t, -2.39, row.OutputPower, 0.01, "unexpected output_power for Ethernet1")
			require.InDelta(t, 6.19, row.LaserBiasCurrent, 0.01, "unexpected laser_bias_current for Ethernet1")
			break
		}
	}
	require.True(t, eth1Found, "expected Ethernet1 channel 0 to exist")

	// Verify there's exactly one row per (interface, channel) - no duplicates
	seen := make(map[string]bool)
	for _, row := range rows {
		key := fmt.Sprintf("%s/%d", row.InterfaceName, row.ChannelIndex)
		require.False(t, seen[key], "duplicate row for %s", key)
		seen[key] = true
	}
}

func verifyInterfaceState(t *testing.T, h *testHarness) {
	type interfaceStateRow struct {
		DevicePubkey       string
		InterfaceName      string
		AdminStatus        string
		OperStatus         string
		Ifindex            uint32
		CarrierTransitions uint64
	}

	rows := queryRows(h, fmt.Sprintf(`
		SELECT device_pubkey, interface_name, admin_status, oper_status, ifindex, carrier_transitions
		FROM %s.interface_state
	`, chDbname), func(r *sql.Rows) (interfaceStateRow, error) {
		var row interfaceStateRow
		err := r.Scan(&row.DevicePubkey, &row.InterfaceName, &row.AdminStatus,
			&row.OperStatus, &row.Ifindex, &row.CarrierTransitions)
		return row, err
	})

	t.Logf("found %d interface state records", len(rows))
	require.GreaterOrEqual(t, len(rows), 1)

	// Build map for easy lookup
	ifMap := make(map[string]interfaceStateRow)
	for _, row := range rows {
		require.Equal(t, "DZi011111111111111111111111111111111111111111", row.DevicePubkey, "unexpected device_pubkey")
		ifMap[row.InterfaceName] = row
	}

	// Verify Ethernet1 state
	require.Contains(t, ifMap, "Ethernet1", "expected Ethernet1 to exist")
	eth1 := ifMap["Ethernet1"]
	require.Equal(t, "UP", eth1.AdminStatus, "unexpected admin_status for Ethernet1")
	require.Equal(t, "UP", eth1.OperStatus, "unexpected oper_status for Ethernet1")
	require.Equal(t, uint32(1), eth1.Ifindex, "unexpected ifindex for Ethernet1")
	require.Equal(t, uint64(4), eth1.CarrierTransitions, "unexpected carrier_transitions for Ethernet1")

	// Verify Ethernet2 state (NOT_PRESENT)
	require.Contains(t, ifMap, "Ethernet2", "expected Ethernet2 to exist")
	eth2 := ifMap["Ethernet2"]
	require.Equal(t, "UP", eth2.AdminStatus, "unexpected admin_status for Ethernet2")
	require.Equal(t, "NOT_PRESENT", eth2.OperStatus, "unexpected oper_status for Ethernet2")
}

func verifyTransceiverThresholds(t *testing.T, h *testHarness) {
	type thresholdRow struct {
		DevicePubkey           string
		InterfaceName          string
		Severity               string
		InputPowerLower        float64
		InputPowerUpper        float64
		OutputPowerLower       float64
		OutputPowerUpper       float64
		LaserBiasCurrentLower  float64
		LaserBiasCurrentUpper  float64
		ModuleTemperatureLower float64
		ModuleTemperatureUpper float64
		SupplyVoltageLower     float64
		SupplyVoltageUpper     float64
	}

	rows := queryRows(h, fmt.Sprintf(`
		SELECT device_pubkey, interface_name, severity,
		       input_power_lower, input_power_upper,
		       output_power_lower, output_power_upper,
		       laser_bias_current_lower, laser_bias_current_upper,
		       module_temperature_lower, module_temperature_upper,
		       supply_voltage_lower, supply_voltage_upper
		FROM %s.transceiver_thresholds
	`, chDbname), func(r *sql.Rows) (thresholdRow, error) {
		var row thresholdRow
		err := r.Scan(&row.DevicePubkey, &row.InterfaceName, &row.Severity,
			&row.InputPowerLower, &row.InputPowerUpper,
			&row.OutputPowerLower, &row.OutputPowerUpper,
			&row.LaserBiasCurrentLower, &row.LaserBiasCurrentUpper,
			&row.ModuleTemperatureLower, &row.ModuleTemperatureUpper,
			&row.SupplyVoltageLower, &row.SupplyVoltageUpper)
		return row, err
	})

	t.Logf("found %d transceiver threshold records", len(rows))

	// The test data has 600 individual updates (54 interfaces × 2 severities × ~5 fields each).
	// After aggregation, we should have exactly 108 rows (54 interfaces × 2 severities).
	require.Equal(t, 108, len(rows), "expected 108 aggregated threshold rows (54 interfaces × 2 severities)")

	// Verify all records have expected device pubkey
	for _, row := range rows {
		require.Equal(t, "DZth11111111111111111111111111111111111111111", row.DevicePubkey, "unexpected device_pubkey")
	}

	// Find a WARNING threshold for Ethernet50 and verify all fields are aggregated
	var found bool
	for _, row := range rows {
		if row.InterfaceName == "Ethernet50" && row.Severity == "WARNING" {
			found = true
			// Verify multiple fields are populated in the same row (proves aggregation worked)
			require.InDelta(t, 70.0, row.ModuleTemperatureUpper, 0.01, "unexpected module_temperature_upper")
			require.InDelta(t, 0.0, row.ModuleTemperatureLower, 0.01, "unexpected module_temperature_lower")
			require.InDelta(t, 0.0, row.OutputPowerUpper, 0.01, "unexpected output_power_upper")
			require.InDelta(t, -7.3, row.OutputPowerLower, 0.1, "unexpected output_power_lower")
			require.InDelta(t, -9.9, row.InputPowerLower, 0.1, "unexpected input_power_lower")
			require.InDelta(t, 3.465, row.SupplyVoltageUpper, 0.01, "unexpected supply_voltage_upper")
			require.InDelta(t, 3.1, row.SupplyVoltageLower, 0.01, "unexpected supply_voltage_lower")
			require.InDelta(t, 13.0, row.LaserBiasCurrentUpper, 0.01, "unexpected laser_bias_current_upper")
			require.InDelta(t, 3.0, row.LaserBiasCurrentLower, 0.01, "unexpected laser_bias_current_lower")
			break
		}
	}
	require.True(t, found, "expected Ethernet50 WARNING threshold to exist")

	// Verify there's exactly one row per (interface, severity) - no duplicates
	seen := make(map[string]bool)
	for _, row := range rows {
		key := row.InterfaceName + "/" + row.Severity
		require.False(t, seen[key], "duplicate row for %s", key)
		seen[key] = true
	}
}
