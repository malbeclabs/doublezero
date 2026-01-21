//go:build evals

package evals_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	dztelemusage "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/telemetry/usage"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_MulticastSubscriberBandwidth(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_MulticastSubscriberBandwidth(t, newAnthropicLLMClient)
}


func runTest_MulticastSubscriberBandwidth(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed multicast subscriber bandwidth data
	seedMulticastSubscriberBandwidthData(t, ctx, conn)

	// Validate database query results before testing agent
	validateMulticastSubscriberBandwidthQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query
	question := "which multicast subscriber consumes the most bandwidth"
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

	// Basic validation - the response should identify the subscriber
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

	// The response should be non-empty and contain subscriber identification
	require.Greater(t, len(response), 50, "Response should be substantial")

	// Evaluate with Ollama - include specific expectations
	expectations := []Expectation{
		{
			Description:   "Highest bandwidth subscriber identified",
			ExpectedValue: "owner3 (owner_pubkey) is the top consumer",
			Rationale:     "owner3 consumed the most multicast bandwidth",
		},
		{
			Description:   "DZ IP identifier for top subscriber",
			ExpectedValue: "10.0.0.3 (dz_ip) appears as the subscriber identifier",
			Rationale:     "DZ IP is the primary network identifier for subscribers",
		},
		{
			Description:   "Bandwidth reported as a rate",
			ExpectedValue: "bandwidth shown as Gbps or Mbps (rate), not just GB (volume)",
			Rationale:     "Bandwidth should always be reported as rates (Gbps/Mbps), not data volumes (GB). Total data transferred (GB) may be shown alongside but rate must be included.",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly answer the question")
}

// seedMulticastSubscriberBandwidthData seeds multicast subscriber bandwidth data for TestLake_Agent_Evals_Anthropic_MulticastSubscriberBandwidth
// Sets up multiple subscribers with different bandwidth consumption, where user3 consumes the most
func seedMulticastSubscriberBandwidthData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	// Seed metros (need metro1 and metro2 for devices)
	metroDS, err := serviceability.NewMetroDataset(log)
	require.NoError(t, err)
	err = metroDS.WriteBatch(ctx, conn, 2, func(i int) ([]any, error) {
		metros := []struct{ pk, code, name string }{
			{"metro1", "nyc", "New York"},
			{"metro2", "lon", "London"},
		}
		m := metros[i]
		return []any{m.pk, m.code, m.name, -74.0060, 40.7128}, nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Seed devices
	deviceDS, err := serviceability.NewDeviceDataset(log)
	require.NoError(t, err)
	err = deviceDS.WriteBatch(ctx, conn, 2, func(i int) ([]any, error) {
		devices := []struct{ pk, code, metroPK string }{
			{"device1", "nyc-dzd1", "metro1"},
			{"device2", "lon-dzd1", "metro2"},
		}
		d := devices[i]
		return []any{d.pk, "activated", "DZD", d.code, "", "", d.metroPK, 100}, nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Seed DZ users (subscribers)
	// user1: Low bandwidth (5 GB)
	// user2: Medium bandwidth (10 GB)
	// user3: High bandwidth (15 GB) - this is the answer
	userDS, err := serviceability.NewUserDataset(log)
	require.NoError(t, err)
	err = userDS.WriteBatch(ctx, conn, 3, func(i int) ([]any, error) {
		users := []struct {
			pk, ownerPubkey, clientIP, dzIP string
			tunnelID                        int32
		}{
			{"user1", "owner1", "1.1.1.1", "10.0.0.1", 501},
			{"user2", "owner2", "2.2.2.2", "10.0.0.2", 502},
			{"user3", "owner3", "3.3.3.3", "10.0.0.3", 503},
		}
		u := users[i]
		return []any{u.pk, u.ownerPubkey, "activated", "multicast", u.clientIP, u.dzIP, "device1", u.tunnelID}, nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-30 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Seed interface usage with multicast traffic on tunnel interfaces
	// Use recent timestamps (past 24 hours)
	// CRITICAL: On tunnel interfaces, in_multicast_pkts_delta and out_multicast_pkts_delta are NOT reliable (will be empty/NULL)
	// Must use in_pkts_delta and out_pkts_delta for multicast traffic on tunnel interfaces
	// user1 (tunnel 501): 5,000,000,000 bytes (5 GB) - low bandwidth
	// user2 (tunnel 502): 10,000,000,000 bytes (10 GB) - medium bandwidth
	// user3 (tunnel 503): 15,000,000,000 bytes (15 GB) - high bandwidth (most)
	ifaceUsageDS, err := dztelemusage.NewDeviceInterfaceCountersDataset(log)
	require.NoError(t, err)
	ingestedAt := now
	err = ifaceUsageDS.WriteBatch(ctx, conn, 6, func(i int) ([]any, error) {
		// Order: event_ts, ingested_at, then all columns from schema
		devicePK := "device1"
		host := "nyc-dzd1"
		var tunnelID int64
		var inOctetsDelta, outOctetsDelta, inPktsDelta, outPktsDelta int64
		var eventTS time.Time

		if i < 2 {
			// user1 (tunnel 501): 5 GB total (2 records × 2.5GB each = 5GB)
			tunnelID = 501
			inOctetsDelta = 1250000000  // 1.25GB per record
			outOctetsDelta = 1250000000 // 1.25GB per record
			inPktsDelta = 2500000
			outPktsDelta = 2500000
			eventTS = now.Add(-time.Duration(i+1) * time.Hour)
		} else if i < 4 {
			// user2 (tunnel 502): 10 GB total (2 records × 5GB each = 10GB)
			tunnelID = 502
			inOctetsDelta = 2500000000  // 2.5GB per record
			outOctetsDelta = 2500000000 // 2.5GB per record
			inPktsDelta = 5000000
			outPktsDelta = 5000000
			eventTS = now.Add(-time.Duration(i-1) * time.Hour)
		} else {
			// user3 (tunnel 503): 15 GB total (2 records × 7.5GB each = 15GB)
			tunnelID = 503
			inOctetsDelta = 3750000000  // 3.75GB per record
			outOctetsDelta = 3750000000 // 3.75GB per record
			inPktsDelta = 7500000
			outPktsDelta = 7500000
			eventTS = now.Add(-time.Duration(i-3) * time.Hour)
		}

		intf := fmt.Sprintf("tunnel%d", tunnelID)
		deltaDuration := 3600.0

		// Return in order: event_ts, ingested_at, then all columns from schema
		return []any{
			eventTS,        // event_ts
			ingestedAt,     // ingested_at
			devicePK,       // device_pk
			host,           // host
			intf,           // intf
			tunnelID,       // user_tunnel_id
			nil,            // link_pk
			nil,            // link_side
			nil,            // model_name
			nil,            // serial_number
			nil,            // carrier_transitions
			nil,            // in_broadcast_pkts
			nil,            // in_discards
			nil,            // in_errors
			nil,            // in_fcs_errors
			nil,            // in_multicast_pkts
			nil,            // in_octets
			nil,            // in_pkts
			nil,            // in_unicast_pkts
			nil,            // out_broadcast_pkts
			nil,            // out_discards
			nil,            // out_errors
			nil,            // out_multicast_pkts
			nil,            // out_octets
			nil,            // out_pkts
			nil,            // out_unicast_pkts
			nil,            // carrier_transitions_delta
			nil,            // in_broadcast_pkts_delta
			nil,            // in_discards_delta
			nil,            // in_errors_delta
			nil,            // in_fcs_errors_delta
			nil,            // in_multicast_pkts_delta
			inOctetsDelta,  // in_octets_delta
			inPktsDelta,    // in_pkts_delta
			nil,            // in_unicast_pkts_delta
			nil,            // out_broadcast_pkts_delta
			nil,            // out_discards_delta
			nil,            // out_errors_delta
			nil,            // out_multicast_pkts_delta
			outOctetsDelta, // out_octets_delta
			outPktsDelta,   // out_pkts_delta
			nil,            // out_unicast_pkts_delta
			deltaDuration,  // delta_duration
		}, nil
	})
	require.NoError(t, err)
}

// validateMulticastSubscriberBandwidthQuery runs the ideal query to answer the question
// and validates that the database returns the expected results (owner3 with highest bandwidth)
func validateMulticastSubscriberBandwidthQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Query for multicast subscriber with highest bandwidth
	// On tunnel interfaces, use in_pkts_delta and out_pkts_delta (not in_multicast_pkts_delta)
	query := `
SELECT
  u.owner_pubkey,
  u.client_ip,
  u.dz_ip,
  u.tunnel_id,
  SUM(COALESCE(iface.in_octets_delta, 0) + COALESCE(iface.out_octets_delta, 0)) AS total_bytes
FROM dz_users_current u
JOIN fact_dz_device_interface_counters iface ON u.device_pk = iface.device_pk
  AND iface.user_tunnel_id = u.tunnel_id
  AND iface.intf LIKE 'tunnel%'
WHERE u.status = 'activated'
  AND u.kind = 'multicast'
  AND iface.event_ts >= now() - INTERVAL 24 HOUR
GROUP BY u.owner_pubkey, u.client_ip, u.dz_ip, u.tunnel_id
ORDER BY total_bytes DESC
LIMIT 1
`

	result, err := dataset.Query(ctx, conn, query, nil)
	require.NoError(t, err, "Failed to execute multicast subscriber bandwidth query")
	require.Equal(t, 1, result.Count, "Query should return exactly one row")

	ownerPubkey, ok := result.Rows[0]["owner_pubkey"].(string)
	require.True(t, ok, "owner_pubkey should be a string")
	require.Equal(t, "owner3", ownerPubkey,
		"Expected owner3 to have the highest bandwidth, but got %s", ownerPubkey)

	clientIP, ok := result.Rows[0]["client_ip"].(string)
	require.True(t, ok, "client_ip should be a string")
	require.Equal(t, "3.3.3.3", clientIP,
		"Expected client_ip 3.3.3.3 for owner3, but got %s", clientIP)

	totalBytes, ok := result.Rows[0]["total_bytes"].(uint64)
	if !ok {
		switch v := result.Rows[0]["total_bytes"].(type) {
		case int64:
			totalBytes = uint64(v)
		case int:
			totalBytes = uint64(v)
		case uint32:
			totalBytes = uint64(v)
		case int32:
			totalBytes = uint64(v)
		default:
			t.Fatalf("Unexpected type for total_bytes: %T, value: %v", v, v)
		}
	}

	// Expected: 15,000,000,000 bytes (15 GB) for owner3
	expectedBytes := uint64(15000000000)
	require.Equal(t, expectedBytes, totalBytes,
		"Expected owner3 to have %d bytes (15 GB), but got %d", expectedBytes, totalBytes)

	t.Logf("Database validation passed: owner3 (client_ip=%s) has highest bandwidth: %d bytes (%.2f GB)",
		clientIP, totalBytes, float64(totalBytes)/1e9)
}
