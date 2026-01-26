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
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/sol"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_SolanaValidatorsOnDZVsOffDZ(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_SolanaValidatorsOnDZVsOffDZ(t, newAnthropicLLMClient)
}


func runTest_SolanaValidatorsOnDZVsOffDZ(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed Solana validators on DZ vs off DZ comparison data
	seedSolanaValidatorsOnDZVsOffDZData(t, ctx, conn)

	// Validate database query results before testing agent
	validateSolanaValidatorsOnDZVsOffDZQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query
	question := "compare solana validators on dz vs off dz"
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

	// Basic validation - the response should compare validators on DZ vs off DZ
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
			Description:   "On-DZ validators data",
			ExpectedValue: "3 validators on DZ with better performance metrics (lower vote lag ~50 slots OR lower skip rate ~2%)",
			Rationale:     "vote1/node1, vote2/node2, vote3/node3 are on DZ (either identifier type acceptable) - either vote lag or skip rate metric is acceptable",
		},
		{
			Description:   "Off-DZ validators data",
			ExpectedValue: "3 validators off DZ with worse performance metrics (higher vote lag ~200 slots OR higher skip rate ~8%)",
			Rationale:     "vote4/node4, vote5/node5, vote6/node6 are off DZ (either identifier type acceptable) - either vote lag or skip rate metric is acceptable",
		},
		{
			Description:   "Comparison conclusion",
			ExpectedValue: "on-DZ validators perform better than off-DZ validators",
			Rationale:     "The data shows on-DZ validators have better performance (lower vote lag OR lower skip rate)",
		},
		{
			Description:   "CRITICAL: Response must NOT say off-DZ data is unavailable",
			ExpectedValue: "data for BOTH groups should be present and compared",
			Rationale:     "If the response says off-DZ data is missing, mark as NO - the data exists and should be found",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly answer the question")
}

