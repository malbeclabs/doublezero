package dataset

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
)

type DimensionType2DatasetWriteConfig struct {
	OpID                uuid.UUID
	SnapshotTS          time.Time
	IngestedAt          time.Time
	MissingMeansDeleted bool
	CleanupStaging      *bool
}

func (c *DimensionType2DatasetWriteConfig) Validate() error {
	now := time.Now().UTC()

	if c.SnapshotTS.IsZero() {
		c.SnapshotTS = now
	}
	// Truncate timestamps to milliseconds to match DateTime64(3) precision
	c.SnapshotTS = c.SnapshotTS.Truncate(time.Millisecond)

	if c.IngestedAt.IsZero() {
		c.IngestedAt = now
	}
	c.IngestedAt = c.IngestedAt.Truncate(time.Millisecond)

	if c.OpID == uuid.Nil {
		c.OpID = uuid.New()
	}

	return nil
}

// WriteBatch implements the new 2-step ingestion flow:
// Step 1: Load snapshot into staging table (with attrs_hash computed)
// Step 2: Compute delta and write directly to history using INSERT INTO ... SELECT
func (d *DimensionType2Dataset) WriteBatch(
	ctx context.Context,
	conn clickhouse.Connection,
	count int,
	writeRowFn func(int) ([]any, error),
	cfg *DimensionType2DatasetWriteConfig,
) error {
	if cfg == nil {
		cfg = &DimensionType2DatasetWriteConfig{}
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("failed to validate write config: %w", err)
	}

	if count == 0 {
		// Handle empty snapshot: if MissingMeansDeleted, we need to insert tombstones
		if cfg.MissingMeansDeleted {
			return d.processEmptySnapshot(ctx, conn, cfg.OpID, cfg.SnapshotTS, cfg.IngestedAt)
		}
		return nil
	}

	// Idempotency check: if this op_id has already been processed, skip
	// This prevents duplicate writes on retries
	alreadyProcessed, err := d.checkOpIDAlreadyProcessed(ctx, conn, cfg.OpID)
	if err != nil {
		return fmt.Errorf("failed to check op_id: %w", err)
	}
	if alreadyProcessed {
		d.log.Info("op_id already processed, skipping (idempotent retry)", "dataset", d.schema.Name(), "op_id", cfg.OpID)
		return nil // Idempotent: already processed, nothing to do
	}

	// Step 1: Load snapshot into staging table with attrs_hash computed
	d.log.Debug("loading snapshot into staging", "dataset", d.schema.Name(), "count", count)
	if err := d.loadSnapshotIntoStaging(ctx, conn, count, writeRowFn, cfg.SnapshotTS, cfg.IngestedAt, cfg.OpID); err != nil {
		return fmt.Errorf("failed to load snapshot into staging: %w", err)
	}

	// Verify staging data is visible before computing delta
	// This catches issues where async insert or other factors cause data to not be immediately visible
	// Use same sync context for read to ensure consistency in replicated setups
	syncCtx := clickhouse.ContextWithSyncInsert(ctx)
	stagingCount, err := d.countStagingRows(syncCtx, conn, cfg.OpID)
	if err != nil {
		return fmt.Errorf("failed to verify staging data: %w", err)
	}
	if stagingCount == 0 {
		// Additional debugging: check total staging table count and sample op_ids
		totalCount, _ := d.countAllStagingRows(syncCtx, conn)
		sampleOpIDs, _ := d.sampleStagingOpIDs(syncCtx, conn, 5)
		return fmt.Errorf("staging data not visible after insert: expected %d rows for op_id %s, got 0 (total staging rows: %d, sample op_ids: %v)", count, cfg.OpID, totalCount, sampleOpIDs)
	}
	if stagingCount != int64(count) {
		d.log.Warn("staging row count mismatch", "dataset", d.schema.Name(), "expected", count, "actual", stagingCount, "op_id", cfg.OpID)
	}

	// Step 2: Compute delta and write directly to history using INSERT INTO ... SELECT
	d.log.Debug("computing and writing delta", "dataset", d.schema.Name(), "staging", d.StagingTableName(), "history", d.HistoryTableName(), "snapshot_ts", cfg.SnapshotTS, "ingested_at", cfg.IngestedAt, "op_id", cfg.OpID)

	// Build explicit column list from single source of truth to avoid drift
	// Order: internal columns (entity_id, snapshot_ts, ingested_at, op_id, is_deleted, attrs_hash) + PK columns + payload columns
	explicitColList := make([]string, 0)
	explicitColList = append(explicitColList, d.internalCols...)
	explicitColList = append(explicitColList, d.pkCols...)
	explicitColList = append(explicitColList, d.payloadCols...)
	colList := strings.Join(explicitColList, ", ")

	// Build staging raw select columns (exclude attrs_hash since it's recomputed in CTE)
	stagingRawCols := make([]string, 0)
	stagingRawCols = append(stagingRawCols, "entity_id", "snapshot_ts", "ingested_at", "op_id", "is_deleted")
	// Note: attrs_hash is excluded from staging_rawSelect since it's recomputed in staging CTE
	stagingRawCols = append(stagingRawCols, d.pkCols...)
	stagingRawCols = append(stagingRawCols, d.payloadCols...)
	stagingRawSelect := strings.Join(stagingRawCols, ", ")

	// Build argMax select for latest CTE
	latestArgMaxSelect := d.buildArgMaxSelect("h")

	// colList is already built from explicitColList above, use it for final SELECT

	// Build select list for new_or_changed: all columns from staging in colList order
	newOrChangedSelect := make([]string, 0)
	newOrChangedSelect = append(newOrChangedSelect, "s.entity_id", "s.snapshot_ts", "s.ingested_at", "s.op_id", "s.is_deleted", "s.attrs_hash")
	newOrChangedSelect = append(newOrChangedSelect, func() []string {
		result := make([]string, 0, len(d.pkCols))
		for _, pkCol := range d.pkCols {
			result = append(result, "s."+pkCol)
		}
		return result
	}()...)
	newOrChangedSelect = append(newOrChangedSelect, func() []string {
		result := make([]string, 0, len(d.payloadCols))
		for _, payloadCol := range d.payloadCols {
			result = append(result, "s."+payloadCol)
		}
		return result
	}()...)
	newOrChangedSelectStr := strings.Join(newOrChangedSelect, ", ")

	// Build select list for deleted: latest_active columns with new snapshot_ts/ingested_at/op_id and is_deleted=1
	// IMPORTANT: Recompute attrs_hash with is_deleted=1 since attrs_hash includes is_deleted in its calculation
	// Column order must match colList exactly
	deletedSelect := make([]string, 0)
	deletedSelect = append(deletedSelect, "c.entity_id")
	deletedSelect = append(deletedSelect, "run_snapshot_ts AS snapshot_ts")
	deletedSelect = append(deletedSelect, "run_ingested_at AS ingested_at")
	deletedSelect = append(deletedSelect, "run_op_id AS op_id")
	deletedSelect = append(deletedSelect, "toUInt8(1) AS is_deleted")
	// Recompute attrs_hash with is_deleted=1
	deletedSelect = append(deletedSelect, fmt.Sprintf("(%s) AS attrs_hash", d.AttrsHashExpressionWithPrefix("c", true)))
	for _, pkCol := range d.pkCols {
		deletedSelect = append(deletedSelect, "c."+pkCol)
	}
	for _, payloadCol := range d.payloadCols {
		deletedSelect = append(deletedSelect, "c."+payloadCol)
	}
	deletedSelectStr := strings.Join(deletedSelect, ", ")

	// Build explicit column list for latest_active matching latest CTE output
	// latest_active should select the same columns that latest produces (from buildArgMaxSelect)
	latestActiveSelect := make([]string, 0)
	latestActiveSelect = append(latestActiveSelect, "l.entity_id", "l.snapshot_ts", "l.ingested_at", "l.op_id", "l.is_deleted", "l.attrs_hash")
	for _, pkCol := range d.pkCols {
		latestActiveSelect = append(latestActiveSelect, "l."+pkCol)
	}
	for _, payloadCol := range d.payloadCols {
		latestActiveSelect = append(latestActiveSelect, "l."+payloadCol)
	}
	latestActiveSelectStr := strings.Join(latestActiveSelect, ", ")

	insertQuery := fmt.Sprintf(`
		INSERT INTO %s (%s)
		WITH
			toUUID(?) AS run_op_id,
			toDateTime64(?, 3) AS run_snapshot_ts,
			toDateTime64(?, 3) AS run_ingested_at,
			staging_raw AS (
				SELECT %s
				FROM %s
				WHERE op_id = run_op_id
			),
			staging_agg AS (
				SELECT
					%s
				FROM staging_raw s
				GROUP BY s.entity_id
			),
			staging AS (
				SELECT
					sa.entity_id,
					sa.snapshot_ts,
					sa.ingested_at,
					sa.op_id,
					sa.is_deleted,
					(%s) AS attrs_hash%s%s
				FROM staging_agg sa
			),
			latest AS (
				SELECT
					%s
				FROM %s h
				WHERE h.snapshot_ts <= run_snapshot_ts
				GROUP BY h.entity_id
			),
			latest_active AS (
				SELECT %s FROM latest l WHERE l.is_deleted = 0
			),
			new_or_changed AS (
				SELECT %s
				FROM staging s
				LEFT JOIN latest_active c ON s.entity_id = c.entity_id
				-- Check both empty string (join_use_nulls=0 default) and NULL (join_use_nulls=1)
				WHERE (c.entity_id = '' OR c.entity_id IS NULL) OR s.attrs_hash != c.attrs_hash
			),
			deleted AS (
				SELECT %s
				FROM latest_active c
				LEFT JOIN staging s ON c.entity_id = s.entity_id
				-- Check both empty string (join_use_nulls=0 default) and NULL (join_use_nulls=1)
				WHERE s.entity_id = '' OR s.entity_id IS NULL
			)
		SELECT %s FROM new_or_changed
		%s
	`,
		d.HistoryTableName(),
		colList,
		stagingRawSelect,
		d.StagingTableName(),
		// Build staging_agg CTE with argMax (attrs_hash will be recomputed in staging CTE)
		func() string {
			parts := make([]string, 0)
			parts = append(parts, "s.entity_id")
			tupleExpr := "tuple(s.snapshot_ts, s.ingested_at, s.op_id)"
			parts = append(parts, fmt.Sprintf("argMax(s.snapshot_ts, %s) AS snapshot_ts", tupleExpr))
			parts = append(parts, fmt.Sprintf("argMax(s.ingested_at, %s) AS ingested_at", tupleExpr))
			parts = append(parts, fmt.Sprintf("argMax(s.op_id, %s) AS op_id", tupleExpr))
			parts = append(parts, fmt.Sprintf("argMax(s.is_deleted, %s) AS is_deleted", tupleExpr))
			// Don't include attrs_hash here - will be recomputed in staging CTE
			for _, pkCol := range d.pkCols {
				parts = append(parts, fmt.Sprintf("argMax(s.%s, %s) AS %s", pkCol, tupleExpr, pkCol))
			}
			for _, payloadCol := range d.payloadCols {
				parts = append(parts, fmt.Sprintf("argMax(s.%s, %s) AS %s", payloadCol, tupleExpr, payloadCol))
			}
			return strings.Join(parts, ", ")
		}(),
		// Recompute attrs_hash in staging CTE using aggregated values
		d.AttrsHashExpressionWithPrefix("sa", false),
		func() string {
			parts := make([]string, 0)
			for _, pkCol := range d.pkCols {
				parts = append(parts, fmt.Sprintf(", sa.%s", pkCol))
			}
			return strings.Join(parts, "")
		}(),
		func() string {
			parts := make([]string, 0)
			for _, payloadCol := range d.payloadCols {
				parts = append(parts, fmt.Sprintf(", sa.%s", payloadCol))
			}
			return strings.Join(parts, "")
		}(),
		latestArgMaxSelect,
		d.HistoryTableName(),
		latestActiveSelectStr, // Explicit columns from latest CTE
		newOrChangedSelectStr,
		deletedSelectStr,
		colList, // Explicit column list for final SELECT (matches INSERT column list)
		// Conditionally include UNION ALL SELECT from deleted only when MissingMeansDeleted is true
		func() string {
			if cfg.MissingMeansDeleted {
				return fmt.Sprintf("UNION ALL\n\t\tSELECT %s FROM deleted", colList)
			}
			return ""
		}(),
	)

	// Debug: count rows in new_or_changed and deleted CTEs before inserting
	// Use sync context to ensure we see the staging data we just inserted
	deltaSyncCtx := clickhouse.ContextWithSyncInsert(ctx)
	newChangedCount, deletedCount, err := d.countDeltaRows(deltaSyncCtx, conn, cfg.OpID, cfg.SnapshotTS, cfg.IngestedAt)
	if err != nil {
		d.log.Warn("failed to count delta rows for debugging", "dataset", d.schema.Name(), "error", err)
	} else {
		// Only log deleted count if MissingMeansDeleted is true (otherwise deletes aren't applied)
		if cfg.MissingMeansDeleted {
			d.log.Debug("delta row counts", "dataset", d.schema.Name(), "new_or_changed", newChangedCount, "deleted", deletedCount, "op_id", cfg.OpID)
			if deletedCount > 0 && deletedCount > newChangedCount {
				d.log.Warn("high delete count in delta", "dataset", d.schema.Name(), "new_or_changed", newChangedCount, "deleted", deletedCount, "op_id", cfg.OpID)
			}
		} else {
			d.log.Debug("delta row counts", "dataset", d.schema.Name(), "new_or_changed", newChangedCount, "op_id", cfg.OpID)
			deletedCount = 0 // Reset for logging since deletes won't be applied
		}
	}

	// Execute INSERT INTO ... SELECT query
	// Parameters: run_op_id (as string for toUUID), run_snapshot_ts, run_ingested_at
	// Use sync context to ensure we see the staging data we just inserted
	d.log.Debug("executing delta insert query", "dataset", d.schema.Name(), "snapshot_ts", cfg.SnapshotTS, "op_id", cfg.OpID, "ingested_at", cfg.IngestedAt)

	if err := conn.Exec(deltaSyncCtx, insertQuery, cfg.OpID.String(), cfg.SnapshotTS, cfg.IngestedAt); err != nil {
		return fmt.Errorf("failed to compute and write delta: %w", err)
	}

	d.log.Info("wrote delta to history", "dataset", d.schema.Name(), "new_or_changed", newChangedCount, "deleted", deletedCount, "op_id", cfg.OpID)

	// Optional: Clean up staging rows for this op_id
	// This helps with fast turnover and reduces staging table size
	// ALTER TABLE ... DELETE is a mutation operation and can be costly on busy clusters
	// Make it configurable via CleanupStaging flag (defaults to true for backward compatibility)
	cleanupStaging := true // Default to true for backward compatibility
	if cfg.CleanupStaging != nil {
		cleanupStaging = *cfg.CleanupStaging
	}
	if cleanupStaging {
		if err := d.cleanupStagingForOpID(ctx, conn, cfg.OpID); err != nil {
			// Log but don't fail - TTL will clean up eventually
			d.log.Warn("failed to cleanup staging rows", "dataset", d.schema.Name(), "op_id", cfg.OpID, "error", err)
		}
	}

	return nil
}

