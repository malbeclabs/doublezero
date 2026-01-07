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

func TestLake_Agent_Evals_Anthropic_MulticastSubscriberBandwidth(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_MulticastSubscriberBandwidth(t)
}

func runTest_MulticastSubscriberBandwidth(t *testing.T) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)

	// Set up test data
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed multicast subscriber bandwidth data
	seedMulticastSubscriberBandwidthData(t, ctx, conn)

	// Set up agent with Anthropic LLM client
	agentInstance := setupAgent(t, ctx, db, newAnthropicLLMClient, debug, debugLevel, nil)

	// Run the query
	var output bytes.Buffer
	question := "which multicast subscriber consumes the most bandwidth"
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

	// Basic validation - the response should identify the subscriber
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

	// The response should be non-empty and contain subscriber identification
	require.Greater(t, len(response), 50, "Response should be substantial")

	// Validate that the response contains specific data points from the seeded test data
	validateMulticastSubscriberBandwidthResponse(t, response)

	// Evaluate with Ollama
	isCorrect, reason, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not correctly answer the question. Reason: %s", reason)
}

// validateMulticastSubscriberBandwidthResponse validates the response for TestLake_Agent_Evals_Anthropic_MulticastSubscriberBandwidth
func validateMulticastSubscriberBandwidthResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should mention subscriber or user
	subscriberMentioned := strings.Contains(responseLower, "subscriber") ||
		strings.Contains(responseLower, "user") ||
		strings.Contains(responseLower, "client")
	require.True(t, subscriberMentioned,
		"Response should mention subscriber, user, or client. Got: %s",
		truncateForError(response, 200))

	// Should mention bandwidth or consumption
	bandwidthMentioned := strings.Contains(responseLower, "bandwidth") ||
		strings.Contains(responseLower, "consum") ||
		strings.Contains(responseLower, "traffic") ||
		strings.Contains(responseLower, "throughput") ||
		strings.Contains(responseLower, "bps") ||
		strings.Contains(responseLower, "mbps") ||
		strings.Contains(responseLower, "gbps")
	require.True(t, bandwidthMentioned,
		"Response should mention bandwidth, consumption, traffic, or throughput. Got: %s",
		truncateForError(response, 200))

	// Should mention multicast
	multicastMentioned := strings.Contains(responseLower, "multicast")
	require.True(t, multicastMentioned,
		"Response should mention multicast. Got: %s",
		truncateForError(response, 200))

	// Should identify the specific subscriber with highest bandwidth
	// Expected: owner3 with client_ip='3.3.3.3' consumes the most bandwidth (15,000,000,000 bytes = 15 GB in the test period)
	// CRITICAL: Must include owner_pk and client_ip - these are the stable user identifiers
	// Note: User pk (pubkey) is NOT stable - it changes after disconnects/reconnects. Only (owner_pk, client_ip) is stable.
	owner3Mentioned := strings.Contains(responseLower, "owner3")
	// Accept explicit client IP (3.3.3.3) or dz_ip (10.0.0.3) or tunnel_id (503) - all are stable identifiers
	clientIPMentioned := strings.Contains(response, "3.3.3.3") ||
		strings.Contains(response, "10.0.0.3") ||
		(strings.Contains(responseLower, "owner3") && strings.Contains(response, "503"))
	// At least owner_pk must be mentioned
	require.True(t, owner3Mentioned,
		"Response should mention owner_pk 'owner3' (part of the stable user identifier). Got: %s",
		truncateForError(response, 200))
	// Client IP or dz_ip should be mentioned (both are stable identifiers when combined with owner_pk)
	require.True(t, clientIPMentioned,
		"Response should mention client IP '3.3.3.3' or dz_ip '10.0.0.3' or identify subscriber via tunnel_id along with owner_pk. Got: %s",
		truncateForError(response, 200))

	// Should contain numeric data (bandwidth amounts, bytes, etc.)
	hasNumbers := false
	for _, char := range response {
		if char >= '0' && char <= '9' {
			hasNumbers = true
			break
		}
	}
	require.True(t, hasNumbers,
		"Response should contain numeric data (bandwidth amounts, bytes, etc.). Got: %s",
		truncateForError(response, 200))

	// Should mention the bandwidth amount for the top subscriber
	// user3 consumes 15,000,000,000 bytes (15 GB) in the test period
	// Accept various formats: "15", "15000", "15gb", "15 gb", "15,000", etc.
	hasBandwidthAmount := strings.Contains(response, "15") ||
		strings.Contains(responseLower, "fifteen") ||
		strings.Contains(responseLower, "15000")
	require.True(t, hasBandwidthAmount,
		"Response should mention the bandwidth amount for the top subscriber (15 GB or similar). Got: %s",
		truncateForError(response, 200))

	// Log the response for debugging
	t.Logf("Response mentions: subscriber=%v, bandwidth=%v, multicast=%v, owner3=%v, client_ip=%v, numbers=%v, bandwidth_amount=%v. Full response: %s",
		subscriberMentioned, bandwidthMentioned, multicastMentioned, owner3Mentioned, clientIPMentioned, hasNumbers, hasBandwidthAmount,
		truncateForError(response, 500))
}

