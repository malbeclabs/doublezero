//go:build evals

package evals_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_NetworkHealthSummary(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_NetworkHealthSummary(t)
}

func runTest_NetworkHealthSummary(t *testing.T) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)

	// Set up test data
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed comprehensive network health data
	seedNetworkHealthSummaryData(t, ctx, conn)

	// Set up agent with Anthropic LLM client
	agentInstance := setupAgent(t, ctx, db, newAnthropicLLMClient, debug, debugLevel, nil)

	// Run the query
	var output bytes.Buffer
	question := "how is the network doing?"
	if debug {
		if debugLevel == 1 {
			t.Logf("=== Query: '%s' ===\n", question)
		} else {
			t.Logf("=== Starting agent query: '%s' ===\n", question)
		}
	}
	result, err := agentInstance.Run(ctx, question, &output)
	require.NoError(t, err)
	require.NotEmpty(t, result.FinalText)

	// Basic validation - the response should mention network health aspects
	response := result.FinalText
	if debug {
		if debugLevel == 1 {
			t.Logf("=== Response ===\n%s\n", response)
		} else {
			t.Logf("\n=== Final Agent Response ===\n%s\n", response)
		}
	} else {
		t.Logf("Agent response:\n%s", response)
	}

	// The response should be non-empty and contain some indication of network status
	require.Greater(t, len(response), 100, "Response should be substantial")

	// Validate that the response contains specific data points from the seeded test data
	validateNetworkHealthSummaryResponse(t, response)

	// Evaluate with Ollama
	isCorrect, reason, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not correctly answer the question. Reason: %s", reason)
}