// checkOpIDAlreadyProcessed checks if an op_id has already been committed to history
// Returns true if already processed (for idempotency), false otherwise
func (d *DimensionType2Dataset) checkOpIDAlreadyProcessed(ctx context.Context, conn clickhouse.Connection, opID uuid.UUID) (bool, error) {
	checkQuery := fmt.Sprintf(`
		SELECT count()
		FROM %s
		WHERE op_id = ?
		LIMIT 1
	`, d.HistoryTableName())

	rows, err := conn.Query(ctx, checkQuery, opID)
	if err != nil {
		return false, fmt.Errorf("failed to check op_id in history: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		var count uint64
		if err := rows.Scan(&count); err != nil {
			return false, fmt.Errorf("failed to scan op_id check result: %w", err)
		}
		return count > 0, nil
	}

	return false, nil
}

// countStagingRows counts the number of rows in staging for a specific op_id
// Used to verify staging data is visible before computing delta
func (d *DimensionType2Dataset) countStagingRows(ctx context.Context, conn clickhouse.Connection, opID uuid.UUID) (int64, error) {
	countQuery := fmt.Sprintf(`
		SELECT count()
		FROM %s
		WHERE op_id = ?
	`, d.StagingTableName())

	rows, err := conn.Query(ctx, countQuery, opID)
	if err != nil {
		return 0, fmt.Errorf("failed to count staging rows: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		var count uint64
		if err := rows.Scan(&count); err != nil {
			return 0, fmt.Errorf("failed to scan staging count: %w", err)
		}
		return int64(count), nil
	}

	return 0, nil
}

// countAllStagingRows counts total rows in staging table (for debugging)
func (d *DimensionType2Dataset) countAllStagingRows(ctx context.Context, conn clickhouse.Connection) (int64, error) {
	countQuery := fmt.Sprintf(`SELECT count() FROM %s`, d.StagingTableName())
	rows, err := conn.Query(ctx, countQuery)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	if rows.Next() {
		var count uint64
		if err := rows.Scan(&count); err != nil {
			return 0, err
		}
		return int64(count), nil
	}
	return 0, nil
}

// countDeltaRows counts rows in new_or_changed and deleted CTEs for debugging
func (d *DimensionType2Dataset) countDeltaRows(ctx context.Context, conn clickhouse.Connection, opID uuid.UUID, snapshotTS, ingestedAt time.Time) (newChanged, deleted int64, err error) {
	// Build the same CTEs as the main query but just count the results
	countQuery := fmt.Sprintf(`
		WITH
			toUUID(?) AS run_op_id,
			toDateTime64(?, 3) AS run_snapshot_ts,
			staging_raw AS (
				SELECT entity_id, op_id
				FROM %s
				WHERE op_id = run_op_id
			),
			staging AS (
				SELECT entity_id FROM staging_raw GROUP BY entity_id
			),
			latest AS (
				SELECT h.entity_id, argMax(h.is_deleted, tuple(h.snapshot_ts, h.ingested_at, h.op_id)) AS is_deleted
				FROM %s h
				WHERE h.snapshot_ts <= run_snapshot_ts
				GROUP BY h.entity_id
			),
			latest_active AS (
				SELECT entity_id FROM latest WHERE is_deleted = 0
			),
			new_or_changed AS (
				SELECT s.entity_id
				FROM staging s
				LEFT JOIN latest_active c ON s.entity_id = c.entity_id
				WHERE c.entity_id = '' OR c.entity_id IS NULL
			),
			deleted AS (
				SELECT c.entity_id
				FROM latest_active c
				LEFT JOIN staging s ON c.entity_id = s.entity_id
				WHERE s.entity_id = '' OR s.entity_id IS NULL
			)
		SELECT
			(SELECT count() FROM new_or_changed) AS new_changed_count,
			(SELECT count() FROM deleted) AS deleted_count
	`, d.StagingTableName(), d.HistoryTableName())

	rows, err := conn.Query(ctx, countQuery, opID.String(), snapshotTS)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	if rows.Next() {
		var nc, del uint64
		if err := rows.Scan(&nc, &del); err != nil {
			return 0, 0, err
		}
		return int64(nc), int64(del), nil
	}
	return 0, 0, nil
}

// sampleStagingOpIDs returns a sample of distinct op_ids from staging (for debugging)
func (d *DimensionType2Dataset) sampleStagingOpIDs(ctx context.Context, conn clickhouse.Connection, limit int) ([]string, error) {
	query := fmt.Sprintf(`SELECT DISTINCT toString(op_id) FROM %s LIMIT %d`, d.StagingTableName(), limit)
	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var opIDs []string
	for rows.Next() {
		var opID string
		if err := rows.Scan(&opID); err != nil {
			return nil, err
		}
		opIDs = append(opIDs, opID)
	}
	return opIDs, nil
}

// cleanupStagingForOpID removes staging rows for a specific op_id
// This is optional but helps with fast turnover and reduces staging table size
func (d *DimensionType2Dataset) cleanupStagingForOpID(ctx context.Context, conn clickhouse.Connection, opID uuid.UUID) error {
	// Use ALTER TABLE DELETE for efficient cleanup
	// Note: This is a mutation operation in ClickHouse, but it's efficient for cleanup
	cleanupQuery := fmt.Sprintf(`
		ALTER TABLE %s
		DELETE WHERE op_id = ?
	`, d.StagingTableName())

	if err := conn.Exec(ctx, cleanupQuery, opID); err != nil {
		return fmt.Errorf("failed to cleanup staging rows: %w", err)
	}

	d.log.Debug("cleaned up staging rows", "dataset", d.schema.Name(), "op_id", opID)
	return nil
}

// loadSnapshotIntoStaging loads the full snapshot into the staging table
// Computes attrs_hash at insert time using ClickHouse's cityHash64 function
func (d *DimensionType2Dataset) loadSnapshotIntoStaging(
	ctx context.Context,
	conn clickhouse.Connection,
	count int,
	writeRowFn func(int) ([]any, error),
	snapshotTS time.Time,
	ingestedAt time.Time,
	opID uuid.UUID,
) error {
	snapshotTS = snapshotTS.Truncate(time.Millisecond)
	ingestedAt = ingestedAt.Truncate(time.Millisecond)

	// Use INSERT INTO ... SELECT with computed attrs_hash
	// We'll insert data first, then update attrs_hash using ClickHouse's cityHash64 function
	// This ensures the hash is computed consistently with the delta query

	// Use synchronous insert context to ensure staging data is immediately visible
	// for the subsequent delta query that reads from staging
	syncCtx := clickhouse.ContextWithSyncInsert(ctx)
	insertSQL := fmt.Sprintf("INSERT INTO %s", d.StagingTableName())
	batch, err := conn.PrepareBatch(syncCtx, insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare staging batch: %w", err)
	}

	for i := range count {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during staging insert: %w", ctx.Err())
		default:
		}

		// Get row data from callback
		record, err := writeRowFn(i)
		if err != nil {
			return fmt.Errorf("failed to get row data %d: %w", i, err)
		}

		expectedColCount := len(d.pkCols) + len(d.payloadCols)
		if len(record) != expectedColCount {
			return fmt.Errorf("row %d has %d columns, expected exactly %d (PK: %d, payload: %d)", i, len(record), expectedColCount, len(d.pkCols), len(d.payloadCols))
		}
		pkValues := record[:len(d.pkCols)]

		naturalKey := NewNaturalKey(pkValues...)
		entityID := naturalKey.ToSurrogate()

		// Build row: entity_id, snapshot_ts, ingested_at, op_id, is_deleted, ...pkCols, ...payloadCols
		// Note: attrs_hash is excluded from staging insert since it's recomputed in staging CTE
		row := make([]any, 0, 5+len(d.pkCols)+len(d.payloadCols))
		row = append(row, string(entityID)) // entity_id
		row = append(row, snapshotTS)       // snapshot_ts
		row = append(row, ingestedAt)       // ingested_at
		row = append(row, opID)             // op_id
		row = append(row, uint8(0))         // is_deleted
		row = append(row, uint64(0))        // attrs_hash (placeholder, recomputed in staging CTE)

		// Add PK columns (we've validated the count, so we can safely index)
		for j := 0; j < len(d.pkCols); j++ {
			row = append(row, record[j])
		}

		// Add payload columns
		payloadStart := len(d.pkCols)
		for j := 0; j < len(d.payloadCols); j++ {
			row = append(row, record[payloadStart+j])
		}

		if err := batch.Append(row...); err != nil {
			return fmt.Errorf("failed to append row %d: %w", i, err)
		}
	}

	d.log.Debug("sending staging batch", "dataset", d.schema.Name(), "rows", count, "op_id", opID)
	if err := batch.Send(); err != nil {
		_ = batch.Close()
		return fmt.Errorf("failed to send staging batch: %w", err)
	}
	d.log.Debug("staging batch sent successfully", "dataset", d.schema.Name(), "rows", count, "op_id", opID)

	if err := batch.Close(); err != nil {
		return fmt.Errorf("failed to close staging batch: %w", err)
	}

	// Note: attrs_hash is stored as placeholder (0) in staging but is recomputed
	// in the staging CTE from aggregated values, so no UPDATE mutation is needed.
	// This avoids async mutation delays and complexity.

	return nil
}

// processEmptySnapshot handles empty snapshots by inserting tombstones for all current entities
// Queries latest state from history (not current table)
func (d *DimensionType2Dataset) processEmptySnapshot(
	ctx context.Context,
	conn clickhouse.Connection,
	opID uuid.UUID,
	snapshotTS time.Time,
	ingestedAt time.Time,
) error {
	snapshotTS = snapshotTS.Truncate(time.Millisecond)
	ingestedAt = ingestedAt.Truncate(time.Millisecond)

	// Get all columns for the query and row construction
	allCols, err := d.AllColumns()
	if err != nil {
		return fmt.Errorf("failed to extract all columns: %w", err)
	}
	colList := strings.Join(allCols, ", ")

	// Build argMax select for latest non-deleted entities (same pattern as main write)
	latestArgMaxSelect := d.buildArgMaxSelect("h")

	// Build explicit column list for tombstone SELECT matching colList order
	tombstoneSelect := make([]string, 0)
	tombstoneSelect = append(tombstoneSelect, "h.entity_id")
	tombstoneSelect = append(tombstoneSelect, "toDateTime64(?, 3) AS snapshot_ts")
	tombstoneSelect = append(tombstoneSelect, "toDateTime64(?, 3) AS ingested_at")
	tombstoneSelect = append(tombstoneSelect, "toUUID(?) AS op_id")
	tombstoneSelect = append(tombstoneSelect, "toUInt8(1) AS is_deleted")
	tombstoneSelect = append(tombstoneSelect, fmt.Sprintf("(%s) AS attrs_hash", d.AttrsHashExpressionWithPrefix("h", true)))
	for _, pkCol := range d.pkCols {
		tombstoneSelect = append(tombstoneSelect, "h."+pkCol)
	}
	for _, payloadCol := range d.payloadCols {
		tombstoneSelect = append(tombstoneSelect, "h."+payloadCol)
	}
	tombstoneSelectStr := strings.Join(tombstoneSelect, ", ")

	// Build INSERT INTO ... SELECT query to insert tombstones directly
	// Use argMax approach for consistency with main write logic
	insertQuery := fmt.Sprintf(`
		INSERT INTO %s (%s)
		SELECT
			%s
		FROM (
			SELECT
				%s
			FROM %s h
			GROUP BY h.entity_id
		) h
		WHERE h.is_deleted = 0
	`,
		d.HistoryTableName(),
		colList,
		tombstoneSelectStr,
		latestArgMaxSelect,
		d.HistoryTableName(),
	)

	d.log.Debug("inserting tombstones for empty snapshot", "dataset", d.schema.Name(), "query", insertQuery)
	if err := conn.Exec(ctx, insertQuery, snapshotTS, ingestedAt, opID.String()); err != nil {
		return fmt.Errorf("failed to insert tombstones: %w", err)
	}

	d.log.Info("inserted tombstones for empty snapshot", "dataset", d.schema.Name(), "op_id", opID)

	return nil
}

// buildArgMaxSelect builds argMax SELECT expressions for use in GROUP BY queries
// entity_id is in GROUP BY, so we select it directly
// For other columns, use argMax with ordering tuple
// IMPORTANT: Fully qualify all column references with table aliases to avoid alias resolution issues
// Use tuple() function explicitly and prefix all columns with table alias (s. or h.)
func (d *DimensionType2Dataset) buildArgMaxSelect(alias string) string {
	parts := make([]string, 0)
	parts = append(parts, fmt.Sprintf("%s.entity_id", alias))
	// Build tuple with fully-qualified column references
	tupleExpr := fmt.Sprintf("tuple(%s.snapshot_ts, %s.ingested_at, %s.op_id)", alias, alias, alias)
	parts = append(parts, fmt.Sprintf("argMax(%s.snapshot_ts, %s) AS snapshot_ts", alias, tupleExpr))
	parts = append(parts, fmt.Sprintf("argMax(%s.ingested_at, %s) AS ingested_at", alias, tupleExpr))
	parts = append(parts, fmt.Sprintf("argMax(%s.op_id, %s) AS op_id", alias, tupleExpr))
	parts = append(parts, fmt.Sprintf("argMax(%s.is_deleted, %s) AS is_deleted", alias, tupleExpr))
	parts = append(parts, fmt.Sprintf("argMax(%s.attrs_hash, %s) AS attrs_hash", alias, tupleExpr))
	for _, pkCol := range d.pkCols {
		parts = append(parts, fmt.Sprintf("argMax(%s.%s, %s) AS %s", alias, pkCol, tupleExpr, pkCol))
	}
	for _, payloadCol := range d.payloadCols {
		parts = append(parts, fmt.Sprintf("argMax(%s.%s, %s) AS %s", alias, payloadCol, tupleExpr, payloadCol))
	}
	return strings.Join(parts, ", ")
}
