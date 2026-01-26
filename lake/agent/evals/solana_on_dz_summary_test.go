//go:build evals

package evals_test

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_SolanaValidatorsGossipNodesOnDZSummary(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SolanaValidatorsGossipNodesOnDZSummary(t, newAnthropicLLMClient)
}


func runTest_SolanaValidatorsGossipNodesOnDZSummary(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed Solana data
	seedSolanaValidatorsGossipNodesOnDZSummaryData(t, ctx, conn)

	// Validate database query results before testing agent
	validateSolanaValidatorsGossipNodesOnDZSummaryQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query
	question := "how many solana validators and gossip nodes on dz"
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

	// Basic validation - the response should mention counts
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

	// The response should be non-empty and contain some indication of counts
	require.Greater(t, len(response), 50, "Response should be substantial")

	// Evaluate with Ollama - include specific expectations
	expectations := []Expectation{
		{
			Description:   "Number of validators currently on DZ",
			ExpectedValue: "3",
			Rationale:     "There are 3 validators with activated stake connected to DZ",
		},
		{
			Description:   "Number of gossip nodes currently on DZ",
			ExpectedValue: "5",
			Rationale:     "There are 5 gossip nodes connected to DZ (more nodes than validators due to non-voting nodes)",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly answer the question")
}

// seedSolanaValidatorsGossipNodesOnDZSummaryData seeds Solana validators and gossip nodes data for TestLake_Agent_Evals_SolanaValidatorsGossipNodesOnDZSummary
func seedSolanaValidatorsGossipNodesOnDZSummaryData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	now := testTime()

	// Seed metros
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "chi", Name: "Chicago"},
		{PK: "metro4", Code: "sf", Name: "San Francisco"},
		{PK: "metro5", Code: "tok", Name: "Tokyo"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed devices
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device3", Code: "chi-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
		{PK: "device4", Code: "sf-dzd1", Status: "activated", MetroPK: "metro4", DeviceType: "DZD"},
		{PK: "device5", Code: "tok-dzd1", Status: "activated", MetroPK: "metro5", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	// Seed DZ users with dz_ip addresses that will match gossip nodes
	// Currently on DZ: 5 users (3 validators + 2 gossip-only nodes)
	// Historical: 2 users that were on DZ in the past but disconnected
	currentUsers := []serviceability.User{
		{PK: "user1", OwnerPubkey: "owner1", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("1.1.1.1"), DZIP: net.ParseIP("10.0.0.1"), DevicePK: "device1", TunnelID: 501},
		{PK: "user2", OwnerPubkey: "owner2", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("2.2.2.2"), DZIP: net.ParseIP("10.0.0.2"), DevicePK: "device2", TunnelID: 502},
		{PK: "user3", OwnerPubkey: "owner3", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("3.3.3.3"), DZIP: net.ParseIP("10.0.0.3"), DevicePK: "device3", TunnelID: 503},
		{PK: "user4", OwnerPubkey: "owner4", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("4.4.4.4"), DZIP: net.ParseIP("10.0.0.4"), DevicePK: "device4", TunnelID: 504},
		{PK: "user5", OwnerPubkey: "owner5", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("5.5.5.5"), DZIP: net.ParseIP("10.0.0.5"), DevicePK: "device5", TunnelID: 505},
	}
	seedUsers(t, ctx, conn, []serviceability.User{currentUsers[0]}, now.Add(-7*24*time.Hour), now, testOpID())  // user1: 7 days ago
	seedUsers(t, ctx, conn, []serviceability.User{currentUsers[1]}, now.Add(-30*24*time.Hour), now, testOpID()) // user2: 30 days ago
	seedUsers(t, ctx, conn, []serviceability.User{currentUsers[2]}, now.Add(-60*24*time.Hour), now, testOpID()) // user3: 60 days ago
	seedUsers(t, ctx, conn, []serviceability.User{currentUsers[3]}, now.Add(-45*24*time.Hour), now, testOpID()) // user4: 45 days ago
	seedUsers(t, ctx, conn, []serviceability.User{currentUsers[4]}, now.Add(-20*24*time.Hour), now, testOpID()) // user5: 20 days ago

	// Historical users - seed them first, then delete them using MissingMeansDeleted
	historicalUsers := []serviceability.User{
		{PK: "user6", OwnerPubkey: "owner6", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("6.6.6.6"), DZIP: net.ParseIP("10.0.0.6"), DevicePK: "device1", TunnelID: 506},
		{PK: "user7", OwnerPubkey: "owner7", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("7.7.7.7"), DZIP: net.ParseIP("10.0.0.7"), DevicePK: "device2", TunnelID: 507},
	}
	seedUsers(t, ctx, conn, []serviceability.User{historicalUsers[0]}, now.Add(-90*24*time.Hour), now, testOpID())  // user6: connected 90 days ago
	seedUsers(t, ctx, conn, []serviceability.User{historicalUsers[1]}, now.Add(-120*24*time.Hour), now, testOpID()) // user7: connected 120 days ago

	// Delete historical users using MissingMeansDeleted
	log := testLogger(t)
	userDS, err := serviceability.NewUserDataset(log)
	require.NoError(t, err)
	deleteUser := func(pkToDelete string, deletedTS time.Time) {
		currentRows, err := userDS.GetCurrentRows(ctx, conn, nil)
		require.NoError(t, err)
		var remainingUsers []map[string]any
		for _, row := range currentRows {
			if row["pk"] != pkToDelete {
				remainingUsers = append(remainingUsers, row)
			}
		}
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
		}
	}
	deleteUser("user6", now.Add(-10*24*time.Hour)) // user6 disconnected 10 days ago
	deleteUser("user7", now.Add(-15*24*time.Hour)) // user7 disconnected 15 days ago

	// Seed Solana gossip nodes
	// Currently on DZ: nodes 1-5 (matching current users)
	// Historical on DZ: nodes 6-7 (matching historical users)
	// Not on DZ: nodes 8-15 (various IPs, not matching any DZ users)
	gossipNodesOnDZ := []*testGossipNode{
		{Pubkey: "node1", GossipIP: net.ParseIP("10.0.0.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.1"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node2", GossipIP: net.ParseIP("10.0.0.2"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.2"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node3", GossipIP: net.ParseIP("10.0.0.3"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.3"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node4", GossipIP: net.ParseIP("10.0.0.4"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.4"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node5", GossipIP: net.ParseIP("10.0.0.5"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.5"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
	}
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOnDZ[0]}, now.Add(-7*24*time.Hour), now, testOpID())  // node1: 7 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOnDZ[1]}, now.Add(-30*24*time.Hour), now, testOpID()) // node2: 30 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOnDZ[2]}, now.Add(-60*24*time.Hour), now, testOpID()) // node3: 60 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOnDZ[3]}, now.Add(-45*24*time.Hour), now, testOpID()) // node4: 45 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOnDZ[4]}, now.Add(-20*24*time.Hour), now, testOpID()) // node5: 20 days ago

	// Historical gossip nodes - these still exist in the gossip network but their users were disconnected
	// They won't be counted as "on DZ" because there's no matching user in dz_users_current
	gossipNodesHistorical := []*testGossipNode{
		{Pubkey: "node6", GossipIP: net.ParseIP("10.0.0.6"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.6"), TPUQUICPort: 8002, Version: "1.17.0", Epoch: 100},
		{Pubkey: "node7", GossipIP: net.ParseIP("10.0.0.7"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.7"), TPUQUICPort: 8002, Version: "1.17.0", Epoch: 100},
	}
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesHistorical[0]}, now.Add(-90*24*time.Hour), now, testOpID())  // node6: 90 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesHistorical[1]}, now.Add(-120*24*time.Hour), now, testOpID()) // node7: 120 days ago

	gossipNodesOffDZ := []*testGossipNode{
		{Pubkey: "node8", GossipIP: net.ParseIP("192.168.1.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.1"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node9", GossipIP: net.ParseIP("192.168.1.2"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.2"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node10", GossipIP: net.ParseIP("192.168.1.3"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.3"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node11", GossipIP: net.ParseIP("192.168.1.4"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.4"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node12", GossipIP: net.ParseIP("192.168.1.5"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.5"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node13", GossipIP: net.ParseIP("192.168.1.6"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.6"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node14", GossipIP: net.ParseIP("192.168.1.7"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.7"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node15", GossipIP: net.ParseIP("192.168.1.8"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.8"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
	}
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOffDZ[0]}, now.Add(-180*24*time.Hour), now, testOpID()) // node8: 180 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOffDZ[1]}, now.Add(-150*24*time.Hour), now, testOpID()) // node9: 150 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOffDZ[2]}, now.Add(-200*24*time.Hour), now, testOpID()) // node10: 200 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOffDZ[3]}, now.Add(-100*24*time.Hour), now, testOpID()) // node11: 100 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOffDZ[4]}, now.Add(-90*24*time.Hour), now, testOpID())  // node12: 90 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOffDZ[5]}, now.Add(-75*24*time.Hour), now, testOpID())  // node13: 75 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOffDZ[6]}, now.Add(-60*24*time.Hour), now, testOpID())  // node14: 60 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOffDZ[7]}, now.Add(-45*24*time.Hour), now, testOpID())  // node15: 45 days ago

	// Seed Solana vote accounts
	// Currently on DZ: vote1-3 (nodes 1-3)
	// Historical on DZ: vote6 (node6)
	// Not on DZ: vote8-11 (nodes 8-11)
	// Nodes 4, 5, 7, 12-15 are gossip-only (no vote accounts)
	voteAccountsOnDZ := []testVoteAccount{
		{VotePubkey: "vote1", NodePubkey: "node1", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1000000000000, Commission: 5},
		{VotePubkey: "vote2", NodePubkey: "node2", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 2000000000000, Commission: 5},
		{VotePubkey: "vote3", NodePubkey: "node3", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1500000000000, Commission: 5},
	}
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOnDZ[0]}, now.Add(-7*24*time.Hour), now, testOpID())  // vote1: 7 days ago
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOnDZ[1]}, now.Add(-30*24*time.Hour), now, testOpID()) // vote2: 30 days ago
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOnDZ[2]}, now.Add(-60*24*time.Hour), now, testOpID()) // vote3: 60 days ago

	// Historical vote account - still in the vote accounts table but won't be counted as "on DZ"
	// because its node's user was disconnected (not in dz_users_current)
	voteAccountHistorical := testVoteAccount{VotePubkey: "vote6", NodePubkey: "node6", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1800000000000, Commission: 5}
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountHistorical}, now.Add(-90*24*time.Hour), now, testOpID()) // vote6: 90 days ago

	voteAccountsOffDZ := []testVoteAccount{
		{VotePubkey: "vote8", NodePubkey: "node8", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 3000000000000, Commission: 5},
		{VotePubkey: "vote9", NodePubkey: "node9", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 2500000000000, Commission: 5},
		{VotePubkey: "vote10", NodePubkey: "node10", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 4000000000000, Commission: 5},
		{VotePubkey: "vote11", NodePubkey: "node11", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 2200000000000, Commission: 5},
	}
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOffDZ[0]}, now.Add(-180*24*time.Hour), now, testOpID()) // vote8: 180 days ago
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOffDZ[1]}, now.Add(-150*24*time.Hour), now, testOpID()) // vote9: 150 days ago
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOffDZ[2]}, now.Add(-200*24*time.Hour), now, testOpID()) // vote10: 200 days ago
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOffDZ[3]}, now.Add(-100*24*time.Hour), now, testOpID()) // vote11: 100 days ago
}

