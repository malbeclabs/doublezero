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

func TestGetCatalog(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	ctx := t.Context()

	// Create test tables
	err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS catalog_test_table (
			id UInt64,
			name String,
			value Float64
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/catalog", nil)
	rr := httptest.NewRecorder()

	handlers.GetCatalog(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	var response handlers.CatalogResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Should have at least our test table
	found := false
	for _, table := range response.Tables {
		if table.Name == "catalog_test_table" {
			found = true
			assert.Equal(t, "table", table.Type)
			assert.Contains(t, table.Columns, "id")
			assert.Contains(t, table.Columns, "name")
			assert.Contains(t, table.Columns, "value")
			break
		}
	}
	assert.True(t, found, "catalog_test_table should be in catalog")
}

func TestGetCatalog_ExcludesStaging(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	ctx := t.Context()

	// Create a staging table (should be excluded)
	err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS stg_excluded_table (
			id UInt64
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create a normal table
	err = config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS normal_table (
			id UInt64
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/catalog", nil)
	rr := httptest.NewRecorder()

	handlers.GetCatalog(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.CatalogResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Check that staging table is excluded
	for _, table := range response.Tables {
		assert.False(t, table.Name == "stg_excluded_table", "staging tables should be excluded")
	}

	// Check that normal table is included
	foundNormal := false
	for _, table := range response.Tables {
		if table.Name == "normal_table" {
			foundNormal = true
			break
		}
	}
	assert.True(t, foundNormal, "normal_table should be in catalog")
}

func TestGetCatalog_IdentifiesViews(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	ctx := t.Context()

	// Create a base table
	err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS catalog_view_base (
			id UInt64,
			value Float64
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Create a view
	err = config.DB.Exec(ctx, `
		CREATE VIEW IF NOT EXISTS catalog_test_view AS
		SELECT id, value * 2 as doubled_value
		FROM catalog_view_base
	`)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/catalog", nil)
	rr := httptest.NewRecorder()

	handlers.GetCatalog(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.CatalogResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Check table type
	foundTable := false
	foundView := false
	for _, table := range response.Tables {
		if table.Name == "catalog_view_base" {
			foundTable = true
			assert.Equal(t, "table", table.Type)
		}
		if table.Name == "catalog_test_view" {
			foundView = true
			assert.Equal(t, "view", table.Type)
		}
	}
	assert.True(t, foundTable, "base table should be in catalog")
	assert.True(t, foundView, "view should be in catalog")
}

func TestGetCatalog_ColumnsOrdered(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	ctx := t.Context()

	// Create a table with multiple columns
	err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS catalog_column_order (
			first_col UInt64,
			second_col String,
			third_col DateTime,
			fourth_col Float64
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/catalog", nil)
	rr := httptest.NewRecorder()

	handlers.GetCatalog(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.CatalogResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Find our table and verify column order
	for _, table := range response.Tables {
		if table.Name == "catalog_column_order" {
			require.Len(t, table.Columns, 4)
			assert.Equal(t, "first_col", table.Columns[0])
			assert.Equal(t, "second_col", table.Columns[1])
			assert.Equal(t, "third_col", table.Columns[2])
			assert.Equal(t, "fourth_col", table.Columns[3])
			return
		}
	}
	t.Fatal("catalog_column_order not found in response")
}

func TestGetCatalog_EmptyDatabase(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)

	// Don't create any tables - test with empty database

	req := httptest.NewRequest(http.MethodGet, "/api/catalog", nil)
	rr := httptest.NewRecorder()

	handlers.GetCatalog(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.CatalogResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)

	// Tables may be nil or empty slice
	if response.Tables != nil {
		assert.Empty(t, response.Tables)
	}
}

func TestTableInfo_Structure(t *testing.T) {
	info := handlers.TableInfo{
		Name:     "test_table",
		Database: "default",
		Engine:   "MergeTree",
		Type:     "table",
		Columns:  []string{"id", "name"},
	}

	data, err := json.Marshal(info)
	require.NoError(t, err)

	var decoded handlers.TableInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, info.Name, decoded.Name)
	assert.Equal(t, info.Database, decoded.Database)
	assert.Equal(t, info.Engine, decoded.Engine)
	assert.Equal(t, info.Type, decoded.Type)
	assert.Equal(t, info.Columns, decoded.Columns)
}
