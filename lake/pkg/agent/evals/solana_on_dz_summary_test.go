//go:build evals

package evals_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/joho/godotenv"
	"github.com/malbeclabs/doublezero/lake/pkg/duck"
	"github.com/stretchr/testify/require"
)

func init() {
	possiblePaths := []string{".env"}

	for _, path := range possiblePaths {
		if err := godotenv.Load(path); err == nil {
			break
		}
	}
}

func TestLake_Agent_Evals_Anthropic_SolanaValidatorsGossipNodesOnDZSummary(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SolanaValidatorsGossipNodesOnDZSummary(t)
}

func runTest_SolanaValidatorsGossipNodesOnDZSummary(t *testing.T) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	db := testDB(t)

	// Set up test data
	conn, err := db.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed Solana data
	seedSolanaValidatorsGossipNodesOnDZSummaryData(t, ctx, conn)

	// Set up agent with Anthropic LLM client
	agentInstance := setupAgent(t, ctx, db, newAnthropicLLMClient, debug, debugLevel, nil)

	// Run the query
	var output bytes.Buffer
	question := "how many solana validators and gossip nodes on dz"
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

	// Basic validation - the response should mention counts
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

	// The response should be non-empty and contain some indication of counts
	require.Greater(t, len(response), 50, "Response should be substantial")

	// Validate that the response contains specific data points from the seeded test data
	validateSolanaValidatorsGossipNodesOnDZSummaryResponse(t, response)

	// Evaluate with Ollama
	isCorrect, reason, err := ollamaEvaluateResponse(t, ctx, question, response)
	require.NoError(t, err, "Ollama evaluation must be available")
	require.True(t, isCorrect, "Ollama evaluation indicates the response does not correctly answer the question. Reason: %s", reason)
}

// validateSolanaValidatorsGossipNodesOnDZSummaryResponse validates the response for TestLake_Agent_Evals_SolanaValidatorsGossipNodesOnDZSummary
func validateSolanaValidatorsGossipNodesOnDZSummaryResponse(t *testing.T, response string) {
	responseLower := strings.ToLower(response)

	// Should mention validators
	validatorMentioned := strings.Contains(responseLower, "validator") || strings.Contains(responseLower, "validators")
	require.True(t, validatorMentioned,
		"Response should mention validators. Got: %s",
		truncateForError(response, 200))

	// Should mention gossip nodes
	gossipMentioned := strings.Contains(responseLower, "gossip") || strings.Contains(responseLower, "node") || strings.Contains(responseLower, "nodes")
	require.True(t, gossipMentioned,
		"Response should mention gossip nodes. Got: %s",
		truncateForError(response, 200))

	// Should contain numeric counts
	hasNumbers := false
	for _, char := range response {
		if char >= '0' && char <= '9' {
			hasNumbers = true
			break
		}
	}
	require.True(t, hasNumbers,
		"Response should contain numeric counts. Got: %s",
		truncateForError(response, 200))

	// Should mention "dz" or "doublezero" or "double zero"
	dzMentioned := strings.Contains(responseLower, "dz") ||
		strings.Contains(responseLower, "doublezero") ||
		strings.Contains(responseLower, "double zero")
	require.True(t, dzMentioned,
		"Response should mention DZ/DoubleZero. Got: %s",
		truncateForError(response, 200))

	// Verify the correct counts: 3 validators and 5 gossip nodes currently on DZ
	// The response should contain both numbers 3 and 5, and mention validators and gossip nodes
	hasThree := strings.Contains(response, "3") || strings.Contains(responseLower, "three")
	hasFive := strings.Contains(response, "5") || strings.Contains(responseLower, "five")

	// Check that both numbers are present
	require.True(t, hasThree && hasFive,
		"Response should contain both counts: 3 validators and 5 gossip nodes. Got: %s",
		truncateForError(response, 200))

	// Log the response for debugging if validation passes basic checks
	t.Logf("Response contains counts: 3=%v, 5=%v. Full response: %s", hasThree, hasFive, truncateForError(response, 500))
}

