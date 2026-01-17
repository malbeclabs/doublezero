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

func TestLake_Agent_Evals_Anthropic_SolanaValidatorsConnectedStakeIncrease(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SolanaValidatorsConnectedStakeIncrease(t, newAnthropicLLMClient)
}


func runTest_SolanaValidatorsConnectedStakeIncrease(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed Solana validators connected during stake increase data
	seedSolanaValidatorsConnectedStakeIncreaseData(t, ctx, conn)

	// Validate database query results before testing agent
	validateSolanaValidatorsConnectedStakeIncreaseQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query
	question := "which solana validators connected to dz between 24 hours ago and 22 hours ago"
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

	// Evaluate with Ollama - include specific expectations
	expectations := []Expectation{
		{
			Description:   "Response lists vote1 and vote2",
			ExpectedValue: "vote1 and vote2 appear in the response (these are the correct validators)",
			Rationale:     "vote1 and vote2 connected during the window - listing them is CORRECT",
		},
		{
			Description:   "Response does NOT list vote3 or vote4",
			ExpectedValue: "vote3 and vote4 should NOT appear (they were already connected before the window)",
			Rationale:     "Excluding vote3 and vote4 is CORRECT behavior - they were connected before the time window",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly answer the question")
}

// seedSolanaValidatorsConnectedStakeIncreaseData seeds data for testing validators connected during a time window
// Test scenario:
// - T1 (24h ago): vote3 and vote4 already connected
// - T1+30min: vote2 connects
// - T1+1hour: vote1 connects
// - T2 (22h ago): End of window
// The question asks "which validators connected between 24h and 22h ago"
// Expected answer: vote1 and vote2 (they connected during the window)
// Should NOT include: vote3 or vote4 (they were already connected before T1)
func seedSolanaValidatorsConnectedStakeIncreaseData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	now := testTime()
	// T1 = 24 hours ago (the "before" state)
	// T1+30min = vote2 connects
	// T1+1hour = vote1 connects, vote3 receives stake
	// T1+2hours = current time

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
	// user1: Connected at T1+1 hour (for vote1 - actually connected during increase)
	// user2: Connected at T1+30 minutes (for vote2 - actually connected during increase)
	// user3: Connected before T1, still connected (for vote3 - already connected, received stake)
	// user4: Connected before T1, still connected (for vote4 - already connected, no stake change)
	usersBeforeT1 := []serviceability.User{
		{PK: "user3", OwnerPubkey: "owner3", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("3.3.3.3"), DZIP: net.ParseIP("10.0.0.3"), DevicePK: "device1", TunnelID: 503},
		{PK: "user4", OwnerPubkey: "owner4", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("4.4.4.4"), DZIP: net.ParseIP("10.0.0.4"), DevicePK: "device1", TunnelID: 504},
	}
	seedUsers(t, ctx, conn, usersBeforeT1, now.Add(-30*24*time.Hour), now, testOpID()) // Connected 30 days ago

	user2 := serviceability.User{PK: "user2", OwnerPubkey: "owner2", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("2.2.2.2"), DZIP: net.ParseIP("10.0.0.2"), DevicePK: "device1", TunnelID: 502}
	seedUsers(t, ctx, conn, []serviceability.User{user2}, now.Add(-23*time.Hour-30*time.Minute), now, testOpID()) // Connected at T1+30min

	user1 := serviceability.User{PK: "user1", OwnerPubkey: "owner1", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("1.1.1.1"), DZIP: net.ParseIP("10.0.0.1"), DevicePK: "device1", TunnelID: 501}
	seedUsers(t, ctx, conn, []serviceability.User{user1}, now.Add(-23*time.Hour), now, testOpID()) // Connected at T1+1hour

	// Seed Solana gossip nodes history
	gossipNodesBeforeT1 := []*testGossipNode{
		{Pubkey: "node3", GossipIP: net.ParseIP("10.0.0.3"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.3"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node4", GossipIP: net.ParseIP("10.0.0.4"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.4"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
	}
	seedGossipNodes(t, ctx, conn, gossipNodesBeforeT1, now.Add(-30*24*time.Hour), now, testOpID()) // Connected 30 days ago

	node2 := &testGossipNode{Pubkey: "node2", GossipIP: net.ParseIP("10.0.0.2"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.2"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100}
	seedGossipNodes(t, ctx, conn, []*testGossipNode{node2}, now.Add(-23*time.Hour-30*time.Minute), now, testOpID()) // Connected at T1+30min

	node1 := &testGossipNode{Pubkey: "node1", GossipIP: net.ParseIP("10.0.0.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.1"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100}
	seedGossipNodes(t, ctx, conn, []*testGossipNode{node1}, now.Add(-23*time.Hour), now, testOpID()) // Connected at T1+1hour

	// Seed Solana vote accounts history
	// vote1: Connected at T1+1 hour, stake 15,000 SOL
	// vote2: Connected at T1+30 minutes, stake 1,000 SOL
	// vote3: Connected before T1, stake 5,000 SOL
	// vote4: Connected before T1, stake 3,000 SOL
	voteAccountsBeforeT1 := []testVoteAccount{
		{VotePubkey: "vote3", NodePubkey: "node3", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 5000000000000, Commission: 5},
		{VotePubkey: "vote4", NodePubkey: "node4", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 3000000000000, Commission: 5},
	}
	seedVoteAccounts(t, ctx, conn, voteAccountsBeforeT1, now.Add(-30*24*time.Hour), now, testOpID()) // Before T1

	vote2 := testVoteAccount{VotePubkey: "vote2", NodePubkey: "node2", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1000000000000, Commission: 5}
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{vote2}, now.Add(-23*time.Hour-30*time.Minute), now, testOpID()) // Connected at T1+30min

	vote1 := testVoteAccount{VotePubkey: "vote1", NodePubkey: "node1", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 15000000000000, Commission: 5}
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{vote1}, now.Add(-23*time.Hour), now, testOpID()) // Connected at T1+1hour
}

// validateSolanaValidatorsConnectedStakeIncreaseQuery runs the ideal query to answer the question
// and validates that the database returns the expected results (vote1 and vote2 connected during the window)
//
// This query finds validators that first connected during a specific time window, excluding those
// that were already connected before the window. This demonstrates how to find "new connections"
// during a time period using SCD Type 2 history tables.
func validateSolanaValidatorsConnectedStakeIncreaseQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	now := testTime()
	// Window: 24 hours ago (T1) to 22 hours ago (T2)
	t1 := now.Add(-24 * time.Hour)
	t2 := now.Add(-22 * time.Hour)

	// Query for validators that connected during the time window (between T1 and T2)
	// This finds validators where the connection started (first appeared as connected) during the window
	// Strategy: Find the earliest snapshot_ts where user, gossip node, and vote account all exist together
	// and that snapshot_ts is within the window, and the validator was NOT connected before T1
	// This pattern works reliably in ClickHouse and is something the agent can execute
	query := `
-- Find validators that first connected during the window (between T1 and T2)
-- A validator "connects" when user, gossip node, and vote account all exist together for the first time
-- We find the earliest snapshot_ts where all three exist together (the maximum of their individual snapshot_ts values)
WITH connection_events AS (
  -- Find all times when a validator was connected (user, gossip node, and vote account all exist together)
  -- The connection timestamp is the maximum of the three snapshot_ts values (when all three first exist together)
  SELECT
    va.vote_pubkey,
    GREATEST(u.snapshot_ts, gn.snapshot_ts, va.snapshot_ts) AS connection_ts
  FROM dim_dz_users_history u
  JOIN dim_solana_gossip_nodes_history gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL
  JOIN dim_solana_vote_accounts_history va ON gn.pubkey = va.node_pubkey
  WHERE u.is_deleted = 0 AND u.status = 'activated' AND u.dz_ip IS NOT NULL
    AND gn.is_deleted = 0
    AND va.is_deleted = 0
    AND va.epoch_vote_account = 'true' AND va.activated_stake_lamports > 0
),
first_connections AS (
  -- Find the first connection time for each validator (the earliest time they were connected)
  SELECT vote_pubkey, MIN(connection_ts) AS first_connection_ts
  FROM connection_events
  GROUP BY vote_pubkey
),
validators_connected_during_window AS (
  -- Validators whose first connection was during the window (between T1 and T2)
  SELECT vote_pubkey
  FROM first_connections
  WHERE first_connection_ts >= ? AND first_connection_ts <= ?
),
validators_connected_before_window AS (
  -- Find validators that were already connected before T1 (should be excluded)
  -- Uses SCD Type 2 pattern: ROW_NUMBER() to get latest snapshot at/before T1
  SELECT DISTINCT va.vote_pubkey
  FROM (
    SELECT *,
      ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_dz_users_history
    WHERE snapshot_ts < ? AND is_deleted = 0
  ) u
  JOIN (
    SELECT *,
      ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_gossip_nodes_history
    WHERE snapshot_ts < ? AND is_deleted = 0
  ) gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL AND gn.rn = 1
  JOIN (
    SELECT *,
      ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_vote_accounts_history
    WHERE snapshot_ts < ? AND is_deleted = 0
  ) va ON gn.pubkey = va.node_pubkey AND va.rn = 1
  WHERE u.rn = 1 AND u.status = 'activated' AND u.dz_ip IS NOT NULL
    AND va.epoch_vote_account = 'true' AND va.activated_stake_lamports > 0
)
-- Validators that connected during the window but were NOT connected before T1
-- Use NOT IN pattern which works reliably in ClickHouse
SELECT DISTINCT vcdw.vote_pubkey
FROM validators_connected_during_window vcdw
WHERE vcdw.vote_pubkey NOT IN (SELECT vote_pubkey FROM validators_connected_before_window)
ORDER BY vcdw.vote_pubkey
`

	result, err := dataset.Query(ctx, conn, query, []any{
		t1, // window start
		t2, // window end
		t1, // before window (users)
		t1, // before window (gossip nodes)
		t1, // before window (vote accounts)
	})
	require.NoError(t, err, "Failed to execute connected validators query")

	// Expected: vote1 and vote2 connected during the window
	// vote3 was already connected before T1, so should NOT be in results
	require.Equal(t, 2, result.Count, "Expected 2 validators connected during the window (vote1, vote2), but got %d", result.Count)

	votePubkeys := make([]string, 0, 2)
	for _, row := range result.Rows {
		votePubkey, ok := row["vote_pubkey"].(string)
		require.True(t, ok, "vote_pubkey should be a string")
		votePubkeys = append(votePubkeys, votePubkey)
	}

	expectedVotes := []string{"vote1", "vote2"}
	require.ElementsMatch(t, expectedVotes, votePubkeys,
		fmt.Sprintf("Expected connected validators to be %v, but got %v. vote3 should NOT be included as it was already connected before T1.", expectedVotes, votePubkeys))

	t.Logf("Database validation passed: Found %d validators connected during the time window: %v", result.Count, votePubkeys)
}
