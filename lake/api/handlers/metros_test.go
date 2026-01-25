package handlers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/handlers"
	apitesting "github.com/malbeclabs/doublezero/lake/api/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupMetrosTables(t *testing.T) {
	ctx := t.Context()

	// Create metros table
	err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_metros_current (
			pk String,
			code String,
			name Nullable(String),
			latitude Nullable(Float64),
			longitude Nullable(Float64)
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create devices table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_devices_current (
			pk String,
			code String,
			device_type String,
			metro_pk Nullable(String),
			public_ip String
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create users table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_users_current (
			pk String,
			status String,
			device_pk String,
			kind String,
			owner_pubkey String,
			dz_ip Nullable(String)
		) ENGINE = Memory
	`)
	require.NoError(t, err)
}

func insertMetrosTestData(t *testing.T) {
	ctx := t.Context()

	// Insert metros
	err := config.DB.Exec(ctx, `
		INSERT INTO dz_metros_current (pk, code, name, latitude, longitude) VALUES
		('metro-nyc', 'NYC', 'New York', 40.7128, -74.0060),
		('metro-lax', 'LAX', 'Los Angeles', 34.0522, -118.2437),
		('metro-chi', 'CHI', 'Chicago', 41.8781, -87.6298)
	`)
	require.NoError(t, err)

	// Insert devices
	err = config.DB.Exec(ctx, `
		INSERT INTO dz_devices_current (pk, code, device_type, metro_pk, public_ip) VALUES
		('dev-1', 'NYC-CORE-01', 'router', 'metro-nyc', '10.0.0.1'),
		('dev-2', 'NYC-EDGE-01', 'switch', 'metro-nyc', '10.0.0.2'),
		('dev-3', 'LAX-CORE-01', 'router', 'metro-lax', '10.0.1.1'),
		('dev-4', 'CHI-CORE-01', 'router', 'metro-chi', '10.0.2.1')
	`)
	require.NoError(t, err)

	// Insert users
	err = config.DB.Exec(ctx, `
		INSERT INTO dz_users_current (pk, status, device_pk, kind, owner_pubkey, dz_ip) VALUES
		('user-1', 'activated', 'dev-1', 'validator', 'pubkey1', '192.168.1.1'),
		('user-2', 'activated', 'dev-1', 'validator', 'pubkey2', '192.168.1.2'),
		('user-3', 'activated', 'dev-3', 'validator', 'pubkey3', '192.168.2.1'),
		('user-4', 'pending', 'dev-2', 'validator', 'pubkey4', '192.168.1.3')
	`)
	require.NoError(t, err)
}

func TestGetMetros_Empty(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupMetrosTables(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/metros", nil)
	rr := httptest.NewRecorder()
	handlers.GetMetros(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.MetroListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Items)
	assert.Equal(t, 0, response.Total)
}

func TestGetMetros_ReturnsAllMetros(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupMetrosTables(t)
	insertMetrosTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/metros", nil)
	rr := httptest.NewRecorder()
	handlers.GetMetros(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.MetroListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, 3, response.Total)
	assert.Len(t, response.Items, 3)

	// Verify order (should be by code)
	assert.Equal(t, "CHI", response.Items[0].Code)
	assert.Equal(t, "LAX", response.Items[1].Code)
	assert.Equal(t, "NYC", response.Items[2].Code)
}

func TestGetMetros_IncludesDeviceCounts(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupMetrosTables(t)
	insertMetrosTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/metros", nil)
	rr := httptest.NewRecorder()
	handlers.GetMetros(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.MetroListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Find NYC metro (has 2 devices)
	var nycMetro *handlers.MetroListItem
	for i := range response.Items {
		if response.Items[i].Code == "NYC" {
			nycMetro = &response.Items[i]
			break
		}
	}
	require.NotNil(t, nycMetro)
	assert.Equal(t, uint64(2), nycMetro.DeviceCount)
}

func TestGetMetros_IncludesUserCounts(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupMetrosTables(t)
	insertMetrosTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/metros", nil)
	rr := httptest.NewRecorder()
	handlers.GetMetros(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.MetroListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Find NYC metro (has 2 activated users on dev-1, 1 pending on dev-2)
	var nycMetro *handlers.MetroListItem
	for i := range response.Items {
		if response.Items[i].Code == "NYC" {
			nycMetro = &response.Items[i]
			break
		}
	}
	require.NotNil(t, nycMetro)
	assert.Equal(t, uint64(2), nycMetro.UserCount) // Only activated users
}

func TestGetMetros_Pagination(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupMetrosTables(t)
	insertMetrosTestData(t)

	// First page
	req := httptest.NewRequest(http.MethodGet, "/api/dz/metros?limit=2&offset=0", nil)
	rr := httptest.NewRecorder()
	handlers.GetMetros(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.MetroListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, 3, response.Total)
	assert.Len(t, response.Items, 2)
	assert.Equal(t, 2, response.Limit)
	assert.Equal(t, 0, response.Offset)

	// Second page
	req = httptest.NewRequest(http.MethodGet, "/api/dz/metros?limit=2&offset=2", nil)
	rr = httptest.NewRecorder()
	handlers.GetMetros(rr, req)

	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, 3, response.Total)
	assert.Len(t, response.Items, 1)
	assert.Equal(t, 2, response.Offset)
}

func TestGetMetros_IncludesCoordinates(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupMetrosTables(t)
	insertMetrosTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/metros", nil)
	rr := httptest.NewRecorder()
	handlers.GetMetros(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.MetroListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Find NYC and check coordinates
	var nycMetro *handlers.MetroListItem
	for i := range response.Items {
		if response.Items[i].Code == "NYC" {
			nycMetro = &response.Items[i]
			break
		}
	}
	require.NotNil(t, nycMetro)
	assert.InDelta(t, 40.7128, nycMetro.Latitude, 0.001)
	assert.InDelta(t, -74.0060, nycMetro.Longitude, 0.001)
}

func TestGetMetro_NotFound(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupMetrosTables(t)
	setupMetroDetailTables(t)
	insertMetrosTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/metros/nonexistent", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("pk", "nonexistent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	handlers.GetMetro(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetMetro_MissingPK(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupMetrosTables(t)
	setupMetroDetailTables(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/metros/", nil)
	rctx := chi.NewRouteContext()
	// Don't add pk param
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	handlers.GetMetro(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func setupMetroDetailTables(t *testing.T) {
	ctx := t.Context()

	// Additional tables needed for metro detail
	err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS solana_gossip_nodes_current (
			pubkey String,
			gossip_ip Nullable(String),
			version Nullable(String),
			gossip_port Int32
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS solana_vote_accounts_current (
			vote_pubkey String,
			node_pubkey String,
			activated_stake_lamports Int64,
			epoch_vote_account String,
			commission_percentage Nullable(Int64)
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS fact_dz_device_interface_counters (
			event_ts DateTime,
			device_pk String,
			in_octets_delta UInt64,
			out_octets_delta UInt64,
			delta_duration Float64,
			user_tunnel_id Nullable(String),
			link_pk String
		) ENGINE = Memory
	`)
	require.NoError(t, err)
}

func TestGetMetro_ReturnsDetails(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupMetrosTables(t)
	setupMetroDetailTables(t)
	insertMetrosTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/metros/metro-nyc", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("pk", "metro-nyc")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	handlers.GetMetro(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var metro handlers.MetroDetail
	err := json.NewDecoder(rr.Body).Decode(&metro)
	require.NoError(t, err)

	assert.Equal(t, "metro-nyc", metro.PK)
	assert.Equal(t, "NYC", metro.Code)
	assert.Equal(t, "New York", metro.Name)
	assert.Equal(t, uint64(2), metro.DeviceCount)
	assert.Equal(t, uint64(2), metro.UserCount)
}
