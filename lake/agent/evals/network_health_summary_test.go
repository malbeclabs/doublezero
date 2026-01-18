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
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_NetworkHealthSummary(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_NetworkHealthSummary(t, newAnthropicLLMClient)
}


func runTest_NetworkHealthSummary(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed comprehensive network health data
	seedNetworkHealthSummaryData(t, ctx, conn)

	// Validate database query results before testing agent
	validateNetworkHealthSummaryQuery(t, ctx, conn)

	// Get Neo4j client and seed graph data if available
	neo4jClient := testNeo4jClient(t)
	if neo4jClient != nil {
		seedNetworkHealthSummaryGraphData(t, ctx, neo4jClient)
	}

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client and Neo4j support
	p := setupWorkflowWithNeo4j(t, ctx, clientInfo, neo4jClient, llmFactory, debug, debugLevel)

	// Run the query
	question := "how is the network doing?"
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

	// Basic validation - the response should mention network health aspects
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
			Description:   "Response mentions tok-fra-1 packet loss",
			ExpectedValue: "tok-fra-1 appears with loss percentage (50%, 75%, 100%, or similar)",
			Rationale:     "tok-fra-1 link has high packet loss that should be highlighted",
		},
		{
			Description:   "Response mentions nyc-lon-1 packet loss",
			ExpectedValue: "nyc-lon-1 appears with loss percentage (40% or similar)",
			Rationale:     "nyc-lon-1 link has packet loss that should be reported",
		},
		{
			Description:   "Response mentions drained status",
			ExpectedValue: "drained devices or drained links mentioned (count or names acceptable)",
			Rationale:     "There are drained devices in the network - either count or names is acceptable",
		},
		{
			Description:   "Response does NOT contain spurious warnings for healthy metrics",
			ExpectedValue: "no ⚠️ Data Note warnings about low confidence or needing verification for zero-result queries",
			Rationale:     "Zero results for health checks (e.g., no high utilization) should be omitted, not flagged with warnings",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly answer the question")
}

func TestLake_Agent_Evals_Anthropic_NetworkHealthAllHealthy(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_NetworkHealthAllHealthy(t, newAnthropicLLMClient)
}