// seedSolanaValidatorsOnDZVsOffDZData seeds data for comparing validators on DZ vs off DZ
// On DZ validators (vote1, vote2, vote3): Better performance (lower skip rate, lower vote lag, higher produce rate)
// Off DZ validators (vote4, vote5, vote6): Worse performance (higher skip rate, higher vote lag, lower produce rate)
func seedSolanaValidatorsOnDZVsOffDZData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()

	// Seed metros
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed devices
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	// Seed DZ users for validators on DZ
	// user1-3: Connected to DZ (for validators on DZ)
	users := []serviceability.User{
		{PK: "user1", OwnerPubkey: "owner1", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("1.1.1.1"), DZIP: net.ParseIP("10.0.0.1"), DevicePK: "device1", TunnelID: 501},
		{PK: "user2", OwnerPubkey: "owner2", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("2.2.2.2"), DZIP: net.ParseIP("10.0.0.2"), DevicePK: "device1", TunnelID: 502},
		{PK: "user3", OwnerPubkey: "owner3", Status: "activated", Kind: "IBRL", ClientIP: net.ParseIP("3.3.3.3"), DZIP: net.ParseIP("10.0.0.3"), DevicePK: "device2", TunnelID: 503},
	}
	seedUsers(t, ctx, conn, users, now.Add(-30*24*time.Hour), now, testOpID())

	// Seed Solana gossip nodes
	// node1-3: On DZ (matching user1-3 dz_ip)
	// node4-6: Off DZ (different IPs, not matching any user)
	gossipNodesOnDZ := []*testGossipNode{
		{Pubkey: "node1", GossipIP: net.ParseIP("10.0.0.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.1"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node2", GossipIP: net.ParseIP("10.0.0.2"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.2"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node3", GossipIP: net.ParseIP("10.0.0.3"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.0.0.3"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
	}
	gossipNodesOffDZ := []*testGossipNode{
		{Pubkey: "node4", GossipIP: net.ParseIP("192.168.1.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.1"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node5", GossipIP: net.ParseIP("192.168.1.2"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.2"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
		{Pubkey: "node6", GossipIP: net.ParseIP("192.168.1.3"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.168.1.3"), TPUQUICPort: 8002, Version: "1.18.0", Epoch: 100},
	}
	allGossipNodes := append(gossipNodesOnDZ, gossipNodesOffDZ...)
	seedGossipNodes(t, ctx, conn, allGossipNodes, now.Add(-30*24*time.Hour), now, testOpID())

	// Seed Solana vote accounts
	// vote1-3: On DZ (better performance)
	// vote4-6: Off DZ (worse performance)
	voteAccountsOnDZ := []testVoteAccount{
		{VotePubkey: "vote1", NodePubkey: "node1", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1000000000000, Commission: 5},
		{VotePubkey: "vote2", NodePubkey: "node2", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1500000000000, Commission: 5},
		{VotePubkey: "vote3", NodePubkey: "node3", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1200000000000, Commission: 5},
	}
	voteAccountsOffDZ := []testVoteAccount{
		{VotePubkey: "vote4", NodePubkey: "node4", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 2000000000000, Commission: 5},
		{VotePubkey: "vote5", NodePubkey: "node5", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1800000000000, Commission: 5},
		{VotePubkey: "vote6", NodePubkey: "node6", EpochVoteAccount: true, Epoch: 100, ActivatedStake: 1600000000000, Commission: 5},
	}
	allVoteAccounts := append(voteAccountsOnDZ, voteAccountsOffDZ...)
	seedVoteAccounts(t, ctx, conn, allVoteAccounts, now.Add(-30*24*time.Hour), now, testOpID())

	// Seed leader schedule (slots assigned to validators)
	// Note: Leader schedule uses solana.PublicKey which requires parsing, so we'll skip it for now
	// The test should still work without leader schedule data

	// Seed block production data
	// On DZ validators: Better performance (lower skip rate ~2%, higher produce rate ~98%)
	// Off DZ validators: Worse performance (higher skip rate ~8%, lower produce rate ~92%)
	blockProdDS, err := sol.NewBlockProductionDataset(log)
	require.NoError(t, err)
	ingestedAt := now
	blockProdEntries := []struct {
		time                   time.Time
		epoch                  int
		leaderIdentityPubkey   string
		leaderSlotsAssignedCum uint64
		blocksProducedCum      uint64
	}{
		// On DZ: vote1 (node1) - 1000 assigned, 980 produced
		{now.Add(-2 * time.Hour), 100, "node1", 1000, 980},
		{now.Add(-1 * time.Hour), 100, "node1", 2000, 1960},
		{now, 100, "node1", 3000, 2940},
		// On DZ: vote2 (node2) - 1200 assigned, 1176 produced
		{now.Add(-2 * time.Hour), 100, "node2", 1200, 1176},
		{now.Add(-1 * time.Hour), 100, "node2", 2400, 2352},
		{now, 100, "node2", 3600, 3528},
		// On DZ: vote3 (node3) - 1100 assigned, 1078 produced
		{now.Add(-2 * time.Hour), 100, "node3", 1100, 1078},
		{now.Add(-1 * time.Hour), 100, "node3", 2200, 2156},
		{now, 100, "node3", 3300, 3234},
		// Off DZ: vote4 (node4) - 1500 assigned, 1380 produced
		{now.Add(-2 * time.Hour), 100, "node4", 1500, 1380},
		{now.Add(-1 * time.Hour), 100, "node4", 3000, 2760},
		{now, 100, "node4", 4500, 4140},
		// Off DZ: vote5 (node5) - 1400 assigned, 1288 produced
		{now.Add(-2 * time.Hour), 100, "node5", 1400, 1288},
		{now.Add(-1 * time.Hour), 100, "node5", 2800, 2576},
		{now, 100, "node5", 4200, 3864},
		// Off DZ: vote6 (node6) - 1300 assigned, 1196 produced
		{now.Add(-2 * time.Hour), 100, "node6", 1300, 1196},
		{now.Add(-1 * time.Hour), 100, "node6", 2600, 2392},
		{now, 100, "node6", 3900, 3588},
	}
	err = blockProdDS.WriteBatch(ctx, conn, len(blockProdEntries), func(i int) ([]any, error) {
		e := blockProdEntries[i]
		return []any{
			int32(e.epoch),                  // epoch
			e.time.UTC(),                    // event_ts
			ingestedAt,                      // ingested_at
			e.leaderIdentityPubkey,          // leader_identity_pubkey
			int64(e.leaderSlotsAssignedCum), // leader_slots_assigned_cum
			int64(e.blocksProducedCum),      // blocks_produced_cum
		}, nil
	})
	require.NoError(t, err)

	// Seed vote account activity (vote lag)
	// On DZ validators: Lower vote lag (~50 slots)
	// Off DZ validators: Higher vote lag (~200 slots)
	voteActivityDS, err := sol.NewVoteAccountActivityDataset(log)
	require.NoError(t, err)
	voteActivityEntries := []sol.VoteAccountActivityEntry{
		// On DZ: vote1 - low vote lag (~50 slots)
		{Time: now.Add(-2 * time.Hour), VoteAccountPubkey: "vote1", NodeIdentityPubkey: "node1", ClusterSlot: 100000, LastVoteSlot: 99950, RootSlot: 99900, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1000.0), Commission: uint8Ptr(5), CollectorRunID: "run1", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1000},
		{Time: now.Add(-1 * time.Hour), VoteAccountPubkey: "vote1", NodeIdentityPubkey: "node1", ClusterSlot: 100100, LastVoteSlot: 100050, RootSlot: 100000, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1000.0), Commission: uint8Ptr(5), CollectorRunID: "run2", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1100},
		{Time: now, VoteAccountPubkey: "vote1", NodeIdentityPubkey: "node1", ClusterSlot: 100200, LastVoteSlot: 100150, RootSlot: 100100, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1000.0), Commission: uint8Ptr(5), CollectorRunID: "run3", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1200},
		// On DZ: vote2 - low vote lag (~50 slots)
		{Time: now.Add(-2 * time.Hour), VoteAccountPubkey: "vote2", NodeIdentityPubkey: "node2", ClusterSlot: 100000, LastVoteSlot: 99950, RootSlot: 99900, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1500.0), Commission: uint8Ptr(5), CollectorRunID: "run1", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1000},
		{Time: now.Add(-1 * time.Hour), VoteAccountPubkey: "vote2", NodeIdentityPubkey: "node2", ClusterSlot: 100100, LastVoteSlot: 100050, RootSlot: 100000, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1500.0), Commission: uint8Ptr(5), CollectorRunID: "run2", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1100},
		{Time: now, VoteAccountPubkey: "vote2", NodeIdentityPubkey: "node2", ClusterSlot: 100200, LastVoteSlot: 100150, RootSlot: 100100, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1500.0), Commission: uint8Ptr(5), CollectorRunID: "run3", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1200},
		// On DZ: vote3 - low vote lag (~50 slots)
		{Time: now.Add(-2 * time.Hour), VoteAccountPubkey: "vote3", NodeIdentityPubkey: "node3", ClusterSlot: 100000, LastVoteSlot: 99950, RootSlot: 99900, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1200.0), Commission: uint8Ptr(5), CollectorRunID: "run1", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1000},
		{Time: now.Add(-1 * time.Hour), VoteAccountPubkey: "vote3", NodeIdentityPubkey: "node3", ClusterSlot: 100100, LastVoteSlot: 100050, RootSlot: 100000, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1200.0), Commission: uint8Ptr(5), CollectorRunID: "run2", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1100},
		{Time: now, VoteAccountPubkey: "vote3", NodeIdentityPubkey: "node3", ClusterSlot: 100200, LastVoteSlot: 100150, RootSlot: 100100, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1200.0), Commission: uint8Ptr(5), CollectorRunID: "run3", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1200},
		// Off DZ: vote4 - high vote lag (~200 slots)
		{Time: now.Add(-2 * time.Hour), VoteAccountPubkey: "vote4", NodeIdentityPubkey: "node4", ClusterSlot: 100000, LastVoteSlot: 99800, RootSlot: 99700, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(2000.0), Commission: uint8Ptr(5), CollectorRunID: "run1", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1000},
		{Time: now.Add(-1 * time.Hour), VoteAccountPubkey: "vote4", NodeIdentityPubkey: "node4", ClusterSlot: 100100, LastVoteSlot: 99900, RootSlot: 99800, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(2000.0), Commission: uint8Ptr(5), CollectorRunID: "run2", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1100},
		{Time: now, VoteAccountPubkey: "vote4", NodeIdentityPubkey: "node4", ClusterSlot: 100200, LastVoteSlot: 100000, RootSlot: 99900, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(2000.0), Commission: uint8Ptr(5), CollectorRunID: "run3", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1200},
		// Off DZ: vote5 - high vote lag (~200 slots)
		{Time: now.Add(-2 * time.Hour), VoteAccountPubkey: "vote5", NodeIdentityPubkey: "node5", ClusterSlot: 100000, LastVoteSlot: 99800, RootSlot: 99700, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1800.0), Commission: uint8Ptr(5), CollectorRunID: "run1", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1000},
		{Time: now.Add(-1 * time.Hour), VoteAccountPubkey: "vote5", NodeIdentityPubkey: "node5", ClusterSlot: 100100, LastVoteSlot: 99900, RootSlot: 99800, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1800.0), Commission: uint8Ptr(5), CollectorRunID: "run2", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1100},
		{Time: now, VoteAccountPubkey: "vote5", NodeIdentityPubkey: "node5", ClusterSlot: 100200, LastVoteSlot: 100000, RootSlot: 99900, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1800.0), Commission: uint8Ptr(5), CollectorRunID: "run3", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1200},
		// Off DZ: vote6 - high vote lag (~200 slots)
		{Time: now.Add(-2 * time.Hour), VoteAccountPubkey: "vote6", NodeIdentityPubkey: "node6", ClusterSlot: 100000, LastVoteSlot: 99800, RootSlot: 99700, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1600.0), Commission: uint8Ptr(5), CollectorRunID: "run1", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1000},
		{Time: now.Add(-1 * time.Hour), VoteAccountPubkey: "vote6", NodeIdentityPubkey: "node6", ClusterSlot: 100100, LastVoteSlot: 99900, RootSlot: 99800, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1600.0), Commission: uint8Ptr(5), CollectorRunID: "run2", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1100},
		{Time: now, VoteAccountPubkey: "vote6", NodeIdentityPubkey: "node6", ClusterSlot: 100200, LastVoteSlot: 100000, RootSlot: 99900, CreditsDelta: int64Ptr(100), IsDelinquent: false, ActivatedStakeSol: floatPtr(1600.0), Commission: uint8Ptr(5), CollectorRunID: "run3", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 1200},
		// Delinquent validator (should be filtered out of vote lag calculations)
		// This validator has massive vote lag (~50000 slots) because it stopped voting
		{Time: now.Add(-2 * time.Hour), VoteAccountPubkey: "vote7", NodeIdentityPubkey: "node7", ClusterSlot: 100000, LastVoteSlot: 50000, RootSlot: 49900, CreditsDelta: int64Ptr(0), IsDelinquent: true, ActivatedStakeSol: floatPtr(500.0), Commission: uint8Ptr(5), CollectorRunID: "run1", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 500},
		{Time: now.Add(-1 * time.Hour), VoteAccountPubkey: "vote7", NodeIdentityPubkey: "node7", ClusterSlot: 100100, LastVoteSlot: 50000, RootSlot: 49900, CreditsDelta: int64Ptr(0), IsDelinquent: true, ActivatedStakeSol: floatPtr(500.0), Commission: uint8Ptr(5), CollectorRunID: "run2", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 500},
		{Time: now, VoteAccountPubkey: "vote7", NodeIdentityPubkey: "node7", ClusterSlot: 100200, LastVoteSlot: 50000, RootSlot: 49900, CreditsDelta: int64Ptr(0), IsDelinquent: true, ActivatedStakeSol: floatPtr(500.0), Commission: uint8Ptr(5), CollectorRunID: "run3", Epoch: 100, EpochCreditsJSON: "[]", CreditsEpoch: 100, CreditsEpochCredits: 500},
	}
	var voteActivitySchema sol.VoteAccountActivitySchema
	err = voteActivityDS.WriteBatch(ctx, conn, len(voteActivityEntries), func(i int) ([]any, error) {
		return voteActivitySchema.ToRow(voteActivityEntries[i], ingestedAt), nil
	})
	require.NoError(t, err)
}