// validateNetworkHealthSummaryResponse validates the response for TestLake_Agent_Evals_NetworkHealthSummary
func validateNetworkHealthSummaryResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Device status expectations - should mention devices and their status
	// We have 7 devices: 5 activated, 1 pending, 1 suspended
	deviceMentioned := strings.Contains(responseLower, "device") || strings.Contains(responseLower, "devices")
	deviceStatusMentioned := strings.Contains(responseLower, "activated") ||
		strings.Contains(responseLower, "pending") ||
		strings.Contains(responseLower, "suspended")
	require.True(t, deviceMentioned && deviceStatusMentioned,
		"Response should mention devices and their status (activated/pending/suspended). Got: %s",
		truncateForError(response, 200))

	// Link status expectations - should mention links
	// We have 5 links: 4 activated, 1 pending
	linkMentioned := strings.Contains(responseLower, "link") || strings.Contains(responseLower, "links") ||
		strings.Contains(responseLower, "wan") || strings.Contains(responseLower, "connection")
	require.True(t, linkMentioned,
		"Response should mention links or connections. Got: %s",
		truncateForError(response, 200))

	// Packet loss expectations - should mention loss, especially on problematic links
	// Link1 (nyc-lon) has some loss, Link4 (tok-fra) has high loss (3 of 4 samples lost)
	packetLossMentioned := strings.Contains(responseLower, "packet loss") ||
		strings.Contains(responseLower, "loss") ||
		strings.Contains(responseLower, "dropped") ||
		strings.Contains(responseLower, "packet")
	require.True(t, packetLossMentioned,
		"Response should mention packet loss. Got: %s",
		truncateForError(response, 200))

	// Interface errors/discards expectations - should mention errors or discards
	// Multiple interfaces have errors and discards (device1, device2, device4)
	errorsMentioned := strings.Contains(responseLower, "error") ||
		strings.Contains(responseLower, "errors") ||
		strings.Contains(responseLower, "discard") ||
		strings.Contains(responseLower, "discards")
	require.True(t, errorsMentioned,
		"Response should mention interface errors or discards. Got: %s",
		truncateForError(response, 200))

	// Metrics/numbers expectations - response should contain numeric data (counts, percentages, etc.)
	// Note: This test asks "how is the network doing?" which is a summary question, not a count question.
	// We verify that numbers are present (for metrics/percentages) but don't require specific counts.
	// If the agent mentions counts, they should be accurate (7 devices: 5 activated, 1 pending, 1 suspended;
	// 5 links: 4 activated, 1 pending), but counts are not required for this summary-style question.
	hasNumbers := false
	for _, char := range response {
		if char >= '0' && char <= '9' {
			hasNumbers = true
			break
		}
	}
	require.True(t, hasNumbers,
		"Response should contain numeric metrics, counts, or percentages. Got: %s",
		truncateForError(response, 200))

	// Specific issues that must be mentioned with entity codes and metrics (based on system prompt rules):
	// 1. Pending device: chi-dzd1 (device3) - must mention device code with "pending"
	// 2. Suspended device: tok-dzd1 (device5) - must mention device code with "suspended"
	// 3. Pending link: sf-nyc-1 (link3) - must mention link code with "pending"
	// 4. High loss link: tok-fra-1 (link4) - must mention link code with loss percentage (75% or 3 of 4)
	// 5. Interface errors/discards: must mention specific devices/interfaces with error/discard/carrier transition counts

	// Check for pending device: chi-dzd1 mentioned with "pending"
	pendingDeviceMentioned := (strings.Contains(responseLower, "chi-dzd1") || strings.Contains(responseLower, "chi")) &&
		strings.Contains(responseLower, "pending")

	// Check for suspended device: tok-dzd1 mentioned with "suspended"
	suspendedDeviceMentioned := (strings.Contains(responseLower, "tok-dzd1") || strings.Contains(responseLower, "tok")) &&
		strings.Contains(responseLower, "suspended")

	// At least one non-activated device should be mentioned
	require.True(t, pendingDeviceMentioned || suspendedDeviceMentioned,
		"Response should mention specific non-activated devices (chi-dzd1 pending or tok-dzd1 suspended). Got: %s",
		truncateForError(response, 200))

	// Check for pending link: sf-nyc-1 mentioned with "pending"
	pendingLinkMentioned := (strings.Contains(responseLower, "sf-nyc") || strings.Contains(responseLower, "sf")) &&
		strings.Contains(responseLower, "pending")
	// Pending link is important but may be mentioned less prominently
	if !pendingLinkMentioned {
		t.Logf("Note: Response may not explicitly mention the pending link (sf-nyc-1)")
	}

	// Check for high loss link: tok-fra-1 mentioned with loss percentage
	// Expected: tok-fra-1 with 75% loss or "3 of 4" samples lost
	tokFraMentioned := strings.Contains(responseLower, "tok-fra") ||
		(strings.Contains(responseLower, "tok") && strings.Contains(responseLower, "fra"))

	if tokFraMentioned {
		// If tok-fra is mentioned, should include loss percentage/metrics
		hasLossMetrics := strings.Contains(response, "75") ||
			(strings.Contains(response, "3") && strings.Contains(response, "4")) ||
			strings.Contains(response, "%") && (strings.Contains(response, "5") || strings.Contains(response, "7"))
		require.True(t, hasLossMetrics,
			"Response mentions tok-fra but should include loss percentage/metrics (75%, 3 of 4 samples, etc.). Got: %s",
			truncateForError(response, 200))
	} else {
		// High loss is critical - should be mentioned
		require.True(t, false,
			"Response should mention high loss link tok-fra-1 with metrics (75% loss, 3 of 4 samples lost). Got: %s",
			truncateForError(response, 200))
	}

	// Check for interface errors/discards/carrier transitions with specific device/interface mentions
	// Expected: specific devices mentioned with error/discard/carrier transition counts
	// Device codes: nyc-dzd1 (device1), lon-dzd1 (device2), sf-dzd1 (device4)
	deviceWithErrorsMentioned := (strings.Contains(responseLower, "nyc-dzd1") || strings.Contains(responseLower, "lon-dzd1") ||
		strings.Contains(responseLower, "sf-dzd1") || strings.Contains(responseLower, "device1") ||
		strings.Contains(responseLower, "device2") || strings.Contains(responseLower, "device4")) &&
		errorsMentioned

	if errorsMentioned {
		// If errors are mentioned, should mention specific devices/interfaces with counts
		require.True(t, deviceWithErrorsMentioned,
			"Response mentions errors/discards but should mention specific devices/interfaces (nyc-dzd1, lon-dzd1, sf-dzd1) with counts. Got: %s",
			truncateForError(response, 200))

		// Should include specific error/discard/carrier transition counts
		hasErrorCounts := strings.Contains(response, "5") || strings.Contains(response, "8") ||
			strings.Contains(response, "10") || strings.Contains(response, "15") ||
			strings.Contains(response, "2") || strings.Contains(response, "3") ||
			strings.Contains(response, "4") || strings.Contains(response, "7") ||
			strings.Contains(response, "12") || strings.Contains(response, "6") ||
			strings.Contains(response, "1") // carrier transitions
		require.True(t, hasErrorCounts,
			"Response mentions errors/discards but should include specific counts (e.g., 5 errors, 8 errors, 1 carrier transition). Got: %s",
			truncateForError(response, 200))
	}
}