// seedSolanaValidatorsGossipNodesOnDZSummaryData seeds Solana validators and gossip nodes data for TestLake_Agent_Evals_SolanaValidatorsGossipNodesOnDZSummary
func seedSolanaValidatorsGossipNodesOnDZSummaryData(t *testing.T, ctx context.Context, conn duck.Connection) {
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

	// Seed DZ users with dz_ip addresses that will match gossip nodes
	// Currently on DZ: 5 users (3 validators + 2 gossip-only nodes)
	// Historical: 2 users that were on DZ in the past but disconnected
	// Being "on dz" means they exist in the users dataset
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_current (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, as_of_ts, row_hash) VALUES
		('user1', 'owner1', 'activated', 'IBRL', '1.1.1.1', '10.0.0.1', 'device1', CURRENT_TIMESTAMP, 'userhash1'),
		('user2', 'owner2', 'activated', 'IBRL', '2.2.2.2', '10.0.0.2', 'device2', CURRENT_TIMESTAMP, 'userhash2'),
		('user3', 'owner3', 'activated', 'IBRL', '3.3.3.3', '10.0.0.3', 'device3', CURRENT_TIMESTAMP, 'userhash3'),
		('user4', 'owner4', 'activated', 'IBRL', '4.4.4.4', '10.0.0.4', 'device4', CURRENT_TIMESTAMP, 'userhash4'),
		('user5', 'owner5', 'activated', 'IBRL', '5.5.5.5', '10.0.0.5', 'device5', CURRENT_TIMESTAMP, 'userhash5')
	`)
	require.NoError(t, err)

	// Seed history table with current users and historical users
	// user1-5: Currently active
	// user6-7: Were on DZ in the past but disconnected (valid_to is set)
	// user1: Was created recently (valid_from is recent)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO dz_users_history (pk, owner_pk, status, kind, client_ip, dz_ip, device_pk, valid_from, valid_to, op, row_hash) VALUES
		-- Currently active users
		('user1', 'owner1', 'activated', 'IBRL', '1.1.1.1', '10.0.0.1', 'device1', CURRENT_TIMESTAMP - INTERVAL '7 days', NULL, 'I', 'userhash1'),
		('user2', 'owner2', 'activated', 'IBRL', '2.2.2.2', '10.0.0.2', 'device2', CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'userhash2'),
		('user3', 'owner3', 'activated', 'IBRL', '3.3.3.3', '10.0.0.3', 'device3', CURRENT_TIMESTAMP - INTERVAL '60 days', NULL, 'I', 'userhash3'),
		('user4', 'owner4', 'activated', 'IBRL', '4.4.4.4', '10.0.0.4', 'device4', CURRENT_TIMESTAMP - INTERVAL '45 days', NULL, 'I', 'userhash4'),
		('user5', 'owner5', 'activated', 'IBRL', '5.5.5.5', '10.0.0.5', 'device5', CURRENT_TIMESTAMP - INTERVAL '20 days', NULL, 'I', 'userhash5'),
		-- Historical users that were on DZ but disconnected
		('user6', 'owner6', 'activated', 'IBRL', '6.6.6.6', '10.0.0.6', 'device1', CURRENT_TIMESTAMP - INTERVAL '90 days', CURRENT_TIMESTAMP - INTERVAL '10 days', 'D', 'userhash6'),
		('user7', 'owner7', 'activated', 'IBRL', '7.7.7.7', '10.0.0.7', 'device2', CURRENT_TIMESTAMP - INTERVAL '120 days', CURRENT_TIMESTAMP - INTERVAL '15 days', 'D', 'userhash7')
	`)
	require.NoError(t, err)

	// Seed Solana gossip nodes
	// Currently on DZ: nodes 1-5 (matching current users)
	// Historical on DZ: nodes 6-7 (matching historical users)
	// Not on DZ: nodes 8-15 (various IPs, not matching any DZ users)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_gossip_nodes_current (
			pubkey, gossip_ip, tpuquic_ip, version, epoch, gossip_port, tpuquic_port, as_of_ts, row_hash
		) VALUES
		-- Currently on DZ
		('node1', '10.0.0.1', '10.0.0.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash1'),
		('node2', '10.0.0.2', '10.0.0.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash2'),
		('node3', '10.0.0.3', '10.0.0.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash3'),
		('node4', '10.0.0.4', '10.0.0.4', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash4'),
		('node5', '10.0.0.5', '10.0.0.5', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash5'),
		-- Historical on DZ (still exist but not currently connected)
		('node6', '10.0.0.6', '10.0.0.6', '1.17.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash6'),
		('node7', '10.0.0.7', '10.0.0.7', '1.17.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash7'),
		-- Not on DZ - validators
		('node8', '192.168.1.1', '192.168.1.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash8'),
		('node9', '192.168.1.2', '192.168.1.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash9'),
		('node10', '192.168.1.3', '192.168.1.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash10'),
		('node11', '192.168.1.4', '192.168.1.4', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash11'),
		-- Not on DZ - gossip-only nodes
		('node12', '192.168.1.5', '192.168.1.5', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash12'),
		('node13', '192.168.1.6', '192.168.1.6', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash13'),
		('node14', '192.168.1.7', '192.168.1.7', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash14'),
		('node15', '192.168.1.8', '192.168.1.8', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP, 'nodehash15')
	`)
	require.NoError(t, err)

	// Seed history table with temporal data
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_gossip_nodes_history (
			pubkey, gossip_ip, tpuquic_ip, version, epoch, gossip_port, tpuquic_port, valid_from, valid_to, op, row_hash
		) VALUES
		-- Currently on DZ (active)
		('node1', '10.0.0.1', '10.0.0.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '7 days', NULL, 'I', 'nodehash1'),
		('node2', '10.0.0.2', '10.0.0.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'nodehash2'),
		('node3', '10.0.0.3', '10.0.0.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '60 days', NULL, 'I', 'nodehash3'),
		('node4', '10.0.0.4', '10.0.0.4', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '45 days', NULL, 'I', 'nodehash4'),
		('node5', '10.0.0.5', '10.0.0.5', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '20 days', NULL, 'I', 'nodehash5'),
		-- Historical on DZ (were connected but disconnected)
		('node6', '10.0.0.6', '10.0.0.6', '1.17.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '90 days', NULL, 'I', 'nodehash6'),
		('node7', '10.0.0.7', '10.0.0.7', '1.17.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '120 days', NULL, 'I', 'nodehash7'),
		-- Not on DZ - validators (long-standing)
		('node8', '192.168.1.1', '192.168.1.1', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '180 days', NULL, 'I', 'nodehash8'),
		('node9', '192.168.1.2', '192.168.1.2', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '150 days', NULL, 'I', 'nodehash9'),
		('node10', '192.168.1.3', '192.168.1.3', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '200 days', NULL, 'I', 'nodehash10'),
		('node11', '192.168.1.4', '192.168.1.4', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '100 days', NULL, 'I', 'nodehash11'),
		-- Not on DZ - gossip-only nodes
		('node12', '192.168.1.5', '192.168.1.5', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '90 days', NULL, 'I', 'nodehash12'),
		('node13', '192.168.1.6', '192.168.1.6', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '75 days', NULL, 'I', 'nodehash13'),
		('node14', '192.168.1.7', '192.168.1.7', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '60 days', NULL, 'I', 'nodehash14'),
		('node15', '192.168.1.8', '192.168.1.8', '1.18.0', 100, 8001, 8002, CURRENT_TIMESTAMP - INTERVAL '45 days', NULL, 'I', 'nodehash15')
	`)
	require.NoError(t, err)

	// Seed Solana vote accounts
	// Currently on DZ: vote1-3 (nodes 1-3)
	// Historical on DZ: vote6 (node6)
	// Not on DZ: vote8-11 (nodes 8-11)
	// Nodes 4, 5, 7, 12-15 are gossip-only (no vote accounts)
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_vote_accounts_current (
			vote_pubkey, node_pubkey, epoch_vote_account, epoch, activated_stake_lamports, commission_percentage, as_of_ts, row_hash
		) VALUES
		-- Currently on DZ validators
		('vote1', 'node1', 'true', 100, 1000000000000, 5, CURRENT_TIMESTAMP, 'votehash1'),
		('vote2', 'node2', 'true', 100, 2000000000000, 5, CURRENT_TIMESTAMP, 'votehash2'),
		('vote3', 'node3', 'true', 100, 1500000000000, 5, CURRENT_TIMESTAMP, 'votehash3'),
		-- Historical on DZ validator (was connected but disconnected)
		('vote6', 'node6', 'true', 100, 1800000000000, 5, CURRENT_TIMESTAMP, 'votehash6'),
		-- Not on DZ validators
		('vote8', 'node8', 'true', 100, 3000000000000, 5, CURRENT_TIMESTAMP, 'votehash8'),
		('vote9', 'node9', 'true', 100, 2500000000000, 5, CURRENT_TIMESTAMP, 'votehash9'),
		('vote10', 'node10', 'true', 100, 4000000000000, 5, CURRENT_TIMESTAMP, 'votehash10'),
		('vote11', 'node11', 'true', 100, 2200000000000, 5, CURRENT_TIMESTAMP, 'votehash11')
	`)
	require.NoError(t, err)

	// Seed history table with temporal data
	_, err = conn.ExecContext(ctx, `
		INSERT INTO solana_vote_accounts_history (
			vote_pubkey, node_pubkey, epoch_vote_account, epoch, activated_stake_lamports, commission_percentage, valid_from, valid_to, op, row_hash
		) VALUES
		-- Currently on DZ validators
		('vote1', 'node1', 'true', 100, 1000000000000, 5, CURRENT_TIMESTAMP - INTERVAL '7 days', NULL, 'I', 'votehash1'),
		('vote2', 'node2', 'true', 100, 2000000000000, 5, CURRENT_TIMESTAMP - INTERVAL '30 days', NULL, 'I', 'votehash2'),
		('vote3', 'node3', 'true', 100, 1500000000000, 5, CURRENT_TIMESTAMP - INTERVAL '60 days', NULL, 'I', 'votehash3'),
		-- Historical on DZ validator
		('vote6', 'node6', 'true', 100, 1800000000000, 5, CURRENT_TIMESTAMP - INTERVAL '90 days', NULL, 'I', 'votehash6'),
		-- Not on DZ validators
		('vote8', 'node8', 'true', 100, 3000000000000, 5, CURRENT_TIMESTAMP - INTERVAL '180 days', NULL, 'I', 'votehash8'),
		('vote9', 'node9', 'true', 100, 2500000000000, 5, CURRENT_TIMESTAMP - INTERVAL '150 days', NULL, 'I', 'votehash9'),
		('vote10', 'node10', 'true', 100, 4000000000000, 5, CURRENT_TIMESTAMP - INTERVAL '200 days', NULL, 'I', 'votehash10'),
		('vote11', 'node11', 'true', 100, 2200000000000, 5, CURRENT_TIMESTAMP - INTERVAL '100 days', NULL, 'I', 'votehash11')
	`)
	require.NoError(t, err)
}