// seedMulticastSubscriberBandwidthData seeds multicast subscriber bandwidth data for TestLake_Agent_Evals_Anthropic_MulticastSubscriberBandwidth
// Sets up multiple subscribers with different bandwidth consumption, where user3 consumes the most
func seedMulticastSubscriberBandwidthData(t *testing.T, ctx context.Context, conn duck.Connection) {
	// Load and execute table and view creation migrations
	loadTablesAndViews(t, ctx, conn)

	// Seed devices
	_, err := conn.ExecContext(ctx, `
		INSERT INTO dz_devices_current (pk, code, status, metro_pk, device_type, as_of_ts, row_hash) VALUES
		('device1', 'nyc-dzd1', 'activated', 'metro1', 'DZD', CURRENT_TIMESTAMP, 'hash1'),
		('device2', 'lon-dzd1', 'activated', 'metro2', 'DZD', CURRENT_TIMESTAMP, 'hash2')
	`)
	require.NoError(t, err)

	// Seed DZ users (subscribers)
	// user1: Low bandwidth (5 GB)
	// user2: Medium bandwidth (10 GB)
	// user3: High bandwidth (15 GB) - this is the answer
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_current (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id, as_of_ts, row_hash) VALUES
		('user1', 'owner1', 'activated', 'IBRL', '1.1.1.1', '10.0.0.1', 'device1', 501, CURRENT_TIMESTAMP, 'userhash1'),
		('user2', 'owner2', 'activated', 'IBRL', '2.2.2.2', '10.0.0.2', 'device1', 502, CURRENT_TIMESTAMP, 'userhash2'),
		('user3', 'owner3', 'activated', 'IBRL', '3.3.3.3', '10.0.0.3', 'device1', 503, CURRENT_TIMESTAMP, 'userhash3')
	`)
	require.NoError(t, err)

	// Seed history table
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_history (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id, valid_from, valid_to, op, row_hash) VALUES
		('user1', 'owner1', 'activated', 'IBRL', '1.1.1.1', '10.0.0.1', 'device1', 501, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'userhash1'),
		('user2', 'owner2', 'activated', 'IBRL', '2.2.2.2', '10.0.0.2', 'device1', 502, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'userhash2'),
		('user3', 'owner3', 'activated', 'IBRL', '3.3.3.3', '10.0.0.3', 'device1', 503, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'userhash3')
	`)
	require.NoError(t, err)

	// Seed interface usage with multicast traffic on tunnel interfaces
	// Use recent timestamps (past 24 hours)
	// CRITICAL: On tunnel interfaces, in_multicast_pkts_delta and out_multicast_pkts_delta are NOT reliable (will be empty/NULL)
	// Must use in_pkts_delta and out_pkts_delta for multicast traffic on tunnel interfaces
	// user1 (tunnel 501): 5,000,000,000 bytes (5 GB) - low bandwidth
	// user2 (tunnel 502): 10,000,000,000 bytes (10 GB) - medium bandwidth
	// user3 (tunnel 503): 15,000,000,000 bytes (15 GB) - high bandwidth (most)
	now := "CURRENT_TIMESTAMP"
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_device_iface_usage_raw (
			time, device_pk, host, intf, user_tunnel_id, link_pk, link_side,
			in_octets_delta, out_octets_delta, in_multicast_pkts_delta, out_multicast_pkts_delta,
			in_pkts_delta, out_pkts_delta, delta_duration
		) VALUES
		-- user1 (tunnel 501): 5 GB total (low bandwidth)
		-- Note: in_multicast_pkts_delta and out_multicast_pkts_delta are NULL on tunnel interfaces (not reliable)
		-- Must use out_pkts_delta and out_octets_delta for multicast traffic
		(`+now+` - INTERVAL '1 hour', 'device1', 'nyc-dzd1', 'tunnel501', 501, NULL, NULL, 2500000000, 2500000000, NULL, NULL, 5000000, 5000000, 3600.0),
		(`+now+` - INTERVAL '2 hours', 'device1', 'nyc-dzd1', 'tunnel501', 501, NULL, NULL, 2500000000, 2500000000, NULL, NULL, 5000000, 5000000, 3600.0),
		-- user2 (tunnel 502): 10 GB total (medium bandwidth)
		(`+now+` - INTERVAL '1 hour', 'device1', 'nyc-dzd1', 'tunnel502', 502, NULL, NULL, 5000000000, 5000000000, NULL, NULL, 10000000, 10000000, 3600.0),
		(`+now+` - INTERVAL '2 hours', 'device1', 'nyc-dzd1', 'tunnel502', 502, NULL, NULL, 5000000000, 5000000000, NULL, NULL, 10000000, 10000000, 3600.0),
		-- user3 (tunnel 503): 15 GB total (high bandwidth - most)
		(`+now+` - INTERVAL '1 hour', 'device1', 'nyc-dzd1', 'tunnel503', 503, NULL, NULL, 7500000000, 7500000000, NULL, NULL, 15000000, 15000000, 3600.0),
		(`+now+` - INTERVAL '2 hours', 'device1', 'nyc-dzd1', 'tunnel503', 503, NULL, NULL, 7500000000, 7500000000, NULL, NULL, 15000000, 15000000, 3600.0)
	`)
	require.NoError(t, err)
}
