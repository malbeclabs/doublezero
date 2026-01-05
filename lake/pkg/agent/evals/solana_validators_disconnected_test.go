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

func TestLake_Agent_Evals_Anthropic_SolanaValidatorsDisconnected(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SolanaValidatorsDisconnected(t)
}

func runTest_SolanaValidatorsDisconnected(t *testing.T) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)

	// Set up test data
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed Solana validators disconnected data
	seedSolanaValidatorsDisconnectedData(t, ctx, conn)

	// Set up agent with Anthropic LLM client
	agentInstance := setupAgent(t, ctx, db, newAnthropicLLMClient, debug, debugLevel, nil)

	// Run the query
	var output bytes.Buffer
	question := "which solana validators disconnected from dz in the past 24 hours"
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

	// Basic validation - the response should identify disconnected validators
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

	// Validate the response
	validateSolanaValidatorsDisconnectedResponse(t, response)

	// Evaluate with Ollama
	isCorrect, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not correctly answer the question")
}

// validateSolanaValidatorsDisconnectedResponse validates that the response includes required elements
func validateSolanaValidatorsDisconnectedResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should mention disconnection or disconnected
	disconnectMentioned := strings.Contains(responseLower, "disconnect") ||
		strings.Contains(responseLower, "disconnected") ||
		strings.Contains(responseLower, "no longer connected") ||
		strings.Contains(responseLower, "ceased connection")
	require.True(t, disconnectMentioned,
		"Response should mention disconnection. Got: %s",
		truncateForError(response, 200))

	// Should mention past 24 hours or recent time period
	timeMentioned := strings.Contains(responseLower, "24 hours") ||
		strings.Contains(responseLower, "past day") ||
		strings.Contains(responseLower, "yesterday") ||
		strings.Contains(responseLower, "recent") ||
		strings.Contains(responseLower, "last 24")
	require.True(t, timeMentioned,
		"Response should mention the time period (past 24 hours). Got: %s",
		truncateForError(response, 200))

	// CRITICAL: Should mention vote_pubkey for each disconnected validator
	// Expected disconnected validators: vote1, vote2, vote3, vote6
	// vote1 disconnected 12 hours ago (gossip_ip changed from 10.0.0.1 to 192.168.1.1)
	// vote2 disconnected 6 hours ago (user disconnected, gossip_ip stayed same)
	// vote3 disconnected 18 hours ago (user disconnected, gossip_ip stayed same)
	// vote6 disconnected 2 hours ago, but had reconnected 8 hours ago after disconnecting 15 hours ago (flapping - should be included as currently disconnected)
	vote1Mentioned := strings.Contains(responseLower, "vote1")
	vote2Mentioned := strings.Contains(responseLower, "vote2")
	vote3Mentioned := strings.Contains(responseLower, "vote3")
	vote6Mentioned := strings.Contains(responseLower, "vote6")
	// At least one vote_pubkey should be mentioned
	atLeastOneVotePubkey := vote1Mentioned || vote2Mentioned || vote3Mentioned || vote6Mentioned
	require.True(t, atLeastOneVotePubkey,
		"Response should mention at least one vote_pubkey (vote1, vote2, vote3, or vote6). Got: %s",
		truncateForError(response, 200))

	// CRITICAL: Should mention IP addresses (gossip_ip, dz_ip, or client_ip) for each disconnected validator
	// vote1: gossip_ip changed from 10.0.0.1 to 192.168.1.1, client_ip='1.1.1.1' (should mention one of these)
	// vote2: gossip_ip was 10.0.0.2 (dz_ip was 10.0.0.2), client_ip='2.2.2.2'
	// vote3: gossip_ip was 10.0.0.3 (dz_ip was 10.0.0.3), client_ip='3.3.3.3'
	// vote6: gossip_ip was 10.0.0.6 (dz_ip was 10.0.0.6), client_ip='6.6.6.6'
	ipMentioned := strings.Contains(response, "10.0.0.1") ||
		strings.Contains(response, "10.0.0.2") ||
		strings.Contains(response, "10.0.0.3") ||
		strings.Contains(response, "10.0.0.6") ||
		strings.Contains(response, "192.168.1.1") ||
		strings.Contains(response, "1.1.1.1") || // client_ip for vote1
		strings.Contains(response, "2.2.2.2") || // client_ip for vote2
		strings.Contains(response, "3.3.3.3") || // client_ip for vote3
		strings.Contains(response, "6.6.6.6") // client_ip for vote6
	require.True(t, ipMentioned,
		"Response should mention IP addresses (gossip_ip, dz_ip, or client_ip) for disconnected validators. Got: %s",
		truncateForError(response, 200))

	// Should NOT mention validators that reconnected (vote4 reconnected after disconnecting)
	// vote4 should NOT be listed as a disconnected validator
	vote4Mentioned := strings.Contains(responseLower, "vote4")
	// If vote4 is mentioned, it must be explicitly excluded or mentioned as reconnected
	if vote4Mentioned {
		// Accept if explicitly excluded or mentioned as reconnected
		explicitlyExcluded := strings.Contains(responseLower, "vote4") &&
			(strings.Contains(responseLower, "reconnect") ||
				strings.Contains(responseLower, "reconnected") ||
				strings.Contains(responseLower, "connected again") ||
				strings.Contains(responseLower, "not included") ||
				strings.Contains(responseLower, "excluded") ||
				strings.Contains(responseLower, "no longer disconnected"))
		require.True(t, explicitlyExcluded,
			"Response mentions vote4 but should exclude it (it reconnected) or explicitly state it reconnected. Got: %s",
			truncateForError(response, 200))
	}
	// Ideally, vote4 should not be mentioned at all in the list of disconnected validators
	// But we'll be lenient if it's explicitly excluded

	// Should contain numeric data (stake amounts, timestamps, etc.)
	hasNumbers := false
	for _, char := range response {
		if char >= '0' && char <= '9' {
			hasNumbers = true
			break
		}
	}
	require.True(t, hasNumbers,
		"Response should contain numeric data (stake amounts, timestamps, etc.). Got: %s",
		truncateForError(response, 200))
}