// runTest_NetworkHealthAllHealthy tests that when the network is completely healthy,
// the agent doesn't include spurious warnings or "no issues found" sections.
// This validates the simplified confidence scoring (only errors = LOW confidence).
func runTest_NetworkHealthAllHealthy(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed a completely healthy network
	seedHealthyNetworkData(t, ctx, conn)

	// Get Neo4j client and seed graph data if available
	neo4jClient := testNeo4jClient(t)
	if neo4jClient != nil {
		seedHealthyNetworkGraphData(t, ctx, neo4jClient)
	}

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client and Neo4j support
	p := setupWorkflowWithNeo4j(t, ctx, clientInfo, neo4jClient, llmFactory, debug, debugLevel)

	// Run the query
	question := "how is the network doing?"
	if debug {
		t.Logf("=== Query: '%s' ===\n", question)
	}
	result, err := p.Run(ctx, question)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.Answer)

	response := result.Answer
	if debug {
		t.Logf("=== Response ===\n%s\n", response)
	} else {
		t.Logf("Workflow response:\n%s", response)
	}

	// Evaluate with Ollama - for a healthy network, we expect:
	// 1. NO warning symbols (⚠️) for zero-result queries
	// 2. NO "low confidence" or "needs verification" language
	// 3. A positive summary that the network is healthy
	expectations := []Expectation{
		{
			Description:   "Response indicates network is healthy",
			ExpectedValue: "positive assessment - 'network is healthy', 'all systems operational', 'all devices activated', or similar positive phrasing",
			Rationale:     "All devices are activated, so a positive status summary is expected",
		},
		{
			Description:   "Response does NOT contain warning symbols for healthy data",
			ExpectedValue: "no ⚠️ symbols for query errors when the network is healthy",
			Rationale:     "Failed queries for problems (packet loss, errors) should not show warnings if network is healthy",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response contains spurious warnings or 'no issues' sections")
}

// seedHealthyNetworkData seeds a completely healthy network for the all-healthy test
func seedHealthyNetworkData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	// Seed metros
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "chi", Name: "Chicago"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed devices - ALL activated (healthy)
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device3", Code: "chi-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	// Seed links - ALL activated (healthy)
	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	// CommittedRTTNs must be higher than actual latency values to avoid SLA breach events
	// Link1 latency: 50ms, Link2 latency: 75ms - set committed RTT above these
	links := []serviceability.Link{
		{PK: "link1", Code: "nyc-lon-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000, CommittedRTTNs: 60000000},
		{PK: "link2", Code: "chi-nyc-1", Status: "activated", LinkType: "WAN", SideAPK: "device3", SideZPK: "device1", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000, CommittedRTTNs: 80000000},
	}
	var linkSchema serviceability.LinkSchema
	err = linkDS.WriteBatch(ctx, conn, len(links), func(i int) ([]any, error) {
		return linkSchema.ToRow(links[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Seed latency samples - ALL healthy (no packet loss)
	latencyDS, err := dztelemlatency.NewDeviceLinkLatencyDataset(log)
	require.NoError(t, err)
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
		// Link1 (nyc-lon): Healthy - use -30 minutes to ensure samples are within the 1-hour window
		{now.Add(-30 * time.Minute), 1, 1, "device1", "device2", "link1", 50000, false, int64Ptr(2000)},
		{now.Add(-30 * time.Minute), 1, 2, "device1", "device2", "link1", 51000, false, int64Ptr(2100)},
		{now.Add(-45 * time.Minute), 1, 1, "device2", "device1", "link1", 49000, false, int64Ptr(1900)},
		// Link2 (chi-nyc): Healthy
		{now.Add(-30 * time.Minute), 1, 1, "device3", "device1", "link2", 75000, false, int64Ptr(3000)},
		{now.Add(-30 * time.Minute), 1, 2, "device1", "device3", "link2", 73000, false, int64Ptr(2800)},
	}
	err = latencyDS.WriteBatch(ctx, conn, len(latencySamples), func(i int) ([]any, error) {
		s := latencySamples[i]
		return []any{
			s.time.UTC(),
			now,
			s.epoch,
			s.sampleIndex,
			s.originDevicePK,
			s.targetDevicePK,
			s.linkPK,
			int64(s.rttUs),
			s.loss,
			s.ipdvUs,
		}, nil
	})
	require.NoError(t, err)

	// Seed interface usage - NO errors, discards, or carrier transitions (healthy)
	ifaceUsageDS, err := dztelemusage.NewDeviceInterfaceCountersDataset(log)
	require.NoError(t, err)
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
		// Normal traffic, no errors (30% utilization)
		{now.Add(-1 * time.Hour), "device1", "nyc-dzd1", "Ethernet1", strPtr("link1"), strPtr("A"), int64Ptr(2250000000), int64Ptr(2250000000), float64Ptr(60.0)},
		{now.Add(-1 * time.Hour), "device2", "lon-dzd1", "Ethernet1", strPtr("link1"), strPtr("Z"), int64Ptr(2250000000), int64Ptr(2250000000), float64Ptr(60.0)},
		{now.Add(-1 * time.Hour), "device3", "chi-dzd1", "Ethernet1", strPtr("link2"), strPtr("A"), int64Ptr(2250000000), int64Ptr(2250000000), float64Ptr(60.0)},
	}
	err = ifaceUsageDS.WriteBatch(ctx, conn, len(ifaceUsageEntries), func(i int) ([]any, error) {
		e := ifaceUsageEntries[i]
		// Order: event_ts, ingested_at, then all columns from schema (must match exactly)
		return []any{
			e.time.UTC(),      // event_ts
			now,               // ingested_at
			e.devicePK,        // device_pk
			e.host,            // host
			e.intf,            // intf
			nil,               // user_tunnel_id
			e.linkPK,          // link_pk
			e.linkSide,        // link_side
			nil,               // model_name
			nil,               // serial_number
			nil,               // carrier_transitions
			nil,               // in_broadcast_pkts
			nil,               // in_discards
			nil,               // in_errors
			nil,               // in_fcs_errors
			nil,               // in_multicast_pkts
			nil,               // in_octets
			nil,               // in_pkts
			nil,               // in_unicast_pkts
			nil,               // out_broadcast_pkts
			nil,               // out_discards
			nil,               // out_errors
			nil,               // out_multicast_pkts
			nil,               // out_octets
			nil,               // out_pkts
			nil,               // out_unicast_pkts
			nil,               // carrier_transitions_delta
			nil,               // in_broadcast_pkts_delta
			nil,               // in_discards_delta
			nil,               // in_errors_delta
			nil,               // in_fcs_errors_delta
			nil,               // in_multicast_pkts_delta
			e.inOctetsDelta,   // in_octets_delta
			nil,               // in_pkts_delta
			nil,               // in_unicast_pkts_delta
			nil,               // out_broadcast_pkts_delta
			nil,               // out_discards_delta
			nil,               // out_errors_delta
			nil,               // out_multicast_pkts_delta
			e.outOctetsDelta,  // out_octets_delta
			nil,               // out_pkts_delta
			nil,               // out_unicast_pkts_delta
			&e.deltaDuration,  // delta_duration
		}, nil
	})
	require.NoError(t, err)
}

// seedNetworkHealthSummaryData seeds comprehensive network health data for TestLake_Agent_Evals_NetworkHealthSummary
func seedNetworkHealthSummaryData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	// Seed metros
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "chi", Name: "Chicago"},
		{PK: "metro4", Code: "sf", Name: "San Francisco"},
		{PK: "metro5", Code: "tok", Name: "Tokyo"},
		{PK: "metro6", Code: "fra", Name: "Frankfurt"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed devices - mix of activated and non-activated
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device3", Code: "chi-dzd1", Status: "drained", MetroPK: "metro3", DeviceType: "DZD"},
		{PK: "device4", Code: "sf-dzd1", Status: "activated", MetroPK: "metro4", DeviceType: "DZD"},
		{PK: "device5", Code: "tok-dzd1", Status: "drained", MetroPK: "metro5", DeviceType: "DZD"},
		{PK: "device6", Code: "fra-dzd1", Status: "activated", MetroPK: "metro6", DeviceType: "DZD"},
		{PK: "device7", Code: "nyc-dzd2", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	// Seed links - mix of activated and non-activated, WAN and DZX
	linkDS, err := serviceability.NewLinkDataset(log)
	require.NoError(t, err)
	links := []serviceability.Link{
		{PK: "link1", Code: "nyc-lon-1", Status: "activated", LinkType: "WAN", SideAPK: "device1", SideZPK: "device2", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000, CommittedRTTNs: 10000000},
		{PK: "link2", Code: "chi-nyc-1", Status: "activated", LinkType: "WAN", SideAPK: "device3", SideZPK: "device1", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000, CommittedRTTNs: 15000000},
		{PK: "link3", Code: "sf-nyc-1", Status: "drained", LinkType: "WAN", SideAPK: "device4", SideZPK: "device1", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000, CommittedRTTNs: 12000000},
		{PK: "link4", Code: "tok-fra-1", Status: "activated", LinkType: "WAN", SideAPK: "device5", SideZPK: "device6", SideAIfaceName: "Ethernet1", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000, CommittedRTTNs: 20000000},
		{PK: "link5", Code: "nyc-local-1", Status: "activated", LinkType: "DZX", SideAPK: "device1", SideZPK: "device7", SideAIfaceName: "Ethernet2", SideZIfaceName: "Ethernet1", Bandwidth: 10000000000, CommittedRTTNs: 5000000},
	}
	var linkSchema serviceability.LinkSchema
	err = linkDS.WriteBatch(ctx, conn, len(links), func(i int) ([]any, error) {
		return linkSchema.ToRow(links[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Seed latency samples - include some with packet loss (rtt_us = 0)
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
		// Link1 (nyc-lon): Mostly healthy, some loss - use -30 minutes to ensure within 1-hour window
		{now.Add(-30 * time.Minute), 1, 1, "device1", "device2", "link1", 50000, false, int64Ptr(2000)},
		{now.Add(-30 * time.Minute), 1, 2, "device1", "device2", "link1", 51000, false, int64Ptr(2100)},
		{now.Add(-30 * time.Minute), 1, 3, "device1", "device2", "link1", 0, true, nil},
		{now.Add(-45 * time.Minute), 1, 1, "device2", "device1", "link1", 49000, false, int64Ptr(1900)},
		{now.Add(-45 * time.Minute), 1, 2, "device2", "device1", "link1", 0, true, nil},
		// Link2 (chi-nyc): Healthy
		{now.Add(-30 * time.Minute), 1, 1, "device3", "device1", "link2", 75000, false, int64Ptr(3000)},
		{now.Add(-30 * time.Minute), 1, 2, "device1", "device3", "link2", 73000, false, int64Ptr(2800)},
		// Link4 (tok-fra): High loss (75% - 3 out of 4 samples)
		{now.Add(-30 * time.Minute), 1, 1, "device5", "device6", "link4", 0, true, nil},
		{now.Add(-30 * time.Minute), 1, 2, "device5", "device6", "link4", 0, true, nil},
		{now.Add(-45 * time.Minute), 1, 1, "device6", "device5", "link4", 0, true, nil},
		{now.Add(-45 * time.Minute), 1, 2, "device6", "device5", "link4", 180000, false, int64Ptr(5000)},
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

	// Seed interface usage - mix of link interfaces and non-link interfaces
	// Include errors, discards, and carrier transitions
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
		inDiscardsDelta         *int64
		outErrorsDelta          *int64
		outDiscardsDelta        *int64
		carrierTransitionsDelta *int64
		inOctetsDelta           *int64
		outOctetsDelta          *int64
		deltaDuration           *float64
	}{
		// Link interfaces with errors/discards (link_pk IS NOT NULL)
		{now.Add(-1 * time.Hour), "device1", "nyc-dzd1", "Ethernet1", strPtr("link1"), strPtr("A"), int64Ptr(5), int64Ptr(2), int64Ptr(3), int64Ptr(1), nil, int64Ptr(1000000), int64Ptr(1000000), float64Ptr(60.0)},
		{now.Add(-1 * time.Hour), "device2", "lon-dzd1", "Ethernet1", strPtr("link1"), strPtr("Z"), int64Ptr(8), int64Ptr(3), int64Ptr(4), int64Ptr(2), int64Ptr(1), int64Ptr(1000000), int64Ptr(1000000), float64Ptr(60.0)},
		{now.Add(-2 * time.Hour), "device1", "nyc-dzd1", "Ethernet1", strPtr("link1"), strPtr("A"), nil, nil, nil, nil, nil, int64Ptr(1000000), int64Ptr(1000000), float64Ptr(60.0)},
		{now.Add(-2 * time.Hour), "device2", "lon-dzd1", "Ethernet1", strPtr("link1"), strPtr("Z"), nil, nil, nil, nil, nil, int64Ptr(1000000), int64Ptr(1000000), float64Ptr(60.0)},
		// Link interface with high utilization (for link traffic view) - 90% of 10Gbps
		{now.Add(-1 * time.Hour), "device1", "nyc-dzd1", "Ethernet1", strPtr("link2"), strPtr("A"), nil, nil, nil, nil, nil, int64Ptr(6750000000), int64Ptr(6750000000), float64Ptr(60.0)},
		{now.Add(-1 * time.Hour), "device3", "chi-dzd1", "Ethernet1", strPtr("link2"), strPtr("Z"), nil, nil, nil, nil, nil, int64Ptr(6750000000), int64Ptr(6750000000), float64Ptr(60.0)},
		// Non-link interfaces with errors/discards (link_pk IS NULL)
		{now.Add(-1 * time.Hour), "device1", "nyc-dzd1", "Ethernet3", nil, nil, int64Ptr(10), int64Ptr(5), int64Ptr(8), int64Ptr(4), int64Ptr(2), int64Ptr(500000), int64Ptr(500000), float64Ptr(60.0)},
		{now.Add(-1 * time.Hour), "device4", "sf-dzd1", "Ethernet2", nil, nil, int64Ptr(15), int64Ptr(7), int64Ptr(12), int64Ptr(6), int64Ptr(3), int64Ptr(500000), int64Ptr(500000), float64Ptr(60.0)},
		{now.Add(-2 * time.Hour), "device1", "nyc-dzd1", "Ethernet3", nil, nil, nil, nil, nil, nil, nil, int64Ptr(500000), int64Ptr(500000), float64Ptr(60.0)},
		{now.Add(-2 * time.Hour), "device4", "sf-dzd1", "Ethernet2", nil, nil, nil, nil, nil, nil, nil, int64Ptr(500000), int64Ptr(500000), float64Ptr(60.0)},
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
			&e.deltaDuration,          // delta_duration
		}, nil
	})
	require.NoError(t, err)
}

// validateNetworkHealthSummaryQuery validates that key data exists in the database
func validateNetworkHealthSummaryQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Verify devices exist with expected statuses
	deviceQuery := `
SELECT code, status
FROM dz_devices_current
WHERE code IN ('nyc-dzd1', 'lon-dzd1', 'chi-dzd1', 'sf-dzd1', 'tok-dzd1', 'fra-dzd1', 'ams-dzd1')
ORDER BY code
`

	result, err := dataset.Query(ctx, conn, deviceQuery, nil)
	require.NoError(t, err, "Failed to execute device query")
	require.GreaterOrEqual(t, result.Count, 5, "Should have at least 5 devices")

	// Verify links exist
	linkQuery := `
SELECT code, status
FROM dz_links_current
WHERE code IN ('nyc-lon-1', 'lon-sf-1', 'sf-nyc-1', 'tok-fra-1')
ORDER BY code
`

	linkResult, err := dataset.Query(ctx, conn, linkQuery, nil)
	require.NoError(t, err, "Failed to execute link query")
	require.GreaterOrEqual(t, linkResult.Count, 3, "Should have at least 3 links")

	t.Logf("Database validation passed: Found %d devices and %d links", result.Count, linkResult.Count)
}

// seedNetworkHealthSummaryGraphData seeds Neo4j with topology matching ClickHouse data
func seedNetworkHealthSummaryGraphData(t *testing.T, ctx context.Context, client neo4j.Client) {
	metros := []graphMetro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "chi", Name: "Chicago"},
		{PK: "metro4", Code: "sf", Name: "San Francisco"},
		{PK: "metro5", Code: "tok", Name: "Tokyo"},
		{PK: "metro6", Code: "fra", Name: "Frankfurt"},
	}
	devices := []graphDevice{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2"},
		{PK: "device3", Code: "chi-dzd1", Status: "drained", MetroPK: "metro3"},
		{PK: "device4", Code: "sf-dzd1", Status: "activated", MetroPK: "metro4"},
		{PK: "device5", Code: "tok-dzd1", Status: "drained", MetroPK: "metro5"},
		{PK: "device6", Code: "fra-dzd1", Status: "activated", MetroPK: "metro6"},
		{PK: "device7", Code: "nyc-dzd2", Status: "activated", MetroPK: "metro1"},
	}
	links := []graphLink{
		{PK: "link1", Code: "nyc-lon-1", Status: "activated", SideAPK: "device1", SideZPK: "device2"},
		{PK: "link2", Code: "chi-nyc-1", Status: "activated", SideAPK: "device3", SideZPK: "device1"},
		{PK: "link3", Code: "sf-nyc-1", Status: "drained", SideAPK: "device4", SideZPK: "device1"},
		{PK: "link4", Code: "tok-fra-1", Status: "activated", SideAPK: "device5", SideZPK: "device6"},
		{PK: "link5", Code: "nyc-local-1", Status: "activated", SideAPK: "device1", SideZPK: "device7"},
	}
	seedGraphData(t, ctx, client, metros, devices, links)
}

// seedHealthyNetworkGraphData seeds Neo4j with healthy network topology
func seedHealthyNetworkGraphData(t *testing.T, ctx context.Context, client neo4j.Client) {
	metros := []graphMetro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "chi", Name: "Chicago"},
	}
	devices := []graphDevice{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2"},
		{PK: "device3", Code: "chi-dzd1", Status: "activated", MetroPK: "metro3"},
	}
	links := []graphLink{
		{PK: "link1", Code: "nyc-lon-1", Status: "activated", SideAPK: "device1", SideZPK: "device2"},
		{PK: "link2", Code: "chi-nyc-1", Status: "activated", SideAPK: "device3", SideZPK: "device1"},
	}
	seedGraphData(t, ctx, client, metros, devices, links)
}
