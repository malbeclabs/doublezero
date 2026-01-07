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

func TestLake_Agent_Evals_Anthropic_SolanaValidatorsOnDZVsOffDZ(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SolanaValidatorsOnDZVsOffDZ(t)
}

func runTest_SolanaValidatorsOnDZVsOffDZ(t *testing.T) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)

	// Set up test data
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed Solana validators on DZ vs off DZ comparison data
	seedSolanaValidatorsOnDZVsOffDZData(t, ctx, conn)

	// Set up agent with Anthropic LLM client
	agentInstance := setupAgent(t, ctx, db, newAnthropicLLMClient, debug, debugLevel, nil)

	// Run the query
	var output bytes.Buffer
	question := "compare solana validators on dz vs off dz"
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

	// Basic validation - the response should compare validators on DZ vs off DZ
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
	validateSolanaValidatorsOnDZVsOffDZResponse(t, response)

	// Evaluate with Ollama
	isCorrect, reason, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not correctly answer the question. Reason: %s", reason)
}

// validateSolanaValidatorsOnDZVsOffDZResponse validates that the response includes required comparison elements
func validateSolanaValidatorsOnDZVsOffDZResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should mention comparison between on DZ and off DZ validators
	comparisonMentioned := (strings.Contains(responseLower, "on dz") || strings.Contains(responseLower, "on-dz") || strings.Contains(responseLower, "connected")) &&
		(strings.Contains(responseLower, "off dz") || strings.Contains(responseLower, "off-dz") || strings.Contains(responseLower, "not on dz") || strings.Contains(responseLower, "non-dz"))
	require.True(t, comparisonMentioned,
		"Response should mention comparison between validators on DZ and off DZ. Got: %s",
		truncateForError(response, 200))

	// Should mention skip rate (explicitly or implicitly via produce rate)
	// Skip rate = 1 - produce rate, so mentioning produce rate with percentages implies skip rate
	skipRateMentioned := strings.Contains(responseLower, "skip") ||
		strings.Contains(responseLower, "skipped") ||
		strings.Contains(responseLower, "skip rate") ||
		// Implicit: produce rate with percentages indicates skip rate understanding
		(strings.Contains(responseLower, "produce rate") &&
			(strings.Contains(responseLower, "%") || strings.Contains(responseLower, "percent")))
	require.True(t, skipRateMentioned,
		"Response should mention skip rate (explicitly or implicitly via produce rate with percentages). Got: %s",
		truncateForError(response, 200))

	// Should mention vote latency or vote lag
	voteLatencyMentioned := strings.Contains(responseLower, "vote") &&
		(strings.Contains(responseLower, "latency") ||
			strings.Contains(responseLower, "lag") ||
			strings.Contains(responseLower, "delay"))
	require.True(t, voteLatencyMentioned,
		"Response should mention vote latency or vote lag. Got: %s",
		truncateForError(response, 200))

	// Should mention block produce rate or production rate
	produceRateMentioned := strings.Contains(responseLower, "produce") ||
		strings.Contains(responseLower, "production") ||
		(strings.Contains(responseLower, "block") && strings.Contains(responseLower, "rate"))
	require.True(t, produceRateMentioned,
		"Response should mention block produce rate or production rate. Got: %s",
		truncateForError(response, 200))

	// Should contain numeric data (percentages, rates, counts, etc.)
	hasNumbers := false
	for _, char := range response {
		if char >= '0' && char <= '9' {
			hasNumbers = true
			break
		}
	}
	require.True(t, hasNumbers,
		"Response should contain numeric data (skip rates, produce rates, vote lag, etc.). Got: %s",
		truncateForError(response, 200))

	// Should mention specific vote_pubkeys OR provide aggregate comparisons with specific numbers
	// On DZ: vote1, vote2, vote3
	// Off DZ: vote4, vote5, vote6
	onDZValidatorMentioned := strings.Contains(responseLower, "vote1") ||
		strings.Contains(responseLower, "vote2") ||
		strings.Contains(responseLower, "vote3")
	offDZValidatorMentioned := strings.Contains(responseLower, "vote4") ||
		strings.Contains(responseLower, "vote5") ||
		strings.Contains(responseLower, "vote6")
	// Accept either specific validators OR aggregate comparisons with numbers (e.g., "3 validators", "9,900 slots")
	hasAggregateComparison := (strings.Contains(responseLower, "on dz") || strings.Contains(responseLower, "on-dz")) &&
		(strings.Contains(responseLower, "off dz") || strings.Contains(responseLower, "off-dz")) &&
		hasNumbers // hasNumbers was already checked above
	// At least one validator should be mentioned OR aggregate comparison with numbers should be present
	require.True(t, onDZValidatorMentioned || offDZValidatorMentioned || hasAggregateComparison,
		"Response should mention at least one validator vote_pubkey OR provide aggregate comparisons with numbers. Got: %s",
		truncateForError(response, 200))
}

