package dataset

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLake_Clickhouse_Dataset_Fact_GetRows(t *testing.T) {
	t.Parallel()
	log := testLogger()
	conn := testConn(t)
	ctx := t.Context()

	ds, err := NewFactDataset(log, &testFactSchema{})
	require.NoError(t, err)
	require.NotNil(t, ds, "fact dataset should be created")

	// Create test table
	createTestFactTable(t, conn, ds.TableName(), ds.schema.Columns())

	// Insert test data
	ingestedAt := time.Now().UTC()
	clearTable(t, conn, ds.TableName())

	err = ds.WriteBatch(ctx, conn, 10, func(i int) ([]any, error) {
		return []any{
			time.Date(2024, 1, 1, 10, i, 0, 0, time.UTC), // event_ts
			ingestedAt,                  // ingested_at
			i * 10,                      // value
			fmt.Sprintf("label%d", i+1), // label
		}, nil
	})
	require.NoError(t, err)
	// Test 1: Get all rows
	t.Run("get_all_rows", func(t *testing.T) {
		rows, err := ds.GetRows(ctx, conn, GetRowsOptions{})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(rows), 10, "should have at least 10 rows")
	})

	// Test 2: Get rows with time range
	t.Run("get_rows_with_time_range", func(t *testing.T) {
		startTime := time.Date(2024, 1, 1, 10, 2, 0, 0, time.UTC)
		endTime := time.Date(2024, 1, 1, 10, 5, 0, 0, time.UTC)

		rows, err := ds.GetRows(ctx, conn, GetRowsOptions{
			StartTime: &startTime,
			EndTime:   &endTime,
		})
		require.NoError(t, err)
		require.Equal(t, 4, len(rows), "should have 4 rows (minutes 2, 3, 4, 5)")

		// Verify all rows are within time range
		for _, row := range rows {
			eventTS, ok := row["event_ts"].(time.Time)
			require.True(t, ok, "event_ts should be time.Time")
			require.True(t, eventTS.After(startTime) || eventTS.Equal(startTime), "event_ts should be >= startTime")
			require.True(t, eventTS.Before(endTime) || eventTS.Equal(endTime), "event_ts should be <= endTime")
		}
	})

	// Test 3: Get rows with WHERE clause
	t.Run("get_rows_with_where_clause", func(t *testing.T) {
		rows, err := ds.GetRows(ctx, conn, GetRowsOptions{
			WhereClause: "label = ?",
			WhereArgs:   []any{"label5"},
		})
		require.NoError(t, err)
		require.Equal(t, 1, len(rows), "should have 1 row with label5")

		if len(rows) > 0 {
			// Nullable columns return concrete value or nil (not **type)
			label, ok := rows[0]["label"].(string)
			require.True(t, ok, "label should be string (or nil) for nullable column")
			require.Equal(t, "label5", label)
		}
	})

	// Test 4: Get rows with limit
	t.Run("get_rows_with_limit", func(t *testing.T) {
		rows, err := ds.GetRows(ctx, conn, GetRowsOptions{
			Limit: 3,
		})
		require.NoError(t, err)
		require.Equal(t, 3, len(rows), "should have exactly 3 rows")
	})

	// Test 5: Get rows with custom ORDER BY
	t.Run("get_rows_with_order_by", func(t *testing.T) {
		rows, err := ds.GetRows(ctx, conn, GetRowsOptions{
			OrderBy: "value ASC",
			Limit:   5,
		})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(rows), 5, "should have at least 5 rows")

		// Verify ordering (values should be ascending)
		// Note: We may have more than 5 rows due to previous test data, so we check the first 5
		for i := 0; i < len(rows) && i < 5; i++ {
			row := rows[i]
			// Nullable columns return concrete value or nil (not **type)
			value, ok := row["value"].(int32)
			require.True(t, ok, "value should be int32 for nullable column (or nil)")

			// Check that values are in ascending order
			if i > 0 {
				prevValue, ok := rows[i-1]["value"].(int32)
				require.True(t, ok)
				require.GreaterOrEqual(t, value, prevValue, "values should be in ascending order")
			}
		}
	})

	// Test 6: Get rows with time range and WHERE clause
	t.Run("get_rows_with_time_range_and_where", func(t *testing.T) {
		startTime := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		endTime := time.Date(2024, 1, 1, 10, 9, 0, 0, time.UTC)

		rows, err := ds.GetRows(ctx, conn, GetRowsOptions{
			StartTime:   &startTime,
			EndTime:     &endTime,
			WhereClause: "value >= ?",
			WhereArgs:   []any{30},
		})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(rows), 7, "should have at least 7 rows (value >= 30)")

		// Verify all rows meet criteria
		for _, row := range rows {
			eventTS, ok := row["event_ts"].(time.Time)
			require.True(t, ok)
			require.True(t, eventTS.After(startTime) || eventTS.Equal(startTime))
			require.True(t, eventTS.Before(endTime) || eventTS.Equal(endTime))

			// Nullable columns return concrete value or nil (not **type)
			value, ok := row["value"].(int32)
			require.True(t, ok, "value should be int32 (or nil)")
			require.GreaterOrEqual(t, value, int32(30))
		}
	})

	// Test 7: Get rows with only start time
	t.Run("get_rows_with_start_time_only", func(t *testing.T) {
		startTime := time.Date(2024, 1, 1, 10, 5, 0, 0, time.UTC)

		rows, err := ds.GetRows(ctx, conn, GetRowsOptions{
			StartTime: &startTime,
		})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(rows), 5, "should have at least 5 rows (minutes 5-9)")

		// Verify all rows are after start time
		for _, row := range rows {
			eventTS, ok := row["event_ts"].(time.Time)
			require.True(t, ok)
			require.True(t, eventTS.After(startTime) || eventTS.Equal(startTime))
		}
	})

	// Test 8: Get rows with only end time
	t.Run("get_rows_with_end_time_only", func(t *testing.T) {
		endTime := time.Date(2024, 1, 1, 10, 4, 0, 0, time.UTC)

		rows, err := ds.GetRows(ctx, conn, GetRowsOptions{
			EndTime: &endTime,
		})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(rows), 5, "should have at least 5 rows (minutes 0-4)")

		// Verify all rows are before end time
		for _, row := range rows {
			eventTS, ok := row["event_ts"].(time.Time)
			require.True(t, ok)
			require.True(t, eventTS.Before(endTime) || eventTS.Equal(endTime))
		}
	})

	// Test 9: Get rows with no time column configured
	t.Run("get_rows_no_time_column", func(t *testing.T) {
		dsNoTime, err := NewFactDataset(log, &testFactSchemaNoTime{})
		require.NoError(t, err)
		require.NotNil(t, dsNoTime)
		require.Equal(t, "fact_test_events_no_time", dsNoTime.TableName())
		require.Equal(t, []string{"ingested_at:TIMESTAMP", "value:INTEGER", "label:VARCHAR"}, dsNoTime.schema.Columns())
	})

	// Test 10: Empty result set
	t.Run("get_rows_empty_result", func(t *testing.T) {
		startTime := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
		endTime := time.Date(2025, 1, 1, 10, 1, 0, 0, time.UTC)

		rows, err := ds.GetRows(ctx, conn, GetRowsOptions{
			StartTime: &startTime,
			EndTime:   &endTime,
		})
		require.NoError(t, err)
		require.Equal(t, 0, len(rows), "should return empty result set")
	})
}