// seedSolanaValidatorsDisconnectedData seeds data for testing disconnected validators
// Test cases:
// - vote1: Disconnected 12 hours ago, gossip_ip changed from 10.0.0.1 to 192.168.1.1 (validator changed IP)
// - vote2: Disconnected 6 hours ago, user disconnected (gossip_ip stayed 10.0.0.2)
// - vote3: Disconnected 18 hours ago, user disconnected (gossip_ip stayed 10.0.0.3)
// - vote4: Disconnected 20 hours ago but reconnected 10 hours ago (should NOT be in results)
// - vote5: Still connected (should NOT be in results)
// - vote6: Disconnected 15 hours ago, reconnected 8 hours ago, disconnected again 2 hours ago (flapping - should be in results as currently disconnected)
func seedSolanaValidatorsDisconnectedData(t *testing.T, ctx context.Context, conn duck.Connection) {
	loadTablesAndViews(t, ctx, conn)

	// Seed metros
	_, err := conn.ExecContext(ctx, `
		INSERT INTO dz_metros_current (pk, code, name, as_of_ts, row_hash) VALUES
		('metro1', 'nyc', 'New York', CURRENT_TIMESTAMP, 'metrohash1')
	`)
	require.NoError(t, err)

	// Seed devices
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_devices_current (pk, code, status, metro_pk, device_type, as_of_ts, row_hash) VALUES
		('device1', 'nyc-dzd1', 'activated', 'metro1', 'DZD', CURRENT_TIMESTAMP, 'devicehash1')
	`)
	require.NoError(t, err)

	now := "CURRENT_TIMESTAMP"

	// Seed DZ users history
	// user1: Connected 30 days ago, disconnected 12 hours ago (for vote1 - validator changed IP)
	// user2: Connected 30 days ago, disconnected 6 hours ago (for vote2)
	// user3: Connected 30 days ago, disconnected 18 hours ago (for vote3)
	// user4: Connected 30 days ago, disconnected 20 hours ago, reconnected 10 hours ago (for vote4 - should not appear)
	// user5: Connected 30 days ago, still connected (for vote5 - should not appear)
	// user6: Connected 30 days ago, disconnected 15 hours ago, reconnected 8 hours ago, disconnected 2 hours ago (for vote6 - flapping, should appear as currently disconnected)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_history (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id, valid_from, valid_to, op, row_hash) VALUES
		-- user1: Disconnected 12 hours ago (vote1 - validator changed IP)
		('user1', 'owner1', 'activated', 'IBRL', '1.1.1.1', '10.0.0.1', 'device1', 501, `+now+` - INTERVAL '30 days', `+now+` - INTERVAL '12 hours', 'I', 'userhash1'),
		-- user2: Disconnected 6 hours ago (vote2)
		('user2', 'owner2', 'activated', 'IBRL', '2.2.2.2', '10.0.0.2', 'device1', 502, `+now+` - INTERVAL '30 days', `+now+` - INTERVAL '6 hours', 'I', 'userhash2'),
		-- user3: Disconnected 18 hours ago (vote3)
		('user3', 'owner3', 'activated', 'IBRL', '3.3.3.3', '10.0.0.3', 'device1', 503, `+now+` - INTERVAL '30 days', `+now+` - INTERVAL '18 hours', 'I', 'userhash3'),
		-- user4: Disconnected 20 hours ago, reconnected 10 hours ago (vote4 - should not appear as disconnected)
		('user4', 'owner4', 'activated', 'IBRL', '4.4.4.4', '10.0.0.4', 'device1', 504, `+now+` - INTERVAL '30 days', `+now+` - INTERVAL '20 hours', 'I', 'userhash4'),
		('user4', 'owner4', 'activated', 'IBRL', '4.4.4.4', '10.0.0.4', 'device1', 504, `+now+` - INTERVAL '10 hours', NULL, 'I', 'userhash4'),
		-- user5: Still connected (vote5 - should not appear)
		('user5', 'owner5', 'activated', 'IBRL', '5.5.5.5', '10.0.0.5', 'device1', 505, `+now+` - INTERVAL '30 days', NULL, 'I', 'userhash5'),
		-- user6: Disconnected 15 hours ago, reconnected 8 hours ago, disconnected 2 hours ago (vote6 - flapping, should appear as currently disconnected)
		('user6', 'owner6', 'activated', 'IBRL', '6.6.6.6', '10.0.0.6', 'device1', 506, `+now+` - INTERVAL '30 days', `+now+` - INTERVAL '15 hours', 'I', 'userhash6'),
		('user6', 'owner6', 'activated', 'IBRL', '6.6.6.6', '10.0.0.6', 'device1', 506, `+now+` - INTERVAL '8 hours', `+now+` - INTERVAL '2 hours', 'I', 'userhash6')
	`)
	require.NoError(t, err)

	// Seed DZ users current (only user4 and user5 are currently connected; user6 is disconnected)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_current (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id, as_of_ts, row_hash) VALUES
		('user4', 'owner4', 'activated', 'IBRL', '4.4.4.4', '10.0.0.4', 'device1', 504, CURRENT_TIMESTAMP, 'userhash4'),
		('user5', 'owner5', 'activated', 'IBRL', '5.5.5.5', '10.0.0.5', 'device1', 505, CURRENT_TIMESTAMP, 'userhash5')
	`)
	require.NoError(t, err)

	// Seed Solana gossip nodes history
	// node1 (vote1): gossip_ip changed from 10.0.0.1 to 192.168.1.1 (validator changed IP)
	// node2 (vote2): gossip_ip stayed 10.0.0.2
	// node3 (vote3): gossip_ip stayed 10.0.0.3
	// node4 (vote4): gossip_ip stayed 10.0.0.4 (reconnected)
	// node5 (vote5): gossip_ip stayed 10.0.0.5 (still connected)
	// node6 (vote6): gossip_ip stayed 10.0.0.6 (flapping - disconnected 2 hours ago)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_gossip_nodes_history (
			pubkey, gossip_ip, tpuquic_ip, version, epoch, gossip_port, tpuquic_port, valid_from, valid_to, op, row_hash
		) VALUES
		-- node1 (vote1): gossip_ip was 10.0.0.1, changed to 192.168.1.1 (disconnected 12 hours ago)
		('node1', '10.0.0.1', '10.0.0.1', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '30 days', `+now+` - INTERVAL '12 hours', 'I', 'nodehash1'),
		('node1', '192.168.1.1', '192.168.1.1', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '12 hours', NULL, 'I', 'nodehash1'),
		-- node2 (vote2): gossip_ip stayed 10.0.0.2 (disconnected 6 hours ago)
		('node2', '10.0.0.2', '10.0.0.2', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '30 days', NULL, 'I', 'nodehash2'),
		-- node3 (vote3): gossip_ip stayed 10.0.0.3 (disconnected 18 hours ago)
		('node3', '10.0.0.3', '10.0.0.3', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '30 days', NULL, 'I', 'nodehash3'),
		-- node4 (vote4): gossip_ip stayed 10.0.0.4 (reconnected 10 hours ago)
		('node4', '10.0.0.4', '10.0.0.4', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '30 days', NULL, 'I', 'nodehash4'),
		-- node5 (vote5): gossip_ip stayed 10.0.0.5 (still connected)
		('node5', '10.0.0.5', '10.0.0.5', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '30 days', NULL, 'I', 'nodehash5'),
		-- node6 (vote6): gossip_ip stayed 10.0.0.6 (flapping - disconnected 2 hours ago)
		('node6', '10.0.0.6', '10.0.0.6', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '30 days', NULL, 'I', 'nodehash6')
	`)
	require.NoError(t, err)

	// Seed Solana gossip nodes current
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_gossip_nodes_current (
			pubkey, gossip_ip, tpuquic_ip, version, epoch, gossip_port, tpuquic_port, as_of_ts, row_hash
		) VALUES
		('node1', '192.168.1.1', '192.168.1.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash1'),
		('node2', '10.0.0.2', '10.0.0.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash2'),
		('node3', '10.0.0.3', '10.0.0.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash3'),
		('node4', '10.0.0.4', '10.0.0.4', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash4'),
		('node5', '10.0.0.5', '10.0.0.5', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash5'),
		('node6', '10.0.0.6', '10.0.0.6', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash6')
	`)
	require.NoError(t, err)

	// Seed Solana vote accounts history
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_vote_accounts_history (
			vote_pubkey, node_pubkey, epoch_vote_account, epoch, activated_stake_lamports, commission_percentage, valid_from, valid_to, op, row_hash
		) VALUES
		('vote1', 'node1', 'true', 100, 1000000000000, 5, `+now+` - INTERVAL '30 days', NULL, 'I', 'votehash1'),
		('vote2', 'node2', 'true', 100, 1500000000000, 5, `+now+` - INTERVAL '30 days', NULL, 'I', 'votehash2'),
		('vote3', 'node3', 'true', 100, 1200000000000, 5, `+now+` - INTERVAL '30 days', NULL, 'I', 'votehash3'),
		('vote4', 'node4', 'true', 100, 2000000000000, 5, `+now+` - INTERVAL '30 days', NULL, 'I', 'votehash4'),
		('vote5', 'node5', 'true', 100, 1800000000000, 5, `+now+` - INTERVAL '30 days', NULL, 'I', 'votehash5'),
		('vote6', 'node6', 'true', 100, 1600000000000, 5, `+now+` - INTERVAL '30 days', NULL, 'I', 'votehash6')
	`)
	require.NoError(t, err)

	// Seed Solana vote accounts current
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_vote_accounts_current (
			vote_pubkey, node_pubkey, epoch_vote_account, epoch, activated_stake_lamports, commission_percentage, as_of_ts, row_hash
		) VALUES
		('vote1', 'node1', 'true', 100, 1000000000000, 5, CURRENT_TIMESTAMP, 'votehash1'),
		('vote2', 'node2', 'true', 100, 1500000000000, 5, CURRENT_TIMESTAMP, 'votehash2'),
		('vote3', 'node3', 'true', 100, 1200000000000, 5, CURRENT_TIMESTAMP, 'votehash3'),
		('vote4', 'node4', 'true', 100, 2000000000000, 5, CURRENT_TIMESTAMP, 'votehash4'),
		('vote5', 'node5', 'true', 100, 1800000000000, 5, CURRENT_TIMESTAMP, 'votehash5'),
		('vote6', 'node6', 'true', 100, 1600000000000, 5, CURRENT_TIMESTAMP, 'votehash6')
	`)
	require.NoError(t, err)
}