// seedSolanaValidatorsOnDZVsOffDZData seeds data for comparing validators on DZ vs off DZ
// On DZ validators (vote1, vote2, vote3): Better performance (lower skip rate, lower vote lag, higher produce rate)
// Off DZ validators (vote4, vote5, vote6): Worse performance (higher skip rate, higher vote lag, lower produce rate)
func seedSolanaValidatorsOnDZVsOffDZData(t *testing.T, ctx context.Context, conn duck.Connection) {
	loadTablesAndViews(t, ctx, conn)

	// Seed metros
	_, err := conn.ExecContext(ctx, `
		INSERT INTO dz_metros_current (pk, code, name, as_of_ts, row_hash) VALUES
		('metro1', 'nyc', 'New York', CURRENT_TIMESTAMP, 'metrohash1'),
		('metro2', 'lon', 'London', CURRENT_TIMESTAMP, 'metrohash2')
	`)
	require.NoError(t, err)

	// Seed devices
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_devices_current (pk, code, status, metro_pk, device_type, as_of_ts, row_hash) VALUES
		('device1', 'nyc-dzd1', 'activated', 'metro1', 'DZD', CURRENT_TIMESTAMP, 'devicehash1'),
		('device2', 'lon-dzd1', 'activated', 'metro2', 'DZD', CURRENT_TIMESTAMP, 'devicehash2')
	`)
	require.NoError(t, err)

	// Seed DZ users for validators on DZ
	// user1-3: Connected to DZ (for validators on DZ)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_current (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id, as_of_ts, row_hash) VALUES
		('user1', 'owner1', 'activated', 'IBRL', '1.1.1.1', '10.0.0.1', 'device1', 501, CURRENT_TIMESTAMP, 'userhash1'),
		('user2', 'owner2', 'activated', 'IBRL', '2.2.2.2', '10.0.0.2', 'device1', 502, CURRENT_TIMESTAMP, 'userhash2'),
		('user3', 'owner3', 'activated', 'IBRL', '3.3.3.3', '10.0.0.3', 'device2', 503, CURRENT_TIMESTAMP, 'userhash3')
	`)
	require.NoError(t, err)

	// Seed user history
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_history (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id, valid_from, valid_to, op, row_hash) VALUES
		('user1', 'owner1', 'activated', 'IBRL', '1.1.1.1', '10.0.0.1', 'device1', 501, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'userhash1'),
		('user2', 'owner2', 'activated', 'IBRL', '2.2.2.2', '10.0.0.2', 'device1', 502, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'userhash2'),
		('user3', 'owner3', 'activated', 'IBRL', '3.3.3.3', '10.0.0.3', 'device2', 503, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'userhash3')
	`)
	require.NoError(t, err)

	// Seed Solana gossip nodes
	// node1-3: On DZ (matching user1-3 dz_ip)
	// node4-6: Off DZ (different IPs, not matching any user)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_gossip_nodes_current (
			pubkey, gossip_ip, tpuquic_ip, version, epoch, gossip_port, tpuquic_port, as_of_ts, row_hash
		) VALUES
		-- On DZ
		('node1', '10.0.0.1', '10.0.0.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash1'),
		('node2', '10.0.0.2', '10.0.0.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash2'),
		('node3', '10.0.0.3', '10.0.0.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash3'),
		-- Off DZ
		('node4', '192.168.1.1', '192.168.1.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash4'),
		('node5', '192.168.1.2', '192.168.1.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash5'),
		('node6', '192.168.1.3', '192.168.1.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash6')
	`)
	require.NoError(t, err)

	// Seed gossip nodes history
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_gossip_nodes_history (
			pubkey, gossip_ip, tpuquic_ip, version, epoch, gossip_port, tpuquic_port, valid_from, valid_to, op, row_hash
		) VALUES
		-- On DZ
		('node1', '10.0.0.1', '10.0.0.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'nodehash1'),
		('node2', '10.0.0.2', '10.0.0.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'nodehash2'),
		('node3', '10.0.0.3', '10.0.0.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'nodehash3'),
		-- Off DZ
		('node4', '192.168.1.1', '192.168.1.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'nodehash4'),
		('node5', '192.168.1.2', '192.168.1.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'nodehash5'),
		('node6', '192.168.1.3', '192.168.1.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'nodehash6')
	`)
	require.NoError(t, err)

	// Seed Solana vote accounts
	// vote1-3: On DZ (better performance)
	// vote4-6: Off DZ (worse performance)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_vote_accounts_current (
			vote_pubkey, node_pubkey, epoch_vote_account, epoch, activated_stake_lamports, commission_percentage, as_of_ts, row_hash
		) VALUES
		-- On DZ validators
		('vote1', 'node1', 'true', 100, 1000000000000, 5, CURRENT_TIMESTAMP, 'votehash1'),
		('vote2', 'node2', 'true', 100, 1500000000000, 5, CURRENT_TIMESTAMP, 'votehash2'),
		('vote3', 'node3', 'true', 100, 1200000000000, 5, CURRENT_TIMESTAMP, 'votehash3'),
		-- Off DZ validators
		('vote4', 'node4', 'true', 100, 2000000000000, 5, CURRENT_TIMESTAMP, 'votehash4'),
		('vote5', 'node5', 'true', 100, 1800000000000, 5, CURRENT_TIMESTAMP, 'votehash5'),
		('vote6', 'node6', 'true', 100, 1600000000000, 5, CURRENT_TIMESTAMP, 'votehash6')
	`)
	require.NoError(t, err)

	// Seed vote accounts history
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_vote_accounts_history (
			vote_pubkey, node_pubkey, epoch_vote_account, epoch, activated_stake_lamports, commission_percentage, valid_from, valid_to, op, row_hash
		) VALUES
		-- On DZ validators
		('vote1', 'node1', 'true', 100, 1000000000000, 5, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'votehash1'),
		('vote2', 'node2', 'true', 100, 1500000000000, 5, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'votehash2'),
		('vote3', 'node3', 'true', 100, 1200000000000, 5, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'votehash3'),
		-- Off DZ validators
		('vote4', 'node4', 'true', 100, 2000000000000, 5, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'votehash4'),
		('vote5', 'node5', 'true', 100, 1800000000000, 5, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'votehash5'),
		('vote6', 'node6', 'true', 100, 1600000000000, 5, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'votehash6')
	`)
	require.NoError(t, err)

	now := "CURRENT_TIMESTAMP"

	// Seed leader schedule (slots assigned to validators)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_leader_schedule_current (
			node_pubkey, epoch, slot_count, slots, as_of_ts, row_hash
		) VALUES
		-- On DZ validators: assigned slots
		('node1', 100, 1000, '1000-1999', CURRENT_TIMESTAMP, 'schedhash1'),
		('node2', 100, 1200, '2000-3199', CURRENT_TIMESTAMP, 'schedhash2'),
		('node3', 100, 1100, '3200-4299', CURRENT_TIMESTAMP, 'schedhash3'),
		-- Off DZ validators: assigned slots
		('node4', 100, 1500, '4300-5799', CURRENT_TIMESTAMP, 'schedhash4'),
		('node5', 100, 1400, '5800-7199', CURRENT_TIMESTAMP, 'schedhash5'),
		('node6', 100, 1300, '7200-8499', CURRENT_TIMESTAMP, 'schedhash6')
	`)
	require.NoError(t, err)

	// Seed block production data
	// On DZ validators: Better performance (lower skip rate ~2%, higher produce rate ~98%)
	// Off DZ validators: Worse performance (higher skip rate ~8%, lower produce rate ~92%)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_block_production_raw (
			time, epoch, leader_identity_pubkey, leader_slots_assigned_cum, blocks_produced_cum
		) VALUES
		-- On DZ: vote1 (node1) - 1000 assigned, 980 produced (20 skipped, 98% produce rate, 2% skip rate)
		(`+now+` - INTERVAL '2 hours', 100, 'node1', 1000, 980),
		(`+now+` - INTERVAL '1 hour', 100, 'node1', 2000, 1960),
		(`+now+` - INTERVAL '0 hours', 100, 'node1', 3000, 2940),
		-- On DZ: vote2 (node2) - 1200 assigned, 1176 produced (24 skipped, 98% produce rate, 2% skip rate)
		(`+now+` - INTERVAL '2 hours', 100, 'node2', 1200, 1176),
		(`+now+` - INTERVAL '1 hour', 100, 'node2', 2400, 2352),
		(`+now+` - INTERVAL '0 hours', 100, 'node2', 3600, 3528),
		-- On DZ: vote3 (node3) - 1100 assigned, 1078 produced (22 skipped, 98% produce rate, 2% skip rate)
		(`+now+` - INTERVAL '2 hours', 100, 'node3', 1100, 1078),
		(`+now+` - INTERVAL '1 hour', 100, 'node3', 2200, 2156),
		(`+now+` - INTERVAL '0 hours', 100, 'node3', 3300, 3234),
		-- Off DZ: vote4 (node4) - 1500 assigned, 1380 produced (120 skipped, 92% produce rate, 8% skip rate)
		(`+now+` - INTERVAL '2 hours', 100, 'node4', 1500, 1380),
		(`+now+` - INTERVAL '1 hour', 100, 'node4', 3000, 2760),
		(`+now+` - INTERVAL '0 hours', 100, 'node4', 4500, 4140),
		-- Off DZ: vote5 (node5) - 1400 assigned, 1288 produced (112 skipped, 92% produce rate, 8% skip rate)
		(`+now+` - INTERVAL '2 hours', 100, 'node5', 1400, 1288),
		(`+now+` - INTERVAL '1 hour', 100, 'node5', 2800, 2576),
		(`+now+` - INTERVAL '0 hours', 100, 'node5', 4200, 3864),
		-- Off DZ: vote6 (node6) - 1300 assigned, 1196 produced (104 skipped, 92% produce rate, 8% skip rate)
		(`+now+` - INTERVAL '2 hours', 100, 'node6', 1300, 1196),
		(`+now+` - INTERVAL '1 hour', 100, 'node6', 2600, 2392),
		(`+now+` - INTERVAL '0 hours', 100, 'node6', 3900, 3588)
	`)
	require.NoError(t, err)

	// Seed vote account activity (vote lag)
	// On DZ validators: Lower vote lag (~50 slots)
	// Off DZ validators: Higher vote lag (~200 slots)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_vote_account_activity_raw (
			time, vote_account_pubkey, node_identity_pubkey,
			cluster_slot, last_vote_slot, root_slot, credits_delta, is_delinquent,
			activated_stake_sol, commission, collector_run_id
		) VALUES
		-- On DZ: vote1 - low vote lag (~50 slots)
		(`+now+` - INTERVAL '2 hours', 'vote1', 'node1', 100000, 99950, 99900, 100, false, 1000.0, 5, 'run1'),
		(`+now+` - INTERVAL '1 hour', 'vote1', 'node1', 100100, 100050, 100000, 100, false, 1000.0, 5, 'run2'),
		(`+now+` - INTERVAL '0 hours', 'vote1', 'node1', 100200, 100150, 100100, 100, false, 1000.0, 5, 'run3'),
		-- On DZ: vote2 - low vote lag (~50 slots)
		(`+now+` - INTERVAL '2 hours', 'vote2', 'node2', 100000, 99950, 99900, 100, false, 1500.0, 5, 'run1'),
		(`+now+` - INTERVAL '1 hour', 'vote2', 'node2', 100100, 100050, 100000, 100, false, 1500.0, 5, 'run2'),
		(`+now+` - INTERVAL '0 hours', 'vote2', 'node2', 100200, 100150, 100100, 100, false, 1500.0, 5, 'run3'),
		-- On DZ: vote3 - low vote lag (~50 slots)
		(`+now+` - INTERVAL '2 hours', 'vote3', 'node3', 100000, 99950, 99900, 100, false, 1200.0, 5, 'run1'),
		(`+now+` - INTERVAL '1 hour', 'vote3', 'node3', 100100, 100050, 100000, 100, false, 1200.0, 5, 'run2'),
		(`+now+` - INTERVAL '0 hours', 'vote3', 'node3', 100200, 100150, 100100, 100, false, 1200.0, 5, 'run3'),
		-- Off DZ: vote4 - high vote lag (~200 slots)
		(`+now+` - INTERVAL '2 hours', 'vote4', 'node4', 100000, 99800, 99700, 100, false, 2000.0, 5, 'run1'),
		(`+now+` - INTERVAL '1 hour', 'vote4', 'node4', 100100, 99900, 99800, 100, false, 2000.0, 5, 'run2'),
		(`+now+` - INTERVAL '0 hours', 'vote4', 'node4', 100200, 100000, 99900, 100, false, 2000.0, 5, 'run3'),
		-- Off DZ: vote5 - high vote lag (~200 slots)
		(`+now+` - INTERVAL '2 hours', 'vote5', 'node5', 100000, 99800, 99700, 100, false, 1800.0, 5, 'run1'),
		(`+now+` - INTERVAL '1 hour', 'vote5', 'node5', 100100, 99900, 99800, 100, false, 1800.0, 5, 'run2'),
		(`+now+` - INTERVAL '0 hours', 'vote5', 'node5', 100200, 100000, 99900, 100, false, 1800.0, 5, 'run3'),
		-- Off DZ: vote6 - high vote lag (~200 slots)
		(`+now+` - INTERVAL '2 hours', 'vote6', 'node6', 100000, 99800, 99700, 100, false, 1600.0, 5, 'run1'),
		(`+now+` - INTERVAL '1 hour', 'vote6', 'node6', 100100, 99900, 99800, 100, false, 1600.0, 5, 'run2'),
		(`+now+` - INTERVAL '0 hours', 'vote6', 'node6', 100200, 100000, 99900, 100, false, 1600.0, 5, 'run3')
	`)
	require.NoError(t, err)
}