func TestLake_Clickhouse_Dataset_Fact_Query(t *testing.T) {
	t.Parallel()
	log := testLogger()
	conn := testConn(t)
	ctx := t.Context()

	ds, err := NewFactDataset(log, &testFactSchema{})
	require.NoError(t, err)
	require.NotNil(t, ds, "fact dataset should be created")

	// Create test table
	createTestFactTable(t, conn, ds.TableName(), ds.schema.Columns())

	// Insert test data
	ingestedAt := time.Now().UTC()
	clearTable(t, conn, ds.TableName())

	err = ds.WriteBatch(ctx, conn, 5, func(i int) ([]any, error) {
		return []any{
			time.Date(2024, 1, 1, 10, i, 0, 0, time.UTC), // event_ts
			ingestedAt,                  // ingested_at
			i * 10,                      // value
			fmt.Sprintf("label%d", i+1), // label
		}, nil
	})
	require.NoError(t, err)
	// Test 1: Simple SELECT query
	t.Run("simple_select", func(t *testing.T) {
		rows, err := ds.Query(ctx, conn, "SELECT * FROM fact_test_events WHERE value >= ? ORDER BY value ASC", []any{20})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(rows), 3, "should have at least 3 rows with value >= 20")
	})

	// Test 2: Query with COUNT
	t.Run("count_query", func(t *testing.T) {
		rows, err := ds.Query(ctx, conn, "SELECT COUNT(*) as cnt FROM fact_test_events WHERE value > ?", []any{30})
		require.NoError(t, err)
		require.Equal(t, 1, len(rows), "should have 1 row")
		if len(rows) > 0 {
			cnt, ok := rows[0]["cnt"].(uint64)
			require.True(t, ok, "cnt should be uint64")
			require.GreaterOrEqual(t, cnt, uint64(1), "should have at least 1 row with value > 30")
		}
	})

	// Test 3: Query with aggregation
	t.Run("aggregation_query", func(t *testing.T) {
		rows, err := ds.Query(ctx, conn, "SELECT label, SUM(value) as total FROM fact_test_events GROUP BY label ORDER BY label", []any{})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(rows), 1, "should have at least 1 row")
	})

	// Test 4: Query with no results
	t.Run("no_results", func(t *testing.T) {
		rows, err := ds.Query(ctx, conn, "SELECT * FROM fact_test_events WHERE value > ?", []any{1000})
		require.NoError(t, err)
		require.Equal(t, 0, len(rows), "should return empty result set")
	})

	// Test 5: Query with time filter
	t.Run("time_filter", func(t *testing.T) {
		startTime := time.Date(2024, 1, 1, 10, 2, 0, 0, time.UTC)
		rows, err := ds.Query(ctx, conn, "SELECT * FROM fact_test_events WHERE event_ts >= ? ORDER BY event_ts", []any{startTime})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(rows), 3, "should have at least 3 rows")
	})
}

