package dataset

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/stretchr/testify/require"
)

func TestLake_Clickhouse_Dataset_Fact_WriteBatch(t *testing.T) {
	t.Parallel()
	log := testLogger()
	conn := testConn(t)
	ctx := t.Context()

	ds, err := NewFactDataset(log, &testFactSchema{})
	require.NoError(t, err)
	require.NotNil(t, ds, "fact dataset should be created")

	// Create test table
	createTestFactTable(t, conn, ds.TableName(), ds.schema.Columns())

	// Test 1: Successful write of multiple rows
	t.Run("successful_write", func(t *testing.T) {
		// Clear table before test
		clearTable(t, conn, ds.TableName())

		ingestedAt := time.Now().UTC()

		err := ds.WriteBatch(ctx, conn, 3, func(i int) ([]any, error) {
			return []any{
				time.Date(2024, 1, 1, 10, i, 0, 0, time.UTC), // event_ts
				ingestedAt,                  // ingested_at
				i * 10,                      // value
				fmt.Sprintf("label%d", i+1), // label
			}, nil
		})
		require.NoError(t, err)
		// Verify data was written correctly
		query := fmt.Sprintf(`
			SELECT event_ts, ingested_at, value, label
			FROM %s
			WHERE event_ts >= '2024-01-01 10:00:00' AND event_ts < '2024-01-01 11:00:00'
			ORDER BY event_ts
		`, ds.TableName())

		rows, err := conn.Query(ctx, query)
		require.NoError(t, err)
		defer rows.Close()

		count := 0
		for rows.Next() {
			var eventTS time.Time
			var ingestedAtRow time.Time
			var value *int32
			var label *string

			err := rows.Scan(&eventTS, &ingestedAtRow, &value, &label)
			require.NoError(t, err)
			expectedValue := int32(count * 10)
			expectedLabel := fmt.Sprintf("label%d", count+1)

			require.Equal(t, time.Date(2024, 1, 1, 10, count, 0, 0, time.UTC), eventTS)
			require.Equal(t, ingestedAt.Truncate(time.Second), ingestedAtRow.Truncate(time.Second))
			require.NotNil(t, value, "value should not be nil")
			require.Equal(t, expectedValue, *value)
			require.NotNil(t, label, "label should not be nil")
			require.Equal(t, expectedLabel, *label)

			count++
		}

		require.Equal(t, 3, count, "should have 3 rows")
	})

	// Test 2: Empty batch (should return nil without error)
	t.Run("empty_batch", func(t *testing.T) {
		err := ds.WriteBatch(ctx, conn, 0, func(i int) ([]any, error) {
			return nil, nil
		})
		require.NoError(t, err, "empty batch should not error")
	})

	// Test 3: Single row write
	t.Run("single_row", func(t *testing.T) {
		ingestedAt := time.Now().UTC()

		err := ds.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{
				time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC), // event_ts
				ingestedAt, // ingested_at
				100,        // value
				"single",   // label
			}, nil
		})
		require.NoError(t, err)
		// Verify single row
		query := fmt.Sprintf(`
			SELECT COUNT(*) as cnt
			FROM %s
			WHERE label = 'single'
		`, ds.TableName())

		rows, err := conn.Query(ctx, query)
		require.NoError(t, err)
		defer rows.Close()

		require.True(t, rows.Next())
		var count uint64
		err = rows.Scan(&count)
		require.NoError(t, err)
		require.Equal(t, uint64(1), count)
	})

	// Test 4: Large batch
	t.Run("large_batch", func(t *testing.T) {
		ingestedAt := time.Now().UTC()
		batchSize := 100

		err := ds.WriteBatch(ctx, conn, batchSize, func(i int) ([]any, error) {
			return []any{
				time.Date(2024, 1, 3, 10, i%60, 0, 0, time.UTC), // event_ts
				ingestedAt,                // ingested_at
				i,                         // value
				fmt.Sprintf("batch%d", i), // label
			}, nil
		})
		require.NoError(t, err)
		// Verify count
		query := fmt.Sprintf(`
			SELECT COUNT(*) as cnt
			FROM %s
			WHERE label LIKE 'batch%%'
		`, ds.TableName())

		rows, err := conn.Query(ctx, query)
		require.NoError(t, err)
		defer rows.Close()

		require.True(t, rows.Next())
		var count uint64
		err = rows.Scan(&count)
		require.NoError(t, err)
		require.Equal(t, uint64(batchSize), count)
	})

	// Test 5: Error from writeRowFn
	t.Run("writeRowFn_error", func(t *testing.T) {
		expectedErr := fmt.Errorf("test error")
		err := ds.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return nil, expectedErr
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "test error")
	})

	// Test 6: Context cancellation
	t.Run("context_cancellation", func(t *testing.T) {
		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		err := ds.WriteBatch(cancelledCtx, conn, 1, func(i int) ([]any, error) {
			return []any{
				time.Now().UTC(),
				time.Now().UTC(),
				0,
				"test",
			}, nil
		})
		require.Error(t, err)
		// Accept both "cancelled" (British) and "canceled" (American) spelling
		errMsg := err.Error()
		require.True(t,
			strings.Contains(errMsg, "context cancelled") || strings.Contains(errMsg, "context canceled"),
			"error should mention context cancellation: %s", errMsg)
	})

	// Test 7: Invalid column count (too few)
	t.Run("invalid_column_count_too_few", func(t *testing.T) {
		err := ds.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			// Return fewer columns than expected
			return []any{
				time.Now().UTC(),
				time.Now().UTC(),
				// Missing value and label
			}, nil
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "expected exactly")
	})

	// Test 7b: Invalid column count (too many)
	t.Run("invalid_column_count_too_many", func(t *testing.T) {
		err := ds.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			// Return more columns than expected
			return []any{
				time.Now().UTC(),
				time.Now().UTC(),
				100,
				"label",
				"EXTRA_COLUMN", // Extra column
			}, nil
		})
		require.Error(t, err)
		require.Contains(t, err.Error(), "expected exactly")
	})

	// Test 8: Nullable values
	t.Run("nullable_values", func(t *testing.T) {
		ingestedAt := time.Now().UTC()

		err := ds.WriteBatch(ctx, conn, 2, func(i int) ([]any, error) {
			row := []any{
				time.Date(2024, 1, 4, 10, i, 0, 0, time.UTC), // event_ts
				ingestedAt, // ingested_at
			}
			if i == 0 {
				// First row: all values present
				row = append(row, 42, "present")
			} else {
				// Second row: nullable values
				row = append(row, nil, nil)
			}
			return row, nil
		})
		require.NoError(t, err)
		// Verify nullable row was written
		query := fmt.Sprintf(`
			SELECT COUNT(*) as cnt
			FROM %s
			WHERE event_ts >= '2024-01-04 10:00:00'
		`, ds.TableName())

		rows, err := conn.Query(ctx, query)
		require.NoError(t, err)
		defer rows.Close()

		require.True(t, rows.Next())
		var count uint64
		err = rows.Scan(&count)
		require.NoError(t, err)
		require.GreaterOrEqual(t, count, uint64(2))
	})
}

// createTestFactTable creates a test fact table with the given name and columns
func createTestFactTable(t *testing.T, conn clickhouse.Connection, tableName string, columnDefs []string) {
	ctx := t.Context()

	// Drop table if exists
	dropSQL := fmt.Sprintf("DROP TABLE IF EXISTS %s", tableName)
	_ = conn.Exec(ctx, dropSQL)

	// Parse column definitions and build CREATE TABLE statement
	// For simplicity, we'll create a table with common types
	// In real usage, you'd parse the column definitions more carefully
	createSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			event_ts DateTime,
			ingested_at DateTime,
			value Nullable(Int32),
			label Nullable(String)
		) ENGINE = MergeTree
		PARTITION BY toYYYYMM(event_ts)
		ORDER BY (event_ts, ingested_at)
	`, tableName)

	err := conn.Exec(ctx, createSQL)
	require.NoError(t, err, "failed to create test fact table")
}

// clearTable clears all data from a table
func clearTable(t *testing.T, conn clickhouse.Connection, tableName string) {
	ctx := t.Context()
	query := fmt.Sprintf("TRUNCATE TABLE %s", tableName)
	err := conn.Exec(ctx, query)
	require.NoError(t, err, "failed to clear table")
}
