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

func TestLake_Agent_Evals_Anthropic_LinkOutagesDetection(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_LinkOutagesDetection(t, newAnthropicLLMClient)
}

func TestLake_Agent_Evals_OllamaLocal_LinkOutagesDetection(t *testing.T) {
	t.Parallel()
	if !isOllamaAvailable() {
		t.Skip("Ollama not available, skipping eval test")
	}

	runTest_LinkOutagesDetection(t, newOllamaLLMClient)
}

func runTest_LinkOutagesDetection(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed link outage data
	seedLinkOutagesDetectionData(t, ctx, conn)

	// Validate database query results before testing agent
	validateLinkOutagesDetectionQuery(t, ctx, conn)

	// Skip pipeline execution in short mode
	if testing.Short() {
		t.Log("Skipping pipeline execution in short mode")
		return
	}

	// Set up pipeline with LLM client
	p := setupPipeline(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query
	question := "what links have been down in the last 48 hours?"
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
			Description:   "Response mentions nyc-lon-1 outage with timestamps",
			ExpectedValue: "nyc-lon-1 identified as having been down with timing info (went down ~24h ago, recovered ~12h ago)",
			Rationale:     "nyc-lon-1 went soft-drained 24h ago and recovered 12h ago - timestamps/timing must be included",
		},
		{
			Description:   "Response mentions tok-fra-1 ongoing outage with start time",
			ExpectedValue: "tok-fra-1 identified as currently down/ongoing with timing info (started ~6h ago)",
			Rationale:     "tok-fra-1 is currently soft-drained since 6h ago - start time must be included",
		},
		{
			Description:   "Response does NOT mention chi-nyc-1 as having outage",
			ExpectedValue: "chi-nyc-1 should NOT be listed as down since it was always activated",
			Rationale:     "chi-nyc-1 never changed status - it was always healthy",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly identify link outages")
}

// seedLinkOutagesDetectionData seeds data for link outage detection test
// Scenario:
// - nyc-lon-1: Was activated, went soft-drained 24h ago, recovered 12h ago (resolved outage)
// - tok-fra-1: Was activated, went soft-drained 6h ago, still down (ongoing outage)
// - chi-nyc-1: Always activated (no outage)
func seedLinkOutagesDetectionData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	// Seed metros
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "chi", Name: "Chicago"},
		{PK: "metro4", Code: "tok", Name: "Tokyo"},
		{PK: "metro5", Code: "fra", Name: "Frankfurt"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed devices
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device3", Code: "chi-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
		{PK: "device4", Code: "tok-dzd1", Status: "activated", MetroPK: "metro4", DeviceType: "DZD"},
		{PK: "device5", Code: "fra-dzd1", Status: "activated", MetroPK: "metro5", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	// Seed link history showing outages
	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	var linkSchema serviceability.LinkSchema

	// Link 1: nyc-lon-1 - resolved outage
	// T-72h: activated (initial state before our window)
	linkNycLonActivated := serviceability.Link{
		PK: "link1", Code: "nyc-lon-1", Status: "activated", LinkType: "WAN",
		SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1",
		Bandwidth: 10000000000, CommittedRTTNs: 50000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNycLonActivated), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-72 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// T-24h: soft-drained (outage start)
	linkNycLonDrained := serviceability.Link{
		PK: "link1", Code: "nyc-lon-1", Status: "soft-drained", LinkType: "WAN",
		SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1",
		Bandwidth: 10000000000, CommittedRTTNs: 50000000, ISISDelayOverrideNs: 1000000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNycLonDrained), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// T-12h: activated again (outage end/recovery)
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNycLonActivated), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-12 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Link 2: tok-fra-1 - ongoing outage
	// T-72h: activated
	linkTokFraActivated := serviceability.Link{
		PK: "link2", Code: "tok-fra-1", Status: "activated", LinkType: "WAN",
		SideAPK: "device4", SideZPK: "device5", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1",
		Bandwidth: 10000000000, CommittedRTTNs: 120000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkTokFraActivated), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-72 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// T-6h: soft-drained (outage start, still ongoing)
	linkTokFraDrained := serviceability.Link{
		PK: "link2", Code: "tok-fra-1", Status: "soft-drained", LinkType: "WAN",
		SideAPK: "device4", SideZPK: "device5", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1",
		Bandwidth: 10000000000, CommittedRTTNs: 120000000, ISISDelayOverrideNs: 1000000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkTokFraDrained), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-6 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Link 3: chi-nyc-1 - always healthy (no outage)
	// T-72h: activated (and never changed)
	linkChiNycActivated := serviceability.Link{
		PK: "link3", Code: "chi-nyc-1", Status: "activated", LinkType: "WAN",
		SideAPK: "device3", SideZPK: "device1", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet2",
		Bandwidth: 10000000000, CommittedRTTNs: 15000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkChiNycActivated), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-72 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)
}

// validateLinkOutagesDetectionQuery validates that key data exists in the database
func validateLinkOutagesDetectionQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Verify links exist with expected current statuses
	linkQuery := `
SELECT code, status
FROM dz_links_current
ORDER BY code
`
	result, err := dataset.Query(ctx, conn, linkQuery, nil)
	require.NoError(t, err, "Failed to execute link query")
	require.Equal(t, 3, result.Count, "Should have exactly 3 links")

	// Verify link history has the expected status changes
	historyQuery := `
SELECT code, status, snapshot_ts
FROM dim_dz_links_history
WHERE snapshot_ts >= now() - INTERVAL 48 HOUR
ORDER BY code, snapshot_ts
`
	historyResult, err := dataset.Query(ctx, conn, historyQuery, nil)
	require.NoError(t, err, "Failed to execute history query")
	require.GreaterOrEqual(t, historyResult.Count, 2, "Should have status changes in last 48 hours")

	t.Logf("Database validation passed: Found %d links, %d history entries in last 48h", result.Count, historyResult.Count)

	// Validate the link issue events view directly
	viewQuery := `
SELECT
    link_code,
    event_type,
    start_ts,
    end_ts,
    is_ongoing,
    duration_minutes,
    new_status
FROM dz_link_issue_events
WHERE start_ts >= now() - INTERVAL 48 HOUR
ORDER BY link_code, event_type, start_ts
`
	viewResult, err := dataset.Query(ctx, conn, viewQuery, nil)
	require.NoError(t, err, "Failed to execute view query")
	t.Logf("Link issue events view returned %d rows:", viewResult.Count)
	for _, row := range viewResult.Rows {
		t.Logf("  %v", row)
	}

	// Verify nyc-lon-1 has recovered (is_ongoing = false)
	nycLonQuery := `
SELECT
    link_code,
    event_type,
    is_ongoing,
    start_ts,
    end_ts
FROM dz_link_issue_events
WHERE link_code = 'nyc-lon-1'
  AND event_type = 'status_change'
`
	nycLonResult, err := dataset.Query(ctx, conn, nycLonQuery, nil)
	require.NoError(t, err, "Failed to query nyc-lon-1 status_change event")
	require.Equal(t, 1, nycLonResult.Count, "Should have exactly 1 status_change event for nyc-lon-1")
	t.Logf("nyc-lon-1 status_change event: %v", nycLonResult.Rows[0])
}
