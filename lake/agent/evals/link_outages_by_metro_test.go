//go:build evals

package evals_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_LinkOutagesByMetro(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_LinkOutagesByMetro(t, newAnthropicLLMClient)
}

func TestLake_Agent_Evals_OllamaLocal_LinkOutagesByMetro(t *testing.T) {
	t.Parallel()
	if !isOllamaAvailable() {
		t.Skip("Ollama not available, skipping eval test")
	}

	runTest_LinkOutagesByMetro(t, newOllamaLLMClient)
}

func runTest_LinkOutagesByMetro(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed link outage data by metro
	seedLinkOutagesByMetroData(t, ctx, conn)

	// Validate database query results before testing agent
	validateLinkOutagesByMetroQuery(t, ctx, conn)

	// Skip pipeline execution in short mode
	if testing.Short() {
		t.Log("Skipping pipeline execution in short mode")
		return
	}

	// Set up pipeline with LLM client
	p := setupPipeline(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query - ask about outages for links going into Sao Paulo
	question := "identify the timestamps of outages on links going into Sao Paulo in the last 30 days"
	if debug {
		if debugLevel == 1 {
			t.Logf("=== Query: '%s' ===\n", question)
		} else {
			t.Logf("=== Starting pipeline query: '%s' ===\n", question)
		}
	}
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	response := result.Answer
	if debug {
		if debugLevel == 1 {
			t.Logf("=== Response ===\n%s\n", response)
		} else {
			t.Logf("\n=== Final Pipeline Response ===\n%s\n", response)
		}
	} else {
		t.Logf("Pipeline response:\n%s", response)
	}

	// Evaluate with expectations
	expectations := []Expectation{
		{
			Description:   "Response mentions nyc-sao-1 outage",
			ExpectedValue: "nyc-sao-1 identified as having an outage with start/stop timestamps",
			Rationale:     "nyc-sao-1 connects NYC to SAO and had a resolved outage",
		},
		{
			Description:   "Response mentions sao-lon-1 outage",
			ExpectedValue: "sao-lon-1 identified as having an ongoing outage",
			Rationale:     "sao-lon-1 connects SAO to LON and is currently down",
		},
		{
			Description:   "Response does NOT mention nyc-lon-1",
			ExpectedValue: "nyc-lon-1 should NOT be mentioned as it doesn't connect to Sao Paulo",
			Rationale:     "nyc-lon-1 connects NYC-LON, not SAO",
		},
		{
			Description:   "Response includes timestamps or time references",
			ExpectedValue: "timestamps, dates, or relative time references for outage start/end",
			Rationale:     "User asked specifically for timestamps of outages",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly identify SAO link outages with timestamps")
}

// seedLinkOutagesByMetroData seeds data for link outages filtered by metro
// Scenario:
// - nyc-sao-1: NYC to SAO, had outage 10 days ago, recovered 8 days ago
// - sao-lon-1: SAO to LON, currently down (since 2 days ago)
// - nyc-lon-1: NYC to LON, always healthy (should not appear - doesn't connect to SAO)
func seedLinkOutagesByMetroData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	// Seed metros
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "sao", Name: "Sao Paulo"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed devices
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device3", Code: "sao-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	var linkSchema serviceability.LinkSchema

	// Link 1: nyc-sao-1 - resolved outage (NYC to SAO)
	// T-35d: activated (before our 30-day window)
	linkNycSaoActivated := serviceability.Link{
		PK: "link1", Code: "nyc-sao-1", Status: "activated", LinkType: "WAN",
		SideAPK: "device1", SideZPK: "device3", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1",
		Bandwidth: 10000000000, CommittedRTTNs: 100000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNycSaoActivated), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-35 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// T-10d: soft-drained (outage start)
	linkNycSaoDrained := serviceability.Link{
		PK: "link1", Code: "nyc-sao-1", Status: "soft-drained", LinkType: "WAN",
		SideAPK: "device1", SideZPK: "device3", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1",
		Bandwidth: 10000000000, CommittedRTTNs: 100000000, ISISDelayOverrideNs: 1000000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNycSaoDrained), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-10 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// T-8d: activated again (outage end)
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNycSaoActivated), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-8 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Link 2: sao-lon-1 - ongoing outage (SAO to LON)
	// T-35d: activated
	linkSaoLonActivated := serviceability.Link{
		PK: "link2", Code: "sao-lon-1", Status: "activated", LinkType: "WAN",
		SideAPK: "device3", SideZPK: "device2", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet2",
		Bandwidth: 10000000000, CommittedRTTNs: 150000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkSaoLonActivated), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-35 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// T-2d: soft-drained (ongoing outage)
	linkSaoLonDrained := serviceability.Link{
		PK: "link2", Code: "sao-lon-1", Status: "soft-drained", LinkType: "WAN",
		SideAPK: "device3", SideZPK: "device2", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet2",
		Bandwidth: 10000000000, CommittedRTTNs: 150000000, ISISDelayOverrideNs: 1000000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkSaoLonDrained), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-2 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Link 3: nyc-lon-1 - always healthy, does NOT connect to SAO
	// T-35d: activated (never changed)
	linkNycLonActivated := serviceability.Link{
		PK: "link3", Code: "nyc-lon-1", Status: "activated", LinkType: "WAN",
		SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet3", SideZIfaceName: "Ethernet3",
		Bandwidth: 10000000000, CommittedRTTNs: 50000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNycLonActivated), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-35 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)
}

// validateLinkOutagesByMetroQuery validates that key data exists in the database
func validateLinkOutagesByMetroQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Verify SAO metro exists
	metroQuery := `
SELECT code, name
FROM dz_metros_current
WHERE code = 'sao'
`
	metroResult, err := dataset.Query(ctx, conn, metroQuery, nil)
	require.NoError(t, err, "Failed to execute metro query")
	require.Equal(t, 1, metroResult.Count, "Should have SAO metro")

	// Verify links connected to SAO
	linkQuery := `
SELECT l.code, ma.code AS side_a_metro, mz.code AS side_z_metro
FROM dz_links_current l
JOIN dz_devices_current da ON l.side_a_pk = da.pk
JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
JOIN dz_metros_current ma ON da.metro_pk = ma.pk
JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
WHERE ma.code = 'sao' OR mz.code = 'sao'
ORDER BY l.code
`
	linkResult, err := dataset.Query(ctx, conn, linkQuery, nil)
	require.NoError(t, err, "Failed to execute link query")
	require.Equal(t, 2, linkResult.Count, "Should have exactly 2 links connected to SAO")

	// Verify link history has status changes for SAO links
	historyQuery := `
SELECT lh.code, lh.status, lh.snapshot_ts
FROM dim_dz_links_history lh
WHERE lh.pk IN (
    SELECT l.pk FROM dz_links_current l
    JOIN dz_devices_current da ON l.side_a_pk = da.pk
    JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
    JOIN dz_metros_current ma ON da.metro_pk = ma.pk
    JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
    WHERE ma.code = 'sao' OR mz.code = 'sao'
)
AND lh.snapshot_ts >= now() - INTERVAL 30 DAY
ORDER BY lh.code, lh.snapshot_ts
`
	historyResult, err := dataset.Query(ctx, conn, historyQuery, nil)
	require.NoError(t, err, "Failed to execute history query")
	require.GreaterOrEqual(t, historyResult.Count, 2, "Should have status changes for SAO links in last 30 days")

	t.Logf("Database validation passed: Found SAO metro, %d SAO-connected links, %d history entries", linkResult.Count, historyResult.Count)
}
