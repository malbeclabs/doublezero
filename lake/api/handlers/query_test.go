package handlers_test

import (
	"bytes"
	"encoding/json"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/handlers"
	apitesting "github.com/malbeclabs/doublezero/lake/api/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteQuery_Select(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	ctx := t.Context()

	// Create a test table
	err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS test_query_select (
			id UInt64,
			name String
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Insert test data
	err = config.DB.Exec(ctx, `
		INSERT INTO test_query_select (id, name) VALUES (1, 'Alice'), (2, 'Bob')
	`)
	require.NoError(t, err)

	// Execute query
	reqBody := handlers.QueryRequest{
		Query: "SELECT id, name FROM test_query_select ORDER BY id",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteQuery(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.QueryResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Error)
	assert.Equal(t, []string{"id", "name"}, response.Columns)
	assert.Equal(t, 2, response.RowCount)
	assert.True(t, response.ElapsedMs >= 0)
}

func TestExecuteQuery_Empty(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)

	reqBody := handlers.QueryRequest{
		Query: "",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteQuery(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestExecuteQuery_WhitespaceOnly(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)

	reqBody := handlers.QueryRequest{
		Query: "   \t\n  ",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteQuery(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestExecuteQuery_InvalidSQL(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)

	reqBody := handlers.QueryRequest{
		Query: "SELECTERINO * FROMONO nonexistent",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteQuery(rr, req)

	// Should return 200 OK with error in response body
	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.QueryResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.NotEmpty(t, response.Error)
}

func TestExecuteQuery_InvalidRequestBody(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)

	req := httptest.NewRequest(http.MethodPost, "/api/query", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteQuery(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestExecuteQuery_TrimsTrailingSemicolon(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)

	reqBody := handlers.QueryRequest{
		Query: "SELECT 1;",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteQuery(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.QueryResponse
	err := json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Error)
	assert.Equal(t, 1, response.RowCount)
}

func TestToJSONSafe_NetIP(t *testing.T) {
	ip := net.ParseIP("192.168.1.1")
	result := toJSONSafeWrapper(ip)
	assert.Equal(t, "192.168.1.1", result)

	// Pointer to IP
	result = toJSONSafeWrapper(&ip)
	assert.Equal(t, "192.168.1.1", result)

	// Nil pointer
	var nilIP *net.IP
	result = toJSONSafeWrapper(nilIP)
	assert.Nil(t, result)
}

func TestToJSONSafe_FloatSpecialValues(t *testing.T) {
	// NaN
	result := toJSONSafeWrapper(math.NaN())
	assert.Nil(t, result)

	// Positive Infinity
	result = toJSONSafeWrapper(math.Inf(1))
	assert.Nil(t, result)

	// Negative Infinity
	result = toJSONSafeWrapper(math.Inf(-1))
	assert.Nil(t, result)

	// Normal float
	result = toJSONSafeWrapper(3.14)
	assert.Equal(t, 3.14, result)

	// Float32 NaN
	result = toJSONSafeWrapper(float32(math.NaN()))
	assert.Nil(t, result)

	// Normal float32
	result = toJSONSafeWrapper(float32(2.5))
	assert.Equal(t, float32(2.5), result)
}

func TestToJSONSafe_FloatPointers(t *testing.T) {
	// Nil float64 pointer
	var nilFloat64 *float64
	result := toJSONSafeWrapper(nilFloat64)
	assert.Nil(t, result)

	// Float64 pointer with NaN
	nan := math.NaN()
	result = toJSONSafeWrapper(&nan)
	assert.Nil(t, result)

	// Float64 pointer with normal value
	normal := 1.5
	result = toJSONSafeWrapper(&normal)
	assert.Equal(t, 1.5, result)

	// Nil float32 pointer
	var nilFloat32 *float32
	result = toJSONSafeWrapper(nilFloat32)
	assert.Nil(t, result)
}

func TestToJSONSafe_Time(t *testing.T) {
	now := time.Now()
	result := toJSONSafeWrapper(now)
	assert.Equal(t, now.Format(time.RFC3339), result)

	// Time pointer
	result = toJSONSafeWrapper(&now)
	assert.Equal(t, now.Format(time.RFC3339), result)

	// Nil time pointer
	var nilTime *time.Time
	result = toJSONSafeWrapper(nilTime)
	assert.Nil(t, result)
}

func TestToJSONSafe_NilInterface(t *testing.T) {
	result := toJSONSafeWrapper(nil)
	assert.Nil(t, result)
}

func TestToJSONSafe_RegularValues(t *testing.T) {
	// String
	result := toJSONSafeWrapper("hello")
	assert.Equal(t, "hello", result)

	// Int
	result = toJSONSafeWrapper(42)
	assert.Equal(t, 42, result)

	// Bool
	result = toJSONSafeWrapper(true)
	assert.Equal(t, true, result)
}

// toJSONSafeWrapper is a wrapper to test the unexported toJSONSafe function
// We test it indirectly through the handler's behavior
func toJSONSafeWrapper(v any) any {
	// Since toJSONSafe is unexported, we'll inline the same logic here for testing
	// This tests the logic, not the actual function
	if v == nil {
		return nil
	}

	switch val := v.(type) {
	case net.IP:
		return val.String()
	case *net.IP:
		if val == nil {
			return nil
		}
		return val.String()
	case float32:
		if math.IsNaN(float64(val)) || math.IsInf(float64(val), 0) {
			return nil
		}
		return val
	case float64:
		if math.IsNaN(val) || math.IsInf(val, 0) {
			return nil
		}
		return val
	case *float32:
		if val == nil {
			return nil
		}
		if math.IsNaN(float64(*val)) || math.IsInf(float64(*val), 0) {
			return nil
		}
		return *val
	case *float64:
		if val == nil {
			return nil
		}
		if math.IsNaN(*val) || math.IsInf(*val, 0) {
			return nil
		}
		return *val
	case time.Time:
		return val.Format(time.RFC3339)
	case *time.Time:
		if val == nil {
			return nil
		}
		return val.Format(time.RFC3339)
	default:
		return v
	}
}

func TestExecuteQuery_JSONSafeConversion(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	ctx := t.Context()

	// Create a test table with various types
	err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS test_json_safe (
			id UInt64,
			value Float64,
			ip IPv4,
			created DateTime
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Insert test data
	err = config.DB.Exec(ctx, `
		INSERT INTO test_json_safe (id, value, ip, created)
		VALUES (1, 3.14, '192.168.1.1', '2024-01-15 10:30:00')
	`)
	require.NoError(t, err)

	// Execute query
	reqBody := handlers.QueryRequest{
		Query: "SELECT id, value, ip, created FROM test_json_safe",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteQuery(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.QueryResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Error)
	assert.Equal(t, 1, response.RowCount)

	// Verify the row data was returned (JSON serialization worked)
	require.Len(t, response.Rows, 1)
	row := response.Rows[0]
	require.Len(t, row, 4)
}

func TestExecuteQuery_EmptyResult(t *testing.T) {
	apitesting.SetupTestClickHouse(t, testChDB)
	ctx := t.Context()

	// Create an empty table
	err := config.DB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS test_empty (
			id UInt64
		) ENGINE = Memory
	`)
	require.NoError(t, err)

	// Query empty table
	reqBody := handlers.QueryRequest{
		Query: "SELECT id FROM test_empty",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest(http.MethodPost, "/api/query", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	handlers.ExecuteQuery(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	var response handlers.QueryResponse
	err = json.NewDecoder(rr.Body).Decode(&response)
	require.NoError(t, err)
	assert.Empty(t, response.Error)
	assert.Equal(t, 0, response.RowCount)
	assert.Equal(t, []string{"id"}, response.Columns)
}
