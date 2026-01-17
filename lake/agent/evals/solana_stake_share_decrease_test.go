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

func TestLake_Agent_Evals_Anthropic_SolanaStakeShareDecrease(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SolanaStakeShareDecrease(t, newAnthropicLLMClient)
}


func runTest_SolanaStakeShareDecrease(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed Solana stake share decrease data
	seedSolanaStakeShareDecreaseData(t, ctx, conn)

	// Validate database query results before testing agent
	validateSolanaStakeShareDecreaseQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query
	question := "the solana network stake share on dz decreased recently, why"
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

	// Basic validation - the response should explain the decrease
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

	// The response should be non-empty and contain some explanation
	require.Greater(t, len(response), 100, "Response should be substantial")

	// Evaluate with Ollama - include specific expectations
	expectations := []Expectation{
		{
			Description:   "Validators that disconnected",
			ExpectedValue: "vote4/node4 and/or vote5/node5 (either vote_pubkey or node_pubkey identifier is acceptable)",
			Rationale:     "These two validators disconnected in the past day, causing the stake share decrease",
		},
		{
			Description:   "Stake amount for vote4/node4",
			ExpectedValue: "5000 SOL or 5,000 SOL or 5T lamports",
			Rationale:     "vote4/node4 had 5000 SOL (5 trillion lamports) of activated stake when it disconnected",
		},
		{
			Description:   "Stake amount for vote5/node5",
			ExpectedValue: "4000 SOL or 4,000 SOL or 4T lamports",
			Rationale:     "vote5/node5 had 4000 SOL (4 trillion lamports) of activated stake when it disconnected",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly answer the question")
}

// seedSolanaStakeShareDecreaseData seeds Solana data for TestLake_Agent_Evals_Anthropic_SolanaStakeShareDecrease
// Sets up a scenario where 2 validators disconnected from DZ in the past day, causing stake share to decrease
//
// Historical epoch data structure (to enable stake share trend analysis):
// - Epoch 98 (5 days ago): vote1-6 all on DZ = 14.7T lamports on DZ / 29.7T total = ~49% stake share
// - Epoch 99 (2 days ago): vote1-6 all on DZ = 14.7T lamports on DZ / 29.7T total = ~49% stake share
// - Epoch 100 (current): vote1-3 on DZ (vote4/5 disconnected in past 24h) = 3.7T on DZ / 29.7T total = ~12% stake share
//
// This shows a clear decrease from ~49% to ~12% due to vote4 (5000 SOL) and vote5 (4000 SOL) disconnecting.
func seedSolanaStakeShareDecreaseData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	now := testTime()

	// Seed metros (need 5 metros for 5 devices)
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

	// Seed DZ users:
	// - user1-3: Currently on DZ (still connected)
	// - user4-5: Disconnected in the past day
	// - user6: Disconnected longer ago (not recent)

	// Set up user dataset for deletion helper
	log := testLogger(t)
	userDS, err := serviceability.NewUserDataset(log)
	require.NoError(t, err)

	// Helper to mark user as deleted at a timestamp using WriteBatch
	deleteUser := func(pkToDelete string, deletedTS time.Time) error {
		// Get all current users
		currentRows, err := userDS.GetCurrentRows(ctx, conn, nil)
		if err != nil {
			return fmt.Errorf("failed to get current rows: %w", err)
		}

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
			if err != nil {
				return fmt.Errorf("failed to write batch: %w", err)
			}
		} else {
			// No remaining users, write empty snapshot to delete all
			err = userDS.WriteBatch(ctx, conn, 0, nil, &dataset.DimensionType2DatasetWriteConfig{
				SnapshotTS:          deletedTS,
				OpID:                testOpID(),
				MissingMeansDeleted: true,
			})
			if err != nil {
				return fmt.Errorf("failed to write empty batch: %w", err)
			}
		}
		return nil
	}

	// Currently active users
	activeUsers := []serviceability.User{
		{PK: "user1", OwnerPubkey: "owner1", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("1.1.1.1"), DZIP: net.ParseIP("10.0.0.1"), DevicePK: "device1", TunnelID: 501},
		{PK: "user2", OwnerPubkey: "owner2", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("2.2.2.2"), DZIP: net.ParseIP("10.0.0.2"), DevicePK: "device2", TunnelID: 502},
		{PK: "user3", OwnerPubkey: "owner3", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("3.3.3.3"), DZIP: net.ParseIP("10.0.0.3"), DevicePK: "device3", TunnelID: 503},
	}
	seedUsers(t, ctx, conn, []serviceability.User{activeUsers[0]}, now.Add(-30*24*time.Hour), now, testOpID()) // user1: 30 days ago
	seedUsers(t, ctx, conn, []serviceability.User{activeUsers[1]}, now.Add(-60*24*time.Hour), now, testOpID()) // user2: 60 days ago
	seedUsers(t, ctx, conn, []serviceability.User{activeUsers[2]}, now.Add(-45*24*time.Hour), now, testOpID()) // user3: 45 days ago

	// Disconnected users - need to create deletion records using MissingMeansDeleted
	disconnectedUsers := []serviceability.User{
		{PK: "user4", OwnerPubkey: "owner4", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("4.4.4.4"), DZIP: net.ParseIP("10.0.0.4"), DevicePK: "device4", TunnelID: 504},
		{PK: "user5", OwnerPubkey: "owner5", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("5.5.5.5"), DZIP: net.ParseIP("10.0.0.5"), DevicePK: "device5", TunnelID: 505},
		{PK: "user6", OwnerPubkey: "owner6", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("6.6.6.6"), DZIP: net.ParseIP("10.0.0.6"), DevicePK: "device1", TunnelID: 506},
	}
	// user4: connected 20 days ago, disconnected 12 hours ago
	seedUsers(t, ctx, conn, []serviceability.User{disconnectedUsers[0]}, now.Add(-20*24*time.Hour), now, testOpID())
	err = deleteUser("user4", now.Add(-12*time.Hour))
	require.NoError(t, err, "Failed to delete user4")
	// user5: connected 15 days ago, disconnected 6 hours ago
	seedUsers(t, ctx, conn, []serviceability.User{disconnectedUsers[1]}, now.Add(-15*24*time.Hour), now, testOpID())
	err = deleteUser("user5", now.Add(-6*time.Hour))
	require.NoError(t, err, "Failed to delete user5")
	// user6: connected 90 days ago, disconnected 3 days ago (not in past 24h, so won't be in results)
	seedUsers(t, ctx, conn, []serviceability.User{disconnectedUsers[2]}, now.Add(-90*24*time.Hour), now, testOpID())
	err = deleteUser("user6", now.Add(-3*24*time.Hour))
	require.NoError(t, err, "Failed to delete user6")

	// Seed Solana gossip nodes
	// - node1-3: Currently on DZ (matching user1-3)
	// - node4-5: Were on DZ but disconnected recently (matching user4-5)
	// - node6: Was on DZ but disconnected longer ago (matching user6)
	// - node7-10: Not on DZ (never were)
	gossipNodesOnDZ := []*testGossipNode{
		{Pubkey: "node1", GossipIP: net.ParseIP("10.0.0.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.1"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node2", GossipIP: net.ParseIP("10.0.0.2"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.2"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node3", GossipIP: net.ParseIP("10.0.0.3"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.3"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
	}
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOnDZ[0]}, now.Add(-30*24*time.Hour), now, testOpID()) // node1: 30 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOnDZ[1]}, now.Add(-60*24*time.Hour), now, testOpID()) // node2: 60 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOnDZ[2]}, now.Add(-45*24*time.Hour), now, testOpID()) // node3: 45 days ago

	gossipNodesDisconnected := []*testGossipNode{
		{Pubkey: "node4", GossipIP: net.ParseIP("10.0.0.4"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.4"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node5", GossipIP: net.ParseIP("10.0.0.5"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.5"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node6", GossipIP: net.ParseIP("10.0.0.6"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.6"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
	}
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesDisconnected[0]}, now.Add(-20*24*time.Hour), now.Add(-12*time.Hour), testOpID())   // node4: connected 20 days ago, disconnected 12 hours ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesDisconnected[1]}, now.Add(-15*24*time.Hour), now.Add(-6*time.Hour), testOpID())    // node5: connected 15 days ago, disconnected 6 hours ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesDisconnected[2]}, now.Add(-90*24*time.Hour), now.Add(-3*24*time.Hour), testOpID()) // node6: connected 90 days ago, disconnected 3 days ago

	gossipNodesOffDZ := []*testGossipNode{
		{Pubkey: "node7", GossipIP: net.ParseIP("192.168.1.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.1"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node8", GossipIP: net.ParseIP("192.168.1.2"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.2"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node9", GossipIP: net.ParseIP("192.168.1.3"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.3"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node10", GossipIP: net.ParseIP("192.168.1.4"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.4"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
	}
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOffDZ[0]}, now.Add(-180*24*time.Hour), now, testOpID()) // node7: 180 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOffDZ[1]}, now.Add(-150*24*time.Hour), now, testOpID()) // node8: 150 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOffDZ[2]}, now.Add(-200*24*time.Hour), now, testOpID()) // node9: 200 days ago
	seedGossipNodes(t, ctx, conn, []*testGossipNode{gossipNodesOffDZ[3]}, now.Add(-100*24*time.Hour), now, testOpID()) // node10: 100 days ago

	// Seed Solana vote accounts for historical epochs
	// This enables the agent to analyze stake share trends across epochs.
	//
	// Stake distribution (same across all epochs):
	// - vote1: 1000 SOL (1T lamports)
	// - vote2: 1500 SOL (1.5T lamports)
	// - vote3: 1200 SOL (1.2T lamports)
	// - vote4: 5000 SOL (5T lamports) - high stake, disconnected recently
	// - vote5: 4000 SOL (4T lamports) - high stake, disconnected recently
	// - vote6: 2000 SOL (2T lamports) - disconnected 3 days ago
	// - vote7-10: 15000 SOL total (off-DZ, network background)
	//
	// Historical stake share on DZ:
	// - Epoch 98 (5 days ago): vote1-6 on DZ = 14.7T / 29.7T = ~49.5%
	// - Epoch 99 (2 days ago): vote1-6 on DZ = 14.7T / 29.7T = ~49.5%
	// - Epoch 100 (now): vote1-3 on DZ = 3.7T / 29.7T = ~12.5% (vote4/5 disconnected)

	// Define stake amounts in lamports
	const (
		vote1Stake = 1000000000000  // 1000 SOL
		vote2Stake = 1500000000000  // 1500 SOL
		vote3Stake = 1200000000000  // 1200 SOL
		vote4Stake = 5000000000000  // 5000 SOL
		vote5Stake = 4000000000000  // 4000 SOL
		vote6Stake = 2000000000000  // 2000 SOL
		vote7Stake = 3000000000000  // 3000 SOL
		vote8Stake = 2500000000000  // 2500 SOL
		vote9Stake = 6000000000000  // 6000 SOL
		vote10Stake = 3500000000000 // 3500 SOL
	)

	// Epoch 98 - 5 days ago: All validators including vote4-6 on DZ
	epoch98Time := now.Add(-5 * 24 * time.Hour)
	voteAccountsEpoch98 := []testVoteAccount{
		// On DZ (will stay connected)
		{VotePubkey: "vote1", NodePubkey: "node1", EpochVoteAccount: true, Epoch: 98, ActivatedStake: vote1Stake, Commission: 5},
		{VotePubkey: "vote2", NodePubkey: "node2", EpochVoteAccount: true, Epoch: 98, ActivatedStake: vote2Stake, Commission: 5},
		{VotePubkey: "vote3", NodePubkey: "node3", EpochVoteAccount: true, Epoch: 98, ActivatedStake: vote3Stake, Commission: 5},
		// On DZ (will disconnect)
		{VotePubkey: "vote4", NodePubkey: "node4", EpochVoteAccount: true, Epoch: 98, ActivatedStake: vote4Stake, Commission: 5},
		{VotePubkey: "vote5", NodePubkey: "node5", EpochVoteAccount: true, Epoch: 98, ActivatedStake: vote5Stake, Commission: 5},
		{VotePubkey: "vote6", NodePubkey: "node6", EpochVoteAccount: true, Epoch: 98, ActivatedStake: vote6Stake, Commission: 5},
		// Off DZ (never connected)
		{VotePubkey: "vote7", NodePubkey: "node7", EpochVoteAccount: true, Epoch: 98, ActivatedStake: vote7Stake, Commission: 5},
		{VotePubkey: "vote8", NodePubkey: "node8", EpochVoteAccount: true, Epoch: 98, ActivatedStake: vote8Stake, Commission: 5},
		{VotePubkey: "vote9", NodePubkey: "node9", EpochVoteAccount: true, Epoch: 98, ActivatedStake: vote9Stake, Commission: 5},
		{VotePubkey: "vote10", NodePubkey: "node10", EpochVoteAccount: true, Epoch: 98, ActivatedStake: vote10Stake, Commission: 5},
	}
	seedVoteAccounts(t, ctx, conn, voteAccountsEpoch98, epoch98Time, epoch98Time, testOpID())

	// Epoch 99 - 2 days ago: All validators including vote4-6 still on DZ
	epoch99Time := now.Add(-2 * 24 * time.Hour)
	voteAccountsEpoch99 := []testVoteAccount{
		// On DZ (will stay connected)
		{VotePubkey: "vote1", NodePubkey: "node1", EpochVoteAccount: true, Epoch: 99, ActivatedStake: vote1Stake, Commission: 5},
		{VotePubkey: "vote2", NodePubkey: "node2", EpochVoteAccount: true, Epoch: 99, ActivatedStake: vote2Stake, Commission: 5},
		{VotePubkey: "vote3", NodePubkey: "node3", EpochVoteAccount: true, Epoch: 99, ActivatedStake: vote3Stake, Commission: 5},
		// On DZ (will disconnect in epoch 100)
		{VotePubkey: "vote4", NodePubkey: "node4", EpochVoteAccount: true, Epoch: 99, ActivatedStake: vote4Stake, Commission: 5},
		{VotePubkey: "vote5", NodePubkey: "node5", EpochVoteAccount: true, Epoch: 99, ActivatedStake: vote5Stake, Commission: 5},
		// vote6 disconnected 3 days ago, so not on DZ at epoch 99
		{VotePubkey: "vote6", NodePubkey: "node6", EpochVoteAccount: true, Epoch: 99, ActivatedStake: vote6Stake, Commission: 5},
		// Off DZ (never connected)
		{VotePubkey: "vote7", NodePubkey: "node7", EpochVoteAccount: true, Epoch: 99, ActivatedStake: vote7Stake, Commission: 5},
		{VotePubkey: "vote8", NodePubkey: "node8", EpochVoteAccount: true, Epoch: 99, ActivatedStake: vote8Stake, Commission: 5},
		{VotePubkey: "vote9", NodePubkey: "node9", EpochVoteAccount: true, Epoch: 99, ActivatedStake: vote9Stake, Commission: 5},
		{VotePubkey: "vote10", NodePubkey: "node10", EpochVoteAccount: true, Epoch: 99, ActivatedStake: vote10Stake, Commission: 5},
	}
	seedVoteAccounts(t, ctx, conn, voteAccountsEpoch99, epoch99Time, epoch99Time, testOpID())

	// Epoch 100 - Current: vote4/5 disconnected in past 24h, vote6 disconnected 3 days ago
	// Only vote1-3 remain on DZ
	voteAccountsOnDZ := []testVoteAccount{
		{VotePubkey: "vote1", NodePubkey: "node1", EpochVoteAccount: true, Epoch: 100, ActivatedStake: vote1Stake, Commission: 5},
		{VotePubkey: "vote2", NodePubkey: "node2", EpochVoteAccount: true, Epoch: 100, ActivatedStake: vote2Stake, Commission: 5},
		{VotePubkey: "vote3", NodePubkey: "node3", EpochVoteAccount: true, Epoch: 100, ActivatedStake: vote3Stake, Commission: 5},
	}
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOnDZ[0]}, now.Add(-30*24*time.Hour), now, testOpID()) // vote1: 30 days ago
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOnDZ[1]}, now.Add(-60*24*time.Hour), now, testOpID()) // vote2: 60 days ago
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOnDZ[2]}, now.Add(-45*24*time.Hour), now, testOpID()) // vote3: 45 days ago

	voteAccountsDisconnected := []testVoteAccount{
		{VotePubkey: "vote4", NodePubkey: "node4", EpochVoteAccount: true, Epoch: 100, ActivatedStake: vote4Stake, Commission: 5},
		{VotePubkey: "vote5", NodePubkey: "node5", EpochVoteAccount: true, Epoch: 100, ActivatedStake: vote5Stake, Commission: 5},
		{VotePubkey: "vote6", NodePubkey: "node6", EpochVoteAccount: true, Epoch: 100, ActivatedStake: vote6Stake, Commission: 5},
	}
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsDisconnected[0]}, now.Add(-20*24*time.Hour), now.Add(-12*time.Hour), testOpID())   // vote4: connected 20 days ago, disconnected 12 hours ago
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsDisconnected[1]}, now.Add(-15*24*time.Hour), now.Add(-6*time.Hour), testOpID())    // vote5: connected 15 days ago, disconnected 6 hours ago
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsDisconnected[2]}, now.Add(-90*24*time.Hour), now.Add(-3*24*time.Hour), testOpID()) // vote6: connected 90 days ago, disconnected 3 days ago

	voteAccountsOffDZ := []testVoteAccount{
		{VotePubkey: "vote7", NodePubkey: "node7", EpochVoteAccount: true, Epoch: 100, ActivatedStake: vote7Stake, Commission: 5},
		{VotePubkey: "vote8", NodePubkey: "node8", EpochVoteAccount: true, Epoch: 100, ActivatedStake: vote8Stake, Commission: 5},
		{VotePubkey: "vote9", NodePubkey: "node9", EpochVoteAccount: true, Epoch: 100, ActivatedStake: vote9Stake, Commission: 5},
		{VotePubkey: "vote10", NodePubkey: "node10", EpochVoteAccount: true, Epoch: 100, ActivatedStake: vote10Stake, Commission: 5},
	}
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOffDZ[0]}, now.Add(-180*24*time.Hour), now, testOpID()) // vote7: 180 days ago
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOffDZ[1]}, now.Add(-150*24*time.Hour), now, testOpID()) // vote8: 150 days ago
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOffDZ[2]}, now.Add(-200*24*time.Hour), now, testOpID()) // vote9: 200 days ago
	seedVoteAccounts(t, ctx, conn, []testVoteAccount{voteAccountsOffDZ[3]}, now.Add(-100*24*time.Hour), now, testOpID()) // vote10: 100 days ago
}

