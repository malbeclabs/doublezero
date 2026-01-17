package dataset

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestLake_Clickhouse_Dataset_DimensionType2_GetCurrentRow(t *testing.T) {
	t.Parallel()
	log := testLogger()
	conn := testConn(t)
	createSinglePKTables(t, conn)

	ctx := t.Context()

	// Test 1: Get existing entity
	t.Run("get_existing_entity", func(t *testing.T) {
		d, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{"entity1", "CODE1", "Name1"}, nil
		}, nil)
		require.NoError(t, err)
		entityID := NewNaturalKey("entity1").ToSurrogate()
		current, err := d.GetCurrentRow(ctx, conn, entityID)
		require.NoError(t, err)
		require.NotNil(t, current)

		// Verify all expected fields are present
		require.Equal(t, "CODE1", current["code"])
		require.Equal(t, "Name1", current["name"])
		require.Equal(t, uint8(0), current["is_deleted"])
		require.NotEmpty(t, current["entity_id"])
		require.NotZero(t, current["snapshot_ts"])
		require.NotZero(t, current["ingested_at"])
		require.NotZero(t, current["op_id"])
	})

	// Test 2: Get non-existent entity
	t.Run("get_nonexistent_entity", func(t *testing.T) {
		d, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		entityID := NewNaturalKey("nonexistent").ToSurrogate()
		current, err := d.GetCurrentRow(ctx, conn, entityID)
		require.NoError(t, err)
		require.Nil(t, current, "should return nil for non-existent entity")
	})

	// Test 3: Get deleted entity (tombstone)
	t.Run("get_deleted_entity", func(t *testing.T) {
		t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)

		// First insert an entity
		d1, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d1.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{"entity2", "CODE2", "Name2"}, nil
		}, &DimensionType2DatasetWriteConfig{
			SnapshotTS: t1,
		})
		require.NoError(t, err)
		entityID := NewNaturalKey("entity2").ToSurrogate()
		current1, err := d1.GetCurrentRow(ctx, conn, entityID)
		require.NoError(t, err)
		require.NotNil(t, current1)

		// Delete the entity with empty snapshot
		d2, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d2.WriteBatch(ctx, conn, 0, nil, &DimensionType2DatasetWriteConfig{
			SnapshotTS:          t2,
			MissingMeansDeleted: true,
		})
		require.NoError(t, err)
		// GetCurrentRow should return nil for deleted entity
		current2, err := d2.GetCurrentRow(ctx, conn, entityID)
		require.NoError(t, err)
		require.Nil(t, current2, "should return nil for deleted entity")
	})

	// Test 4: Get latest version when multiple versions exist
	t.Run("get_latest_version", func(t *testing.T) {
		t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)
		t3 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

		d1, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d1.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{"entity3", "CODE3_V1", "Name3_V1"}, nil
		}, &DimensionType2DatasetWriteConfig{
			SnapshotTS: t1,
		})
		require.NoError(t, err)
		// Update with new snapshot_ts
		d2, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d2.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{"entity3", "CODE3_V2", "Name3_V2"}, nil
		}, &DimensionType2DatasetWriteConfig{
			SnapshotTS: t2,
		})
		require.NoError(t, err)
		// Update again
		d3, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d3.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{"entity3", "CODE3_V3", "Name3_V3"}, nil
		}, &DimensionType2DatasetWriteConfig{
			SnapshotTS: t3,
		})
		require.NoError(t, err)
		// GetCurrentRow should return the latest version (V3)
		entityID := NewNaturalKey("entity3").ToSurrogate()
		current, err := d3.GetCurrentRow(ctx, conn, entityID)
		require.NoError(t, err)
		require.NotNil(t, current)
		require.Equal(t, "CODE3_V3", current["code"])
		require.Equal(t, "Name3_V3", current["name"])

		// Verify snapshot_ts is the latest
		snapshotTS, ok := current["snapshot_ts"].(time.Time)
		require.True(t, ok)
		require.Equal(t, t3, snapshotTS)
	})

	// Test 5: Get entity with same snapshot_ts but different ingested_at (should get latest)
	t.Run("get_latest_by_ingested_at", func(t *testing.T) {
		t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

		d1, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d1.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{"entity4", "CODE4_V1", "Name4_V1"}, nil
		}, &DimensionType2DatasetWriteConfig{
			SnapshotTS: t1,
		})
		require.NoError(t, err)
		// Wait a bit to ensure different ingested_at
		time.Sleep(10 * time.Millisecond)

		// Write again with same snapshot_ts but different op_id (will have different ingested_at)
		d2, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d2.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{"entity4", "CODE4_V2", "Name4_V2"}, nil
		}, &DimensionType2DatasetWriteConfig{
			SnapshotTS: t1,
		})
		require.NoError(t, err)
		// GetCurrentRow should return the latest version (by ingested_at)
		entityID := NewNaturalKey("entity4").ToSurrogate()
		current, err := d2.GetCurrentRow(ctx, conn, entityID)
		require.NoError(t, err)
		require.NotNil(t, current)
		// Should get V2 (latest by ingested_at)
		require.Equal(t, "CODE4_V2", current["code"])
		require.Equal(t, "Name4_V2", current["name"])
	})

	// Test 6: Verify all column types are correctly scanned
	t.Run("verify_column_types", func(t *testing.T) {
		d, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{"entity5", "CODE5", "Name5"}, nil
		}, nil)
		require.NoError(t, err)
		entityID := NewNaturalKey("entity5").ToSurrogate()
		current, err := d.GetCurrentRow(ctx, conn, entityID)
		require.NoError(t, err)
		require.NotNil(t, current)

		// Verify types
		require.IsType(t, "", current["entity_id"])
		require.IsType(t, time.Time{}, current["snapshot_ts"])
		require.IsType(t, time.Time{}, current["ingested_at"])
		require.IsType(t, uuid.UUID{}, current["op_id"])
		require.IsType(t, uint8(0), current["is_deleted"])
		require.IsType(t, uint64(0), current["attrs_hash"])
		require.IsType(t, "", current["pk"])
		require.IsType(t, "", current["code"])
		require.IsType(t, "", current["name"])
	})

	// Test 7: Context cancellation
	t.Run("context_cancellation", func(t *testing.T) {
		d, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{"entity6", "CODE6", "Name6"}, nil
		}, nil)
		require.NoError(t, err)
		entityID := NewNaturalKey("entity6").ToSurrogate()

		cancelledCtx, cancel := context.WithCancel(ctx)
		cancel() // Cancel immediately

		current, err := d.GetCurrentRow(cancelledCtx, conn, entityID)
		require.Error(t, err)
		require.Nil(t, current)
		require.Contains(t, err.Error(), "context")
	})
}

