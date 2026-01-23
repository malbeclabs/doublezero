package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/handlers"
	apitesting "github.com/malbeclabs/doublezero/lake/api/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupStatsSchema creates the minimal schema needed for stats queries
func setupStatsSchema(t *testing.T) {
	ctx := t.Context()

	// Create minimal tables for stats queries
	tables := []string{
		`CREATE TABLE IF NOT EXISTS dz_users_current (
			dz_ip String,
			status String
		) ENGINE = Memory`,
		`CREATE TABLE IF NOT EXISTS dz_devices_current (
			pk String
		) ENGINE = Memory`,
		`CREATE TABLE IF NOT EXISTS dz_links_current (
			pk String,
			status String,
			link_type String,
			bandwidth_bps Int64
		) ENGINE = Memory`,
		`CREATE TABLE IF NOT EXISTS dz_contributors_current (
			pk String
		) ENGINE = Memory`,
		`CREATE TABLE IF NOT EXISTS dz_metros_current (
			pk String
		) ENGINE = Memory`,
		`CREATE TABLE IF NOT EXISTS solana_gossip_nodes_current (
			pubkey String,
			gossip_ip String
		) ENGINE = Memory`,
		`CREATE TABLE IF NOT EXISTS solana_vote_accounts_current (
			vote_pubkey String,
			node_pubkey String,
			activated_stake_lamports UInt64
		) ENGINE = Memory`,
		`CREATE TABLE IF NOT EXISTS fact_dz_device_interface_counters (
			event_ts DateTime,
			device_pk String,
			intf String,
			user_tunnel_id Nullable(String),
			in_octets_delta UInt64,
			delta_duration Float64
		) ENGINE = Memory`,
	}

	for _, ddl := range tables {
		err := config.DB.Exec(ctx, ddl)
		require.NoError(t, err)
	}
}

func TestGetStats_Empty(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupStatsSchema(t)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rr := httptest.NewRecorder()

	handlers.GetStats(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.StatsResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// With empty tables, most stats should be zero
	assert.Equal(t, uint64(0), response.ValidatorsOnDZ)
	assert.Equal(t, float64(0), response.TotalStakeSol)
	assert.Equal(t, float64(0), response.StakeSharePct)
	assert.Equal(t, uint64(0), response.Users)
	assert.Equal(t, uint64(0), response.Devices)
	assert.Equal(t, uint64(0), response.Links)
	assert.Equal(t, uint64(0), response.Contributors)
	assert.Equal(t, uint64(0), response.Metros)
	assert.NotEmpty(t, response.FetchedAt)
}

func TestGetStats_WithData(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupStatsSchema(t)
	ctx := t.Context()

	// Insert test data for users
	err := config.DB.Exec(ctx, `INSERT INTO dz_users_current (dz_ip, status) VALUES ('1.2.3.4', 'activated'), ('5.6.7.8', 'activated')`)
	require.NoError(t, err)

	// Insert test data for devices
	err = config.DB.Exec(ctx, `INSERT INTO dz_devices_current (pk) VALUES ('device1'), ('device2'), ('device3')`)
	require.NoError(t, err)

	// Insert test data for links (WAN and PNI types)
	err = config.DB.Exec(ctx, `INSERT INTO dz_links_current (pk, status, link_type, bandwidth_bps) VALUES
		('link1', 'activated', 'WAN', 1000000000),
		('link2', 'activated', 'WAN', 2000000000),
		('link3', 'inactive', 'WAN', 500000000),
		('link4', 'activated', 'PNI', 10000000000)`)
	require.NoError(t, err)

	// Insert test data for contributors
	err = config.DB.Exec(ctx, `INSERT INTO dz_contributors_current (pk) VALUES ('contrib1'), ('contrib2')`)
	require.NoError(t, err)

	// Insert test data for metros
	err = config.DB.Exec(ctx, `INSERT INTO dz_metros_current (pk) VALUES ('NYC'), ('LAX'), ('SFO')`)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rr := httptest.NewRecorder()

	handlers.GetStats(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.StatsResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, uint64(2), response.Users)
	assert.Equal(t, uint64(3), response.Devices)
	assert.Equal(t, uint64(4), response.Links) // All links, not just activated
	assert.Equal(t, uint64(2), response.Contributors)
	assert.Equal(t, uint64(3), response.Metros)
	assert.Equal(t, int64(13000000000), response.BandwidthBps) // All activated links (WAN + PNI)
	assert.NotEmpty(t, response.FetchedAt)
}

func TestGetStats_ResponseHeaders(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupStatsSchema(t)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rr := httptest.NewRecorder()

	handlers.GetStats(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	// Cache header should be set (either HIT or MISS)
	cacheHeader := rr.Header().Get("X-Cache")
	assert.True(t, cacheHeader == "HIT" || cacheHeader == "MISS", "X-Cache header should be HIT or MISS")
}

func TestGetStats_ValidatorsWithStake(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupStatsSchema(t)
	ctx := t.Context()

	// Set up the chain: dz_user -> gossip_node -> vote_account
	// User with gossip IP
	err := config.DB.Exec(ctx, `INSERT INTO dz_users_current (dz_ip, status) VALUES ('10.0.0.1', 'activated')`)
	require.NoError(t, err)

	// Gossip node matching the user's IP
	err = config.DB.Exec(ctx, `INSERT INTO solana_gossip_nodes_current (pubkey, gossip_ip) VALUES ('node_pubkey_1', '10.0.0.1')`)
	require.NoError(t, err)

	// Vote account for the gossip node with stake
	err = config.DB.Exec(ctx, `INSERT INTO solana_vote_accounts_current (vote_pubkey, node_pubkey, activated_stake_lamports) VALUES
		('vote_1', 'node_pubkey_1', 10000000000000)`) // 10000 SOL in lamports
	require.NoError(t, err)

	// Also add a vote account without matching user for total stake calculation
	err = config.DB.Exec(ctx, `INSERT INTO solana_gossip_nodes_current (pubkey, gossip_ip) VALUES ('node_pubkey_2', '20.0.0.1')`)
	require.NoError(t, err)
	err = config.DB.Exec(ctx, `INSERT INTO solana_vote_accounts_current (vote_pubkey, node_pubkey, activated_stake_lamports) VALUES
		('vote_2', 'node_pubkey_2', 10000000000000)`) // Another 10000 SOL
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
	rr := httptest.NewRecorder()

	handlers.GetStats(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.StatsResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	assert.Equal(t, uint64(1), response.ValidatorsOnDZ)
	assert.Equal(t, float64(10000), response.TotalStakeSol) // 10000 SOL
	assert.Equal(t, float64(50), response.StakeSharePct)    // 50% of total stake
}
