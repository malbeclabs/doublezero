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

func setupLinksTables(t *testing.T) {
	ctx := t.Context()

	// Create links table
	err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_links_current (
			pk String,
			code String,
			status String,
			link_type String,
			bandwidth_bps Nullable(Int64),
			side_a_pk Nullable(String),
			side_z_pk Nullable(String),
			contributor_pk Nullable(String)
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

	// Create contributors table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_contributors_current (
			pk String,
			code String,
			name Nullable(String)
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

	// Create latency fact table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS fact_dz_device_link_latency (
			event_ts DateTime,
			link_pk String,
			rtt_us Float64,
			ipdv_us Float64,
			loss UInt8
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create link health table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dz_links_health_current (
			pk String,
			avg_rtt_us Float64,
			p95_rtt_us Float64,
			committed_rtt_ns Int64,
			loss_pct Float64,
			exceeds_committed_rtt UInt8,
			has_packet_loss UInt8,
			is_dark UInt8
		) ENGINE = Memory
	`)
	require.NoError(t, err)
}

func insertLinksTestData(t *testing.T) {
	ctx := t.Context()

	// Insert metros
	err := config.DB.Exec(ctx, `
		INSERT INTO dz_metros_current (pk, code, name) VALUES
		('metro-nyc', 'NYC', 'New York'),
		('metro-lax', 'LAX', 'Los Angeles')
	`)
	require.NoError(t, err)

	// Insert devices
	err = config.DB.Exec(ctx, `
		INSERT INTO dz_devices_current (pk, code, device_type, metro_pk, public_ip) VALUES
		('dev-nyc-1', 'NYC-CORE-01', 'router', 'metro-nyc', '10.0.0.1'),
		('dev-lax-1', 'LAX-CORE-01', 'router', 'metro-lax', '10.0.1.1'),
		('dev-nyc-2', 'NYC-EDGE-01', 'router', 'metro-nyc', '10.0.0.2')
	`)
	require.NoError(t, err)

	// Insert contributors
	err = config.DB.Exec(ctx, `
		INSERT INTO dz_contributors_current (pk, code, name) VALUES
		('contrib-1', 'CONTRIB1', 'Contributor One')
	`)
	require.NoError(t, err)

	// Insert links
	err = config.DB.Exec(ctx, `
		INSERT INTO dz_links_current (pk, code, status, link_type, bandwidth_bps, side_a_pk, side_z_pk, contributor_pk) VALUES
		('link-1', 'NYC-LAX-001', 'up', 'backbone', 10000000000, 'dev-nyc-1', 'dev-lax-1', 'contrib-1'),
		('link-2', 'NYC-EDGE-001', 'up', 'access', 1000000000, 'dev-nyc-1', 'dev-nyc-2', NULL),
		('link-3', 'LAX-INTERNAL', 'down', 'internal', 100000000, 'dev-lax-1', NULL, NULL)
	`)
	require.NoError(t, err)
}

func TestGetLinks_Empty(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupLinksTables(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/links", nil)
	rr := httptest.NewRecorder()
	handlers.GetLinks(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.LinkListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Items)
	assert.Equal(t, 0, response.Total)
}

func TestGetLinks_ReturnsAllLinks(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupLinksTables(t)
	insertLinksTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/links", nil)
	rr := httptest.NewRecorder()
	handlers.GetLinks(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.LinkListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, 3, response.Total)
	assert.Len(t, response.Items, 3)
}

func TestGetLinks_IncludesDeviceInfo(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupLinksTables(t)
	insertLinksTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/links", nil)
	rr := httptest.NewRecorder()
	handlers.GetLinks(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.LinkListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Find the backbone link
	var backboneLink *handlers.LinkListItem
	for i := range response.Items {
		if response.Items[i].Code == "NYC-LAX-001" {
			backboneLink = &response.Items[i]
			break
		}
	}
	require.NotNil(t, backboneLink)
	assert.Equal(t, "NYC-CORE-01", backboneLink.SideACode)
	assert.Equal(t, "NYC", backboneLink.SideAMetro)
	assert.Equal(t, "LAX-CORE-01", backboneLink.SideZCode)
	assert.Equal(t, "LAX", backboneLink.SideZMetro)
	assert.Equal(t, "CONTRIB1", backboneLink.ContributorCode)
}

func TestGetLinks_Pagination(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupLinksTables(t)
	insertLinksTestData(t)

	// First page
	req := httptest.NewRequest(http.MethodGet, "/api/dz/links?limit=2&offset=0", nil)
	rr := httptest.NewRecorder()
	handlers.GetLinks(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.LinkListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, 3, response.Total)
	assert.Len(t, response.Items, 2)
	assert.Equal(t, 2, response.Limit)
	assert.Equal(t, 0, response.Offset)

	// Second page
	req = httptest.NewRequest(http.MethodGet, "/api/dz/links?limit=2&offset=2", nil)
	rr = httptest.NewRecorder()
	handlers.GetLinks(rr, req)

	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, 3, response.Total)
	assert.Len(t, response.Items, 1)
	assert.Equal(t, 2, response.Offset)
}

func TestGetLinks_OrderedByCode(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupLinksTables(t)
	insertLinksTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/links", nil)
	rr := httptest.NewRecorder()
	handlers.GetLinks(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.PaginatedResponse[handlers.LinkListItem]
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Verify sorted by code
	assert.Equal(t, "LAX-INTERNAL", response.Items[0].Code)
	assert.Equal(t, "NYC-EDGE-001", response.Items[1].Code)
	assert.Equal(t, "NYC-LAX-001", response.Items[2].Code)
}

func TestGetLink_NotFound(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupLinksTables(t)
	insertLinksTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/links/nonexistent", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("pk", "nonexistent")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	handlers.GetLink(rr, req)

	assert.Equal(t, http.StatusNotFound, rr.Code)
}

func TestGetLink_MissingPK(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupLinksTables(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/links/", nil)
	rctx := chi.NewRouteContext()
	// Don't add pk param
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	handlers.GetLink(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestGetLink_ReturnsDetails(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupLinksTables(t)
	insertLinksTestData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/links/link-1", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("pk", "link-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	handlers.GetLink(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var link handlers.LinkDetail
	err := json.NewDecoder(rr.Body).Decode(&link)
	require.NoError(t, err)

	assert.Equal(t, "link-1", link.PK)
	assert.Equal(t, "NYC-LAX-001", link.Code)
	assert.Equal(t, "up", link.Status)
	assert.Equal(t, "backbone", link.LinkType)
	assert.Equal(t, int64(10000000000), link.BandwidthBps)
	assert.Equal(t, "NYC-CORE-01", link.SideACode)
	assert.Equal(t, "LAX-CORE-01", link.SideZCode)
}

func setupLinkHealthData(t *testing.T) {
	ctx := t.Context()

	// Insert link health data
	err := config.DB.Exec(ctx, `
		INSERT INTO dz_links_health_current (pk, avg_rtt_us, p95_rtt_us, committed_rtt_ns, loss_pct, exceeds_committed_rtt, has_packet_loss, is_dark) VALUES
		('link-1', 1500.0, 2000.0, 3000000, 0.0, 0, 0, 0),
		('link-2', 500.0, 800.0, 1000000, 0.05, 0, 0, 0)
	`)
	require.NoError(t, err)
}

func TestGetLinkHealth_Empty(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupLinksTables(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/links/health", nil)
	rr := httptest.NewRecorder()
	handlers.GetLinkHealth(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.TopologyLinkHealthResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Links)
	assert.Equal(t, 0, response.TotalLinks)
}

func TestGetLinkHealth_ReturnsHealth(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupLinksTables(t)
	insertLinksTestData(t)
	setupLinkHealthData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/links/health", nil)
	rr := httptest.NewRecorder()
	handlers.GetLinkHealth(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.TopologyLinkHealthResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Only links with both side_a_pk and side_z_pk are returned
	// link-1 and link-2 have both sides, link-3 only has side_a
	assert.Equal(t, 2, response.TotalLinks)
	assert.Len(t, response.Links, 2)
}

func TestGetLinkHealth_CalculatesSlaStatus(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupLinksTables(t)
	insertLinksTestData(t)
	setupLinkHealthData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/links/health", nil)
	rr := httptest.NewRecorder()
	handlers.GetLinkHealth(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.TopologyLinkHealthResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Find link-1 (healthy: avg_rtt_us=1500, committed=3000000ns=3000us, ratio=0.5)
	var link1 *handlers.TopologyLinkHealth
	for i := range response.Links {
		if response.Links[i].LinkPK == "link-1" {
			link1 = &response.Links[i]
			break
		}
	}
	require.NotNil(t, link1)
	assert.Equal(t, "healthy", link1.SlaStatus)
	assert.InDelta(t, 0.5, link1.SlaRatio, 0.01) // 1500 / 3000 = 0.5
}

func TestGetLinkHealth_CountsByStatus(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	setupLinksTables(t)
	insertLinksTestData(t)
	setupLinkHealthData(t)

	req := httptest.NewRequest(http.MethodGet, "/api/dz/links/health", nil)
	rr := httptest.NewRecorder()
	handlers.GetLinkHealth(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.TopologyLinkHealthResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Both links should be healthy based on our test data
	assert.Equal(t, 2, response.HealthyCount)
	assert.Equal(t, 0, response.WarningCount)
	assert.Equal(t, 0, response.CriticalCount)
	assert.Equal(t, 0, response.UnknownCount)
}