func TestLake_Clickhouse_Dataset_DimensionType2_GetCurrentRows(t *testing.T) {
	t.Parallel()
	log := testLogger()
	conn := testConn(t)
	createSinglePKTables(t, conn)

	ctx := t.Context()

	// Test 1: Get multiple specific entities
	t.Run("get_multiple_entities", func(t *testing.T) {
		d, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d.WriteBatch(ctx, conn, 3, func(i int) ([]any, error) {
			switch i {
			case 0:
				return []any{"entity1", "CODE1", "Name1"}, nil
			case 1:
				return []any{"entity2", "CODE2", "Name2"}, nil
			case 2:
				return []any{"entity3", "CODE3", "Name3"}, nil
			default:
				return nil, nil
			}
		}, nil)
		require.NoError(t, err)
		entityID1 := NewNaturalKey("entity1").ToSurrogate()
		entityID2 := NewNaturalKey("entity2").ToSurrogate()
		entityID3 := NewNaturalKey("entity3").ToSurrogate()

		results, err := d.GetCurrentRows(ctx, conn, []SurrogateKey{entityID1, entityID2, entityID3})
		require.NoError(t, err)
		require.Len(t, results, 3)

		// Build a map for easier lookup
		resultMap := make(map[string]map[string]any)
		for _, result := range results {
			pk := result["pk"].(string)
			resultMap[pk] = result
		}

		require.Equal(t, "CODE1", resultMap["entity1"]["code"])
		require.Equal(t, "Name1", resultMap["entity1"]["name"])
		require.Equal(t, "CODE2", resultMap["entity2"]["code"])
		require.Equal(t, "Name2", resultMap["entity2"]["name"])
		require.Equal(t, "CODE3", resultMap["entity3"]["code"])
		require.Equal(t, "Name3", resultMap["entity3"]["name"])
	})

	// Test 2: Get all current entities (empty entityIDs)
	t.Run("get_all_entities", func(t *testing.T) {
		d, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d.WriteBatch(ctx, conn, 2, func(i int) ([]any, error) {
			switch i {
			case 0:
				return []any{"entity4", "CODE4", "Name4"}, nil
			case 1:
				return []any{"entity5", "CODE5", "Name5"}, nil
			default:
				return nil, nil
			}
		}, nil)
		require.NoError(t, err)
		// Get all entities (empty slice)
		results, err := d.GetCurrentRows(ctx, conn, nil)
		require.NoError(t, err)
		// Should have at least entity4 and entity5, plus any from previous tests
		require.GreaterOrEqual(t, len(results), 2)

		// Verify entity4 and entity5 are present
		resultMap := make(map[string]map[string]any)
		for _, result := range results {
			pk := result["pk"].(string)
			resultMap[pk] = result
		}

		require.Equal(t, "CODE4", resultMap["entity4"]["code"])
		require.Equal(t, "Name4", resultMap["entity4"]["name"])
		require.Equal(t, "CODE5", resultMap["entity5"]["code"])
		require.Equal(t, "Name5", resultMap["entity5"]["name"])
	})

	// Test 3: Get non-existent entities
	t.Run("get_nonexistent_entities", func(t *testing.T) {
		d, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		entityID := NewNaturalKey("nonexistent").ToSurrogate()
		results, err := d.GetCurrentRows(ctx, conn, []SurrogateKey{entityID})
		require.NoError(t, err)
		require.Empty(t, results)
	})
}

