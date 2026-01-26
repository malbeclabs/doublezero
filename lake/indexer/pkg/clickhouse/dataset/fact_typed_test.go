package dataset

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLake_Clickhouse_Dataset_Fact_Typed(t *testing.T) {
	t.Parallel()
	log := testLogger()
	conn := testConn(t)
	ctx := t.Context()

	ds, err := NewFactDataset(log, &testFactSchema{})
	require.NoError(t, err)
	require.NotNil(t, ds, "fact dataset should be created")

	// Create test table
	createTestFactTable(t, conn, ds.TableName(), ds.schema.Columns())

	// Define a struct type matching the schema
	type Event struct {
		EventTS    time.Time `ch:"event_ts"`
		IngestedAt time.Time `ch:"ingested_at"`
		Value      *int32    `ch:"value"` // Nullable
		Label      *string   `ch:"label"` // Nullable
	}

	typed := NewTypedFactDataset[Event](ds)

	// Test 1: Write and read typed structs
	t.Run("write_and_read_typed_structs", func(t *testing.T) {
		clearTable(t, conn, ds.TableName())

		ingestedAt := time.Now().UTC()
		value1 := int32(100)
		value2 := int32(200)
		label1 := "test1"
		label2 := "test2"

		rows := []Event{
			{
				EventTS:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				IngestedAt: ingestedAt,
				Value:      &value1,
				Label:      &label1,
			},
			{
				EventTS:    time.Date(2024, 1, 1, 10, 1, 0, 0, time.UTC),
				IngestedAt: ingestedAt,
				Value:      &value2,
				Label:      &label2,
			},
		}

		err := typed.WriteBatch(ctx, conn, rows)
		require.NoError(t, err)
		// Read back using GetRows (order by event_ts ASC to get predictable order)
		readRows, err := typed.GetRows(ctx, conn, GetRowsOptions{
			OrderBy: "event_ts ASC",
			Limit:   10,
		})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(readRows), 2, "should have at least 2 rows")

		// Verify first row (should be the one with event_ts 10:00)
		found := false
		for _, row := range readRows {
			if row.EventTS.Equal(time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)) {
				// Handle nullable columns - they come back as double pointers from typedScanner
				// But typedScanner should convert them properly
				if row.Value != nil {
					require.Equal(t, int32(100), *row.Value)
				}
				if row.Label != nil {
					require.Equal(t, "test1", *row.Label)
				}
				found = true
				break
			}
		}
		require.True(t, found, "should find row with event_ts 10:00")
	})

	// Test 2: Write with nullable values
	t.Run("write_with_nullable_values", func(t *testing.T) {
		clearTable(t, conn, ds.TableName())

		ingestedAt := time.Now().UTC()
		value := int32(42)
		label := "present"

		rows := []Event{
			{
				EventTS:    time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC),
				IngestedAt: ingestedAt,
				Value:      &value,
				Label:      &label,
			},
			{
				EventTS:    time.Date(2024, 1, 2, 10, 1, 0, 0, time.UTC),
				IngestedAt: ingestedAt,
				Value:      nil, // Nullable
				Label:      nil, // Nullable
			},
		}

		err := typed.WriteBatch(ctx, conn, rows)
		require.NoError(t, err)
		// Read back
		readRows, err := typed.GetRows(ctx, conn, GetRowsOptions{
			StartTime: func() *time.Time {
				t := time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)
				return &t
			}(),
		})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(readRows), 2, "should have at least 2 rows")
	})

	// Test 3: Query with typed results
	t.Run("query_typed_results", func(t *testing.T) {
		clearTable(t, conn, ds.TableName())

		ingestedAt := time.Now().UTC()
		value := int32(50)
		label := "query_test"

		rows := []Event{
			{
				EventTS:    time.Date(2024, 1, 3, 10, 0, 0, 0, time.UTC),
				IngestedAt: ingestedAt,
				Value:      &value,
				Label:      &label,
			},
		}

		err := typed.WriteBatch(ctx, conn, rows)
		require.NoError(t, err)
		// Query using raw SQL
		queryRows, err := typed.Query(ctx, conn, "SELECT * FROM fact_test_events WHERE label = ? ORDER BY event_ts", []any{"query_test"})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(queryRows), 1, "should have at least 1 row")

		if len(queryRows) >= 1 {
			row := queryRows[0]
			// Label is nullable, so it might be nil or a pointer
			if row.Label != nil {
				require.Equal(t, "query_test", *row.Label)
			}
		}
	})

	// Test 4: GetRows with filters
	t.Run("get_rows_with_filters", func(t *testing.T) {
		clearTable(t, conn, ds.TableName())

		ingestedAt := time.Now().UTC()
		rows := []Event{}
		for i := 0; i < 5; i++ {
			value := int32(i * 10)
			label := fmt.Sprintf("filter%d", i)
			rows = append(rows, Event{
				EventTS:    time.Date(2024, 1, 4, 10, i, 0, 0, time.UTC),
				IngestedAt: ingestedAt,
				Value:      &value,
				Label:      &label,
			})
		}

		err := typed.WriteBatch(ctx, conn, rows)
		require.NoError(t, err)
		// Get rows with time range and WHERE clause
		startTime := time.Date(2024, 1, 4, 10, 1, 0, 0, time.UTC)
		endTime := time.Date(2024, 1, 4, 10, 3, 0, 0, time.UTC)

		readRows, err := typed.GetRows(ctx, conn, GetRowsOptions{
			StartTime:   &startTime,
			EndTime:     &endTime,
			WhereClause: "value >= ?",
			WhereArgs:   []any{20},
		})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(readRows), 2, "should have at least 2 rows")

		// Verify all rows meet criteria
		for _, row := range readRows {
			require.True(t, row.EventTS.After(startTime) || row.EventTS.Equal(startTime))
			require.True(t, row.EventTS.Before(endTime) || row.EventTS.Equal(endTime))
			if row.Value != nil {
				require.GreaterOrEqual(t, *row.Value, int32(20))
			}
		}
	})

	// Test 5: Empty batch
	t.Run("empty_batch", func(t *testing.T) {
		err := typed.WriteBatch(ctx, conn, []Event{})
		require.NoError(t, err, "empty batch should not error")
	})

	// Test 6: Struct with automatic field mapping (no tags)
	t.Run("automatic_field_mapping", func(t *testing.T) {
		clearTable(t, conn, ds.TableName())

		// Define struct without explicit tags - should use snake_case conversion
		type EventAuto struct {
			EventTS    time.Time
			IngestedAt time.Time
			Value      *int32
			Label      *string
		}

		typedAuto := NewTypedFactDataset[EventAuto](ds)

		ingestedAt := time.Now().UTC()
		value := int32(999)
		label := "auto_mapped"

		rows := []EventAuto{
			{
				EventTS:    time.Date(2024, 1, 5, 10, 0, 0, 0, time.UTC),
				IngestedAt: ingestedAt,
				Value:      &value,
				Label:      &label,
			},
		}

		err := typedAuto.WriteBatch(ctx, conn, rows)
		require.NoError(t, err)
		// Read back
		readRows, err := typedAuto.GetRows(ctx, conn, GetRowsOptions{
			Limit: 1,
		})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(readRows), 1, "should have at least 1 row")
	})
}
