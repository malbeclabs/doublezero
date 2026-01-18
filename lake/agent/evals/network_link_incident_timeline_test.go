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
	dztelemusage "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/usage"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_NetworkLinkIncidentTimeline(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_NetworkLinkIncidentTimeline(t, newAnthropicLLMClient)
}


func runTest_NetworkLinkIncidentTimeline(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed network link incident timeline data
	seedNetworkLinkIncidentTimelineData(t, ctx, conn)

	// Validate database query results before testing agent
	validateNetworkLinkIncidentTimelineQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query - ask for full incident details including metrics
	question := "show incident timeline for link nyc-lon-1 including when it was drained and any packet loss or errors"
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

	// Basic validation - the response should show a timeline
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

	// Evaluate with Ollama - include specific expectations
	expectations := []Expectation{
		{
			Description:   "Link code mentioned",
			ExpectedValue: "nyc-lon-1 or nyc-lon",
			Rationale:     "This is the specific link that had the incident",
		},
		{
			Description:   "Timeline events mentioned",
			ExpectedValue: "drain, packet loss, errors/discards/carrier transitions, and recovery/undrain",
			Rationale:     "The incident timeline should show the progression of events",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly answer the question")
}

// seedNetworkLinkIncidentTimelineData seeds data for a link incident timeline
// Timeline:
// - T-6h: Normal operation (activated, no issues)
// - T-5h: Issues start (packet loss begins, errors appear)
// - T-4h: Link drained (soft-drained, isis_delay_override_ns = 1000000000)
// - T-3h: Issues continue (packet loss, errors, carrier transitions)
// - T-2h: Recovery begins (errors decrease, packet loss intermittent)
// - T-1h: Link undrained (back to activated, isis_delay_override_ns = NULL)
// - T-0h: Full recovery (no issues, normal operation)
func seedNetworkLinkIncidentTimelineData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	// Seed metros
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed devices
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	// Seed link history showing the timeline
	// T-6h: Normal operation (activated)
	// T-4h: Link drained (soft-drained)
	// T-1h: Link undrained (back to activated)
	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)

	// T-6h: Normal operation (activated)
	linkNormal := serviceability.Link{
		PK: "link1", Code: "nyc-lon-1", Status: "activated", LinkType: "WAN",
		SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1",
		Bandwidth: 10000000000, CommittedRTTNs: 50000000, CommittedJitterNs: 10000000,
	}
	var linkSchema serviceability.LinkSchema
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNormal), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-6 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// T-4h: Link drained (soft-drained)
	linkDrained := serviceability.Link{
		PK: "link1", Code: "nyc-lon-1", Status: "soft-drained", LinkType: "WAN",
		SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1",
		Bandwidth: 10000000000, CommittedRTTNs: 50000000, CommittedJitterNs: 10000000,
		ISISDelayOverrideNs: 1000000000,
	}
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkDrained), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-4 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// T-1h: Link undrained (back to activated)
	err = linkDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return linkSchema.ToRow(linkNormal), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-1 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Seed latency samples
	latencyDS, err := dztelemlatency.NewDeviceLinkLatencyDataset(log)
	require.NoError(t, err)
	ingestedAt := now
	latencySamples := []struct {
		time           time.Time
		epoch          int64
		sampleIndex    int32
		originDevicePK string
		targetDevicePK string
		linkPK         string
		rttUs          uint32
		loss           bool
		ipdvUs         *int64
	}{
		// T-6h to T-5h: Normal operation (no packet loss, no errors)
		{now.Add(-6 * time.Hour), 100, 1, "device1", "device2", "link1", 45000, false, int64Ptr(5000)},
		{now.Add(-5*time.Hour - 50*time.Minute), 100, 2, "device1", "device2", "link1", 46000, false, int64Ptr(5200)},
		{now.Add(-5*time.Hour - 40*time.Minute), 100, 3, "device1", "device2", "link1", 47000, false, int64Ptr(4800)},
		// T-5h: Issues start (packet loss begins, some errors)
		{now.Add(-5*time.Hour - 30*time.Minute), 100, 4, "device1", "device2", "link1", 0, true, nil},
		{now.Add(-5*time.Hour - 20*time.Minute), 100, 5, "device1", "device2", "link1", 50000, false, int64Ptr(8000)},
		{now.Add(-5*time.Hour - 10*time.Minute), 100, 6, "device1", "device2", "link1", 0, true, nil},
		{now.Add(-5 * time.Hour), 100, 7, "device1", "device2", "link1", 48000, false, int64Ptr(7500)},
		// T-4h to T-3h: Link drained, issues continue (packet loss, errors, carrier transitions)
		{now.Add(-4*time.Hour - 50*time.Minute), 100, 8, "device1", "device2", "link1", 0, true, nil},
		{now.Add(-4*time.Hour - 40*time.Minute), 100, 9, "device1", "device2", "link1", 55000, false, int64Ptr(12000)},
		{now.Add(-4*time.Hour - 30*time.Minute), 100, 10, "device1", "device2", "link1", 0, true, nil},
		{now.Add(-4*time.Hour - 20*time.Minute), 100, 11, "device1", "device2", "link1", 60000, false, int64Ptr(15000)},
		{now.Add(-4*time.Hour - 10*time.Minute), 100, 12, "device1", "device2", "link1", 0, true, nil},
		{now.Add(-4 * time.Hour), 100, 13, "device1", "device2", "link1", 58000, false, int64Ptr(14000)},
		{now.Add(-3*time.Hour - 50*time.Minute), 100, 14, "device1", "device2", "link1", 0, true, nil},
		{now.Add(-3*time.Hour - 40*time.Minute), 100, 15, "device1", "device2", "link1", 62000, false, int64Ptr(16000)},
		{now.Add(-3*time.Hour - 30*time.Minute), 100, 16, "device1", "device2", "link1", 0, true, nil},
		{now.Add(-3*time.Hour - 20*time.Minute), 100, 17, "device1", "device2", "link1", 59000, false, int64Ptr(14500)},
		{now.Add(-3*time.Hour - 10*time.Minute), 100, 18, "device1", "device2", "link1", 0, true, nil},
		{now.Add(-3 * time.Hour), 100, 19, "device1", "device2", "link1", 61000, false, int64Ptr(15500)},
		// T-2h: Recovery begins (errors decrease, packet loss intermittent)
		{now.Add(-2*time.Hour - 50*time.Minute), 100, 20, "device1", "device2", "link1", 50000, false, int64Ptr(9000)},
		{now.Add(-2*time.Hour - 40*time.Minute), 100, 21, "device1", "device2", "link1", 0, true, nil},
		{now.Add(-2*time.Hour - 30*time.Minute), 100, 22, "device1", "device2", "link1", 51000, false, int64Ptr(8500)},
		{now.Add(-2*time.Hour - 20*time.Minute), 100, 23, "device1", "device2", "link1", 49000, false, int64Ptr(8000)},
		{now.Add(-2*time.Hour - 10*time.Minute), 100, 24, "device1", "device2", "link1", 52000, false, int64Ptr(9000)},
		{now.Add(-2 * time.Hour), 100, 25, "device1", "device2", "link1", 48000, false, int64Ptr(7500)},
		// T-1h to T-0h: Full recovery (no issues, normal operation)
		{now.Add(-1*time.Hour - 50*time.Minute), 100, 26, "device1", "device2", "link1", 46000, false, int64Ptr(5500)},
		{now.Add(-1*time.Hour - 40*time.Minute), 100, 27, "device1", "device2", "link1", 45000, false, int64Ptr(5000)},
		{now.Add(-1*time.Hour - 30*time.Minute), 100, 28, "device1", "device2", "link1", 47000, false, int64Ptr(5200)},
		{now.Add(-1*time.Hour - 20*time.Minute), 100, 29, "device1", "device2", "link1", 46000, false, int64Ptr(5100)},
		{now.Add(-1*time.Hour - 10*time.Minute), 100, 30, "device1", "device2", "link1", 45000, false, int64Ptr(4900)},
		{now.Add(-1 * time.Hour), 100, 31, "device1", "device2", "link1", 47000, false, int64Ptr(5300)},
		{now.Add(-50 * time.Minute), 100, 32, "device1", "device2", "link1", 46000, false, int64Ptr(5000)},
		{now.Add(-40 * time.Minute), 100, 33, "device1", "device2", "link1", 45000, false, int64Ptr(4800)},
		{now.Add(-30 * time.Minute), 100, 34, "device1", "device2", "link1", 47000, false, int64Ptr(5200)},
		{now.Add(-20 * time.Minute), 100, 35, "device1", "device2", "link1", 46000, false, int64Ptr(5100)},
		{now.Add(-10 * time.Minute), 100, 36, "device1", "device2", "link1", 45000, false, int64Ptr(4900)},
		{now, 100, 37, "device1", "device2", "link1", 47000, false, int64Ptr(5300)},
	}
	err = latencyDS.WriteBatch(ctx, conn, len(latencySamples), func(i int) ([]any, error) {
		s := latencySamples[i]
		return []any{
			s.time.UTC(),     // event_ts
			ingestedAt,       // ingested_at
			s.epoch,          // epoch
			s.sampleIndex,    // sample_index
			s.originDevicePK, // origin_device_pk
			s.targetDevicePK, // target_device_pk
			s.linkPK,         // link_pk
			int64(s.rttUs),   // rtt_us
			s.loss,           // loss
			s.ipdvUs,         // ipdv_us
		}, nil
	})
	require.NoError(t, err)

	// Seed interface usage with errors and carrier transitions
	ifaceUsageDS, err := dztelemusage.NewDeviceInterfaceCountersDataset(log)
	require.NoError(t, err)
	ifaceUsageEntries := []struct {
		time                    time.Time
		devicePK                string
		host                    string
		intf                    string
		linkPK                  *string
		linkSide                *string
		inErrorsDelta           *int64
		outErrorsDelta          *int64
		inDiscardsDelta         *int64
		outDiscardsDelta        *int64
		carrierTransitionsDelta *int64
		inOctetsDelta           *int64
		outOctetsDelta          *int64
		inPktsDelta             *int64
		outPktsDelta            *int64
		deltaDuration           *float64
	}{
		// T-5h: Errors start
		{now.Add(-5 * time.Hour), "device1", "nyc-dzd1", "Ethernet1", strPtr("link1"), strPtr("A"), int64Ptr(5), int64Ptr(3), int64Ptr(2), int64Ptr(1), int64Ptr(0), int64Ptr(1000000), int64Ptr(1000000), int64Ptr(1000), int64Ptr(1000), float64Ptr(3600.0)},
		{now.Add(-5 * time.Hour), "device2", "lon-dzd1", "Ethernet1", strPtr("link1"), strPtr("Z"), int64Ptr(4), int64Ptr(2), int64Ptr(1), int64Ptr(1), int64Ptr(0), int64Ptr(1000000), int64Ptr(1000000), int64Ptr(1000), int64Ptr(1000), float64Ptr(3600.0)},
		// T-4h: Link drained, errors and carrier transitions increase
		{now.Add(-4 * time.Hour), "device1", "nyc-dzd1", "Ethernet1", strPtr("link1"), strPtr("A"), int64Ptr(15), int64Ptr(10), int64Ptr(8), int64Ptr(5), int64Ptr(2), int64Ptr(500000), int64Ptr(500000), int64Ptr(500), int64Ptr(500), float64Ptr(3600.0)},
		{now.Add(-4 * time.Hour), "device2", "lon-dzd1", "Ethernet1", strPtr("link1"), strPtr("Z"), int64Ptr(12), int64Ptr(8), int64Ptr(6), int64Ptr(4), int64Ptr(1), int64Ptr(500000), int64Ptr(500000), int64Ptr(500), int64Ptr(500), float64Ptr(3600.0)},
		// T-3h: Issues continue
		{now.Add(-3 * time.Hour), "device1", "nyc-dzd1", "Ethernet1", strPtr("link1"), strPtr("A"), int64Ptr(18), int64Ptr(12), int64Ptr(10), int64Ptr(6), int64Ptr(3), int64Ptr(400000), int64Ptr(400000), int64Ptr(400), int64Ptr(400), float64Ptr(3600.0)},
		{now.Add(-3 * time.Hour), "device2", "lon-dzd1", "Ethernet1", strPtr("link1"), strPtr("Z"), int64Ptr(14), int64Ptr(9), int64Ptr(7), int64Ptr(5), int64Ptr(2), int64Ptr(400000), int64Ptr(400000), int64Ptr(400), int64Ptr(400), float64Ptr(3600.0)},
		// T-2h: Recovery begins, errors decrease
		{now.Add(-2 * time.Hour), "device1", "nyc-dzd1", "Ethernet1", strPtr("link1"), strPtr("A"), int64Ptr(8), int64Ptr(5), int64Ptr(4), int64Ptr(2), int64Ptr(1), int64Ptr(800000), int64Ptr(800000), int64Ptr(800), int64Ptr(800), float64Ptr(3600.0)},
		{now.Add(-2 * time.Hour), "device2", "lon-dzd1", "Ethernet1", strPtr("link1"), strPtr("Z"), int64Ptr(6), int64Ptr(4), int64Ptr(3), int64Ptr(2), int64Ptr(0), int64Ptr(800000), int64Ptr(800000), int64Ptr(800), int64Ptr(800), float64Ptr(3600.0)},
		// T-1h: Link undrained, errors minimal
		{now.Add(-1 * time.Hour), "device1", "nyc-dzd1", "Ethernet1", strPtr("link1"), strPtr("A"), int64Ptr(2), int64Ptr(1), int64Ptr(1), int64Ptr(0), int64Ptr(0), int64Ptr(1200000), int64Ptr(1200000), int64Ptr(1200), int64Ptr(1200), float64Ptr(3600.0)},
		{now.Add(-1 * time.Hour), "device2", "lon-dzd1", "Ethernet1", strPtr("link1"), strPtr("Z"), int64Ptr(1), int64Ptr(1), int64Ptr(0), int64Ptr(0), int64Ptr(0), int64Ptr(1200000), int64Ptr(1200000), int64Ptr(1200), int64Ptr(1200), float64Ptr(3600.0)},
		// T-0h: Full recovery, no errors
		{now, "device1", "nyc-dzd1", "Ethernet1", strPtr("link1"), strPtr("A"), int64Ptr(0), int64Ptr(0), int64Ptr(0), int64Ptr(0), int64Ptr(0), int64Ptr(1500000), int64Ptr(1500000), int64Ptr(1500), int64Ptr(1500), float64Ptr(3600.0)},
		{now, "device2", "lon-dzd1", "Ethernet1", strPtr("link1"), strPtr("Z"), int64Ptr(0), int64Ptr(0), int64Ptr(0), int64Ptr(0), int64Ptr(0), int64Ptr(1500000), int64Ptr(1500000), int64Ptr(1500), int64Ptr(1500), float64Ptr(3600.0)},
	}
	err = ifaceUsageDS.WriteBatch(ctx, conn, len(ifaceUsageEntries), func(i int) ([]any, error) {
		e := ifaceUsageEntries[i]
		// Order: event_ts, ingested_at, then all columns from schema
		return []any{
			e.time.UTC(),              // event_ts
			ingestedAt,                // ingested_at
			e.devicePK,                // device_pk
			e.host,                    // host
			e.intf,                    // intf
			nil,                       // user_tunnel_id
			e.linkPK,                  // link_pk
			e.linkSide,                // link_side
			nil,                       // model_name
			nil,                       // serial_number
			nil,                       // carrier_transitions
			nil,                       // in_broadcast_pkts
			nil,                       // in_discards
			nil,                       // in_errors
			nil,                       // in_fcs_errors
			nil,                       // in_multicast_pkts
			nil,                       // in_octets
			nil,                       // in_pkts
			nil,                       // in_unicast_pkts
			nil,                       // out_broadcast_pkts
			nil,                       // out_discards
			nil,                       // out_errors
			nil,                       // out_multicast_pkts
			nil,                       // out_octets
			nil,                       // out_pkts
			nil,                       // out_unicast_pkts
			e.carrierTransitionsDelta, // carrier_transitions_delta
			nil,                       // in_broadcast_pkts_delta
			e.inDiscardsDelta,         // in_discards_delta
			e.inErrorsDelta,           // in_errors_delta
			nil,                       // in_fcs_errors_delta
			nil,                       // in_multicast_pkts_delta
			nil,                       // in_octets_delta
			nil,                       // in_pkts_delta
			nil,                       // in_unicast_pkts_delta
			nil,                       // out_broadcast_pkts_delta
			e.outDiscardsDelta,        // out_discards_delta
			e.outErrorsDelta,          // out_errors_delta
			nil,                       // out_multicast_pkts_delta
			e.outOctetsDelta,          // out_octets_delta
			nil,                       // out_pkts_delta
			nil,                       // out_unicast_pkts_delta
			e.deltaDuration,           // delta_duration
		}, nil
	})
	require.NoError(t, err)
}