func TestLake_Clickhouse_Dataset_DimensionType2_GetAsOfRow(t *testing.T) {
	t.Parallel()
	log := testLogger()
	conn := testConn(t)
	createSinglePKTables(t, conn)

	ctx := t.Context()

	// Test 1: Get entity as of a specific time
	t.Run("get_as_of_time", func(t *testing.T) {
		t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)

		d1, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d1.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{"entity1", "CODE1", "Name1"}, nil
		}, &DimensionType2DatasetWriteConfig{
			SnapshotTS: t1,
		})
		require.NoError(t, err)
		d2, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d2.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{"entity1", "CODE2", "Name2"}, nil
		}, &DimensionType2DatasetWriteConfig{
			SnapshotTS: t2,
		})
		require.NoError(t, err)
		entityID := NewNaturalKey("entity1").ToSurrogate()

		// As of time between t1 and t2 should return t1
		asOfTime := time.Date(2024, 1, 1, 10, 30, 0, 0, time.UTC)
		result, err := d2.GetAsOfRow(ctx, conn, entityID, asOfTime)
		require.NoError(t, err)
		require.NotNil(t, result)
		require.Equal(t, "CODE1", result["code"])
		require.Equal(t, "Name1", result["name"])

		// As of time after t2 should return t2
		asOfTime2 := time.Date(2024, 1, 1, 11, 30, 0, 0, time.UTC)
		result2, err := d2.GetAsOfRow(ctx, conn, entityID, asOfTime2)
		require.NoError(t, err)
		require.NotNil(t, result2)
		require.Equal(t, "CODE2", result2["code"])
		require.Equal(t, "Name2", result2["name"])
	})

	// Test 2: Get entity as of time before it existed
	t.Run("get_as_of_before_existence", func(t *testing.T) {
		t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

		d, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{"entity2", "CODE2", "Name2"}, nil
		}, &DimensionType2DatasetWriteConfig{
			SnapshotTS: t1,
		})
		require.NoError(t, err)
		entityID := NewNaturalKey("entity2").ToSurrogate()
		asOfTime := time.Date(2024, 1, 1, 9, 0, 0, 0, time.UTC) // Before t1

		result, err := d.GetAsOfRow(ctx, conn, entityID, asOfTime)
		require.NoError(t, err)
		require.Nil(t, result, "should return nil for entity that didn't exist at that time")
	})

	// Test 3: Get entity as of time after deletion
	t.Run("get_as_of_after_deletion", func(t *testing.T) {
		t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)

		d1, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d1.WriteBatch(ctx, conn, 1, func(i int) ([]any, error) {
			return []any{"entity3", "CODE3", "Name3"}, nil
		}, &DimensionType2DatasetWriteConfig{
			SnapshotTS: t1,
		})
		require.NoError(t, err)
		// Delete the entity
		d2, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d2.WriteBatch(ctx, conn, 0, nil, &DimensionType2DatasetWriteConfig{
			SnapshotTS:          t2,
			MissingMeansDeleted: true,
		})
		require.NoError(t, err)
		entityID := NewNaturalKey("entity3").ToSurrogate()

		// As of time after deletion should return nil (entity is deleted)
		asOfTime := time.Date(2024, 1, 1, 11, 30, 0, 0, time.UTC)
		result, err := d2.GetAsOfRow(ctx, conn, entityID, asOfTime)
		require.NoError(t, err)
		require.Nil(t, result, "should return nil for deleted entity")

		// As of time before deletion should return the entity
		asOfTime2 := time.Date(2024, 1, 1, 10, 30, 0, 0, time.UTC)
		result2, err := d2.GetAsOfRow(ctx, conn, entityID, asOfTime2)
		require.NoError(t, err)
		require.NotNil(t, result2)
		require.Equal(t, "CODE3", result2["code"])
		require.Equal(t, "Name3", result2["name"])
	})
}

