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

func TestLake_Agent_Evals_Anthropic_SolanaValidatorsConnectedCount(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SolanaValidatorsConnectedCount(t)
}

func runTest_SolanaValidatorsConnectedCount(t *testing.T) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)

	// Set up test data
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed Solana validators connected count data
	seedSolanaValidatorsConnectedCountData(t, ctx, conn)

	// Set up agent with Anthropic LLM client
	agentInstance := setupAgent(t, ctx, db, newAnthropicLLMClient, debug, debugLevel, nil)

	// Run the query
	var output bytes.Buffer
	question := "How many Solana validators connected to dz in the last day"
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

	// Basic validation - the response should identify connected validators
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
	validateSolanaValidatorsConnectedCountResponse(t, response)

	// Evaluate with Ollama
	isCorrect, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not correctly answer the question")
}

// validateSolanaValidatorsConnectedCountResponse validates that the response correctly handles the ambiguous question
func validateSolanaValidatorsConnectedCountResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should mention connection or connected
	connectMentioned := strings.Contains(responseLower, "connect") ||
		strings.Contains(responseLower, "connected") ||
		strings.Contains(responseLower, "on dz")
	require.True(t, connectMentioned,
		"Response should mention connection or connected. Got: %s",
		truncateForError(response, 200))

	// Should mention time period (last day, 24 hours, etc.)
	timeMentioned := strings.Contains(responseLower, "24 hours") ||
		strings.Contains(responseLower, "last day") ||
		strings.Contains(responseLower, "past day") ||
		strings.Contains(responseLower, "yesterday") ||
		strings.Contains(responseLower, "recent")
	require.True(t, timeMentioned,
		"Response should mention time period (24 hours, last day, etc.). Got: %s",
		truncateForError(response, 200))

	// CRITICAL: The response should clarify what it's counting OR use the most sensible interpretation
	// Expected: The agent should either:
	// 1. Clarify the question and provide both counts (currently connected vs newly connected)
	// 2. Use the most sensible interpretation: "newly connected" (weren't connected 24 hours ago)
	// 3. If using first_connection_events, should detect bulk ingestion artifacts

	// Should contain a numeric count
	hasNumbers := false
	for _, char := range response {
		if char >= '0' && char <= '9' {
			hasNumbers = true
			break
		}
	}
	require.True(t, hasNumbers,
		"Response should contain numeric data (count of validators). Got: %s",
		truncateForError(response, 200))

	// CRITICAL: The count should be reasonable - if the agent reports a very high number (like 372)
	// and mentions bulk ingestion or identical timestamps, that's actually good (shows awareness)
	// But ideally, the agent should use the historical comparison method to get the correct count

	// The expected answer: 3 validators newly connected (vote1, vote2, and vote5)
	// vote3 and vote4 were already connected before the 24-hour window (should NOT be counted)
	// vote5 connected 12 hours ago (same timestamp as vote1) - since it's currently connected and wasn't connected 24 hours ago, it IS newly connected and should be counted

	// Should mention vote_pubkey for at least some validators if listing them
	// OR should provide a count that makes sense
	voteMentioned := strings.Contains(responseLower, "vote1") ||
		strings.Contains(responseLower, "vote2") ||
		strings.Contains(responseLower, "vote3") ||
		strings.Contains(responseLower, "vote4") ||
		strings.Contains(responseLower, "vote5") ||
		strings.Contains(responseLower, "validator")
	// This is optional - the agent might just provide a count without listing validators
	// But if it does list validators, it should include vote_pubkey

	// Log the response for debugging
	t.Logf("Response mentions: connect=%v, time=%v, numbers=%v, vote=%v. Full response: %s",
		connectMentioned, timeMentioned, hasNumbers, voteMentioned,
		truncateForError(response, 500))
}