// validateSolanaValidatorsGossipNodesOnDZSummaryQuery runs a single query representative of what
// the agent would execute to answer "how many validators and gossip nodes on DZ"
func validateSolanaValidatorsGossipNodesOnDZSummaryQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	query := `
-- Count validators and gossip nodes currently on DoubleZero
-- A node is "on DZ" if its gossip_ip matches an activated user's dz_ip
-- A validator is a gossip node that also has a vote account with activated stake
SELECT
  -- Count validators: gossip nodes with vote accounts
  -- IMPORTANT: ClickHouse returns empty string (not NULL) for unmatched String columns in LEFT JOINs
  -- Must use countIf with != '' filter, NOT COUNT with IS NOT NULL
  countIf(DISTINCT va.vote_pubkey, va.vote_pubkey != '') AS validator_count,
  -- Count all gossip nodes on DZ (including non-validators)
  COUNT(DISTINCT gn.pubkey) AS gossip_node_count
-- Start with activated DZ users
FROM dz_users_current u
-- Join gossip nodes where the gossip IP matches the user's DZ IP
JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
-- LEFT JOIN vote accounts to identify which gossip nodes are validators
-- A gossip node is a validator if it has a vote account with activated stake
-- Note: Unmatched rows will have va.vote_pubkey = '' (empty string), not NULL
LEFT JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey AND va.activated_stake_lamports > 0
WHERE u.status = 'activated' AND gn.gossip_ip IS NOT NULL
`

	result, err := dataset.Query(ctx, conn, query, nil)
	require.NoError(t, err, "Failed to execute query")
	require.Equal(t, 1, result.Count, "Query should return exactly one row")

	row := result.Rows[0]
	validatorCount := toUint64(t, row["validator_count"])
	gossipCount := toUint64(t, row["gossip_node_count"])

	require.Equal(t, uint64(3), validatorCount, "Expected 3 validators on DZ (vote1, vote2, vote3)")
	require.Equal(t, uint64(5), gossipCount, "Expected 5 gossip nodes on DZ (node1-node5)")

	t.Logf("Database validation passed: Found %d validators and %d gossip nodes on DZ", validatorCount, gossipCount)
}

func toUint64(t *testing.T, v any) uint64 {
	switch val := v.(type) {
	case uint64:
		return val
	case int64:
		return uint64(val)
	case int:
		return uint64(val)
	case uint32:
		return uint64(val)
	case int32:
		return uint64(val)
	default:
		t.Fatalf("Unexpected type: %T, value: %v", v, v)
		return 0
	}
}
