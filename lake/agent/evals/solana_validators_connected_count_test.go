//go:build evals

package evals_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_SolanaValidatorsConnectedCount(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SolanaValidatorsConnectedCount(t, newAnthropicLLMClient)
}


func runTest_SolanaValidatorsConnectedCount(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed Solana validators connected count data
	seedSolanaValidatorsConnectedCountData(t, ctx, conn)

	// Validate database query results before testing agent
	validateSolanaValidatorsConnectedCountQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query
	question := "How many Solana validators connected to dz in the last day"
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

	// Basic validation - the response should identify connected validators
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

	// Evaluate with Ollama - include specific numeric expectations
	expectations := []Expectation{
		{
			Description:   "Count of validators newly connected in the last day",
			ExpectedValue: "3 validators (the number 3 must appear)",
			Rationale:     "The question asks 'how many' so the response must include the count of 3",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly answer the question")
}

// seedSolanaValidatorsConnectedCountData seeds data for testing "how many validators connected in the last day"
// Test scenario:
// - vote3 and vote4: Already connected before 24 hours ago (should NOT be counted as "newly connected")
// - vote1: Newly connected 12 hours ago (should be counted)
// - vote2: Newly connected 6 hours ago (should be counted)
// - vote5: Connected 12 hours ago (same timestamp as vote1) - since it's currently connected and wasn't connected 24 hours ago, it IS newly connected
// Expected answer for "newly connected": 3 validators (vote1, vote2, and vote5)
// Expected answer for "currently connected": 5 validators (all are currently connected)
func seedSolanaValidatorsConnectedCountData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	now := testTime()

	// Seed metros
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed devices
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	// Seed DZ users history
	// user1: Connected 12 hours ago (for vote1 - newly connected)
	// user2: Connected 6 hours ago (for vote2 - newly connected)
	// user3: Connected 30 days ago (for vote3 - already connected, should NOT be counted)
	// user4: Connected 30 days ago (for vote4 - already connected, should NOT be counted)
	// user5: Connected 12 hours ago (for vote5 - bulk ingestion, same timestamp as vote1)
	users30DaysAgo := []serviceability.User{
		{PK: "user3", OwnerPubkey: "owner3", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("3.3.3.3"), DZIP: net.ParseIP("10.0.0.3"), DevicePK: "device1", TunnelID: 503},
		{PK: "user4", OwnerPubkey: "owner4", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("4.4.4.4"), DZIP: net.ParseIP("10.0.0.4"), DevicePK: "device1", TunnelID: 504},
	}
	seedUsers(t, ctx, conn, users30DaysAgo, now.Add(-30*24*time.Hour), now, testOpID()) // user3, user4: 30 days ago

	users12HoursAgo := []serviceability.User{
		{PK: "user1", OwnerPubkey: "owner1", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("1.1.1.1"), DZIP: net.ParseIP("10.0.0.1"), DevicePK: "device1", TunnelID: 501},
		{PK: "user5", OwnerPubkey: "owner5", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("5.5.5.5"), DZIP: net.ParseIP("10.0.0.5"), DevicePK: "device1", TunnelID: 505},
	}
	seedUsers(t, ctx, conn, users12HoursAgo, now.Add(-12*time.Hour), now, testOpID()) // user1, user5: 12 hours ago

	users6HoursAgo := []serviceability.User{
		{PK: "user2", OwnerPubkey: "owner2", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("2.2.2.2"), DZIP: net.ParseIP("10.0.0.2"), DevicePK: "device1", TunnelID: 502},
	}
	seedUsers(t, ctx, conn, users6HoursAgo, now.Add(-6*time.Hour), now, testOpID()) // user2: 6 hours ago

	// Seed Solana gossip nodes history
	gossipNodes30DaysAgo := []*testGossipNode{
		{Pubkey: "node3", GossipIP: net.ParseIP("10.0.0.3"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.3"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node4", GossipIP: net.ParseIP("10.0.0.4"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.4"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
	}
	seedGossipNodes(t, ctx, conn, gossipNodes30DaysAgo, now.Add(-30*24*time.Hour), now, testOpID()) // node3, node4: 30 days ago

	gossipNodes12HoursAgo := []*testGossipNode{
		{Pubkey: "node1", GossipIP: net.ParseIP("10.0.0.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.1"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node5", GossipIP: net.ParseIP("10.0.0.5"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.5"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
	}
	seedGossipNodes(t, ctx, conn, gossipNodes12HoursAgo, now.Add(-12*time.Hour), now, testOpID()) // node1, node5: 12 hours ago

	gossipNodes6HoursAgo := []*testGossipNode{
		{Pubkey: "node2", GossipIP: net.ParseIP("10.0.0.2"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.2"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
	}
	seedGossipNodes(t, ctx, conn, gossipNodes6HoursAgo, now.Add(-6*time.Hour), now, testOpID()) // node2: 6 hours ago

	// Seed Solana vote accounts history
	voteAccounts30DaysAgo := []testVoteAccount{
		{VotePubkey: "vote3", NodePubkey: "node3", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1000000000000, Commission: 5},
		{VotePubkey: "vote4", NodePubkey: "node4", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1000000000000, Commission: 5},
	}
	seedVoteAccounts(t, ctx, conn, voteAccounts30DaysAgo, now.Add(-30*24*time.Hour), now, testOpID()) // vote3, vote4: 30 days ago

	voteAccounts12HoursAgo := []testVoteAccount{
		{VotePubkey: "vote1", NodePubkey: "node1", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1000000000000, Commission: 5},
		{VotePubkey: "vote5", NodePubkey: "node5", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1000000000000, Commission: 5},
	}
	seedVoteAccounts(t, ctx, conn, voteAccounts12HoursAgo, now.Add(-12*time.Hour), now, testOpID()) // vote1, vote5: 12 hours ago

	voteAccounts6HoursAgo := []testVoteAccount{
		{VotePubkey: "vote2", NodePubkey: "node2", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1000000000000, Commission: 5},
	}
	seedVoteAccounts(t, ctx, conn, voteAccounts6HoursAgo, now.Add(-6*time.Hour), now, testOpID()) // vote2: 6 hours ago
}

// validateSolanaValidatorsConnectedCountQuery runs the ideal query to answer the question
// and validates that the database returns the expected results (3 newly connected validators)
func validateSolanaValidatorsConnectedCountQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	now := testTime()
	query24HoursAgo := now.Add(-24 * time.Hour)

	// Use the historical comparison method as described in CATALOG.md
	// This finds validators currently connected but NOT connected 24 hours ago
	// History tables use snapshot_ts (SCD Type 2), so we need to find the latest snapshot at/before 24h ago
	// Use NOT IN pattern which the agent can execute

	// Execute the actual query that the agent would use
	// This query finds validators that are currently connected but were NOT connected 24 hours ago
	// Strategy: Get all current validators, then exclude those that existed 24h ago using NOT IN
	// This pattern works reliably in ClickHouse and is something the agent can execute
	mainQuery := `
-- Count validators that are currently connected but were not connected 24 hours ago
SELECT COUNT(DISTINCT cv.vote_pubkey) AS newly_connected_count
FROM (
  -- Get all validators currently connected to DoubleZero
  -- A validator is "connected" if:
  --   1. User is activated and has a DZ IP
  --   2. Gossip node exists and matches the user's DZ IP
  --   3. Vote account exists for that gossip node and has activated stake
  SELECT DISTINCT va.vote_pubkey
  FROM dz_users_current u
  -- Join to gossip nodes: user's DZ IP must match gossip node's gossip IP
  JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL
  -- Join to vote accounts: gossip node's pubkey must match vote account's node_pubkey
  JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
  WHERE u.status = 'activated'
    AND va.activated_stake_lamports > 0  -- Only validators with stake
) cv
WHERE cv.vote_pubkey NOT IN (
  -- Get all validators that WERE connected 24 hours ago
  -- This uses the history tables (SCD Type 2) to reconstruct the state at a point in time
  SELECT DISTINCT va2.vote_pubkey
  FROM (
    -- Get the latest user record at/before the target time (24h ago)
    -- SCD Type 2: Use ROW_NUMBER to get the most recent snapshot for each entity
    -- Order by snapshot_ts DESC, then ingested_at DESC, then op_id DESC to handle multiple records
    SELECT *,
      ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_dz_users_history
    WHERE snapshot_ts <= ?  -- Only records at or before 24h ago
      AND is_deleted = 0     -- Exclude soft-deleted records
  ) u2
  JOIN (
    -- Get the latest gossip node record at/before the target time
    SELECT *,
      ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_gossip_nodes_history
    WHERE snapshot_ts <= ?  -- Only records at or before 24h ago
      AND is_deleted = 0     -- Exclude soft-deleted records
  ) gn2 ON u2.dz_ip = gn2.gossip_ip
       AND gn2.gossip_ip IS NOT NULL
       AND gn2.rn = 1  -- Only join to the latest snapshot for each gossip node
  JOIN (
    -- Get the latest vote account record at/before the target time
    SELECT *,
      ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_vote_accounts_history
    WHERE snapshot_ts <= ?  -- Only records at or before 24h ago
      AND is_deleted = 0     -- Exclude soft-deleted records
  ) va2 ON gn2.pubkey = va2.node_pubkey
       AND va2.rn = 1  -- Only join to the latest snapshot for each vote account
  WHERE u2.rn = 1                    -- Only use the latest user snapshot
    AND u2.status = 'activated'      -- User must be activated
    AND u2.dz_ip IS NOT NULL         -- User must have a DZ IP
    AND va2.epoch_vote_account = 'true'  -- Vote account must be an epoch vote account (stored as string)
    AND va2.activated_stake_lamports > 0  -- Vote account must have activated stake
)
`

	result, err := dataset.Query(ctx, conn, mainQuery, []any{
		query24HoursAgo, // users snapshot_ts
		query24HoursAgo, // gossip_nodes snapshot_ts
		query24HoursAgo, // vote_accounts snapshot_ts
	})
	require.NoError(t, err, "Failed to execute database validation query")
	require.Equal(t, 1, result.Count, "Query should return exactly one row")

	newlyConnectedCount, ok := result.Rows[0]["newly_connected_count"].(uint64)
	if !ok {
		// Try int64 or other numeric types
		switch v := result.Rows[0]["newly_connected_count"].(type) {
		case int64:
			newlyConnectedCount = uint64(v)
		case int:
			newlyConnectedCount = uint64(v)
		case uint32:
			newlyConnectedCount = uint64(v)
		case int32:
			newlyConnectedCount = uint64(v)
		default:
			t.Fatalf("Unexpected type for newly_connected_count: %T, value: %v", v, v)
		}
	}

	// Expected: 3 validators newly connected (vote1, vote2, and vote5)
	// vote3 and vote4 were already connected 30 days ago, so they should NOT be counted
	require.Equal(t, uint64(3), newlyConnectedCount,
		fmt.Sprintf("Expected 3 newly connected validators (vote1, vote2, vote5), but got %d. vote3 and vote4 should NOT be counted as they were already connected 30 days ago.", newlyConnectedCount))

	// Also verify which validators are newly connected
	validatorQuery := `
SELECT DISTINCT cv.vote_pubkey
FROM (
  SELECT DISTINCT va.vote_pubkey
  FROM dz_users_current u
  JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL
  JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
  WHERE u.status = 'activated' AND va.activated_stake_lamports > 0
) cv
WHERE cv.vote_pubkey NOT IN (
  SELECT DISTINCT va2.vote_pubkey
  FROM (
    SELECT *,
      ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_dz_users_history
    WHERE snapshot_ts <= ? AND is_deleted = 0
  ) u2
  JOIN (
    SELECT *,
      ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_gossip_nodes_history
    WHERE snapshot_ts <= ? AND is_deleted = 0
  ) gn2 ON u2.dz_ip = gn2.gossip_ip AND gn2.gossip_ip IS NOT NULL AND gn2.rn = 1
  JOIN (
    SELECT *,
      ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_vote_accounts_history
    WHERE snapshot_ts <= ? AND is_deleted = 0
  ) va2 ON gn2.pubkey = va2.node_pubkey AND va2.rn = 1
  WHERE u2.rn = 1 AND u2.status = 'activated' AND u2.dz_ip IS NOT NULL
    AND va2.epoch_vote_account = 'true' AND va2.activated_stake_lamports > 0
)
ORDER BY cv.vote_pubkey
`

	validatorResult, err := dataset.Query(ctx, conn, validatorQuery, []any{
		query24HoursAgo, // users snapshot_ts
		query24HoursAgo, // gossip_nodes snapshot_ts
		query24HoursAgo, // vote_accounts snapshot_ts
	})
	require.NoError(t, err, "Failed to execute validator list query")
	require.Equal(t, 3, validatorResult.Count, "Should have exactly 3 newly connected validators")

	votePubkeys := make([]string, 0, 3)
	for _, row := range validatorResult.Rows {
		votePubkey, ok := row["vote_pubkey"].(string)
		require.True(t, ok, "vote_pubkey should be a string")
		votePubkeys = append(votePubkeys, votePubkey)
	}

	expectedVotes := []string{"vote1", "vote2", "vote5"}
	require.ElementsMatch(t, expectedVotes, votePubkeys,
		fmt.Sprintf("Expected newly connected validators to be %v, but got %v", expectedVotes, votePubkeys))

	t.Logf("Database validation passed: Found %d newly connected validators: %v", newlyConnectedCount, votePubkeys)
}
