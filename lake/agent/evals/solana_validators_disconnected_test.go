//go:build evals

package evals_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/sol"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_SolanaValidatorsDisconnected(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SolanaValidatorsDisconnected(t, newAnthropicLLMClient)
}


func runTest_SolanaValidatorsDisconnected(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed Solana validators disconnected data
	seedSolanaValidatorsDisconnectedData(t, ctx, conn)

	// Validate database query results before testing agent
	validateSolanaValidatorsDisconnectedQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query
	question := "which solana validators disconnected from dz in the past 24 hours"
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

	// Basic validation - the response should identify disconnected validators
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

	// Evaluate with LLM - include specific expectations
	// Keep expectations simple and binary to avoid evaluator confusion
	expectations := []Expectation{
		{
			Description:   "Response identifies vote1, vote2, vote3 as disconnected",
			ExpectedValue: "The response lists vote1/node1, vote2/node2, and vote3/node3 as validators that disconnected from DZ (either vote_pubkey or node_pubkey identifier is acceptable). vote6/node6 may also appear (acceptable). Each should have stake amounts.",
			Rationale:     "These validators disconnected from DZ within the query timeframe",
		},
		{
			Description:   "Response does NOT claim vote4 or vote5 disconnected",
			ExpectedValue: "vote4/node4 and vote5/node5 are NOT listed among the disconnected validators. They may be mentioned as 'still connected' (which is correct) or not mentioned at all - either is fine.",
			Rationale:     "vote4 reconnected; vote5 never disconnected - neither should be listed as having disconnected",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly answer the question")
}

// seedSolanaValidatorsDisconnectedData seeds data for testing disconnected validators
// Test cases:
// - vote1: Disconnected 12 hours ago, gossip_ip changed from 10.0.0.1 to 192.168.1.1 (validator changed IP)
// - vote2: Disconnected 6 hours ago, user disconnected (gossip_ip stayed 10.0.0.2)
// - vote3: Disconnected 18 hours ago, user disconnected (gossip_ip stayed 10.0.0.3)
// - vote4: Disconnected 20 hours ago but reconnected 10 hours ago (should NOT be in results)
// - vote5: Still connected (should NOT be in results)
// - vote6: Disconnected 15 hours ago, reconnected 8 hours ago, disconnected again 2 hours ago (flapping - should be in results as currently disconnected)
func seedSolanaValidatorsDisconnectedData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	// Seed metros
	metroDS, err := serviceability.NewMetroDataset(log)
	require.NoError(t, err)
	err = metroDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		// PK: pk, Payload: code, name, longitude, latitude
		return []any{"metro1", "nyc", "New York", -74.0060, 40.7128}, nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Seed devices
	deviceDS, err := serviceability.NewDeviceDataset(log)
	require.NoError(t, err)
	err = deviceDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		// PK: pk, Payload: status, device_type, code, public_ip, contributor_pk, metro_pk, max_users, interfaces
		return []any{"device1", "activated", "DZD", "nyc-dzd1", "", "", "metro1", 100, "[]"}, nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// Seed DZ users history
	// user1: Connected 30 days ago, disconnected 12 hours ago (for vote1 - validator changed IP)
	// user2: Connected 30 days ago, disconnected 6 hours ago (for vote2)
	// user3: Connected 30 days ago, disconnected 18 hours ago (for vote3)
	// user4: Connected 30 days ago, disconnected 20 hours ago, reconnected 10 hours ago (for vote4 - should not appear)
	// user5: Connected 30 days ago, still connected (for vote5 - should not appear)
	// user6: Connected 30 days ago, disconnected 15 hours ago, reconnected 8 hours ago, disconnected 2 hours ago (for vote6 - flapping, should appear as currently disconnected)
	userDS, err := serviceability.NewUserDataset(log)
	require.NoError(t, err)

	// Helper to insert user with timeline
	insertUser := func(pk, ownerPubkey, clientIP, dzIP string, tunnelID int32, connectedTS time.Time) {
		// Insert connected state
		err = userDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			// PK: pk, Payload: owner_pubkey, status, kind, client_ip, dz_ip, device_pk, tunnel_id
			return []any{pk, ownerPubkey, "activated", "IBRL", clientIP, dzIP, "device1", tunnelID}, nil
		}, &dataset.DimensionType2DatasetWriteConfig{
			SnapshotTS: connectedTS,
			OpID:       testOpID(),
		})
		require.NoError(t, err)
	}

	// Helper to mark user as deleted at a timestamp using WriteBatch
	deleteUser := func(pkToDelete string, deletedTS time.Time) {
		// Get all current users
		currentRows, err := userDS.GetCurrentRows(ctx, conn, nil)
		require.NoError(t, err)

		// Filter out the user we want to delete
		var remainingUsers []map[string]any
		for _, row := range currentRows {
			if row["pk"] != pkToDelete {
				remainingUsers = append(remainingUsers, row)
			}
		}

		// Write snapshot with only remaining users (MissingMeansDeleted will delete the excluded one)
		if len(remainingUsers) > 0 {
			err = userDS.WriteBatch(ctx, conn, len(remainingUsers), func(i int) ([]any, error) {
				row := remainingUsers[i]
				// PK: pk, Payload: owner_pubkey, status, kind, client_ip, dz_ip, device_pk, tunnel_id
				return []any{
					row["pk"],
					row["owner_pubkey"],
					row["status"],
					row["kind"],
					row["client_ip"],
					row["dz_ip"],
					row["device_pk"],
					row["tunnel_id"],
				}, nil
			}, &dataset.DimensionType2DatasetWriteConfig{
				SnapshotTS:          deletedTS,
				OpID:                testOpID(),
				MissingMeansDeleted: true,
			})
			require.NoError(t, err)
		} else {
			// No remaining users, write empty snapshot to delete all
			err = userDS.WriteBatch(ctx, conn, 0, nil, &dataset.DimensionType2DatasetWriteConfig{
				SnapshotTS:          deletedTS,
				OpID:                testOpID(),
				MissingMeansDeleted: true,
			})
			require.NoError(t, err)
		}
	}

	// user1: Connected 30 days ago, disconnected 12 hours ago
	insertUser("user1", "owner1", "1.1.1.1", "10.0.0.1", 501, now.Add(-30*24*time.Hour))
	deleteUser("user1", now.Add(-12*time.Hour))

	// user2: Connected 30 days ago, disconnected 6 hours ago
	insertUser("user2", "owner2", "2.2.2.2", "10.0.0.2", 502, now.Add(-30*24*time.Hour))
	deleteUser("user2", now.Add(-6*time.Hour))

	// user3: Connected 30 days ago, disconnected 18 hours ago
	insertUser("user3", "owner3", "3.3.3.3", "10.0.0.3", 503, now.Add(-30*24*time.Hour))
	deleteUser("user3", now.Add(-18*time.Hour))

	// user4: Connected 30 days ago, disconnected 20 hours ago, reconnected 10 hours ago
	insertUser("user4", "owner4", "4.4.4.4", "10.0.0.4", 504, now.Add(-30*24*time.Hour))
	deleteUser("user4", now.Add(-20*time.Hour))
	insertUser("user4", "owner4", "4.4.4.4", "10.0.0.4", 504, now.Add(-10*time.Hour))

	// user5: Still connected (30 days ago)
	insertUser("user5", "owner5", "5.5.5.5", "10.0.0.5", 505, now.Add(-30*24*time.Hour))

	// user6: Connected 30 days ago, disconnected 15 hours ago, reconnected 8 hours ago, disconnected 2 hours ago
	insertUser("user6", "owner6", "6.6.6.6", "10.0.0.6", 506, now.Add(-30*24*time.Hour))
	deleteUser("user6", now.Add(-15*time.Hour))
	insertUser("user6", "owner6", "6.6.6.6", "10.0.0.6", 506, now.Add(-8*time.Hour))
	deleteUser("user6", now.Add(-2*time.Hour))

	// Seed Solana gossip nodes history
	// node1 (vote1): gossip_ip changed from 10.0.0.1 to 192.168.1.1 (validator changed IP)
	// node2 (vote2): gossip_ip stayed 10.0.0.2
	// node3 (vote3): gossip_ip stayed 10.0.0.3
	// node4 (vote4): gossip_ip stayed 10.0.0.4 (reconnected)
	// node5 (vote5): gossip_ip stayed 10.0.0.5 (still connected)
	// node6 (vote6): gossip_ip stayed 10.0.0.6 (flapping - disconnected 2 hours ago)
	gossipDS, err := sol.NewGossipNodeDataset(log)
	require.NoError(t, err)

	// Helper to insert gossip node
	insertGossipNode := func(pubkey, gossipIP, tpuquicIP, version string, epoch int64, gossipPort, tpuquicPort int32, snapshotTS time.Time) {
		// Insert connected state
		err = gossipDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			// PK: pubkey, Payload: epoch, gossip_ip, gossip_port, tpuquic_ip, tpuquic_port, version
			return []any{pubkey, epoch, gossipIP, gossipPort, tpuquicIP, tpuquicPort, version}, nil
		}, &dataset.DimensionType2DatasetWriteConfig{
			SnapshotTS: snapshotTS,
			OpID:       testOpID(),
		})
		require.NoError(t, err)
	}

	// Helper to delete gossip node using WriteBatch
	deleteGossipNode := func(pubkeyToDelete string, deletedTS time.Time) {
		// Get all current gossip nodes
		currentRows, err := gossipDS.GetCurrentRows(ctx, conn, nil)
		require.NoError(t, err)

		// Filter out the node we want to delete
		var remainingNodes []map[string]any
		for _, row := range currentRows {
			if row["pubkey"] != pubkeyToDelete {
				remainingNodes = append(remainingNodes, row)
			}
		}

		// Write snapshot with only remaining nodes (MissingMeansDeleted will delete the excluded one)
		if len(remainingNodes) > 0 {
			err = gossipDS.WriteBatch(ctx, conn, len(remainingNodes), func(i int) ([]any, error) {
				row := remainingNodes[i]
				// PK: pubkey, Payload: epoch, gossip_ip, gossip_port, tpuquic_ip, tpuquic_port, version
				return []any{
					row["pubkey"],
					row["epoch"],
					row["gossip_ip"],
					row["gossip_port"],
					row["tpuquic_ip"],
					row["tpuquic_port"],
					row["version"],
				}, nil
			}, &dataset.DimensionType2DatasetWriteConfig{
				SnapshotTS:          deletedTS,
				OpID:                testOpID(),
				MissingMeansDeleted: true,
			})
			require.NoError(t, err)
		} else {
			// No remaining nodes, write empty snapshot to delete all
			err = gossipDS.WriteBatch(ctx, conn, 0, nil, &dataset.DimensionType2DatasetWriteConfig{
				SnapshotTS:          deletedTS,
				OpID:                testOpID(),
				MissingMeansDeleted: true,
			})
			require.NoError(t, err)
		}
	}

	// node1: gossip_ip was 10.0.0.1, changed to 192.168.1.1 (disconnected 12 hours ago)
	insertGossipNode("node1", "10.0.0.1", "10.0.0.1", "1.18.0", 100, 8001, 8002, now.Add(-30*24*time.Hour))
	deleteGossipNode("node1", now.Add(-12*time.Hour))
	// Insert new IP after disconnection
	err = gossipDS.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
		return []any{"node1", int64(100), "192.168.1.1", int32(8001), "192.168.1.1", int32(8002), "1.18.0"}, nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-12 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)

	// node2: disconnected 6 hours ago
	insertGossipNode("node2", "10.0.0.2", "10.0.0.2", "1.18.0", 100, 8001, 8002, now.Add(-30*24*time.Hour))
	deleteGossipNode("node2", now.Add(-6*time.Hour))

	// node3: disconnected 18 hours ago
	insertGossipNode("node3", "10.0.0.3", "10.0.0.3", "1.18.0", 100, 8001, 8002, now.Add(-30*24*time.Hour))
	deleteGossipNode("node3", now.Add(-18*time.Hour))

	// node4: reconnected 10 hours ago (still connected)
	insertGossipNode("node4", "10.0.0.4", "10.0.0.4", "1.18.0", 100, 8001, 8002, now.Add(-10*time.Hour))

	// node5: still connected
	insertGossipNode("node5", "10.0.0.5", "10.0.0.5", "1.18.0", 100, 8001, 8002, now.Add(-30*24*time.Hour))

	// node6: disconnected 2 hours ago
	insertGossipNode("node6", "10.0.0.6", "10.0.0.6", "1.18.0", 100, 8001, 8002, now.Add(-30*24*time.Hour))
	deleteGossipNode("node6", now.Add(-2*time.Hour))

	// Seed Solana vote accounts history
	voteDS, err := sol.NewVoteAccountDataset(log)
	require.NoError(t, err)
	err = voteDS.WriteBatch(ctx, conn, 6, func(i int) ([]any, error) {
		// PK: vote_pubkey, Payload: epoch, node_pubkey, activated_stake_lamports, epoch_vote_account, commission_percentage
		stakes := []int64{1000000000000, 1500000000000, 1200000000000, 2000000000000, 1800000000000, 1600000000000}
		return []any{
			fmt.Sprintf("vote%d", i+1),
			int64(100),
			fmt.Sprintf("node%d", i+1),
			stakes[i],
			"true",
			int64(5),
		}, nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now.Add(-30 * 24 * time.Hour),
		OpID:       testOpID(),
	})
	require.NoError(t, err)
}