// validateNetworkLinkIncidentTimelineQuery validates that key data exists in the database
func validateNetworkLinkIncidentTimelineQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Verify the link exists and has expected status changes
	linkQuery := `
SELECT code, status, isis_delay_override_ns
FROM dz_links_current
WHERE code = 'nyc-lon-1'
`

	result, err := dataset.Query(ctx, conn, linkQuery, nil)
	require.NoError(t, err, "Failed to execute link query")
	require.Equal(t, 1, result.Count, "Should have exactly one link nyc-lon-1")

	// Verify link telemetry data exists
	telemetryQuery := `
SELECT COUNT(*) AS sample_count
FROM fact_dz_device_link_latency
WHERE link_pk = (SELECT pk FROM dz_links_current WHERE code = 'nyc-lon-1')
  AND event_ts >= now() - INTERVAL 6 HOUR
`

	telemetryResult, err := dataset.Query(ctx, conn, telemetryQuery, nil)
	require.NoError(t, err, "Failed to execute telemetry query")
	require.Equal(t, 1, telemetryResult.Count, "Query should return exactly one row")

	sampleCount, ok := telemetryResult.Rows[0]["sample_count"].(uint64)
	if !ok {
		switch v := telemetryResult.Rows[0]["sample_count"].(type) {
		case int64:
			sampleCount = uint64(v)
		case int:
			sampleCount = uint64(v)
		default:
			t.Fatalf("Unexpected type for sample_count: %T", v)
		}
	}

	require.Greater(t, sampleCount, uint64(0), "Should have telemetry samples for the link")

	t.Logf("Database validation passed: Found link nyc-lon-1 with %d telemetry samples", sampleCount)
}