func TestLake_Clickhouse_Dataset_DimensionType2_GetAsOfRows(t *testing.T) {
	t.Parallel()
	log := testLogger()
	conn := testConn(t)
	createSinglePKTables(t, conn)

	ctx := t.Context()

	// Test 1: Get multiple entities as of a specific time
	t.Run("get_multiple_entities_as_of", func(t *testing.T) {
		t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)
		t2 := time.Date(2024, 1, 1, 11, 0, 0, 0, time.UTC)

		d1, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d1.WriteBatch(ctx, conn, 3, func(i int) ([]any, error) {
			switch i {
			case 0:
				return []any{"entity1", "CODE1", "Name1"}, nil
			case 1:
				return []any{"entity2", "CODE2", "Name2"}, nil
			case 2:
				return []any{"entity3", "CODE3", "Name3"}, nil
			default:
				return nil, nil
			}
		}, &DimensionType2DatasetWriteConfig{
			SnapshotTS: t1,
		})
		require.NoError(t, err)

		// Update entity1 and entity2
		d2, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d2.WriteBatch(ctx, conn, 2, func(i int) ([]any, error) {
			switch i {
			case 0:
				return []any{"entity1", "CODE1_V2", "Name1_V2"}, nil
			case 1:
				return []any{"entity2", "CODE2_V2", "Name2_V2"}, nil
			default:
				return nil, nil
			}
		}, &DimensionType2DatasetWriteConfig{
			SnapshotTS: t2,
		})
		require.NoError(t, err)
		entityID1 := NewNaturalKey("entity1").ToSurrogate()
		entityID2 := NewNaturalKey("entity2").ToSurrogate()
		entityID3 := NewNaturalKey("entity3").ToSurrogate()

		// As of time between t1 and t2 should return t1 versions
		asOfTime := time.Date(2024, 1, 1, 10, 30, 0, 0, time.UTC)
		results, err := d2.GetAsOfRows(ctx, conn, []SurrogateKey{entityID1, entityID2, entityID3}, asOfTime)
		require.NoError(t, err)
		require.Len(t, results, 3)

		resultMap := make(map[string]map[string]any)
		for _, result := range results {
			pk := result["pk"].(string)
			resultMap[pk] = result
		}

		// Should have t1 versions
		require.Equal(t, "CODE1", resultMap["entity1"]["code"])
		require.Equal(t, "Name1", resultMap["entity1"]["name"])
		require.Equal(t, "CODE2", resultMap["entity2"]["code"])
		require.Equal(t, "Name2", resultMap["entity2"]["name"])
		require.Equal(t, "CODE3", resultMap["entity3"]["code"])
		require.Equal(t, "Name3", resultMap["entity3"]["name"])
	})

	// Test 2: Get all entities as of a specific time (empty entityIDs)
	t.Run("get_all_entities_as_of", func(t *testing.T) {
		t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

		d, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
		require.NoError(t, err)
		err = d.WriteBatch(ctx, conn, 2, func(i int) ([]any, error) {
			switch i {
			case 0:
				return []any{"entity4", "CODE4", "Name4"}, nil
			case 1:
				return []any{"entity5", "CODE5", "Name5"}, nil
			default:
				return nil, nil
			}
		}, &DimensionType2DatasetWriteConfig{
			SnapshotTS: t1,
		})
		require.NoError(t, err)
		// Get all entities as of t1
		asOfTime := time.Date(2024, 1, 1, 10, 30, 0, 0, time.UTC)
		results, err := d.GetAsOfRows(ctx, conn, nil, asOfTime)
		require.NoError(t, err)
		// Should have at least entity4 and entity5, plus any from previous tests
		require.GreaterOrEqual(t, len(results), 2)

		resultMap := make(map[string]map[string]any)
		for _, result := range results {
			pk := result["pk"].(string)
			resultMap[pk] = result
		}

		require.Equal(t, "CODE4", resultMap["entity4"]["code"])
		require.Equal(t, "Name4", resultMap["entity4"]["name"])
		require.Equal(t, "CODE5", resultMap["entity5"]["code"])
		require.Equal(t, "Name5", resultMap["entity5"]["name"])
	})
}

func testLogger() *slog.Logger {
	debugLevel := os.Getenv("DEBUG")
	var level slog.Level
	switch debugLevel {
	case "2":
		level = slog.LevelDebug
	case "1":
		level = slog.LevelInfo
	default:
		// Suppress logs by default (only show errors and above)
		level = slog.LevelError
	}
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}