// seedSolanaValidatorsConnectedCountData seeds data for testing "how many validators connected in the last day"
// Test scenario:
// - vote3 and vote4: Already connected before 24 hours ago (should NOT be counted as "newly connected")
// - vote1: Newly connected 12 hours ago (should be counted)
// - vote2: Newly connected 6 hours ago (should be counted)
// - vote5: Connected 12 hours ago (same timestamp as vote1) - since it's currently connected and wasn't connected 24 hours ago, it IS newly connected
// Expected answer for "newly connected": 3 validators (vote1, vote2, and vote5)
// Expected answer for "currently connected": 5 validators (all are currently connected)
func seedSolanaValidatorsConnectedCountData(t *testing.T, ctx context.Context, conn duck.Connection) {
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
	// T0 = 30 days ago (vote3 and vote4 connected)
	// T1 = 24 hours ago (the cutoff for "last day")
	// T1+12h = 12 hours ago (vote1 and vote5 connect - bulk ingestion scenario)
	// T1+18h = 6 hours ago (vote2 connects)

	// Seed DZ users history
	// user1: Connected 12 hours ago (for vote1 - newly connected)
	// user2: Connected 6 hours ago (for vote2 - newly connected)
	// user3: Connected 30 days ago (for vote3 - already connected, should NOT be counted)
	// user4: Connected 30 days ago (for vote4 - already connected, should NOT be counted)
	// user5: Connected 12 hours ago (for vote5 - bulk ingestion, same timestamp as vote1)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_history (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id, valid_from, valid_to, op, row_hash) VALUES
		-- user1: Connected 12 hours ago (vote1 - newly connected)
		('user1', 'owner1', 'activated', 'IBRL', '1.1.1.1', '10.0.0.1', 'device1', 501, `+now+` - INTERVAL '12 hours', NULL, 'I', 'userhash1'),
		-- user2: Connected 6 hours ago (vote2 - newly connected)
		('user2', 'owner2', 'activated', 'IBRL', '2.2.2.2', '10.0.0.2', 'device1', 502, `+now+` - INTERVAL '6 hours', NULL, 'I', 'userhash2'),
		-- user3: Connected 30 days ago (vote3 - already connected, should NOT be counted)
		('user3', 'owner3', 'activated', 'IBRL', '3.3.3.3', '10.0.0.3', 'device1', 503, `+now+` - INTERVAL '30 days', NULL, 'I', 'userhash3'),
		-- user4: Connected 30 days ago (vote4 - already connected, should NOT be counted)
		('user4', 'owner4', 'activated', 'IBRL', '4.4.4.4', '10.0.0.4', 'device1', 504, `+now+` - INTERVAL '30 days', NULL, 'I', 'userhash4'),
		-- user5: Connected 12 hours ago (vote5 - bulk ingestion, same timestamp as vote1)
		('user5', 'owner5', 'activated', 'IBRL', '5.5.5.5', '10.0.0.5', 'device1', 505, `+now+` - INTERVAL '12 hours', NULL, 'I', 'userhash5')
	`)
	require.NoError(t, err)

	// Seed DZ users current (all are currently connected)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_current (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id, as_of_ts, row_hash) VALUES
		('user1', 'owner1', 'activated', 'IBRL', '1.1.1.1', '10.0.0.1', 'device1', 501, CURRENT_TIMESTAMP, 'userhash1'),
		('user2', 'owner2', 'activated', 'IBRL', '2.2.2.2', '10.0.0.2', 'device1', 502, CURRENT_TIMESTAMP, 'userhash2'),
		('user3', 'owner3', 'activated', 'IBRL', '3.3.3.3', '10.0.0.3', 'device1', 503, CURRENT_TIMESTAMP, 'userhash3'),
		('user4', 'owner4', 'activated', 'IBRL', '4.4.4.4', '10.0.0.4', 'device1', 504, CURRENT_TIMESTAMP, 'userhash4'),
		('user5', 'owner5', 'activated', 'IBRL', '5.5.5.5', '10.0.0.5', 'device1', 505, CURRENT_TIMESTAMP, 'userhash5')
	`)
	require.NoError(t, err)

	// Seed Solana gossip nodes history
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_gossip_nodes_history (
			pubkey, gossip_ip, tpuquic_ip, version, epoch, gossip_port, tpuquic_port, valid_from, valid_to, op, row_hash
		) VALUES
		-- node1 (vote1): Connected 12 hours ago
		('node1', '10.0.0.1', '10.0.0.1', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '12 hours', NULL, 'I', 'nodehash1'),
		-- node2 (vote2): Connected 6 hours ago
		('node2', '10.0.0.2', '10.0.0.2', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '6 hours', NULL, 'I', 'nodehash2'),
		-- node3 (vote3): Connected 30 days ago
		('node3', '10.0.0.3', '10.0.0.3', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '30 days', NULL, 'I', 'nodehash3'),
		-- node4 (vote4): Connected 30 days ago
		('node4', '10.0.0.4', '10.0.0.4', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '30 days', NULL, 'I', 'nodehash4'),
		-- node5 (vote5): Connected 12 hours ago (bulk ingestion, same timestamp as node1)
		('node5', '10.0.0.5', '10.0.0.5', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '12 hours', NULL, 'I', 'nodehash5')
	`)
	require.NoError(t, err)

	// Seed Solana gossip nodes current
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_gossip_nodes_current (
			pubkey, gossip_ip, tpuquic_ip, version, epoch, gossip_port, tpuquic_port, as_of_ts, row_hash
		) VALUES
		('node1', '10.0.0.1', '10.0.0.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash1'),
		('node2', '10.0.0.2', '10.0.0.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash2'),
		('node3', '10.0.0.3', '10.0.0.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash3'),
		('node4', '10.0.0.4', '10.0.0.4', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash4'),
		('node5', '10.0.0.5', '10.0.0.5', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash5')
	`)
	require.NoError(t, err)

	// Seed Solana vote accounts history
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_vote_accounts_history (
			vote_pubkey, node_pubkey, epoch_vote_account, epoch, activated_stake_lamports, commission_percentage, valid_from, valid_to, op, row_hash
		) VALUES
		-- vote1: Connected 12 hours ago, stake 1M SOL
		('vote1', 'node1', 'true', 100, 1000000000000, 5, `+now+` - INTERVAL '12 hours', NULL, 'I', 'votehash1'),
		-- vote2: Connected 6 hours ago, stake 1M SOL
		('vote2', 'node2', 'true', 100, 1000000000000, 5, `+now+` - INTERVAL '6 hours', NULL, 'I', 'votehash2'),
		-- vote3: Connected 30 days ago, stake 1M SOL
		('vote3', 'node3', 'true', 100, 1000000000000, 5, `+now+` - INTERVAL '30 days', NULL, 'I', 'votehash3'),
		-- vote4: Connected 30 days ago, stake 1M SOL
		('vote4', 'node4', 'true', 100, 1000000000000, 5, `+now+` - INTERVAL '30 days', NULL, 'I', 'votehash4'),
		-- vote5: Connected 12 hours ago (bulk ingestion), stake 1M SOL
		('vote5', 'node5', 'true', 100, 1000000000000, 5, `+now+` - INTERVAL '12 hours', NULL, 'I', 'votehash5')
	`)
	require.NoError(t, err)

	// Seed Solana vote accounts current
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_vote_accounts_current (
			vote_pubkey, node_pubkey, epoch_vote_account, epoch, activated_stake_lamports, commission_percentage, as_of_ts, row_hash
		) VALUES
		('vote1', 'node1', 'true', 100, 1000000000000, 5, CURRENT_TIMESTAMP, 'votehash1'),
		('vote2', 'node2', 'true', 100, 1000000000000, 5, CURRENT_TIMESTAMP, 'votehash2'),
		('vote3', 'node3', 'true', 100, 1000000000000, 5, CURRENT_TIMESTAMP, 'votehash3'),
		('vote4', 'node4', 'true', 100, 1000000000000, 5, CURRENT_TIMESTAMP, 'votehash4'),
		('vote5', 'node5', 'true', 100, 1000000000000, 5, CURRENT_TIMESTAMP, 'votehash5')
	`)
	require.NoError(t, err)

	// The solana_validator_dz_connection_events view will be automatically populated from solana_validator_dz_overlaps_windowed
	// The view should show:
	// - vote1: dz_connected event 12 hours ago (newly connected)
	// - vote2: dz_connected event 6 hours ago (newly connected)
	// - vote3: dz_connected event 30 days ago (already connected, should NOT be counted)
	// - vote4: dz_connected event 30 days ago (already connected, should NOT be counted)
	// - vote5: dz_connected event 12 hours ago (bulk ingestion, same timestamp as vote1)
}
