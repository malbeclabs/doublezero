package dataset

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLake_Clickhouse_Dataset_Query(t *testing.T) {
	t.Parallel()
	conn := testConn(t)
	ctx := t.Context()

	// Create a simple test table directly for query testing
	tableName := "fact_test_query_events"
	err := conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			event_ts DateTime,
			ingested_at DateTime,
			value Int32,
			label Nullable(String)
		) ENGINE = MergeTree()
		ORDER BY event_ts
	`, tableName))
	require.NoError(t, err)
	defer func() {
		_ = conn.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName))
	}()

	// Insert test data
	ingestedAt := time.Now().UTC()
	for i := 0; i < 5; i++ {
		err = conn.Exec(ctx, fmt.Sprintf(`
			INSERT INTO %s (event_ts, ingested_at, value, label) VALUES (?, ?, ?, ?)
		`, tableName),
			time.Date(2024, 1, 1, 10, i, 0, 0, time.UTC), // event_ts
			ingestedAt,                  // ingested_at
			i*10,                        // value
			fmt.Sprintf("label%d", i+1), // label
		)
		require.NoError(t, err)
	}

	// Test 1: Simple SELECT query
	t.Run("simple_select", func(t *testing.T) {
		result, err := Query(ctx, conn, fmt.Sprintf("SELECT * FROM %s WHERE value >= ? ORDER BY value ASC", tableName), []any{20})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.GreaterOrEqual(t, result.Count, 3, "should have at least 3 rows with value >= 20")
		require.Equal(t, result.Count, len(result.Rows))
		require.Equal(t, 4, len(result.Columns), "should have 4 columns")
		require.Equal(t, 4, len(result.ColumnTypes), "should have 4 column types")

		// Verify column names
		expectedColumns := []string{"event_ts", "ingested_at", "value", "label"}
		for _, expectedCol := range expectedColumns {
			require.Contains(t, result.Columns, expectedCol, "should contain column %s", expectedCol)
		}

		// Verify column metadata
		for i, colType := range result.ColumnTypes {
			require.NotEmpty(t, colType.Name, "column name should not be empty")
			require.NotEmpty(t, colType.DatabaseTypeName, "database type name should not be empty")
			require.Equal(t, result.Columns[i], colType.Name, "column name should match")
		}

		// Verify row data
		if len(result.Rows) > 0 {
			row := result.Rows[0]
			require.Contains(t, row, "event_ts", "row should contain event_ts")
			require.Contains(t, row, "value", "row should contain value")
			value, ok := row["value"].(int32)
			require.True(t, ok, "value should be int32")
			require.GreaterOrEqual(t, value, int32(20), "value should be >= 20")
		}
	})

	// Test 2: Query with COUNT
	t.Run("count_query", func(t *testing.T) {
		result, err := Query(ctx, conn, fmt.Sprintf("SELECT COUNT(*) as cnt FROM %s WHERE value > ?", tableName), []any{30})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, 1, result.Count, "should have 1 row")
		require.Equal(t, 1, len(result.Rows), "should have 1 row")
		require.Equal(t, 1, len(result.Columns), "should have 1 column")
		require.Equal(t, "cnt", result.Columns[0], "column should be named cnt")

		if len(result.Rows) > 0 {
			cnt, ok := result.Rows[0]["cnt"].(uint64)
			require.True(t, ok, "cnt should be uint64")
			require.GreaterOrEqual(t, cnt, uint64(1), "should have at least 1 row with value > 30")
		}
	})

	// Test 3: Query with aggregation
	t.Run("aggregation_query", func(t *testing.T) {
		result, err := Query(ctx, conn, fmt.Sprintf("SELECT label, SUM(value) as total FROM %s GROUP BY label ORDER BY label", tableName), []any{})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.GreaterOrEqual(t, result.Count, 1, "should have at least 1 row")
		require.Equal(t, 2, len(result.Columns), "should have 2 columns")
		require.Contains(t, result.Columns, "label")
		require.Contains(t, result.Columns, "total")
	})

	// Test 4: Query with no results
	t.Run("no_results", func(t *testing.T) {
		result, err := Query(ctx, conn, fmt.Sprintf("SELECT * FROM %s WHERE value > ?", tableName), []any{1000})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, 0, result.Count, "should return empty result set")
		require.Equal(t, 0, len(result.Rows), "should have no rows")
		require.Equal(t, 4, len(result.Columns), "should still have column metadata")
	})

	// Test 5: Query with time filter
	t.Run("time_filter", func(t *testing.T) {
		startTime := time.Date(2024, 1, 1, 10, 2, 0, 0, time.UTC)
		result, err := Query(ctx, conn, fmt.Sprintf("SELECT * FROM %s WHERE event_ts >= ? ORDER BY event_ts", tableName), []any{startTime})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.GreaterOrEqual(t, result.Count, 3, "should have at least 3 rows")

		// Verify time values
		if len(result.Rows) > 0 {
			eventTS, ok := result.Rows[0]["event_ts"].(time.Time)
			require.True(t, ok, "event_ts should be time.Time")
			require.True(t, eventTS.After(startTime) || eventTS.Equal(startTime), "event_ts should be >= startTime")
		}
	})

	// Test 6: Query with parameters
	t.Run("query_with_params", func(t *testing.T) {
		result, err := Query(ctx, conn, fmt.Sprintf("SELECT * FROM %s WHERE value = ? AND label = ?", tableName), []any{20, "label3"})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, 1, result.Count, "should have 1 row")
		if len(result.Rows) > 0 {
			label, ok := result.Rows[0]["label"].(string)
			require.True(t, ok, "label should be string")
			require.Equal(t, "label3", label)
		}
	})

	// Test 7: Query with NULL values (nullable columns)
	t.Run("nullable_columns", func(t *testing.T) {
		// Insert a row with NULL label
		err = conn.Exec(ctx, fmt.Sprintf("INSERT INTO %s (event_ts, ingested_at, value, label) VALUES (?, ?, ?, NULL)", tableName),
			time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC),
			ingestedAt,
			100,
		)
		require.NoError(t, err)

		result, err := Query(ctx, conn, fmt.Sprintf("SELECT * FROM %s WHERE value = ?", tableName), []any{100})
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, 1, result.Count, "should have 1 row")
		if len(result.Rows) > 0 {
			label := result.Rows[0]["label"]
			require.Nil(t, label, "label should be nil for NULL value")
		}
	})

	// Test 8: Invalid query (error case)
	t.Run("invalid_query", func(t *testing.T) {
		result, err := Query(ctx, conn, "SELECT * FROM nonexistent_table", nil)
		require.Error(t, err, "should return error for invalid query")
		require.Nil(t, result, "result should be nil on error")
		require.Contains(t, err.Error(), "failed to execute query", "error message should indicate query failure")
	})

	// Test 9: Verify column metadata details
	t.Run("column_metadata", func(t *testing.T) {
		result, err := Query(ctx, conn, fmt.Sprintf("SELECT event_ts, value, label FROM %s LIMIT 1", tableName), nil)
		require.NoError(t, err)
		require.NotNil(t, result)

		// Find column metadata for each column
		colMetadataMap := make(map[string]ColumnMetadata)
		for i, colName := range result.Columns {
			colMetadataMap[colName] = result.ColumnTypes[i]
		}

		// Verify event_ts metadata
		eventTSMeta, ok := colMetadataMap["event_ts"]
		require.True(t, ok, "should have metadata for event_ts")
		require.Equal(t, "event_ts", eventTSMeta.Name)
		require.Contains(t, eventTSMeta.DatabaseTypeName, "DateTime", "should be DateTime type")

		// Verify value metadata
		valueMeta, ok := colMetadataMap["value"]
		require.True(t, ok, "should have metadata for value")
		require.Equal(t, "value", valueMeta.Name)
		require.Contains(t, valueMeta.DatabaseTypeName, "Int", "should be Int type")
	})
}

func TestLake_Clickhouse_Dataset_ScanQueryResults(t *testing.T) {
	t.Parallel()
	conn := testConn(t)
	ctx := t.Context()

	// Create a simple test table
	tableName := "test_scan_results"
	err := conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id UInt64,
			name String,
			value Nullable(Int64),
			created_at DateTime
		) ENGINE = MergeTree()
		ORDER BY id
	`, tableName))
	require.NoError(t, err)
	defer func() {
		_ = conn.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName))
	}()

	// Insert test data
	err = conn.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, name, value, created_at) VALUES
		(1, 'test1', 100, '2024-01-01 10:00:00'),
		(2, 'test2', NULL, '2024-01-01 11:00:00'),
		(3, 'test3', 300, '2024-01-01 12:00:00')
	`, tableName))
	require.NoError(t, err)

	// Test ScanQueryResults
	t.Run("scan_results", func(t *testing.T) {
		rows, err := conn.Query(ctx, fmt.Sprintf("SELECT * FROM %s ORDER BY id", tableName), nil)
		require.NoError(t, err)
		defer rows.Close()

		columns, colMetadata, resultRows, err := ScanQueryResults(rows)
		require.NoError(t, err)

		require.Equal(t, 4, len(columns), "should have 4 columns")
		require.Equal(t, 4, len(colMetadata), "should have 4 column metadata entries")
		require.Equal(t, 3, len(resultRows), "should have 3 rows")

		// Verify columns
		expectedColumns := []string{"id", "name", "value", "created_at"}
		for _, expectedCol := range expectedColumns {
			require.Contains(t, columns, expectedCol, "should contain column %s", expectedCol)
		}

		// Verify first row
		row1 := resultRows[0]
		require.Equal(t, uint64(1), row1["id"], "id should be 1")
		require.Equal(t, "test1", row1["name"], "name should be test1")
		require.Equal(t, int64(100), row1["value"], "value should be 100")

		// Verify second row with NULL value
		row2 := resultRows[1]
		require.Equal(t, uint64(2), row2["id"], "id should be 2")
		require.Equal(t, "test2", row2["name"], "name should be test2")
		require.Nil(t, row2["value"], "value should be nil for NULL")

		// Verify column metadata
		for i, colType := range colMetadata {
			require.Equal(t, columns[i], colType.Name, "column name should match")
			require.NotEmpty(t, colType.DatabaseTypeName, "database type should not be empty")
		}
	})

	// Test ScanQueryResults with empty result set
	t.Run("empty_results", func(t *testing.T) {
		rows, err := conn.Query(ctx, fmt.Sprintf("SELECT * FROM %s WHERE id > 1000", tableName), nil)
		require.NoError(t, err)
		defer rows.Close()

		columns, colMetadata, resultRows, err := ScanQueryResults(rows)
		require.NoError(t, err)

		require.Equal(t, 4, len(columns), "should have 4 columns")
		require.Equal(t, 4, len(colMetadata), "should have 4 column metadata entries")
		require.Equal(t, 0, len(resultRows), "should have 0 rows")
	})
}