func TestLake_Clickhouse_Dataset_DimensionType2_Query(t *testing.T) {
	t.Parallel()
	log := testLogger()
	conn := testConn(t)
	ctx := t.Context()

	t1 := time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC)

	createSinglePKTables(t, conn)

	d, err := NewDimensionType2Dataset(log, &testSchemaSinglePK{})
	require.NoError(t, err)
	// Insert test data
	err = d.WriteBatch(ctx, conn, 3, func(i int) ([]any, error) {
		return []any{
			fmt.Sprintf("entity%d", i+1),
			fmt.Sprintf("CODE%d", i+1),
			fmt.Sprintf("Name%d", i+1),
		}, nil
	}, &DimensionType2DatasetWriteConfig{
		SnapshotTS: t1,
	})
	require.NoError(t, err)
	// Test 1: Simple SELECT query on current state (from history)
	t.Run("query_current_state", func(t *testing.T) {
		// Query current state using deterministic latest row per entity from history
		query := fmt.Sprintf(`
			SELECT * FROM (
				SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM %s
			) ranked
			WHERE rn = 1 AND is_deleted = 0 AND code = ?
		`, d.HistoryTableName())
		rows, err := d.Query(ctx, conn, query, []any{"CODE1"})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(rows), 1, "should have at least 1 row")
		if len(rows) > 0 {
			code, ok := rows[0]["code"].(string)
			require.True(t, ok, "code should be string")
			require.Equal(t, "CODE1", code)
		}
	})

	// Test 2: Query with COUNT on current state
	t.Run("count_query", func(t *testing.T) {
		query := fmt.Sprintf(`
			SELECT COUNT(*) as cnt FROM (
				SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM %s
			) ranked
			WHERE rn = 1 AND is_deleted = 0
		`, d.HistoryTableName())
		rows, err := d.Query(ctx, conn, query, []any{})
		require.NoError(t, err)
		require.Equal(t, 1, len(rows), "should have 1 row")
		if len(rows) > 0 {
			cnt, ok := rows[0]["cnt"].(uint64)
			require.True(t, ok, "cnt should be uint64")
			require.GreaterOrEqual(t, cnt, uint64(3), "should have at least 3 non-deleted rows")
		}
	})

	// Test 3: Query with aggregation on current state
	t.Run("aggregation_query", func(t *testing.T) {
		query := fmt.Sprintf(`
			SELECT code, COUNT(*) as cnt FROM (
				SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM %s
			) ranked
			WHERE rn = 1 AND is_deleted = 0
			GROUP BY code ORDER BY code
		`, d.HistoryTableName())
		rows, err := d.Query(ctx, conn, query, []any{})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(rows), 1, "should have at least 1 row")
	})

	// Test 4: Query history table
	t.Run("query_history_table", func(t *testing.T) {
		rows, err := d.Query(ctx, conn, "SELECT * FROM dim_test_single_pk_history WHERE code = ? ORDER BY snapshot_ts DESC LIMIT 1", []any{"CODE1"})
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(rows), 1, "should have at least 1 row")
	})

	// Test 5: Query with no results
	t.Run("no_results", func(t *testing.T) {
		query := fmt.Sprintf(`
			SELECT * FROM (
				SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM %s
			) ranked
			WHERE rn = 1 AND is_deleted = 0 AND code = ?
		`, d.HistoryTableName())
		rows, err := d.Query(ctx, conn, query, []any{"NONEXISTENT"})
		require.NoError(t, err)
		require.Equal(t, 0, len(rows), "should return empty result set")
	})

	// Test 6: Query with JOIN on current state
	t.Run("join_query", func(t *testing.T) {
		query := fmt.Sprintf(`
			SELECT c1.code, c1.name, c2.code as other_code
			FROM (
				SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM %s
			) c1
			CROSS JOIN (
				SELECT *, ROW_NUMBER() OVER (PARTITION BY entity_id ORDER BY snapshot_ts DESC, ingested_at DESC, op_id DESC) AS rn
				FROM %s
			) c2
			WHERE c1.rn = 1 AND c1.is_deleted = 0 AND c2.rn = 1 AND c2.is_deleted = 0
			AND c1.code = ? AND c2.code != c1.code
			LIMIT 1
		`, d.HistoryTableName(), d.HistoryTableName())
		rows, err := d.Query(ctx, conn, query, []any{"CODE1"})
		require.NoError(t, err)
		// May or may not have results depending on data, but should not error
		require.NotNil(t, rows)
	})
}
