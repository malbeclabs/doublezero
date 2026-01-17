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
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/geoip"
	maxmindgeoip "github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
	"github.com/stretchr/testify/require"
)

func TestLake_Agent_Evals_Anthropic_OffDZValidatorsByRegion(t *testing.T) {
	t.Parallel()
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set, skipping eval test")
	}

	runTest_OffDZValidatorsByRegion(t, newAnthropicLLMClient)
}


func runTest_OffDZValidatorsByRegion(t *testing.T, llmFactory LLMClientFactory) {
	ctx := context.Background()

	// Get debug level
	debugLevel, debug := getDebugLevel()

	// Set up test database
	clientInfo := testClientInfo(t)

	// Set up test data
	conn, err := clientInfo.Client.Conn(ctx)
	require.NoError(t, err)
	defer conn.Close()

	// Seed off-DZ validators by region data
	seedOffDZValidatorsByRegionData(t, ctx, conn)

	// Validate database query results before testing agent
	validateOffDZValidatorsByRegionQuery(t, ctx, conn)

	// Skip workflow execution in short mode
	if testing.Short() {
		t.Log("Skipping workflow execution in short mode")
		return
	}

	// Set up workflow with LLM client
	p := setupWorkflow(t, ctx, clientInfo, llmFactory, debug, debugLevel)

	// Run the query
	question := "list the top 10 by stake validators in Tokyo who are not connected to DoubleZero"
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
	// Note: The test database contains synthetic data - these are the expected values from our test fixtures
	expectations := []Expectation{
		{
			Description:   "Response includes the 3 off-DZ Tokyo validators",
			ExpectedValue: "offvote1/offnode1, offvote2/offnode2, offvote3/offnode3 all appear in the response (either vote_pubkey or node_pubkey identifier is acceptable)",
			Rationale:     "Test data has exactly 3 off-DZ validators in Tokyo",
		},
		{
			Description:   "Validators are ranked with offvote1/offnode1 first (highest stake)",
			ExpectedValue: "offvote1 or offnode1 appears first or at rank 1, as it has the highest stake",
			Rationale:     "offvote1/offnode1 has the most stake and should be ranked first",
		},
		{
			Description:   "Response correctly excludes on-DZ validators",
			ExpectedValue: "onvote1/onnode1 and onvote2/onnode2 do NOT appear in the results",
			Rationale:     "These validators ARE connected to DZ and should be filtered out",
		},
	}
	isCorrect, err := evaluateResponse(t, ctx, question, response, expectations...)
	require.NoError(t, err, "Evaluation must be available")
	require.True(t, isCorrect, "Evaluation indicates the response does not correctly show off-DZ validators by region")
}