// validateSolanaStakeShareDecreaseQuery runs the ideal query to answer the question
// and validates that the database returns the expected results (vote4 and vote5 disconnected in past day)
//
// This query finds validators that were connected 24 hours ago but are NOT currently connected,
// and where the disconnection happened in the past 24 hours. This uses the historical comparison
// method: get validators connected 24h ago, exclude those currently connected, then verify
// the disconnection timestamp is within the past 24 hours.
func validateSolanaStakeShareDecreaseQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	now := testTime()
	query24HoursAgo := now.Add(-24 * time.Hour)

	// Query for validators that disconnected in the past 24 hours
	// Strategy: Find validators that were connected 24h ago, exclude those currently connected,
	// then verify the disconnection happened in the past 24 hours by checking when the user
	// was deleted (is_deleted = 1) with snapshot_ts in the past 24 hours
	// This pattern works reliably in ClickHouse and is something the agent can execute
	query := `
-- Find validators that were connected 24 hours ago but are NOT currently connected
-- AND where the disconnection happened in the past 24 hours
-- This query demonstrates how to find entities that existed in the past but no longer exist,
-- using SCD Type 2 history tables with the NOT IN pattern
SELECT DISTINCT v24h.vote_pubkey, v24h.activated_stake_sol
FROM (
  -- Get all validators that were connected 24 hours ago
  -- A validator is "connected" if:
  --   1. User is activated and has a DZ IP
  --   2. Gossip node exists and matches the user's DZ IP
  --   3. Vote account exists for that gossip node and has activated stake
  -- Uses SCD Type 2 pattern: ROW_NUMBER() to get latest snapshot at/before 24h ago
  SELECT DISTINCT
    va.vote_pubkey,
    va.activated_stake_lamports / 1000000000.0 AS activated_stake_sol,
    u.entity_id AS user_entity_id
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

	// Expected: vote4 and vote5 disconnected in past day (vote6 disconnected 3 days ago, so not included)
	require.GreaterOrEqual(t, result.Count, 2, "Expected at least 2 validators disconnected in past 24 hours (vote4, vote5), but got %d", result.Count)

	votePubkeys := make([]string, 0, result.Count)
	for _, row := range result.Rows {
		votePubkey, ok := row["vote_pubkey"].(string)
		require.True(t, ok, "vote_pubkey should be a string")
		votePubkeys = append(votePubkeys, votePubkey)
	}

	// Ensure vote4 and vote5 are present
	expectedVotes := []string{"vote4", "vote5"}
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

	// Verify stake amounts: vote4 = 5000 SOL, vote5 = 4000 SOL
	for _, row := range result.Rows {
		votePubkey := row["vote_pubkey"].(string)
		activatedStakeSol, ok := row["activated_stake_sol"].(float64)
		if !ok {
			// Try other numeric types
			switch v := row["activated_stake_sol"].(type) {
			case float32:
				activatedStakeSol = float64(v)
			case int64:
				activatedStakeSol = float64(v)
			case int:
				activatedStakeSol = float64(v)
			default:
				t.Logf("WARNING: Unexpected type for activated_stake_sol for %s: %T", votePubkey, v)
				continue
			}
		}

		if votePubkey == "vote4" {
			require.InDelta(t, 5000.0, activatedStakeSol, 0.1, "vote4 should have 5000 SOL stake")
		} else if votePubkey == "vote5" {
			require.InDelta(t, 4000.0, activatedStakeSol, 0.1, "vote5 should have 4000 SOL stake")
		}
	}

	t.Logf("Database validation passed: Found %d validators disconnected in past 24 hours: %v (total stake: 9000 SOL)", result.Count, votePubkeys)
}