// Helper functions for pointers (floatPtr and uint8Ptr are specific to this file)
func floatPtr(f float64) *float64 {
	return &f
}

func uint8Ptr(u uint8) *uint8 {
	return &u
}

// validateSolanaValidatorsOnDZVsOffDZQuery runs the ideal query to answer the question
// and validates that the database returns the expected results (3 validators on DZ, 3 off DZ)
func validateSolanaValidatorsOnDZVsOffDZQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Query for validators on DZ
	onDZQuery := `
SELECT COUNT(DISTINCT va.vote_pubkey) AS validator_count
FROM dz_users_current u
JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL
JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
WHERE u.status = 'activated' AND va.activated_stake_lamports > 0
`

	onDZResult, err := dataset.Query(ctx, conn, onDZQuery, nil)
	require.NoError(t, err, "Failed to execute on-DZ validators query")
	require.Equal(t, 1, onDZResult.Count, "Query should return exactly one row")

	onDZCount, ok := onDZResult.Rows[0]["validator_count"].(uint64)
	if !ok {
		switch v := onDZResult.Rows[0]["validator_count"].(type) {
		case int64:
			onDZCount = uint64(v)
		case int:
			onDZCount = uint64(v)
		case uint32:
			onDZCount = uint64(v)
		case int32:
			onDZCount = uint64(v)
		default:
			t.Fatalf("Unexpected type for validator_count: %T", v)
		}
	}

	require.Equal(t, uint64(3), onDZCount,
		"Expected 3 validators on DZ (vote1, vote2, vote3), but got %d", onDZCount)

	// Query for validators off DZ (have vote accounts but not connected to DZ)
	offDZQuery := `
SELECT COUNT(DISTINCT va.vote_pubkey) AS validator_count
FROM solana_vote_accounts_current va
WHERE va.activated_stake_lamports > 0
  AND va.vote_pubkey NOT IN (
    SELECT DISTINCT va2.vote_pubkey
    FROM dz_users_current u
    JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip AND gn.gossip_ip IS NOT NULL
    JOIN solana_vote_accounts_current va2 ON gn.pubkey = va2.node_pubkey
    WHERE u.status = 'activated' AND va2.activated_stake_lamports > 0
  )
`

	offDZResult, err := dataset.Query(ctx, conn, offDZQuery, nil)
	require.NoError(t, err, "Failed to execute off-DZ validators query")
	require.Equal(t, 1, offDZResult.Count, "Query should return exactly one row")

	offDZCount, ok := offDZResult.Rows[0]["validator_count"].(uint64)
	if !ok {
		switch v := offDZResult.Rows[0]["validator_count"].(type) {
		case int64:
			offDZCount = uint64(v)
		case int:
			offDZCount = uint64(v)
		case uint32:
			offDZCount = uint64(v)
		case int32:
			offDZCount = uint64(v)
		default:
			t.Fatalf("Unexpected type for validator_count: %T", v)
		}
	}

	require.Equal(t, uint64(3), offDZCount,
		"Expected 3 validators off DZ (vote4, vote5, vote6), but got %d", offDZCount)

	t.Logf("Database validation passed: Found %d validators on DZ and %d validators off DZ", onDZCount, offDZCount)
}
