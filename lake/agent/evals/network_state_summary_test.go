//go:build evals

package evals_test

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	dztelemlatency "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/latency"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_NetworkStateSummary(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_NetworkStateSummary(t, newAnthropicLLMClient)
}


func runTest_NetworkStateSummary(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()
	debugLevel, debug := getDebugLevel()
	clientInfo := testClientInfo(t)

	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	seedNetworkStateSummaryData(t, ctx, conn)
	validateNetworkStateSummaryQuery(t, ctx, conn)

	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	question := "summarize the current state of the DZ network with headline stats"
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	response := result.Answer
	t.Logf("Workflow response:\n%s", response)

	expectations := []Expectation{
		{
			Description:   "Response includes device count",
			ExpectedValue: "8 devices or similar device count mentioned (6 activated + 2 drained)",
			Rationale:     "Test data has 8 devices (6 activated + 2 drained)",
		},
		{
			Description:   "Response includes link count",
			ExpectedValue: "5 links or WAN connections mentioned",
			Rationale:     "Test data has 5 WAN links",
		},
		{
			Description:   "Response includes user count",
			ExpectedValue: "6 users or connected users mentioned",
			Rationale:     "Test data has 6 connected users",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err)
	require.True(t, isCorrect, "Evaluation failed for network state summary")
}

// seedNetworkStateSummaryData creates a representative network state for summary
// - 4 metros across different regions
// - 8 devices (6 activated, 2 drained)
// - 5 WAN links
// - 6 users connected
func seedNetworkStateSummaryData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()
	opID := uuid.New()

	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "tyo", Name: "Tokyo"},
		{PK: "metro4", Code: "sin", Name: "Singapore"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "nyc-dzd2", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device3", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device4", Code: "tyo-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
		{PK: "device5", Code: "sin-dzd1", Status: "activated", MetroPK: "metro4", DeviceType: "DZD"},
		{PK: "device6", Code: "sin-dzd2", Status: "activated", MetroPK: "metro4", DeviceType: "DZD"},
		{PK: "device7", Code: "nyc-dzd3", Status: "drained", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device8", Code: "lon-dzd2", Status: "drained", MetroPK: "metro2", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	links := []serviceability.Link{
		{PK: "link1", Code: "nyc-lon-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device3", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link2", Code: "nyc-tyo-1", Status: "activated", LinkType: "WAN", SideAPK: "device2", SideZPK: "device4", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link3", Code: "lon-sin-1", Status: "activated", LinkType: "WAN", SideAPK: "device3", SideZPK: "device5", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link4", Code: "tyo-sin-1", Status: "activated", LinkType: "WAN", SideAPK: "device4", SideZPK: "device6", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet2", Bandwidth: 10000000000},
		{PK: "link5", Code: "nyc-sin-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device5", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet2", Bandwidth: 10000000000},
	}
	var linkSchema serviceability.LinkSchema
	err = linkDS.WriteBatch(ctx, conn, len(links), func(i int) ([]any, error) {
		return linkSchema.ToRow(links[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Seed users connected to the network
	users := []serviceability.User{
		{PK: "user1", Status: "activated", DevicePK: "device1", DZIP: net.ParseIP("10.1.1.1"), ClientIP: net.ParseIP("203.0.113.1"), Kind: "ibrl"},
		{PK: "user2", Status: "activated", DevicePK: "device1", DZIP: net.ParseIP("10.1.1.2"), ClientIP: net.ParseIP("203.0.113.2"), Kind: "ibrl"},
		{PK: "user3", Status: "activated", DevicePK: "device3", DZIP: net.ParseIP("10.2.1.1"), ClientIP: net.ParseIP("45.76.100.1"), Kind: "ibrl"},
		{PK: "user4", Status: "activated", DevicePK: "device4", DZIP: net.ParseIP("10.3.1.1"), ClientIP: net.ParseIP("51.15.0.1"), Kind: "ibrl"},
		{PK: "user5", Status: "activated", DevicePK: "device5", DZIP: net.ParseIP("10.4.1.1"), ClientIP: net.ParseIP("51.15.0.2"), Kind: "ibrl"},
		{PK: "user6", Status: "activated", DevicePK: "device6", DZIP: net.ParseIP("10.4.1.2"), ClientIP: net.ParseIP("51.15.0.3"), Kind: "ibrl"},
	}
	seedUsers(t, ctx, conn, users, now, now, opID)

	// Seed some latency data for the links
	latencyDS, err := dztelemlatency.NewDeviceLinkLatencyDataset(log)
	require.NoError(t, err)

	// Using links schema: nyc-lon-1 is device1(nyc)->device3(lon), etc.
	latencies := []struct {
		eventTS        time.Time
		originDevicePK string
		targetDevicePK string
		linkPK         string
		rttUs          int64
	}{
		{now.Add(-1 * time.Hour), "device1", "device3", "link1", 80000},  // nyc-lon: 80ms
		{now.Add(-1 * time.Hour), "device2", "device4", "link2", 120000}, // nyc-tyo: 120ms
		{now.Add(-1 * time.Hour), "device3", "device5", "link3", 95000},  // lon-sin: 95ms
		{now.Add(-1 * time.Hour), "device4", "device6", "link4", 30000},  // tyo-sin: 30ms
		{now.Add(-1 * time.Hour), "device1", "device5", "link5", 200000}, // nyc-sin: 200ms
	}

	err = latencyDS.WriteBatch(ctx, conn, len(latencies), func(i int) ([]any, error) {
		l := latencies[i]
		return []any{
			l.eventTS.UTC(),   // event_ts
			now,               // ingested_at
			int64(1),          // epoch
			int32(0),          // sample_index
			l.originDevicePK,  // origin_device_pk
			l.targetDevicePK,  // target_device_pk
			l.linkPK,          // link_pk
			l.rttUs,           // rtt_us
			false,             // loss
			int64(0),          // ipdv_us
		}, nil
	})
	require.NoError(t, err)
}

func validateNetworkStateSummaryQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Validate device count
	deviceQuery := `SELECT COUNT(*) as count FROM dz_devices_current`
	result, err := dataset.Query(ctx, conn, deviceQuery, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Count)
	t.Logf("Database validation passed: devices found")

	// Validate link count
	linkQuery := `SELECT COUNT(*) as count FROM dz_links_current`
	result, err = dataset.Query(ctx, conn, linkQuery, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Count)
	t.Logf("Database validation passed: links found")

	// Validate metro count
	metroQuery := `SELECT COUNT(*) as count FROM dz_metros_current`
	result, err = dataset.Query(ctx, conn, metroQuery, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Count)
	t.Logf("Database validation passed: metros found")

	// Validate users
	userQuery := `SELECT COUNT(*) as count FROM dz_users_current`
	result, err = dataset.Query(ctx, conn, userQuery, nil)
	require.NoError(t, err)
	require.Equal(t, 1, result.Count)
	t.Logf("Database validation passed: users found")
}
