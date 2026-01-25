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

func setupSearchTables(t *testing.T) {
	ctx := t.Context()

	// Create metros table
	err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_metros_current (
			pk String,
			code String,
			name String
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create devices table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_devices_current (
			pk String,
			code String,
			device_type String,
			metro_pk String,
			public_ip String
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create links table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_links_current (
			pk String,
			code String,
			side_a_pk String,
			side_z_pk String
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create contributors table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_contributors_current (
			pk String,
			code String,
			name String
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create users table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_users_current (
			pk String,
			kind String,
			owner_pubkey String,
			dz_ip Nullable(String)
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create validators table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS solana_vote_accounts_current (
			vote_pubkey String,
			node_pubkey String,
			activated_stake_lamports Int64,
			epoch_vote_account String
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create gossip nodes table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS solana_gossip_nodes_current (
			pubkey String,
			version Nullable(String),
			gossip_ip Nullable(String)
		) ENGINE = Memory
	`)
	require.NoError(t, err)
}

func insertSearchTestData(t *testing.T) {
	ctx := t.Context()

	// Insert metros
	err := config.DB.Exec(ctx, `
		INSERT INTO dz_metros_current (pk, code, name) VALUES
		('metro-nyc', 'NYC', 'New York'),
		('metro-lax', 'LAX', 'Los Angeles'),
		('metro-chi', 'CHI', 'Chicago')
	`)
	require.NoError(t, err)

	// Insert devices
	err = config.DB.Exec(ctx, `
		INSERT INTO dz_devices_current (pk, code, device_type, metro_pk, public_ip) VALUES
		('dev-1', 'NYC-CORE-01', 'router', 'metro-nyc', '10.0.0.1'),
		('dev-2', 'NYC-EDGE-01', 'switch', 'metro-nyc', '10.0.0.2'),
		('dev-3', 'LAX-CORE-01', 'router', 'metro-lax', '10.0.1.1')
	`)
	require.NoError(t, err)

	// Insert links
	err = config.DB.Exec(ctx, `
		INSERT INTO dz_links_current (pk, code, side_a_pk, side_z_pk) VALUES
		('link-1', 'NYC-LAX-001', 'dev-1', 'dev-3'),
		('link-2', 'NYC-CHI-001', 'dev-1', 'dev-2')
	`)
	require.NoError(t, err)

	// Insert contributors
	err = config.DB.Exec(ctx, `
		INSERT INTO dz_contributors_current (pk, code, name) VALUES
		('contrib-1', 'ACME', 'Acme Corporation'),
		('contrib-2', 'GLOBEX', 'Globex Inc')
	`)
	require.NoError(t, err)

	// Insert validators
	err = config.DB.Exec(ctx, `
		INSERT INTO solana_vote_accounts_current (vote_pubkey, node_pubkey, activated_stake_lamports, epoch_vote_account) VALUES
		('validator1pubkey1234567890abcdefghijk', 'node1pubkey', 1000000000000, 'true'),
		('validator2pubkey1234567890abcdefghijk', 'node2pubkey', 500000000000, 'true')
	`)
	require.NoError(t, err)

	// Insert gossip nodes
	err = config.DB.Exec(ctx, `
		INSERT INTO solana_gossip_nodes_current (pubkey, version, gossip_ip) VALUES
		('gossip1pubkey1234567890abcdefghijklm', '1.18.0', '192.168.1.1'),
		('gossip2pubkey1234567890abcdefghijklm', '1.17.0', '192.168.1.2')
	`)
	require.NoError(t, err)
}

func TestSearchAutocomplete_EmptyQuery(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search/autocomplete?q=", nil)
	rr := httptest.NewRecorder()
	handlers.SearchAutocomplete(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.AutocompleteResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Suggestions)
}

func TestSearchAutocomplete_ShortQuery(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)

	// Query too short (< 2 chars)
	req := httptest.NewRequest(http.MethodGet, "/api/search/autocomplete?q=a", nil)
	rr := httptest.NewRecorder()
	handlers.SearchAutocomplete(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.AutocompleteResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Suggestions)
}

func TestSearchAutocomplete_FindsDevices(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search/autocomplete?q=NYC", nil)
	rr := httptest.NewRecorder()
	handlers.SearchAutocomplete(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.AutocompleteResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.NotEmpty(t, response.Suggestions)

	// Should find NYC devices, metros, and links
	var foundDevice, foundMetro, foundLink bool
	for _, s := range response.Suggestions {
		switch s.Type {
		case "device":
			if s.Label == "NYC-CORE-01" || s.Label == "NYC-EDGE-01" {
				foundDevice = true
			}
		case "metro":
			if s.Label == "NYC" {
				foundMetro = true
			}
		case "link":
			if s.Label == "NYC-LAX-001" || s.Label == "NYC-CHI-001" {
				foundLink = true
			}
		}
	}
	assert.True(t, foundDevice, "should find NYC devices")
	assert.True(t, foundMetro, "should find NYC metro")
	assert.True(t, foundLink, "should find NYC links")
}

func TestSearchAutocomplete_DevicePrefix(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	// Search with device: prefix
	req := httptest.NewRequest(http.MethodGet, "/api/search/autocomplete?q=device:CORE", nil)
	rr := httptest.NewRecorder()
	handlers.SearchAutocomplete(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.AutocompleteResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Should only find devices
	for _, s := range response.Suggestions {
		assert.Equal(t, "device", s.Type, "should only return devices with device: prefix")
	}
}

func TestSearchAutocomplete_MetroPrefix(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search/autocomplete?q=metro:New", nil)
	rr := httptest.NewRecorder()
	handlers.SearchAutocomplete(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.AutocompleteResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Should only find metros
	for _, s := range response.Suggestions {
		assert.Equal(t, "metro", s.Type)
	}
	// Should find New York
	if len(response.Suggestions) > 0 {
		assert.Equal(t, "NYC", response.Suggestions[0].Label)
		assert.Equal(t, "New York", response.Suggestions[0].Sublabel)
	}
}

func TestSearchAutocomplete_LimitParam(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search/autocomplete?q=NYC&limit=2", nil)
	rr := httptest.NewRecorder()
	handlers.SearchAutocomplete(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.AutocompleteResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(response.Suggestions), 2)
}

func TestSearchAutocomplete_IPPrefix(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	// ip: prefix should search devices, users, and gossip nodes
	req := httptest.NewRequest(http.MethodGet, "/api/search/autocomplete?q=ip:10.0", nil)
	rr := httptest.NewRecorder()
	handlers.SearchAutocomplete(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.AutocompleteResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Should find devices with matching IPs
	var foundDevice bool
	for _, s := range response.Suggestions {
		if s.Type == "device" {
			foundDevice = true
		}
	}
	assert.True(t, foundDevice)
}

func TestSearch_EmptyQuery(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=", nil)
	rr := httptest.NewRecorder()
	handlers.Search(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.SearchResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Query)
	assert.Empty(t, response.Results)
}

func TestSearch_ReturnsGroupedResults(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=NYC", nil)
	rr := httptest.NewRecorder()
	handlers.Search(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.SearchResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "NYC", response.Query)

	// Should have grouped results
	assert.NotEmpty(t, response.Results)

	// Check device results
	if deviceGroup, ok := response.Results["device"]; ok {
		assert.NotEmpty(t, deviceGroup.Items)
		assert.Greater(t, deviceGroup.Total, 0)
	}

	// Check metro results
	if metroGroup, ok := response.Results["metro"]; ok {
		assert.NotEmpty(t, metroGroup.Items)
		assert.Greater(t, metroGroup.Total, 0)
	}
}

func TestSearch_TypesFilter(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	// Filter to only devices
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=NYC&types=device", nil)
	rr := httptest.NewRecorder()
	handlers.Search(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.SearchResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Should only have device results
	for entityType := range response.Results {
		assert.Equal(t, "device", entityType)
	}
}

func TestSearch_MultipleTypes(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	// Filter to devices and metros
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=NYC&types=device,metro", nil)
	rr := httptest.NewRecorder()
	handlers.Search(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.SearchResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Should only have device and metro results
	for entityType := range response.Results {
		assert.Contains(t, []string{"device", "metro"}, entityType)
	}
}

func TestSearch_LimitParam(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=NYC&types=device&limit=1", nil)
	rr := httptest.NewRecorder()
	handlers.Search(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.SearchResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	if deviceGroup, ok := response.Results["device"]; ok {
		assert.LessOrEqual(t, len(deviceGroup.Items), 1)
		// Total should still reflect all matches
		assert.GreaterOrEqual(t, deviceGroup.Total, len(deviceGroup.Items))
	}
}

func TestSearch_ValidatorSearch(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=validator:validator1", nil)
	rr := httptest.NewRecorder()
	handlers.Search(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.SearchResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Should only have validator results
	for entityType := range response.Results {
		assert.Equal(t, "validator", entityType)
	}
}

func TestSearch_ContributorSearch(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=contributor:ACME", nil)
	rr := httptest.NewRecorder()
	handlers.Search(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.SearchResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	if contribGroup, ok := response.Results["contributor"]; ok {
		assert.NotEmpty(t, contribGroup.Items)
		assert.Equal(t, "ACME", contribGroup.Items[0].Label)
	}
}

func TestSearch_MultiTokenQuery(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	// Search for "NYC CORE" should match devices with both tokens
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=NYC%20CORE&types=device", nil)
	rr := httptest.NewRecorder()
	handlers.Search(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.SearchResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	if deviceGroup, ok := response.Results["device"]; ok {
		// Should find NYC-CORE-01 but not NYC-EDGE-01
		for _, item := range deviceGroup.Items {
			assert.Contains(t, item.Label, "CORE")
		}
	}
}

func TestSearchAutocomplete_NoResults(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search/autocomplete?q=NONEXISTENT12345", nil)
	rr := httptest.NewRecorder()
	handlers.SearchAutocomplete(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.AutocompleteResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Suggestions)
}

func TestSearch_SuggestionURLFormat(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupSearchTables(t)
	insertSearchTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=NYC&types=device,metro,link", nil)
	rr := httptest.NewRecorder()
	handlers.Search(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.SearchResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Check URL formats
	if deviceGroup, ok := response.Results["device"]; ok && len(deviceGroup.Items) > 0 {
		assert.Contains(t, deviceGroup.Items[0].URL, "/dz/devices/")
	}
	if metroGroup, ok := response.Results["metro"]; ok && len(metroGroup.Items) > 0 {
		assert.Contains(t, metroGroup.Items[0].URL, "/dz/metros/")
	}
	if linkGroup, ok := response.Results["link"]; ok && len(linkGroup.Items) > 0 {
		assert.Contains(t, linkGroup.Items[0].URL, "/dz/links/")
	}
}