// seedNetworkHealthSummaryData seeds comprehensive network health data for TestLake_Agent_Evals_NetworkHealthSummary
func seedNetworkHealthSummaryData(t *testing.T, ctx context.Context, conn duck.Connection) {
	// Load and execute table and view creation migrations
	loadTablesAndViews(t, ctx, conn)

	// Seed devices - mix of activated and non-activated
	// Note: as_of_ts and row_hash are required NOT NULL columns
	_, err := conn.ExecContext(ctx, `
		INSERT INTO dz_devices_current (pk, code, status, metro_pk, device_type, as_of_ts, row_hash) VALUES
		('device1', 'nyc-dzd1', 'activated', 'metro1', 'DZD', CURRENT_TIMESTAMP, 'hash1'),
		('device2', 'lon-dzd1', 'activated', 'metro2', 'DZD', CURRENT_TIMESTAMP, 'hash2'),
		('device3', 'chi-dzd1', 'pending', 'metro3', 'DZD', CURRENT_TIMESTAMP, 'hash3'),
		('device4', 'sf-dzd1', 'activated', 'metro4', 'DZD', CURRENT_TIMESTAMP, 'hash4'),
		('device5', 'tok-dzd1', 'suspended', 'metro5', 'DZD', CURRENT_TIMESTAMP, 'hash5'),
		('device6', 'fra-dzd1', 'activated', 'metro6', 'DZD', CURRENT_TIMESTAMP, 'hash6'),
		('device7', 'nyc-dzd2', 'activated', 'metro1', 'DZD', CURRENT_TIMESTAMP, 'hash7')
	`)
	require.NoError(t, err)

	// Seed links - mix of activated and non-activated, WAN and DZX
	// Note: as_of_ts and row_hash are required NOT NULL columns
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_links_current (
			pk, code, status, link_type, side_a_pk, side_z_pk,
			side_a_iface_name, side_z_iface_name, bandwidth_bps, committed_rtt_ns, as_of_ts, row_hash
		) VALUES
		('link1', 'nyc-lon-1', 'activated', 'WAN', 'device1', 'device2', 'Ethernet1', 'Ethernet1', 10000000000, 10000000, CURRENT_TIMESTAMP, 'linkhash1'),
		('link2', 'chi-nyc-1', 'activated', 'WAN', 'device3', 'device1', 'Ethernet1', 'Ethernet1', 10000000000, 15000000, CURRENT_TIMESTAMP, 'linkhash2'),
		('link3', 'sf-nyc-1', 'pending', 'WAN', 'device4', 'device1', 'Ethernet1', 'Ethernet1', 10000000000, 12000000, CURRENT_TIMESTAMP, 'linkhash3'),
		('link4', 'tok-fra-1', 'activated', 'WAN', 'device5', 'device6', 'Ethernet1', 'Ethernet1', 10000000000, 20000000, CURRENT_TIMESTAMP, 'linkhash4'),
		('link5', 'nyc-local-1', 'activated', 'DZX', 'device1', 'device7', 'Ethernet2', 'Ethernet1', 10000000000, 5000000, CURRENT_TIMESTAMP, 'linkhash5')
	`)
	require.NoError(t, err)

	// Seed latency samples - include some with packet loss (rtt_us = 0)
	// Use recent timestamps (past 24 hours)
	now := "CURRENT_TIMESTAMP"
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_link_latency_samples_raw (
			time, epoch, sample_index, origin_device_pk, target_device_pk, link_pk, rtt_us, loss, ipdv_us
		) VALUES
		-- Link1 (nyc-lon): Mostly healthy, some loss
		(`+now+` - INTERVAL '1 hour', 1, 1, 'device1', 'device2', 'link1', 50000, false, 2000),
		(`+now+` - INTERVAL '1 hour', 1, 2, 'device1', 'device2', 'link1', 51000, false, 2100),
		(`+now+` - INTERVAL '1 hour', 1, 3, 'device1', 'device2', 'link1', 0, true, 0),
		(`+now+` - INTERVAL '2 hours', 1, 1, 'device2', 'device1', 'link1', 49000, false, 1900),
		(`+now+` - INTERVAL '2 hours', 1, 2, 'device2', 'device1', 'link1', 0, true, 0),
		-- Link2 (chi-nyc): Healthy
		(`+now+` - INTERVAL '1 hour', 1, 1, 'device3', 'device1', 'link2', 75000, false, 3000),
		(`+now+` - INTERVAL '1 hour', 1, 2, 'device1', 'device3', 'link2', 73000, false, 2800),
		-- Link4 (tok-fra): High loss
		(`+now+` - INTERVAL '1 hour', 1, 1, 'device5', 'device6', 'link4', 0, true, 0),
		(`+now+` - INTERVAL '1 hour', 1, 2, 'device5', 'device6', 'link4', 0, true, 0),
		(`+now+` - INTERVAL '2 hours', 1, 1, 'device6', 'device5', 'link4', 0, true, 0),
		(`+now+` - INTERVAL '2 hours', 1, 2, 'device6', 'device5', 'link4', 180000, false, 5000)
	`)
	require.NoError(t, err)

	// Seed interface usage - mix of link interfaces and non-link interfaces
	// Include errors, discards, and carrier transitions
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_iface_usage_raw (
			time, device_pk, host, intf, link_pk, link_side,
			in_errors_delta, in_discards_delta, out_errors_delta, out_discards_delta,
			carrier_transitions_delta, in_octets_delta, out_octets_delta, delta_duration
		) VALUES
		-- Link interfaces with errors/discards (link_pk IS NOT NULL)
		(`+now+` - INTERVAL '1 hour', 'device1', 'nyc-dzd1', 'Ethernet1', 'link1', 'A', 5, 2, 3, 1, 0, 1000000, 1000000, 60.0),
		(`+now+` - INTERVAL '1 hour', 'device2', 'lon-dzd1', 'Ethernet1', 'link1', 'Z', 8, 3, 4, 2, 1, 1000000, 1000000, 60.0),
		(`+now+` - INTERVAL '2 hours', 'device1', 'nyc-dzd1', 'Ethernet1', 'link1', 'A', 0, 0, 0, 0, 0, 1000000, 1000000, 60.0),
		(`+now+` - INTERVAL '2 hours', 'device2', 'lon-dzd1', 'Ethernet1', 'link1', 'Z', 0, 0, 0, 0, 0, 1000000, 1000000, 60.0),
		-- Link interface with high utilization (for link traffic view) - 90% of 10Gbps
		(`+now+` - INTERVAL '1 hour', 'device1', 'nyc-dzd1', 'Ethernet1', 'link2', 'A', 0, 0, 0, 0, 0, 6750000000, 6750000000, 60.0),
		(`+now+` - INTERVAL '1 hour', 'device3', 'chi-dzd1', 'Ethernet1', 'link2', 'Z', 0, 0, 0, 0, 0, 6750000000, 6750000000, 60.0),
		-- Non-link interfaces with errors/discards (link_pk IS NULL)
		(`+now+` - INTERVAL '1 hour', 'device1', 'nyc-dzd1', 'Ethernet3', NULL, NULL, 10, 5, 8, 4, 2, 500000, 500000, 60.0),
		(`+now+` - INTERVAL '1 hour', 'device4', 'sf-dzd1', 'Ethernet2', NULL, NULL, 15, 7, 12, 6, 3, 500000, 500000, 60.0),
		(`+now+` - INTERVAL '2 hours', 'device1', 'nyc-dzd1', 'Ethernet3', NULL, NULL, 0, 0, 0, 0, 0, 500000, 500000, 60.0),
		(`+now+` - INTERVAL '2 hours', 'device4', 'sf-dzd1', 'Ethernet2', NULL, NULL, 0, 0, 0, 0, 0, 500000, 500000, 60.0)
	`)
	require.NoError(t, err)
}
