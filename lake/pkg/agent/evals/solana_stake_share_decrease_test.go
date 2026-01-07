//go:build evals

package evals_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/malbeclabs/doublezero/lake/pkg/agent/react"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_SolanaStakeShareDecrease(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SolanaStakeShareDecrease(t)
}

func runTest_SolanaStakeShareDecrease(t *testing.T) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)

	// Set up test data
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed Solana stake share decrease data
	seedSolanaStakeShareDecreaseData(t, ctx, conn)

	// Set up agent with Anthropic LLM client (use more rounds for complex stake share queries)
	agentInstance := setupAgent(t, ctx, db, newAnthropicLLMClient, debug, debugLevel, &react.Config{
		MaxRounds: 15,
	})

	// Run the query
	var output bytes.Buffer
	question := "the solana network stake share on dz decreased recently, why"
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

	// Basic validation - the response should explain the decrease
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

	// The response should be non-empty and contain some explanation
	require.Greater(t, len(response), 100, "Response should be substantial")

	// Validate that the response contains specific data points from the seeded test data
	validateSolanaStakeShareDecreaseResponse(t, response)

	// Evaluate with Ollama
	isCorrect, reason, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not correctly answer the question. Reason: %s", reason)
}

// validateSolanaStakeShareDecreaseResponse validates the response for TestLake_Agent_Evals_Anthropic_SolanaStakeShareDecrease
func validateSolanaStakeShareDecreaseResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should mention stake share or stake percentage
	stakeMentioned := strings.Contains(responseLower, "stake") || strings.Contains(responseLower, "share")
	require.True(t, stakeMentioned,
		"Response should mention stake or stake share. Got: %s",
		truncateForError(response, 200))

	// Should mention decrease or reduction
	decreaseMentioned := strings.Contains(responseLower, "decreas") ||
		strings.Contains(responseLower, "reduc") ||
		strings.Contains(responseLower, "drop") ||
		strings.Contains(responseLower, "fell") ||
		strings.Contains(responseLower, "down")
	require.True(t, decreaseMentioned,
		"Response should mention decrease, reduction, drop, or similar. Got: %s",
		truncateForError(response, 200))

	// Should mention validators disconnecting or users disconnecting
	disconnectMentioned := strings.Contains(responseLower, "disconnect") ||
		strings.Contains(responseLower, "left") ||
		strings.Contains(responseLower, "remov") ||
		strings.Contains(responseLower, "no longer") ||
		strings.Contains(responseLower, "stopped") ||
		strings.Contains(responseLower, "not connected") ||
		strings.Contains(responseLower, "ceased") ||
		strings.Contains(responseLower, "ended") ||
		strings.Contains(responseLower, "departed")
	require.True(t, disconnectMentioned,
		"Response should mention validators/users disconnecting or leaving. Got: %s",
		truncateForError(response, 200))

	// Should mention recent time period (past day, recently, dates, etc.)
	// Accept various formats: "recent", "past day", specific dates, "hours ago", etc.
	recentMentioned := strings.Contains(responseLower, "recent") ||
		strings.Contains(responseLower, "past day") ||
		strings.Contains(responseLower, "yesterday") ||
		strings.Contains(responseLower, "last 24") ||
		strings.Contains(responseLower, "today") ||
		strings.Contains(responseLower, "hours") ||
		strings.Contains(responseLower, "december") || // Accept date mentions
		strings.Contains(responseLower, "2025") || // Accept year mentions
		strings.Contains(responseLower, "2026") || // Accept year mentions
		strings.Contains(responseLower, "between") || // Accept date ranges
		strings.Contains(responseLower, "previously") || // Accept "previously connected"
		strings.Contains(responseLower, "were") // Accept "were connected" (implies past)
	require.True(t, recentMentioned,
		"Response should mention recent time period (past day, recently, dates, etc.). Got: %s",
		truncateForError(response, 200))

	// Should contain numeric data (stake amounts, percentages, counts)
	hasNumbers := false
	for _, char := range response {
		if char >= '0' && char <= '9' {
			hasNumbers = true
			break
		}
	}
	require.True(t, hasNumbers,
		"Response should contain numeric data (stake amounts, percentages, counts). Got: %s",
		truncateForError(response, 200))

	// Should mention specific validators that disconnected (vote4 and vote5)
	// These are the validators that disconnected in the past day
	// CRITICAL: Must include vote_pubkey (vote4, vote5) - this is the stable validator identity
	// Accept either explicit mention or implicit via stake amounts (vote4 has 5000 SOL, vote5 has 4000 SOL)
	vote4PubkeyMentioned := strings.Contains(responseLower, "vote4") ||
		(strings.Contains(responseLower, "5000") && strings.Contains(responseLower, "sol") && strings.Contains(responseLower, "disconnect"))
	vote5PubkeyMentioned := strings.Contains(responseLower, "vote5") ||
		(strings.Contains(responseLower, "4000") && strings.Contains(responseLower, "sol") && strings.Contains(responseLower, "disconnect"))
	// At least one of vote4 or vote5 must be mentioned explicitly
	atLeastOneValidatorMentioned := vote4PubkeyMentioned || vote5PubkeyMentioned
	require.True(t, atLeastOneValidatorMentioned,
		"Response should mention at least one vote_pubkey (vote4 or vote5) that disconnected. Got: %s",
		truncateForError(response, 200))
	// If only one is mentioned, check that the response mentions "validators" (plural) or multiple stake amounts
	if !vote4PubkeyMentioned || !vote5PubkeyMentioned {
		multipleValidatorsMentioned := strings.Contains(responseLower, "validators") ||
			strings.Contains(responseLower, "both") ||
			strings.Contains(responseLower, "two") ||
			(strings.Contains(responseLower, "5000") && strings.Contains(responseLower, "4000"))
		require.True(t, multipleValidatorsMentioned,
			"Response should mention both validators that disconnected (vote4 and vote5) or indicate multiple validators. Got: %s",
			truncateForError(response, 200))
	}

	// Should mention stake amounts in SOL for the disconnected validators
	// vote4: 5,000 SOL (5,000,000,000,000 lamports)
	// vote5: 4,000 SOL (4,000,000,000,000 lamports)
	// Accept various formats: "5000", "5,000", "5 thousand", "5k", etc.
	hasVote4Stake := strings.Contains(response, "5000") ||
		strings.Contains(response, "5,000") ||
		(strings.Contains(responseLower, "5") && (strings.Contains(responseLower, "thousand") || strings.Contains(responseLower, "k")))
	hasVote5Stake := strings.Contains(response, "4000") ||
		strings.Contains(response, "4,000") ||
		(strings.Contains(responseLower, "4") && (strings.Contains(responseLower, "thousand") || strings.Contains(responseLower, "k")))
	require.True(t, hasVote4Stake,
		"Response should mention vote4's stake amount in SOL (5,000 SOL or similar). Got: %s",
		truncateForError(response, 200))
	require.True(t, hasVote5Stake,
		"Response should mention vote5's stake amount in SOL (4,000 SOL or similar). Got: %s",
		truncateForError(response, 200))

	// Should mention the contribution to stake share decrease
	// Total stake from disconnected validators: 9,000 SOL (5,000 + 4,000)
	// Should mention this total or the individual contributions
	hasTotalStake := strings.Contains(response, "9000") ||
		strings.Contains(response, "9,000") ||
		(strings.Contains(responseLower, "9") && (strings.Contains(responseLower, "thousand") || strings.Contains(responseLower, "k"))) ||
		(strings.Contains(responseLower, "5") && strings.Contains(responseLower, "4") && strings.Contains(responseLower, "thousand"))
	// Or should mention percentage contribution or impact on stake share
	hasContribution := strings.Contains(responseLower, "contribution") ||
		strings.Contains(responseLower, "impact") ||
		strings.Contains(responseLower, "share") ||
		strings.Contains(responseLower, "%") ||
		strings.Contains(responseLower, "percent")
	require.True(t, hasTotalStake || hasContribution,
		"Response should mention the total stake from disconnected validators (9,000 SOL) or their contribution/impact on stake share. Got: %s",
		truncateForError(response, 200))

	// Log the response for debugging
	t.Logf("Response mentions: stake=%v, decrease=%v, disconnect=%v, recent=%v, numbers=%v, vote4_pubkey=%v, vote5_pubkey=%v, vote4_stake=%v, vote5_stake=%v, contribution=%v. Full response: %s",
		stakeMentioned, decreaseMentioned, disconnectMentioned, recentMentioned, hasNumbers,
		vote4PubkeyMentioned, vote5PubkeyMentioned, hasVote4Stake, hasVote5Stake, hasTotalStake || hasContribution,
		truncateForError(response, 500))
}

