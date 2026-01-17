//go:build evals

package evals_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	dztelemusage "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/usage"
	"github.com/stretchr/testify/require"
)

// TestLake_Agent_Evals_Anthropic_LinkUtilization tests that the agent correctly
// generates link utilization queries with separate in/out calculations and per-link metrics.
func TestLake_Agent_Evals_Anthropic_LinkUtilization(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_LinkUtilization(t, newAnthropicLLMClient)
}


func runTest_LinkUtilization(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed utilization test data
	seedLinkUtilizationData(t, ctx, conn)

	// Validate database query results before testing agent
	validateLinkUtilizationQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query - asking about link utilization
	question := "Which links have highest utilization in the last hour?"
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

	// Deterministic checks - these must pass regardless of Ollama availability
	responseLower := strings.ToLower(response)

	// Response should mention the link name
	require.True(t, strings.Contains(responseLower, "chi-nyc") || strings.Contains(responseLower, "chi_nyc"),
		"Response should mention chi-nyc link")

	// Response should contain utilization metrics (percentage or the word utilization)
	hasUtilization := strings.Contains(responseLower, "utilization") ||
		strings.Contains(responseLower, "%") ||
		strings.Contains(response, "80") || strings.Contains(response, "79") || strings.Contains(response, "81")
	require.True(t, hasUtilization, "Response should contain utilization metrics (percentages or 'utilization')")

	// Evaluate with Ollama (optional - skips gracefully if unavailable)
	// Key expectations:
	// 1. Response should mention the high-utilization link (chi-nyc-1 at ~80%)
	// 2. Response should report in/out separately OR mention the highest direction
	// 3. Should NOT aggregate across links or combine directions incorrectly
	expectations := []Expectation{
		{
			Description:   "Response mentions chi-nyc-1 link with high utilization",
			ExpectedValue: "chi-nyc-1 appears with utilization around 80% or as highest utilized link",
			Rationale:     "chi-nyc-1 has ~80% outbound utilization, should be identified as high",
		},
		{
			Description:   "Response reports utilization percentages",
			ExpectedValue: "utilization percentages are shown (e.g., 80%, 10%, or similar values)",
			Rationale:     "Link utilization should be expressed as percentage of capacity",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation failed")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly answer the question")
}

// seedLinkUtilizationData seeds test data for link utilization eval.
// Sets up:
// - Two metros (NYC, CHI)
// - Two devices (one per metro)
// - One WAN link between them (10 Gbps)
// - Interface counters showing:
//   - chi-nyc-1: ~80% outbound utilization, ~10% inbound (asymmetric)
func seedLinkUtilizationData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	// Seed metros
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "chi", Name: "Chicago"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed devices
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "chi-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	// Seed link: 10 Gbps WAN link between NYC and CHI
	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	links := []serviceability.Link{
		{
			PK:             "link1",
			Code:           "chi-nyc-1",
			Status:         "activated",
			LinkType:       "WAN",
			SideAPK:        "device2", // CHI side
			SideZPK:        "device1", // NYC side
			SideAIfaceName: "Ethernet1",
			SideZIfaceName: "Ethernet1",
			Bandwidth:      10_000_000_000, // 10 Gbps
			CommittedRTTNs: 15_000_000,     // 15ms
		},
	}
	var linkSchema serviceability.LinkSchema
	err = linkDS.WriteBatch(ctx, conn, len(links), func(i int) ([]any, error) {
		return linkSchema.ToRow(links[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Seed interface usage with asymmetric utilization
	// CHI side (side A) sending 80% of capacity outbound, receiving 10% inbound
	// This creates a clear asymmetric pattern where combining in+out would give wrong answer
	ifaceUsageDS, err := dztelemusage.NewDeviceInterfaceCountersDataset(log)
	require.NoError(t, err)
	ingestedAt := now

	// 10 Gbps = 10,000,000,000 bits/s = 1,250,000,000 bytes/s
	// Over 1 hour (3600s) at 100% = 4,500,000,000,000 bytes
	// 80% utilization = 3,600,000,000,000 bytes in 1 hour
	// 10% utilization = 450,000,000,000 bytes in 1 hour
	//
	// We'll use delta_duration of 3600 seconds (1 hour)
	// 80% out: 3,600,000,000,000 bytes
	// 10% in:  450,000,000,000 bytes

	ifaceUsageEntries := []struct {
		time           time.Time
		devicePK       string
		host           string
		intf           string
		linkPK         *string
		linkSide       *string
		inOctetsDelta  *int64
		outOctetsDelta *int64
		deltaDuration  *float64
	}{
		// Only seed ONE side of the link (CHI side A).
		// In real telemetry, utilization is typically measured from one collection point.
		// If we seed both sides, the agent would double-count when aggregating.
		// CHI device (side A) - high outbound (80%), low inbound (10%)
		{
			time:           now.Add(-30 * time.Minute),
			devicePK:       "device2",
			host:           "chi-dzd1",
			intf:           "Ethernet1",
			linkPK:         strPtr("link1"),
			linkSide:       strPtr("A"),
			inOctetsDelta:  int64Ptr(450_000_000_000),   // 10% of capacity
			outOctetsDelta: int64Ptr(3_600_000_000_000), // 80% of capacity
			deltaDuration:  float64Ptr(3600.0),
		},
	}

	err = ifaceUsageDS.WriteBatch(ctx, conn, len(ifaceUsageEntries), func(i int) ([]any, error) {
		e := ifaceUsageEntries[i]
		return []any{
			e.time.UTC(),     // event_ts
			ingestedAt,       // ingested_at
			e.devicePK,       // device_pk
			e.host,           // host
			e.intf,           // intf
			nil,              // user_tunnel_id
			e.linkPK,         // link_pk
			e.linkSide,       // link_side
			nil,              // model_name
			nil,              // serial_number
			nil,              // carrier_transitions
			nil,              // in_broadcast_pkts
			nil,              // in_discards
			nil,              // in_errors
			nil,              // in_fcs_errors
			nil,              // in_multicast_pkts
			nil,              // in_octets
			nil,              // in_pkts
			nil,              // in_unicast_pkts
			nil,              // out_broadcast_pkts
			nil,              // out_discards
			nil,              // out_errors
			nil,              // out_multicast_pkts
			nil,              // out_octets
			nil,              // out_pkts
			nil,              // out_unicast_pkts
			nil,              // carrier_transitions_delta
			nil,              // in_broadcast_pkts_delta
			nil,              // in_discards_delta
			nil,              // in_errors_delta
			nil,              // in_fcs_errors_delta
			nil,              // in_multicast_pkts_delta
			e.inOctetsDelta,  // in_octets_delta
			nil,              // in_pkts_delta
			nil,              // in_unicast_pkts_delta
			nil,              // out_broadcast_pkts_delta
			nil,              // out_discards_delta
			nil,              // out_errors_delta
			nil,              // out_multicast_pkts_delta
			e.outOctetsDelta, // out_octets_delta
			nil,              // out_pkts_delta
			nil,              // out_unicast_pkts_delta
			e.deltaDuration,  // delta_duration
		}, nil
	})
	require.NoError(t, err)
}

// validateLinkUtilizationQuery validates that the correct utilization can be queried
func validateLinkUtilizationQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Query for per-link utilization (aggregating across collection points)
	query := `
SELECT
    l.code AS link_code,
    l.bandwidth_bps,
    SUM(COALESCE(f.in_octets_delta, 0)) AS total_in_bytes,
    SUM(COALESCE(f.out_octets_delta, 0)) AS total_out_bytes,
    SUM(COALESCE(f.delta_duration, 0)) AS total_duration,
    ROUND(100.0 * SUM(COALESCE(f.in_octets_delta, 0)) * 8 / (l.bandwidth_bps * SUM(COALESCE(f.delta_duration, 0))), 2) AS in_utilization_pct,
    ROUND(100.0 * SUM(COALESCE(f.out_octets_delta, 0)) * 8 / (l.bandwidth_bps * SUM(COALESCE(f.delta_duration, 0))), 2) AS out_utilization_pct
FROM fact_dz_device_interface_counters f
JOIN dz_links_current l ON f.link_pk = l.pk
WHERE f.event_ts > now() - INTERVAL 2 HOUR
  AND f.link_pk IS NOT NULL
  AND l.bandwidth_bps > 0
GROUP BY l.pk, l.code, l.bandwidth_bps
ORDER BY out_utilization_pct DESC
`

	result, err := dataset.Query(ctx, conn, query, nil)
	require.NoError(t, err, "Failed to execute link utilization query")
	require.GreaterOrEqual(t, result.Count, 1, "Should have at least 1 link utilization record")

	// Verify we get the expected utilization (~80% out, ~10% in)
	for _, row := range result.Rows {
		linkCode, _ := row["link_code"].(string)

		var outUtil float64
		switch v := row["out_utilization_pct"].(type) {
		case float64:
			outUtil = v
		case int64:
			outUtil = float64(v)
		}

		var inUtil float64
		switch v := row["in_utilization_pct"].(type) {
		case float64:
			inUtil = v
		case int64:
			inUtil = float64(v)
		}

		t.Logf("Link %s: in=%.1f%%, out=%.1f%%", linkCode, inUtil, outUtil)

		// Link should have ~80% out utilization, ~10% in utilization
		require.InDelta(t, 80.0, outUtil, 5.0, "Link should have ~80%% out utilization")
		require.InDelta(t, 10.0, inUtil, 5.0, "Link should have ~10%% in utilization")
	}

	t.Log("Database validation passed: Link utilization data is correct")
}
