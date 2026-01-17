//go:build evals

package evals_test

import (
	"context"
	"net"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	serviceability "github.com/malbeclabs/doublezero/lake/indexer/pkg/dz/serviceability"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_ValidatorsByRegion(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_ValidatorsByRegion(t, newAnthropicLLMClient)
}


func runTest_ValidatorsByRegion(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed validators by region data
	seedValidatorsByRegionData(t, ctx, conn)

	// Validate database query results before testing agent
	validateValidatorsByRegionQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query
	question := "Analyze the validators connected to DoubleZero and return the regions with the most connected validators"
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

	// Evaluate with expectations
	expectations := []Expectation{
		{
			Description:   "Response identifies Tokyo as having validators",
			ExpectedValue: "Tokyo (tok) mentioned with 3 validators or as having the most validators",
			Rationale:     "Tokyo has 3 validators connected - the most of any metro",
		},
		{
			Description:   "Response identifies New York as having validators",
			ExpectedValue: "New York (nyc) mentioned with 2 validators",
			Rationale:     "NYC has 2 validators connected",
		},
		{
			Description:   "Response identifies London as having validators",
			ExpectedValue: "London (lon) mentioned with 1 validator",
			Rationale:     "London has 1 validator connected",
		},
		{
			Description:   "Response shows regional breakdown or ranking",
			ExpectedValue: "Results organized by region/metro showing validator counts or stake",
			Rationale:     "User asked for regions with most validators - should be ranked/organized",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly show validators by region")
}

// seedValidatorsByRegionData seeds data for validators by region test
// Scenario:
// - Tokyo (tok): 3 validators connected (most)
// - New York (nyc): 2 validators connected
// - London (lon): 1 validator connected
func seedValidatorsByRegionData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	now := testTime()
	opID := uuid.New()

	// Seed metros
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "nyc", Name: "New York"},
		{PK: "metro2", Code: "lon", Name: "London"},
		{PK: "metro3", Code: "tok", Name: "Tokyo"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed devices - one per metro
	devices := []serviceability.Device{
		{PK: "device1", Code: "nyc-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
		{PK: "device2", Code: "lon-dzd1", Status: "activated", MetroPK: "metro2", DeviceType: "DZD"},
		{PK: "device3", Code: "tok-dzd1", Status: "activated", MetroPK: "metro3", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	// Seed users - validators connected to devices
	// NYC: 2 users (validators)
	// LON: 1 user (validator)
	// TOK: 3 users (validators)
	users := []serviceability.User{
		// NYC validators
		{PK: "user1", Status: "activated", DevicePK: "device1", DZIP: net.ParseIP("10.1.1.1"), ClientIP: net.ParseIP("203.0.113.1"), Kind: "ibrl"},
		{PK: "user2", Status: "activated", DevicePK: "device1", DZIP: net.ParseIP("10.1.1.2"), ClientIP: net.ParseIP("203.0.113.2"), Kind: "ibrl"},
		// LON validator
		{PK: "user3", Status: "activated", DevicePK: "device2", DZIP: net.ParseIP("10.2.1.1"), ClientIP: net.ParseIP("198.51.100.1"), Kind: "ibrl"},
		// TOK validators
		{PK: "user4", Status: "activated", DevicePK: "device3", DZIP: net.ParseIP("10.3.1.1"), ClientIP: net.ParseIP("192.0.2.1"), Kind: "ibrl"},
		{PK: "user5", Status: "activated", DevicePK: "device3", DZIP: net.ParseIP("10.3.1.2"), ClientIP: net.ParseIP("192.0.2.2"), Kind: "ibrl"},
		{PK: "user6", Status: "activated", DevicePK: "device3", DZIP: net.ParseIP("10.3.1.3"), ClientIP: net.ParseIP("192.0.2.3"), Kind: "ibrl"},
	}
	seedUsers(t, ctx, conn, users, now, now, opID)

	// Seed gossip nodes - match dz_ip to gossip_ip
	gossipNodes := []*testGossipNode{
		// NYC validators
		{Pubkey: "node1", GossipIP: net.ParseIP("10.1.1.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.1.1.1"), TPUQUICPort: 8003, Version: "1.18.0", Epoch: 500},
		{Pubkey: "node2", GossipIP: net.ParseIP("10.1.1.2"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.1.1.2"), TPUQUICPort: 8003, Version: "1.18.0", Epoch: 500},
		// LON validator
		{Pubkey: "node3", GossipIP: net.ParseIP("10.2.1.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.2.1.1"), TPUQUICPort: 8003, Version: "1.18.0", Epoch: 500},
		// TOK validators
		{Pubkey: "node4", GossipIP: net.ParseIP("10.3.1.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.3.1.1"), TPUQUICPort: 8003, Version: "1.18.0", Epoch: 500},
		{Pubkey: "node5", GossipIP: net.ParseIP("10.3.1.2"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.3.1.2"), TPUQUICPort: 8003, Version: "1.18.0", Epoch: 500},
		{Pubkey: "node6", GossipIP: net.ParseIP("10.3.1.3"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.3.1.3"), TPUQUICPort: 8003, Version: "1.18.0", Epoch: 500},
	}
	seedGossipNodes(t, ctx, conn, gossipNodes, now, now, opID)

	// Seed vote accounts - match node_pubkey to gossip pubkey
	voteAccounts := []testVoteAccount{
		// NYC validators (2)
		{VotePubkey: "vote1", NodePubkey: "node1", EpochVoteAccount: true, Epoch: 500, ActivatedStake: 1000000000000, Commission: 5},
		{VotePubkey: "vote2", NodePubkey: "node2", EpochVoteAccount: true, Epoch: 500, ActivatedStake: 800000000000, Commission: 5},
		// LON validator (1)
		{VotePubkey: "vote3", NodePubkey: "node3", EpochVoteAccount: true, Epoch: 500, ActivatedStake: 1200000000000, Commission: 10},
		// TOK validators (3)
		{VotePubkey: "vote4", NodePubkey: "node4", EpochVoteAccount: true, Epoch: 500, ActivatedStake: 500000000000, Commission: 5},
		{VotePubkey: "vote5", NodePubkey: "node5", EpochVoteAccount: true, Epoch: 500, ActivatedStake: 600000000000, Commission: 5},
		{VotePubkey: "vote6", NodePubkey: "node6", EpochVoteAccount: true, Epoch: 500, ActivatedStake: 700000000000, Commission: 5},
	}
	seedVoteAccounts(t, ctx, conn, voteAccounts, now, now, opID)
}

// validateValidatorsByRegionQuery validates that key data exists in the database
func validateValidatorsByRegionQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Verify validators by metro query works
	query := `
SELECT
    m.code AS metro_code,
    m.name AS metro_name,
    COUNT(DISTINCT va.vote_pubkey) AS validator_count
FROM dz_users_current u
JOIN dz_devices_current d ON u.device_pk = d.pk
JOIN dz_metros_current m ON d.metro_pk = m.pk
JOIN solana_gossip_nodes_current gn ON u.dz_ip = gn.gossip_ip
JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
WHERE u.status = 'activated'
  AND va.activated_stake_lamports > 0
GROUP BY m.pk, m.code, m.name
ORDER BY validator_count DESC
`
	result, err := dataset.Query(ctx, conn, query, nil)
	require.NoError(t, err, "Failed to execute validators by region query")
	require.Equal(t, 3, result.Count, "Should have exactly 3 metros with validators")

	// Verify counts: tok=3, nyc=2, lon=1
	for _, row := range result.Rows {
		code := row["metro_code"].(string)
		count := row["validator_count"].(uint64)
		switch code {
		case "tok":
			require.Equal(t, uint64(3), count, "Tokyo should have 3 validators")
		case "nyc":
			require.Equal(t, uint64(2), count, "NYC should have 2 validators")
		case "lon":
			require.Equal(t, uint64(1), count, "London should have 1 validator")
		}
	}

	t.Logf("Database validation passed: tok=3, nyc=2, lon=1 validators")
}
