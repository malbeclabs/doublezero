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
	dztelemlatency "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/latency"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_DZImprovementThreshold(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_DZImprovementThreshold(t, newAnthropicLLMClient)
}


func runTest_DZImprovementThreshold(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed DZ improvement threshold data
	seedDZImprovementThresholdData(t, ctx, conn)

	// Validate database query results before testing agent
	validateDZImprovementThresholdQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query
	question := "Analyze all DZ links that provide at least 1ms improvement over public internet. What is the min and max improvement using median values?"
	if debug {
		if debugLevel == 1 {
			t.Logf("=== Query: '%s' ===\n", question)
		} else {
			t.Logf("=== Starting workflow query: '%s' ===\n", question)
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
			t.Logf("\n=== Final Workflow Response ===\n%s\n", response)
		}
	} else {
		t.Logf("Workflow response:\n%s", response)
	}

	// Evaluate with expectations
	expectations := []Expectation{
		{
			Description:   "Response identifies links with >= 1ms improvement",
			ExpectedValue: "nyc-lon and lon-tok routes mentioned as having >= 1ms improvement",
			Rationale:     "Test data: nyc-lon has ~40ms improvement, lon-tok has ~100ms improvement",
		},
		{
			Description:   "Response excludes link with < 1ms improvement",
			ExpectedValue: "nyc-sf route NOT included or explicitly noted as below threshold",
			Rationale:     "Test data: nyc-sf has only 0.5ms improvement, below 1ms threshold",
		},
		{
			Description:   "Response shows min and max improvement values",
			ExpectedValue: "Shows minimum improvement around 40ms and maximum around 100ms (or similar range)",
			Rationale:     "Min = nyc-lon (~40ms), Max = lon-tok (~100ms)",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly analyze DZ improvement threshold")
}

// seedDZImprovementThresholdData seeds data for DZ improvement threshold test
// Scenario:
// - nyc-lon: DZ median ~45ms, Internet median ~85ms -> improvement ~40ms (above 1ms threshold)
// - lon-tok: DZ median ~120ms, Internet median ~220ms -> improvement ~100ms (above 1ms threshold)
// - nyc-sf: DZ median ~28ms, Internet median ~28.5ms -> improvement ~0.5ms (BELOW 1ms threshold)
func seedDZImprovementThresholdData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	// Seed metros
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "tok", Name: "Tokyo"},
		{PK: "metro4", Code: "sf", Name: "San Francisco"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed devices
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device3", Code: "tok-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
		{PK: "device4", Code: "sf-dzd1", Status: "activated", MetroPK: "metro4", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	// Seed links
	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	links := []serviceability.Link{
		{PK: "link1", Code: "nyc-lon-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link2", Code: "lon-tok-1", Status: "activated", LinkType: "WAN", SideAPK: "device2", SideZPK: "device3", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
		{PK: "link3", Code: "nyc-sf-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device4", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000},
	}
	var linkSchema serviceability.LinkSchema
	err = linkDS.WriteBatch(ctx, conn, len(links), func(i int) ([]any, error) {
		return linkSchema.ToRow(links[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Seed DZ latency samples
	latencyDS, err := dztelemlatency.NewDeviceLinkLatencyDataset(log)
	require.NoError(t, err)
	ingestedAt := now

	dzSamples := []struct {
		time           time.Time
		originDevicePK string
		targetDevicePK string
		linkPK         string
		rttUs          uint32
	}{
		// NYC-LON: median ~45ms (45000 µs)
		{now.Add(-5 * time.Minute), "device1", "device2", "link1", 44000},
		{now.Add(-4 * time.Minute), "device1", "device2", "link1", 45000},
		{now.Add(-3 * time.Minute), "device1", "device2", "link1", 45000},
		{now.Add(-2 * time.Minute), "device1", "device2", "link1", 46000},
		{now.Add(-1 * time.Minute), "device1", "device2", "link1", 45000},
		// LON-TOK: median ~120ms (120000 µs)
		{now.Add(-5 * time.Minute), "device2", "device3", "link2", 118000},
		{now.Add(-4 * time.Minute), "device2", "device3", "link2", 120000},
		{now.Add(-3 * time.Minute), "device2", "device3", "link2", 120000},
		{now.Add(-2 * time.Minute), "device2", "device3", "link2", 122000},
		{now.Add(-1 * time.Minute), "device2", "device3", "link2", 120000},
		// NYC-SF: median ~28ms (28000 µs)
		{now.Add(-5 * time.Minute), "device1", "device4", "link3", 27000},
		{now.Add(-4 * time.Minute), "device1", "device4", "link3", 28000},
		{now.Add(-3 * time.Minute), "device1", "device4", "link3", 28000},
		{now.Add(-2 * time.Minute), "device1", "device4", "link3", 29000},
		{now.Add(-1 * time.Minute), "device1", "device4", "link3", 28000},
	}
	err = latencyDS.WriteBatch(ctx, conn, len(dzSamples), func(i int) ([]any, error) {
		s := dzSamples[i]
		return []any{
			s.time.UTC(),
			ingestedAt,
			int64(100),       // epoch
			int32(i + 1),     // sample_index
			s.originDevicePK,
			s.targetDevicePK,
			s.linkPK,
			int64(s.rttUs),
			false,            // loss
			(*int64)(nil),    // ipdv_us
		}, nil
	})
	require.NoError(t, err)

	// Seed public internet latency samples
	internetLatencyDS, err := dztelemlatency.NewInternetMetroLatencyDataset(log)
	require.NoError(t, err)

	internetSamples := []struct {
		time          time.Time
		originMetroPK string
		targetMetroPK string
		rttUs         uint32
	}{
		// NYC-LON internet: median ~85ms (85000 µs) -> improvement = 85-45 = 40ms
		{now.Add(-5 * time.Minute), "metro1", "metro2", 83000},
		{now.Add(-4 * time.Minute), "metro1", "metro2", 85000},
		{now.Add(-3 * time.Minute), "metro1", "metro2", 85000},
		{now.Add(-2 * time.Minute), "metro1", "metro2", 87000},
		{now.Add(-1 * time.Minute), "metro1", "metro2", 85000},
		// LON-TOK internet: median ~220ms (220000 µs) -> improvement = 220-120 = 100ms
		{now.Add(-5 * time.Minute), "metro2", "metro3", 215000},
		{now.Add(-4 * time.Minute), "metro2", "metro3", 220000},
		{now.Add(-3 * time.Minute), "metro2", "metro3", 220000},
		{now.Add(-2 * time.Minute), "metro2", "metro3", 225000},
		{now.Add(-1 * time.Minute), "metro2", "metro3", 220000},
		// NYC-SF internet: median ~28.5ms (28500 µs) -> improvement = 28.5-28 = 0.5ms (BELOW threshold)
		{now.Add(-5 * time.Minute), "metro1", "metro4", 28000},
		{now.Add(-4 * time.Minute), "metro1", "metro4", 28500},
		{now.Add(-3 * time.Minute), "metro1", "metro4", 28500},
		{now.Add(-2 * time.Minute), "metro1", "metro4", 29000},
		{now.Add(-1 * time.Minute), "metro1", "metro4", 28500},
	}
	err = internetLatencyDS.WriteBatch(ctx, conn, len(internetSamples), func(i int) ([]any, error) {
		s := internetSamples[i]
		return []any{
			s.time.UTC(),
			ingestedAt,
			int64(100),     // epoch
			int32(i + 1),   // sample_index
			s.originMetroPK,
			s.targetMetroPK,
			"provider1",    // data_provider
			int64(s.rttUs),
			(*int64)(nil),  // ipdv_us
		}, nil
	})
	require.NoError(t, err)
}

// validateDZImprovementThresholdQuery validates that key data exists
func validateDZImprovementThresholdQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Verify we have DZ latency for all 3 links
	dzQuery := `
SELECT link_pk, quantile(0.5)(rtt_us) / 1000 AS median_rtt_ms
FROM fact_dz_device_link_latency
WHERE event_ts >= now() - INTERVAL 1 HOUR AND loss = false
GROUP BY link_pk
ORDER BY link_pk
`
	dzResult, err := dataset.Query(ctx, conn, dzQuery, nil)
	require.NoError(t, err, "Failed to execute DZ latency query")
	require.Equal(t, 3, dzResult.Count, "Should have 3 links with DZ latency")

	// Verify we have internet latency for all 3 metro pairs
	internetQuery := `
SELECT origin_metro_pk, target_metro_pk, quantile(0.5)(rtt_us) / 1000 AS median_rtt_ms
FROM fact_dz_internet_metro_latency
WHERE event_ts >= now() - INTERVAL 1 HOUR
GROUP BY origin_metro_pk, target_metro_pk
ORDER BY origin_metro_pk, target_metro_pk
`
	internetResult, err := dataset.Query(ctx, conn, internetQuery, nil)
	require.NoError(t, err, "Failed to execute internet latency query")
	require.Equal(t, 3, internetResult.Count, "Should have 3 metro pairs with internet latency")

	t.Logf("Database validation passed: 3 DZ links, 3 internet metro pairs")
}