// validateSolanaValidatorsDisconnectedQuery runs the ideal query to answer the question
// and validates that the database returns the expected results (vote1, vote2, vote3, vote6 disconnected)
//
// This query finds validators that were connected 24 hours ago but are NOT currently connected,
// and where the disconnection happened in the past 24 hours. This uses the historical comparison
// method: get validators connected 24h ago, exclude those currently connected, then verify
// the disconnection timestamp is within the past 24 hours.
func validateSolanaValidatorsDisconnectedQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	now := testTime()
	query24HoursAgo := now.Add(-24 * time.Hour)

	// Query for validators that disconnected in the past 24 hours
	// Strategy: Find validators that were connected 24h ago, exclude those currently connected using NOT IN,
	// then verify the disconnection happened in the past 24 hours by checking when the user was deleted
	// This pattern works reliably in ClickHouse and is something the agent can execute
	query := `
-- Find validators that were connected 24 hours ago but are NOT currently connected
-- AND where the disconnection happened in the past 24 hours
-- This query demonstrates how to find entities that existed in the past but no longer exist,
-- using SCD Type 2 history tables with the NOT IN pattern
SELECT DISTINCT v24h.vote_pubkey
FROM (
  -- Get all validators that were connected 24 hours ago
  -- A validator is "connected" if:
  --   1. User is activated and has a DZ IP
  --   2. Gossip node exists and matches the user's DZ IP
  --   3. Vote account exists for that gossip node and has activated stake
  -- Uses SCD Type 2 pattern: ROW_NUMBER() to get latest snapshot at/before 24h ago
  SELECT DISTINCT va.vote_pubkey, u.entity_id AS user_entity_id
  FROM (
    -- Get the latest user record at/before the target time (24h ago)
    -- SCD Type 2: Use ROW_NUMBER to get the most recent snapshot for each entity
    SELECT *,
      ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_dz_users_history
    WHERE snapshot_ts <= ?  -- Only records at or before 24h ago
      AND is_deleted = 0     -- Exclude soft-deleted records
  ) u
  JOIN (
    -- Get the latest gossip node record at/before the target time
    SELECT *,
      ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_gossip_nodes_history
    WHERE snapshot_ts <= ?  -- Only records at or before 24h ago
      AND is_deleted = 0     -- Exclude soft-deleted records
  ) gn ON u.dz_ip = gn.gossip_ip
       AND gn.gossip_ip IS NOT NULL
       AND gn.rn = 1  -- Only join to the latest snapshot for each gossip node
  JOIN (
    -- Get the latest vote account record at/before the target time
    SELECT *,
      ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
    FROM dim_solana_vote_accounts_history
    WHERE snapshot_ts <= ?  -- Only records at or before 24h ago
      AND is_deleted = 0     -- Exclude soft-deleted records
  ) va ON gn.pubkey = va.node_pubkey
       AND va.rn = 1  -- Only join to the latest snapshot for each vote account
  WHERE u.rn = 1                    -- Only use the latest user snapshot
    AND u.status = 'activated'      -- User must be activated
    AND u.dz_ip IS NOT NULL         -- User must have a DZ IP
    AND va.epoch_vote_account = 'true'  -- Vote account must be an epoch vote account (stored as string)
    AND va.activated_stake_lamports > 0  -- Vote account must have activated stake
) v24h
WHERE v24h.vote_pubkey NOT IN (
  -- Exclude validators that are currently connected
  -- This uses the _current views to get the current state
  SELECT DISTINCT va.vote_pubkey
  FROM dz_users_current u
  JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL
  JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
  WHERE u.status = 'activated' AND va.activated_stake_lamports > 0
)
AND v24h.user_entity_id IN (
  -- Only include validators where the user was deleted (disconnected) in the past 24 hours
  -- This filters to only disconnections that happened within the time window
  SELECT entity_id
  FROM dim_dz_users_history
  WHERE is_deleted = 1           -- User was deleted (disconnected)
    AND snapshot_ts >= ?        -- Disconnection happened at or after 24h ago
    AND snapshot_ts <= ?        -- Disconnection happened at or before now
)
ORDER BY v24h.vote_pubkey
`

	result, err := dataset.Query(ctx, conn, query, []any{
		query24HoursAgo, // users snapshot_ts
		query24HoursAgo, // gossip_nodes snapshot_ts
		query24HoursAgo, // vote_accounts snapshot_ts
		query24HoursAgo, // user_disconnections start
		now,             // user_disconnections end
	})
	require.NoError(t, err, "Failed to execute disconnected validators query")

	// Expected: vote1, vote2, vote3, vote6 (4 validators)
	// vote4 reconnected, so should NOT be in results (but might appear if overlap ended, need to check reconnection)
	// vote5 still connected, so should NOT be in results
	require.GreaterOrEqual(t, result.Count, 3, "Expected at least 3 disconnected validators (vote1, vote2, vote3), but got %d", result.Count)

	votePubkeys := make([]string, 0, result.Count)
	for _, row := range result.Rows {
		votePubkey, ok := row["vote_pubkey"].(string)
		require.True(t, ok, "vote_pubkey should be a string")
		votePubkeys = append(votePubkeys, votePubkey)
	}

	// Ensure vote1, vote2, vote3 are all present (vote6 might also appear)
	expectedVotes := []string{"vote1", "vote2", "vote3"}
	hasAllExpected := true
	for _, expected := range expectedVotes {
		found := false
		for _, actual := range votePubkeys {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			hasAllExpected = false
			break
		}
	}
	require.True(t, hasAllExpected,
		fmt.Sprintf("Expected all of %v to be present in disconnected validators, but got %v", expectedVotes, votePubkeys))

	t.Logf("Database validation passed: Found %d disconnected validators in past 24 hours: %v", result.Count, votePubkeys)
}
