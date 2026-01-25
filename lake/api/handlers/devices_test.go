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

func setupDevicesTables(t *testing.T) {
	ctx := t.Context()

	// Create devices table
	err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_devices_current (
			pk String,
			code String,
			status String,
			device_type String,
			contributor_pk Nullable(String),
			metro_pk Nullable(String),
			public_ip Nullable(String),
			max_users Nullable(Int32)
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create contributors table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_contributors_current (
			pk String,
			code String,
			name Nullable(String)
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create metros table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_metros_current (
			pk String,
			code String,
			name Nullable(String),
			latitude Nullable(Float64),
			longitude Nullable(Float64)
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

	// Create traffic counters fact table
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

	// Create gossip nodes table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS solana_gossip_nodes_current (
			pubkey String,
			gossip_ip Nullable(String),
			version Nullable(String),
			gossip_port Int32
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create vote accounts table
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
}

func insertDevicesTestData(t *testing.T) {
	ctx := t.Context()

	// Insert contributors
	err := config.DB.Exec(ctx, `
		INSERT INTO dz_contributors_current (pk, code, name) VALUES
		('contrib-1', 'CONTRIB1', 'Contributor One')
	`)
	require.NoError(t, err)

	// Insert metros
	err = config.DB.Exec(ctx, `
		INSERT INTO dz_metros_current (pk, code, name, latitude, longitude) VALUES
		('metro-nyc', 'NYC', 'New York', 40.7128, -74.0060),
		('metro-lax', 'LAX', 'Los Angeles', 34.0522, -118.2437)
	`)
	require.NoError(t, err)

	// Insert devices
	err = config.DB.Exec(ctx, `
		INSERT INTO dz_devices_current (pk, code, status, device_type, contributor_pk, metro_pk, public_ip, max_users) VALUES
		('dev-1', 'NYC-CORE-01', 'up', 'router', 'contrib-1', 'metro-nyc', '10.0.0.1', 100),
		('dev-2', 'NYC-EDGE-01', 'up', 'switch', NULL, 'metro-nyc', '10.0.0.2', 50),
		('dev-3', 'LAX-CORE-01', 'down', 'router', 'contrib-1', 'metro-lax', '10.0.1.1', 100)
	`)
	require.NoError(t, err)

	// Insert users
	err = config.DB.Exec(ctx, `
		INSERT INTO dz_users_current (pk, status, device_pk, kind, owner_pubkey, dz_ip) VALUES
		('user-1', 'activated', 'dev-1', 'validator', 'pubkey1', '192.168.1.1'),
		('user-2', 'activated', 'dev-1', 'validator', 'pubkey2', '192.168.1.2'),
		('user-3', 'pending', 'dev-1', 'validator', 'pubkey3', '192.168.1.3'),
		('user-4', 'activated', 'dev-3', 'validator', 'pubkey4', '192.168.2.1')
	`)
	require.NoError(t, err)
}

func TestGetDevices_Empty(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupDevicesTables(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/devices", nil)
	rr := httptest.NewRecorder()
	handlers.GetDevices(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.DeviceListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Items)
	assert.Equal(t, 0, response.Total)
}

func TestGetDevices_ReturnsAllDevices(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupDevicesTables(t)
	insertDevicesTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/devices", nil)
	rr := httptest.NewRecorder()
	handlers.GetDevices(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.DeviceListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, 3, response.Total)
	assert.Len(t, response.Items, 3)
}

func TestGetDevices_IncludesMetroInfo(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupDevicesTables(t)
	insertDevicesTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/devices", nil)
	rr := httptest.NewRecorder()
	handlers.GetDevices(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.DeviceListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Find NYC-CORE-01
	var nycDevice *handlers.DeviceListItem
	for i := range response.Items {
		if response.Items[i].Code == "NYC-CORE-01" {
			nycDevice = &response.Items[i]
			break
		}
	}
	require.NotNil(t, nycDevice)
	assert.Equal(t, "metro-nyc", nycDevice.MetroPK)
	assert.Equal(t, "NYC", nycDevice.MetroCode)
	assert.Equal(t, "CONTRIB1", nycDevice.ContributorCode)
}

func TestGetDevices_IncludesUserCounts(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupDevicesTables(t)
	insertDevicesTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/devices", nil)
	rr := httptest.NewRecorder()
	handlers.GetDevices(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.DeviceListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Find NYC-CORE-01 (has 2 activated users, 1 pending)
	var nycDevice *handlers.DeviceListItem
	for i := range response.Items {
		if response.Items[i].Code == "NYC-CORE-01" {
			nycDevice = &response.Items[i]
			break
		}
	}
	require.NotNil(t, nycDevice)
	assert.Equal(t, uint64(2), nycDevice.CurrentUsers) // Only activated users
}

func TestGetDevices_Pagination(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupDevicesTables(t)
	insertDevicesTestData(t)

	// First page
	req := httptest.NewRequest(http.MethodGet, "/api/dz/devices?limit=2&offset=0", nil)
	rr := httptest.NewRecorder()
	handlers.GetDevices(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.DeviceListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, 3, response.Total)
	assert.Len(t, response.Items, 2)
	assert.Equal(t, 2, response.Limit)
	assert.Equal(t, 0, response.Offset)

	// Second page
	req = httptest.NewRequest(http.MethodGet, "/api/dz/devices?limit=2&offset=2", nil)
	rr = httptest.NewRecorder()
	handlers.GetDevices(rr, req)

	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, 3, response.Total)
	assert.Len(t, response.Items, 1)
	assert.Equal(t, 2, response.Offset)
}

func TestGetDevices_OrderedByCode(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupDevicesTables(t)
	insertDevicesTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/devices", nil)
	rr := httptest.NewRecorder()
	handlers.GetDevices(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.DeviceListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Verify sorted by code
	assert.Equal(t, "LAX-CORE-01", response.Items[0].Code)
	assert.Equal(t, "NYC-CORE-01", response.Items[1].Code)
	assert.Equal(t, "NYC-EDGE-01", response.Items[2].Code)
}

func TestGetDevice_NotFound(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupDevicesTables(t)
	insertDevicesTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/devices/nonexistent", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("pk", "nonexistent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	handlers.GetDevice(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetDevice_MissingPK(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupDevicesTables(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/devices/", nil)
	rctx := chi.NewRouteContext()
	// Don't add pk param
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	handlers.GetDevice(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestGetDevice_ReturnsDetails(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupDevicesTables(t)
	insertDevicesTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/devices/dev-1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("pk", "dev-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	handlers.GetDevice(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var device handlers.DeviceDetail
	err := json.NewDecoder(rr.Body).Decode(&device)
	require.NoError(t, err)

	assert.Equal(t, "dev-1", device.PK)
	assert.Equal(t, "NYC-CORE-01", device.Code)
	assert.Equal(t, "up", device.Status)
	assert.Equal(t, "router", device.DeviceType)
	assert.Equal(t, "metro-nyc", device.MetroPK)
	assert.Equal(t, "NYC", device.MetroCode)
	assert.Equal(t, "New York", device.MetroName)
	assert.Equal(t, "CONTRIB1", device.ContributorCode)
	assert.Equal(t, int32(100), device.MaxUsers)
	assert.Equal(t, uint64(2), device.CurrentUsers) // Only activated users
}

func TestGetDevice_IncludesContributorInfo(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupDevicesTables(t)
	insertDevicesTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/devices/dev-1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("pk", "dev-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	handlers.GetDevice(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var device handlers.DeviceDetail
	err := json.NewDecoder(rr.Body).Decode(&device)
	require.NoError(t, err)

	assert.Equal(t, "contrib-1", device.ContributorPK)
	assert.Equal(t, "CONTRIB1", device.ContributorCode)
}

func TestGetDevice_HandlesNullContributor(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupDevicesTables(t)
	insertDevicesTestData(t)

	// dev-2 has no contributor
	req := httptest.NewRequest(http.MethodGet, "/api/dz/devices/dev-2", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("pk", "dev-2")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	handlers.GetDevice(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var device handlers.DeviceDetail
	err := json.NewDecoder(rr.Body).Decode(&device)
	require.NoError(t, err)

	assert.Equal(t, "", device.ContributorPK)
	assert.Equal(t, "", device.ContributorCode)
}