// seedSolanaStakeShareDecreaseData seeds Solana data for TestLake_Agent_Evals_Anthropic_SolanaStakeShareDecrease
// Sets up a scenario where 2 validators disconnected from DZ in the past day, causing stake share to decrease
func seedSolanaStakeShareDecreaseData(t *testing.T, ctx context.Context, conn duck.Connection) {
	// Load and execute table and view creation migrations
	loadTablesAndViews(t, ctx, conn)

	// Seed devices
	_, err := conn.ExecContext(ctx, `
		INSERT INTO dz_devices_current (pk, code, status, metro_pk, device_type, as_of_ts, row_hash) VALUES
		('device1', 'nyc-dzd1', 'activated', 'metro1', 'DZD', CURRENT_TIMESTAMP, 'hash1'),
		('device2', 'lon-dzd1', 'activated', 'metro2', 'DZD', CURRENT_TIMESTAMP, 'hash2'),
		('device3', 'chi-dzd1', 'activated', 'metro3', 'DZD', CURRENT_TIMESTAMP, 'hash3'),
		('device4', 'sf-dzd1', 'activated', 'metro4', 'DZD', CURRENT_TIMESTAMP, 'hash4'),
		('device5', 'tok-dzd1', 'activated', 'metro5', 'DZD', CURRENT_TIMESTAMP, 'hash5')
	`)
	require.NoError(t, err)

	// Seed DZ users:
	// - user1-3: Currently on DZ (still connected)
	// - user4-5: Disconnected in the past day (valid_to set to recent timestamp)
	// - user6: Disconnected longer ago (not recent)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_current (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, as_of_ts, row_hash) VALUES
		('user1', 'owner1', 'activated', 'IBRL', '1.1.1.1', '10.0.0.1', 'device1', CURRENT_TIMESTAMP, 'userhash1'),
		('user2', 'owner2', 'activated', 'IBRL', '2.2.2.2', '10.0.0.2', 'device2', CURRENT_TIMESTAMP, 'userhash2'),
		('user3', 'owner3', 'activated', 'IBRL', '3.3.3.3', '10.0.0.3', 'device3', CURRENT_TIMESTAMP, 'userhash3')
	`)
	require.NoError(t, err)

	// Seed history table with temporal data:
	// - user1-3: Currently active (valid_to = NULL)
	// - user4-5: Disconnected in the past day (valid_to = CURRENT_TIMESTAMP - INTERVAL '12 hours' and '6 hours')
	// - user6: Disconnected longer ago (valid_to = CURRENT_TIMESTAMP - INTERVAL '3 days')
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_history (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, valid_from, valid_to, op, row_hash) VALUES
		-- Currently active users
		('user1', 'owner1', 'activated', 'IBRL', '1.1.1.1', '10.0.0.1', 'device1', CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'userhash1'),
		('user2', 'owner2', 'activated', 'IBRL', '2.2.2.2', '10.0.0.2', 'device2', CURRENT_TIMESTAMP - INTERVAL '60 days', NULL, 'I', 'userhash2'),
		('user3', 'owner3', 'activated', 'IBRL', '3.3.3.3', '10.0.0.3', 'device3', CURRENT_TIMESTAMP - INTERVAL '45 days', NULL, 'I', 'userhash3'),
		-- Disconnected in the past day (recent disconnections)
		('user4', 'owner4', 'activated', 'IBRL', '4.4.4.4', '10.0.0.4', 'device4', CURRENT_TIMESTAMP - INTERVAL '20 days', CURRENT_TIMESTAMP - INTERVAL '12 hours', 'D', 'userhash4'),
		('user5', 'owner5', 'activated', 'IBRL', '5.5.5.5', '10.0.0.5', 'device5', CURRENT_TIMESTAMP - INTERVAL '15 days', CURRENT_TIMESTAMP - INTERVAL '6 hours', 'D', 'userhash5'),
		-- Disconnected longer ago (not recent)
		('user6', 'owner6', 'activated', 'IBRL', '6.6.6.6', '10.0.0.6', 'device1', CURRENT_TIMESTAMP - INTERVAL '90 days', CURRENT_TIMESTAMP - INTERVAL '3 days', 'D', 'userhash6')
	`)
	require.NoError(t, err)

	// Seed Solana gossip nodes
	// - node1-3: Currently on DZ (matching user1-3)
	// - node4-5: Were on DZ but disconnected recently (matching user4-5)
	// - node6: Was on DZ but disconnected longer ago (matching user6)
	// - node7-10: Not on DZ (never were)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_gossip_nodes_current (
			pubkey, gossip_ip, tpuquic_ip, version, epoch, gossip_port, tpuquic_port, as_of_ts, row_hash
		) VALUES
		-- Currently on DZ
		('node1', '10.0.0.1', '10.0.0.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash1'),
		('node2', '10.0.0.2', '10.0.0.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash2'),
		('node3', '10.0.0.3', '10.0.0.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash3'),
		-- Were on DZ but disconnected (still exist in current table but no matching user)
		('node4', '10.0.0.4', '10.0.0.4', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash4'),
		('node5', '10.0.0.5', '10.0.0.5', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash5'),
		('node6', '10.0.0.6', '10.0.0.6', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash6'),
		-- Not on DZ
		('node7', '192.168.1.1', '192.168.1.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash7'),
		('node8', '192.168.1.2', '192.168.1.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash8'),
		('node9', '192.168.1.3', '192.168.1.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash9'),
		('node10', '192.168.1.4', '192.168.1.4', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash10')
	`)
	require.NoError(t, err)

	// Seed history table with temporal data
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_gossip_nodes_history (
			pubkey, gossip_ip, tpuquic_ip, version, epoch, gossip_port, tpuquic_port, valid_from, valid_to, op, row_hash
		) VALUES
		-- Currently on DZ (active)
		('node1', '10.0.0.1', '10.0.0.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'nodehash1'),
		('node2', '10.0.0.2', '10.0.0.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '60 days', NULL, 'I', 'nodehash2'),
		('node3', '10.0.0.3', '10.0.0.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '45 days', NULL, 'I', 'nodehash3'),
		-- Disconnected in the past day (recent)
		('node4', '10.0.0.4', '10.0.0.4', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '20 days', CURRENT_TIMESTAMP - INTERVAL '12 hours', 'D', 'nodehash4'),
		('node5', '10.0.0.5', '10.0.0.5', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '15 days', CURRENT_TIMESTAMP - INTERVAL '6 hours', 'D', 'nodehash5'),
		-- Disconnected longer ago (not recent)
		('node6', '10.0.0.6', '10.0.0.6', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '90 days', NULL, 'I', 'nodehash6'),
		-- Not on DZ (long-standing)
		('node7', '192.168.1.1', '192.168.1.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '180 days', NULL, 'I', 'nodehash7'),
		('node8', '192.168.1.2', '192.168.1.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '150 days', NULL, 'I', 'nodehash8'),
		('node9', '192.168.1.3', '192.168.1.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '200 days', NULL, 'I', 'nodehash9'),
		('node10', '192.168.1.4', '192.168.1.4', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '100 days', NULL, 'I', 'nodehash10')
	`)
	require.NoError(t, err)

	// Seed Solana vote accounts
	// - vote1-3: Currently on DZ (nodes 1-3) - lower stake amounts
	// - vote4-5: Disconnected in the past day (nodes 4-5) - higher stake amounts (these caused the decrease)
	// - vote6: Disconnected longer ago (node6) - not relevant for recent decrease
	// - vote7-10: Not on DZ (nodes 7-10) - part of total network stake
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_vote_accounts_current (
			vote_pubkey, node_pubkey, epoch_vote_account, epoch, activated_stake_lamports, commission_percentage, as_of_ts, row_hash
		) VALUES
		-- Currently on DZ validators (lower stake)
		('vote1', 'node1', 'true', 100, 1000000000000, 5, CURRENT_TIMESTAMP, 'votehash1'),
		('vote2', 'node2', 'true', 100, 1500000000000, 5, CURRENT_TIMESTAMP, 'votehash2'),
		('vote3', 'node3', 'true', 100, 1200000000000, 5, CURRENT_TIMESTAMP, 'votehash3'),
		-- Disconnected in the past day (higher stake - these caused the decrease)
		('vote4', 'node4', 'true', 100, 5000000000000, 5, CURRENT_TIMESTAMP, 'votehash4'),
		('vote5', 'node5', 'true', 100, 4000000000000, 5, CURRENT_TIMESTAMP, 'votehash5'),
		-- Disconnected longer ago (not relevant for recent decrease)
		('vote6', 'node6', 'true', 100, 2000000000000, 5, CURRENT_TIMESTAMP, 'votehash6'),
		-- Not on DZ validators (part of total network stake)
		('vote7', 'node7', 'true', 100, 3000000000000, 5, CURRENT_TIMESTAMP, 'votehash7'),
		('vote8', 'node8', 'true', 100, 2500000000000, 5, CURRENT_TIMESTAMP, 'votehash8'),
		('vote9', 'node9', 'true', 100, 6000000000000, 5, CURRENT_TIMESTAMP, 'votehash9'),
		('vote10', 'node10', 'true', 100, 3500000000000, 5, CURRENT_TIMESTAMP, 'votehash10')
	`)
	require.NoError(t, err)

	// Seed history table with temporal data
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_vote_accounts_history (
			vote_pubkey, node_pubkey, epoch_vote_account, epoch, activated_stake_lamports, commission_percentage, valid_from, valid_to, op, row_hash
		) VALUES
		-- Currently on DZ validators
		('vote1', 'node1', 'true', 100, 1000000000000, 5, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'votehash1'),
		('vote2', 'node2', 'true', 100, 1500000000000, 5, CURRENT_TIMESTAMP - INTERVAL '60 days', NULL, 'I', 'votehash2'),
		('vote3', 'node3', 'true', 100, 1200000000000, 5, CURRENT_TIMESTAMP - INTERVAL '45 days', NULL, 'I', 'votehash3'),
		-- Disconnected in the past day (recent disconnections)
		('vote4', 'node4', 'true', 100, 5000000000000, 5, CURRENT_TIMESTAMP - INTERVAL '20 days', CURRENT_TIMESTAMP - INTERVAL '12 hours', 'D', 'votehash4'),
		('vote5', 'node5', 'true', 100, 4000000000000, 5, CURRENT_TIMESTAMP - INTERVAL '15 days', CURRENT_TIMESTAMP - INTERVAL '6 hours', 'D', 'votehash5'),
		-- Disconnected longer ago
		('vote6', 'node6', 'true', 100, 2000000000000, 5, CURRENT_TIMESTAMP - INTERVAL '90 days', NULL, 'I', 'votehash6'),
		-- Not on DZ validators
		('vote7', 'node7', 'true', 100, 3000000000000, 5, CURRENT_TIMESTAMP - INTERVAL '180 days', NULL, 'I', 'votehash7'),
		('vote8', 'node8', 'true', 100, 2500000000000, 5, CURRENT_TIMESTAMP - INTERVAL '150 days', NULL, 'I', 'votehash8'),
		('vote9', 'node9', 'true', 100, 6000000000000, 5, CURRENT_TIMESTAMP - INTERVAL '200 days', NULL, 'I', 'votehash9'),
		('vote10', 'node10', 'true', 100, 3500000000000, 5, CURRENT_TIMESTAMP - INTERVAL '100 days', NULL, 'I', 'votehash10')
	`)
	require.NoError(t, err)
}