type testFactSchema struct{}

func (s *testFactSchema) Name() string {
	return "test_events"
}

func (s *testFactSchema) UniqueKeyColumns() []string {
	return []string{}
}

func (s *testFactSchema) Columns() []string {
	return []string{"event_ts:TIMESTAMP", "ingested_at:TIMESTAMP", "value:INTEGER", "label:VARCHAR"}
}

func (s *testFactSchema) TimeColumn() string {
	return "event_ts"
}

func (s *testFactSchema) PartitionByTime() bool {
	return true
}

func (s *testFactSchema) Grain() string {
	return "one event per time unit"
}

func (s *testFactSchema) DedupMode() DedupMode {
	return DedupNone
}

func (s *testFactSchema) DedupVersionColumn() string {
	return "ingested_at"
}

type testFactSchemaNoTime struct{}

func (s *testFactSchemaNoTime) Name() string {
	return "test_events_no_time"
}

func (s *testFactSchemaNoTime) UniqueKeyColumns() []string {
	return []string{}
}

func (s *testFactSchemaNoTime) Columns() []string {
	return []string{"ingested_at:TIMESTAMP", "value:INTEGER", "label:VARCHAR"}
}

func (s *testFactSchemaNoTime) TimeColumn() string {
	return ""
}

func (s *testFactSchemaNoTime) PartitionByTime() bool {
	return false
}

func (s *testFactSchemaNoTime) Grain() string {
	return "one event per ingested_at"
}

func (s *testFactSchemaNoTime) DedupMode() DedupMode {
	return DedupNone
}

func (s *testFactSchemaNoTime) DedupVersionColumn() string {
	return "ingested_at"
}