// seedOffDZValidatorsByRegionData seeds data for off-DZ validators by region test
// Scenario:
// - 2 validators ON DZ in Tokyo (should NOT appear)
// - 3 validators OFF DZ in Tokyo (should appear, ranked by stake)
// - 2 validators OFF DZ in New York (should NOT appear - wrong region)
func seedOffDZValidatorsByRegionData(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	log := testLogger(t)
	now := testTime()
	opID := uuid.New()

	// Seed metros (for on-DZ validators)
	metros := []serviceability.Metro{
		{PK: "metro1", Code: "tok", Name: "Tokyo"},
	}
	seedMetros(t, ctx, conn, metros, now, now)

	// Seed device (for on-DZ validators)
	devices := []serviceability.Device{
		{PK: "device1", Code: "tok-dzd1", Status: "activated", MetroPK: "metro1", DeviceType: "DZD"},
	}
	seedDevices(t, ctx, conn, devices, now, now)

	// Seed users for ON-DZ validators (2 validators on DZ in Tokyo)
	users := []serviceability.User{
		{PK: "user1", Status: "activated", DevicePK: "device1", DZIP: net.ParseIP("10.1.1.1"), ClientIP: net.ParseIP("203.0.113.1"), Kind: "ibrl"},
		{PK: "user2", Status: "activated", DevicePK: "device1", DZIP: net.ParseIP("10.1.1.2"), ClientIP: net.ParseIP("203.0.113.2"), Kind: "ibrl"},
	}
	seedUsers(t, ctx, conn, users, now, now, opID)

	// Seed gossip nodes:
	// - 2 ON-DZ (gossip_ip matches dz_ip)
	// - 3 OFF-DZ in Tokyo (no matching dz_user)
	// - 2 OFF-DZ in New York (no matching dz_user)
	gossipNodes := []*testGossipNode{
		// ON-DZ validators (Tokyo)
		{Pubkey: "onnode1", GossipIP: net.ParseIP("10.1.1.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.1.1.1"), TPUQUICPort: 8003, Version: "1.18.0", Epoch: 500},
		{Pubkey: "onnode2", GossipIP: net.ParseIP("10.1.1.2"), GossipPort: 8001, TPUQUICIP: net.ParseIP("10.1.1.2"), TPUQUICPort: 8003, Version: "1.18.0", Epoch: 500},
		// OFF-DZ validators (Tokyo) - different IPs, no dz_user match
		{Pubkey: "offnode1", GossipIP: net.ParseIP("45.76.100.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("45.76.100.1"), TPUQUICPort: 8003, Version: "1.18.0", Epoch: 500},
		{Pubkey: "offnode2", GossipIP: net.ParseIP("45.76.100.2"), GossipPort: 8001, TPUQUICIP: net.ParseIP("45.76.100.2"), TPUQUICPort: 8003, Version: "1.18.0", Epoch: 500},
		{Pubkey: "offnode3", GossipIP: net.ParseIP("45.76.100.3"), GossipPort: 8001, TPUQUICIP: net.ParseIP("45.76.100.3"), TPUQUICPort: 8003, Version: "1.18.0", Epoch: 500},
		// OFF-DZ validators (New York) - different IPs, different city
		{Pubkey: "offnyc1", GossipIP: net.ParseIP("192.0.2.1"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.0.2.1"), TPUQUICPort: 8003, Version: "1.18.0", Epoch: 500},
		{Pubkey: "offnyc2", GossipIP: net.ParseIP("192.0.2.2"), GossipPort: 8001, TPUQUICIP: net.ParseIP("192.0.2.2"), TPUQUICPort: 8003, Version: "1.18.0", Epoch: 500},
	}
	seedGossipNodes(t, ctx, conn, gossipNodes, now, now, opID)

	// Seed vote accounts
	voteAccounts := []testVoteAccount{
		// ON-DZ validators
		{VotePubkey: "onvote1", NodePubkey: "onnode1", EpochVoteAccount: true, Epoch: 500, ActivatedStake: 1000000000000, Commission: 5},
		{VotePubkey: "onvote2", NodePubkey: "onnode2", EpochVoteAccount: true, Epoch: 500, ActivatedStake: 800000000000, Commission: 5},
		// OFF-DZ Tokyo validators (varying stake for ranking test)
		{VotePubkey: "offvote1", NodePubkey: "offnode1", EpochVoteAccount: true, Epoch: 500, ActivatedStake: 3000000000000, Commission: 5},  // 3000 SOL - highest
		{VotePubkey: "offvote2", NodePubkey: "offnode2", EpochVoteAccount: true, Epoch: 500, ActivatedStake: 2500000000000, Commission: 5},  // 2500 SOL
		{VotePubkey: "offvote3", NodePubkey: "offnode3", EpochVoteAccount: true, Epoch: 500, ActivatedStake: 2000000000000, Commission: 10}, // 2000 SOL - lowest Tokyo
		// OFF-DZ NYC validators
		{VotePubkey: "offnycvote1", NodePubkey: "offnyc1", EpochVoteAccount: true, Epoch: 500, ActivatedStake: 5000000000000, Commission: 5}, // 5000 SOL but wrong city
		{VotePubkey: "offnycvote2", NodePubkey: "offnyc2", EpochVoteAccount: true, Epoch: 500, ActivatedStake: 4000000000000, Commission: 5}, // 4000 SOL but wrong city
	}
	seedVoteAccounts(t, ctx, conn, voteAccounts, now, now, opID)

	// Seed GeoIP records for the off-DZ validators
	geoipDS, err := geoip.NewGeoIPRecordDataset(log)
	require.NoError(t, err)

	geoRecords := []*maxmindgeoip.Record{
		// Tokyo IPs
		{IP: net.ParseIP("45.76.100.1"), City: "Tokyo", Country: "Japan", CountryCode: "JP", Region: "Tokyo", Latitude: 35.6762, Longitude: 139.6503},
		{IP: net.ParseIP("45.76.100.2"), City: "Tokyo", Country: "Japan", CountryCode: "JP", Region: "Tokyo", Latitude: 35.6762, Longitude: 139.6503},
		{IP: net.ParseIP("45.76.100.3"), City: "Tokyo", Country: "Japan", CountryCode: "JP", Region: "Tokyo", Latitude: 35.6762, Longitude: 139.6503},
		// New York IPs
		{IP: net.ParseIP("192.0.2.1"), City: "New York", Country: "United States", CountryCode: "US", Region: "New York", Latitude: 40.7128, Longitude: -74.0060},
		{IP: net.ParseIP("192.0.2.2"), City: "New York", Country: "United States", CountryCode: "US", Region: "New York", Latitude: 40.7128, Longitude: -74.0060},
	}

	var geoSchema geoip.GeoIPRecordSchema
	err = geoipDS.WriteBatch(ctx, conn, len(geoRecords), func(i int) ([]any, error) {
		return geoSchema.ToRow(geoRecords[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		SnapshotTS: now,
		OpID:       opID,
	})
	require.NoError(t, err)
}

// validateOffDZValidatorsByRegionQuery validates that key data exists in the database
func validateOffDZValidatorsByRegionQuery(t *testing.T, ctx context.Context, conn clickhouse.Connection) {
	// Verify off-DZ Tokyo validators query works
	query := `
SELECT
    va.vote_pubkey,
    va.activated_stake_lamports / 1e9 AS stake_sol,
    gn.gossip_ip,
    geo.city
FROM solana_gossip_nodes_current gn
JOIN solana_vote_accounts_current va ON gn.pubkey = va.node_pubkey
LEFT JOIN geoip_records_current geo ON gn.gossip_ip = geo.ip
LEFT JOIN dz_users_current u ON gn.gossip_ip = u.dz_ip AND u.status = 'activated'
WHERE u.pk = ''
  AND va.activated_stake_lamports > 0
  AND geo.city = 'Tokyo'
ORDER BY va.activated_stake_lamports DESC
LIMIT 10
`
	result, err := dataset.Query(ctx, conn, query, nil)
	require.NoError(t, err, "Failed to execute off-DZ validators query")
	require.Equal(t, 3, result.Count, "Should have exactly 3 off-DZ Tokyo validators")

	// Verify ordering (highest stake first)
	firstStake := result.Rows[0]["stake_sol"].(float64)
	require.Equal(t, float64(3000), firstStake, "First validator should have 3000 SOL stake")

	t.Logf("Database validation passed: 3 off-DZ Tokyo validators found, correctly ordered by stake")
}
