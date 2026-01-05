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

func TestLake_Agent_Evals_Anthropic_SolanaValidatorsConnectedStakeIncrease(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SolanaValidatorsConnectedStakeIncrease(t)
}

func runTest_SolanaValidatorsConnectedStakeIncrease(t *testing.T) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)

	// Set up test data
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed Solana validators connected during stake increase data
	seedSolanaValidatorsConnectedStakeIncreaseData(t, ctx, conn)

	// Set up agent with Anthropic LLM client (use more rounds for complex queries)
	agentInstance := setupAgent(t, ctx, db, newAnthropicLLMClient, debug, debugLevel, &react.Config{
		MaxRounds: 15,
	})

	// Run the query
	var output bytes.Buffer
	question := "the solana stake share on dz increased from 36.5% to 39.4% between 24 hours ago and 22 hours ago. which validators connected to dz when this increase happened"
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
	validateSolanaValidatorsConnectedStakeIncreaseResponse(t, response)

	// Evaluate with Ollama
	isCorrect, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not correctly answer the question")
}

// validateSolanaValidatorsConnectedStakeIncreaseResponse validates that the response includes required elements
func validateSolanaValidatorsConnectedStakeIncreaseResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should mention connection or connected
	connectMentioned := strings.Contains(responseLower, "connect") ||
		strings.Contains(responseLower, "connected") ||
		strings.Contains(responseLower, "joined") ||
		strings.Contains(responseLower, "activated")
	require.True(t, connectMentioned,
		"Response should mention connection or connected. Got: %s",
		truncateForError(response, 200))

	// CRITICAL: Should mention vote_pubkey for validators that actually connected
	// Expected connected validators during the stake increase window (between T1 and T2):
	// - vote1: Connected at T1+1 hour (actually connected - should be mentioned)
	// - vote2: Connected at T1+30 minutes (actually connected - should be mentioned)
	// - vote3: Already connected before T1, received stake increase (should NOT be mentioned as "connected")
	// - vote4: Already connected before T1, no stake change (should NOT be mentioned)
	vote1Mentioned := strings.Contains(responseLower, "vote1")
	vote2Mentioned := strings.Contains(responseLower, "vote2")
	vote3Mentioned := strings.Contains(responseLower, "vote3")

	// At least vote1 or vote2 should be mentioned (the ones that actually connected)
	atLeastOneConnectedValidator := vote1Mentioned || vote2Mentioned
	require.True(t, atLeastOneConnectedValidator,
		"Response should mention at least one vote_pubkey that actually connected (vote1 or vote2). Got: %s",
		truncateForError(response, 200))

	// CRITICAL: If vote3 is mentioned, it must NOT be listed as a validator that "connected" during the increase
	// vote3 was already connected and only received stake - it did not connect during the window
	if vote3Mentioned {
		// Check if vote3 is incorrectly listed as connecting
		vote3IncorrectlyListed := (strings.Contains(responseLower, "vote3") &&
			(strings.Contains(responseLower, "connect") ||
				strings.Contains(responseLower, "joined") ||
				strings.Contains(responseLower, "activated"))) &&
			!strings.Contains(responseLower, "already") &&
			!strings.Contains(responseLower, "previously") &&
			!strings.Contains(responseLower, "not") &&
			!strings.Contains(responseLower, "excluded")
		require.False(t, vote3IncorrectlyListed,
			"Response should NOT list vote3 as a validator that connected during the increase (it was already connected and only received stake). Got: %s",
			truncateForError(response, 200))
	}

	// CRITICAL: Should mention IP addresses (gossip_ip, dz_ip, or client_ip) for connected validators
	// vote1: gossip_ip=10.0.0.1, client_ip='1.1.1.1'
	// vote2: gossip_ip=10.0.0.2, client_ip='2.2.2.2'
	ipMentioned := strings.Contains(response, "10.0.0.1") ||
		strings.Contains(response, "10.0.0.2") ||
		strings.Contains(response, "1.1.1.1") || // client_ip for vote1
		strings.Contains(response, "2.2.2.2") // client_ip for vote2
	require.True(t, ipMentioned,
		"Response should mention IP addresses (gossip_ip, dz_ip, or client_ip) for connected validators. Got: %s",
		truncateForError(response, 200))

	// Should mention time period or stake increase context
	timeOrStakeMentioned := strings.Contains(responseLower, "increase") ||
		strings.Contains(responseLower, "stake") ||
		strings.Contains(responseLower, "share") ||
		strings.Contains(responseLower, "hour") ||
		strings.Contains(responseLower, "time") ||
		strings.Contains(responseLower, "window") ||
		strings.Contains(responseLower, "period")
	require.True(t, timeOrStakeMentioned,
		"Response should mention stake increase or time period context. Got: %s",
		truncateForError(response, 200))

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

	// CRITICAL: Should NOT confuse stake increases with connections
	// The response should use connection events, not infer from stake changes
	// Check that the response doesn't incorrectly attribute all stake increases to new connections
	// If both vote1 and vote2 are mentioned, that's good (they actually connected)
	// If vote3 is mentioned as connecting, that's wrong (it was already connected)
	if vote1Mentioned && vote2Mentioned && !vote3Mentioned {
		// Good - mentions the validators that actually connected, doesn't mention the one that only got stake
	} else if vote3Mentioned && !strings.Contains(responseLower, "already") && !strings.Contains(responseLower, "not") {
		// Bad - mentions vote3 as connecting when it was already connected
		t.Logf("WARNING: Response may be incorrectly listing vote3 as connecting. Response: %s", truncateForError(response, 300))
	}
}

// seedSolanaValidatorsConnectedStakeIncreaseData seeds data for testing validators connected during stake increase
// Test scenario:
// - T1: Initial state - vote3 and vote4 already connected, stake share ~36.5%
// - T1+30min: vote2 connects (adds stake)
// - T1+1hour: vote1 connects (adds stake)
// - T1+1hour: vote3 receives stake delegation (stake increases but was already connected)
// - T1+2hours: Final state - stake share ~39.4%
// The question asks "which validators connected when the big increase happened"
// Expected answer: vote1 and vote2 (they actually connected)
// Should NOT include: vote3 (was already connected, only received stake)
func seedSolanaValidatorsConnectedStakeIncreaseData(t *testing.T, ctx context.Context, conn duck.Connection) {
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
	// T1 = 24 hours ago (the "before" state)
	// T1+30min = vote2 connects
	// T1+1hour = vote1 connects, vote3 receives stake
	// T1+2hours = current time

	// Seed DZ users history
	// user1: Connected at T1+1 hour (for vote1 - actually connected during increase)
	// user2: Connected at T1+30 minutes (for vote2 - actually connected during increase)
	// user3: Connected before T1, still connected (for vote3 - already connected, received stake)
	// user4: Connected before T1, still connected (for vote4 - already connected, no stake change)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_history (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id, valid_from, valid_to, op, row_hash) VALUES
		-- user1: Connected at T1+1 hour (vote1 - actually connected during increase)
		('user1', 'owner1', 'activated', 'IBRL', '1.1.1.1', '10.0.0.1', 'device1', 501, `+now+` - INTERVAL '23 hours', NULL, 'I', 'userhash1'),
		-- user2: Connected at T1+30 minutes (vote2 - actually connected during increase)
		('user2', 'owner2', 'activated', 'IBRL', '2.2.2.2', '10.0.0.2', 'device1', 502, `+now+` - INTERVAL '23 hours 30 minutes', NULL, 'I', 'userhash2'),
		-- user3: Connected before T1, still connected (vote3 - already connected, received stake)
		('user3', 'owner3', 'activated', 'IBRL', '3.3.3.3', '10.0.0.3', 'device1', 503, `+now+` - INTERVAL '30 days', NULL, 'I', 'userhash3'),
		-- user4: Connected before T1, still connected (vote4 - already connected, no stake change)
		('user4', 'owner4', 'activated', 'IBRL', '4.4.4.4', '10.0.0.4', 'device1', 504, `+now+` - INTERVAL '30 days', NULL, 'I', 'userhash4')
	`)
	require.NoError(t, err)

	// Seed DZ users current (all are currently connected)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_current (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, tunnel_id, as_of_ts, row_hash) VALUES
		('user1', 'owner1', 'activated', 'IBRL', '1.1.1.1', '10.0.0.1', 'device1', 501, CURRENT_TIMESTAMP, 'userhash1'),
		('user2', 'owner2', 'activated', 'IBRL', '2.2.2.2', '10.0.0.2', 'device1', 502, CURRENT_TIMESTAMP, 'userhash2'),
		('user3', 'owner3', 'activated', 'IBRL', '3.3.3.3', '10.0.0.3', 'device1', 503, CURRENT_TIMESTAMP, 'userhash3'),
		('user4', 'owner4', 'activated', 'IBRL', '4.4.4.4', '10.0.0.4', 'device1', 504, CURRENT_TIMESTAMP, 'userhash4')
	`)
	require.NoError(t, err)

	// Seed Solana gossip nodes history
	// node1 (vote1): gossip_ip=10.0.0.1, connected at T1+1 hour
	// node2 (vote2): gossip_ip=10.0.0.2, connected at T1+30 minutes
	// node3 (vote3): gossip_ip=10.0.0.3, connected before T1
	// node4 (vote4): gossip_ip=10.0.0.4, connected before T1
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_gossip_nodes_history (
			pubkey, gossip_ip, tpuquic_ip, version, epoch, gossip_port, tpuquic_port, valid_from, valid_to, op, row_hash
		) VALUES
		-- node1 (vote1): Connected at T1+1 hour
		('node1', '10.0.0.1', '10.0.0.1', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '23 hours', NULL, 'I', 'nodehash1'),
		-- node2 (vote2): Connected at T1+30 minutes
		('node2', '10.0.0.2', '10.0.0.2', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '23 hours 30 minutes', NULL, 'I', 'nodehash2'),
		-- node3 (vote3): Connected before T1
		('node3', '10.0.0.3', '10.0.0.3', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '30 days', NULL, 'I', 'nodehash3'),
		-- node4 (vote4): Connected before T1
		('node4', '10.0.0.4', '10.0.0.4', '1.18.0', 100, 8001, 8002, `+now+` - INTERVAL '30 days', NULL, 'I', 'nodehash4')
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
		('node4', '10.0.0.4', '10.0.0.4', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash4')
	`)
	require.NoError(t, err)

	// Seed Solana vote accounts history
	// vote1: Connected at T1+1 hour, stake 15M SOL (15,000,000,000,000 lamports)
	// vote2: Connected at T1+30 minutes, stake 1M SOL (1,000,000,000,000 lamports)
	// vote3: Connected before T1, stake increased from 5M to 10M SOL at T1+1 hour (received delegation)
	// vote4: Connected before T1, stake 3M SOL (no change)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_vote_accounts_history (
			vote_pubkey, node_pubkey, epoch_vote_account, epoch, activated_stake_lamports, commission_percentage, valid_from, valid_to, op, row_hash
		) VALUES
		-- vote1: Connected at T1+1 hour, stake 15M SOL
		('vote1', 'node1', 'true', 100, 15000000000000, 5, `+now+` - INTERVAL '23 hours', NULL, 'I', 'votehash1'),
		-- vote2: Connected at T1+30 minutes, stake 1M SOL
		('vote2', 'node2', 'true', 100, 1000000000000, 5, `+now+` - INTERVAL '23 hours 30 minutes', NULL, 'I', 'votehash2'),
		-- vote3: Connected before T1, stake increased from 5M to 10M SOL at T1+1 hour
		('vote3', 'node3', 'true', 100, 5000000000000, 5, `+now+` - INTERVAL '30 days', `+now+` - INTERVAL '23 hours', 'I', 'votehash3'),
		('vote3', 'node3', 'true', 100, 10000000000000, 5, `+now+` - INTERVAL '23 hours', NULL, 'I', 'votehash3'),
		-- vote4: Connected before T1, stake 3M SOL (no change)
		('vote4', 'node4', 'true', 100, 3000000000000, 5, `+now+` - INTERVAL '30 days', NULL, 'I', 'votehash4')
	`)
	require.NoError(t, err)

	// Seed Solana vote accounts current
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_vote_accounts_current (
			vote_pubkey, node_pubkey, epoch_vote_account, epoch, activated_stake_lamports, commission_percentage, as_of_ts, row_hash
		) VALUES
		('vote1', 'node1', 'true', 100, 15000000000000, 5, CURRENT_TIMESTAMP, 'votehash1'),
		('vote2', 'node2', 'true', 100, 1000000000000, 5, CURRENT_TIMESTAMP, 'votehash2'),
		('vote3', 'node3', 'true', 100, 10000000000000, 5, CURRENT_TIMESTAMP, 'votehash3'),
		('vote4', 'node4', 'true', 100, 3000000000000, 5, CURRENT_TIMESTAMP, 'votehash4')
	`)
	require.NoError(t, err)

	// The solana_validator_dz_connection_events view will be automatically populated from solana_validator_dz_overlaps_windowed
	// which is based on the overlaps between dz_users_history, solana_gossip_nodes_history, and solana_vote_accounts_history
	// The view should show:
	// - vote1: dz_connected event at T1+1 hour
	// - vote2: dz_connected event at T1+30 minutes
	// - vote3: dz_connected event before T1 (not during the increase window)
	// - vote4: dz_connected event before T1 (not during the increase window)
}
