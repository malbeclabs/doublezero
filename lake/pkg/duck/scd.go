package duck

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// SCDTableConfig holds configuration for SCD2 snapshot ingestion
type SCDTableConfig struct {
	// TableBaseName is the base name for tables (e.g., "dz_contributors" creates
	// dz_contributors_current, dz_contributors_history, dz_contributors_ingest_runs)
	TableBaseName string
	// SnapshotTS is the timestamp for this snapshot (same for all rows in the run)
	SnapshotTS time.Time
	// PrimaryKeyColumns are the columns that form the primary key
	PrimaryKeyColumns []string
	// PayloadColumns are all columns except primary keys (used for row_hash computation)
	PayloadColumns []string
	// MissingMeansDeleted if true, treats rows in current but not in stage as deletes
	MissingMeansDeleted bool
	// TrackIngestRuns if true, creates and updates the _ingest_runs table
	TrackIngestRuns bool
	// RunID is an optional identifier for this ingestion run (used in _ingest_runs)
	RunID string
}

// SCDTableViaCSV performs a full-snapshot ingestion using SCD2 pattern:
// - {table}_current: one row per PK with as_of_ts and row_hash
// - {table}_history: SCD2 append-only versions with validity windows
// - {table}_ingest_runs: optional metadata about runs
//
// Algorithm:
// 0) Load snapshot into staging table with snapshot_ts and row_hash
// 1) Compute deltas (inserts, updates, deletes) by joining stage ↔ current on PK
// 2) Update history: close old versions, append new ones
// 3) Refresh current table
func SCDTableViaCSV(
	ctx context.Context,
	log *slog.Logger,
	conn Connection,
	cfg SCDTableConfig,
	count int,
	writeCSVFn func(*csv.Writer, int) error,
) error {
	ingestStart := time.Now()
	defer func() {
		duration := time.Since(ingestStart)
		log.Debug("SCD2 snapshot ingestion completed",
			"table", cfg.TableBaseName,
			"rows", count,
			"duration", duration.String())
	}()

	if len(cfg.PrimaryKeyColumns) == 0 {
		return fmt.Errorf("primary key columns cannot be empty")
	}
	if len(cfg.PayloadColumns) == 0 {
		return fmt.Errorf("payload columns cannot be empty")
	}

	currentTableName := cfg.TableBaseName + "_current"
	historyTableName := cfg.TableBaseName + "_history"
	ingestRunsTableName := cfg.TableBaseName + "_ingest_runs"

	// Create tables if they don't exist
	if err := createSCDTables(ctx, log, conn, cfg, currentTableName, historyTableName, ingestRunsTableName); err != nil {
		return fmt.Errorf("failed to create SCD2 tables: %w", err)
	}

	if count == 0 {
		// Empty snapshot: only process deletes if missing means deleted
		if cfg.MissingMeansDeleted {
			return processEmptySnapshot(ctx, log, conn, cfg, currentTableName, historyTableName, ingestRunsTableName)
		}
		return nil
	}

	// Create CSV file once before retry loop
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("%s_scd2_*.csv", cfg.TableBaseName))
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	csvWriter := csv.NewWriter(tmpFile)
	csvWriter.Comma = ','

	// Log progress every 5 seconds for long-running operations
	progressLogInterval := 5 * time.Second
	lastProgressLog := time.Now()

	for i := range count {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while writing CSV for %s: %w", cfg.TableBaseName, ctx.Err())
		default:
		}

		if err := writeCSVFn(csvWriter, i); err != nil {
			log.Error("failed to write CSV record", "table", cfg.TableBaseName, "row", i, "total", count, "error", err)
			return fmt.Errorf("failed to write CSV record for %s: %w", cfg.TableBaseName, err)
		}

		if count > 1000 {
			now := time.Now()
			if now.Sub(lastProgressLog) >= progressLogInterval {
				log.Debug("write progress", "table", cfg.TableBaseName, "written", i+1, "total", count)
				lastProgressLog = now
			}
		}
	}

	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("CSV writer error: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	tmpFile.Close()

	// Verify file still exists before retry
	if _, err := os.Stat(tmpFile.Name()); err != nil {
		return fmt.Errorf("CSV file no longer exists for %s: %w", cfg.TableBaseName, err)
	}

	// Retry the transaction with the same CSV file
	return retryWithBackoff(ctx, log, fmt.Sprintf("SCD2 snapshot %s", cfg.TableBaseName), func() error {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled before transaction for %s: %w", cfg.TableBaseName, ctx.Err())
		default:
		}

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for %s: %w", cfg.TableBaseName, err)
		}
		defer func() {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				log.Error("failed to rollback transaction", "table", cfg.TableBaseName, "error", err)
			}
		}()

		// Generate unique suffix for temp table
		suffix := make([]byte, 7)
		if _, err := rand.Read(suffix); err != nil {
			return fmt.Errorf("failed to generate unique suffix: %w", err)
		}
		stageTableName := fmt.Sprintf("%s_stage_%s", cfg.TableBaseName, hex.EncodeToString(suffix))

		// Get DB info before transaction (needed for table creation)
		db := conn.DB()

		// Step 0: Load snapshot into staging table with snapshot_ts and row_hash
		if err := loadStageTable(ctx, tx, log, cfg, stageTableName, tmpFile.Name(), db.Catalog(), db.Schema()); err != nil {
			return fmt.Errorf("failed to load stage table: %w", err)
		}

		// Step 1: Compute deltas
		inserts, updates, deletes, err := computeDeltas(ctx, tx, log, cfg, stageTableName, currentTableName)
		if err != nil {
			return fmt.Errorf("failed to compute deltas: %w", err)
		}

		// Step 2: Update history (SCD2 semantics)
		if err := updateHistory(ctx, tx, log, cfg, stageTableName, historyTableName, currentTableName, inserts, updates, deletes); err != nil {
			return fmt.Errorf("failed to update history: %w", err)
		}

		// Step 3: Refresh current table
		if err := refreshCurrent(ctx, tx, log, cfg, stageTableName, currentTableName); err != nil {
			return fmt.Errorf("failed to refresh current: %w", err)
		}

		// Step 4: Track ingest run (optional)
		if cfg.TrackIngestRuns {
			if err := trackIngestRun(ctx, tx, log, cfg, ingestRunsTableName, count, inserts, updates, deletes); err != nil {
				return fmt.Errorf("failed to track ingest run: %w", err)
			}
		}

		// Cleanup
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", stageTableName)); err != nil {
			log.Error("failed to drop stage table", "table", cfg.TableBaseName, "stage_table", stageTableName, "error", err)
			// Don't fail the operation if cleanup fails
		}

		if err := tx.Commit(); err != nil {
			log.Error("transaction commit failed", "table", cfg.TableBaseName, "error", err)
			return fmt.Errorf("failed to commit transaction for %s: %w", cfg.TableBaseName, err)
		}
		return nil
	})
}

// CreateSCDTables creates the _current, _history, and optionally _ingest_runs tables
// This is a public function that can be called to create tables before validation
func CreateSCDTables(
	ctx context.Context,
	log *slog.Logger,
	conn Connection,
	cfg SCDTableConfig,
) error {
	currentTableName := cfg.TableBaseName + "_current"
	historyTableName := cfg.TableBaseName + "_history"
	ingestRunsTableName := cfg.TableBaseName + "_ingest_runs"
	return createSCDTables(ctx, log, conn, cfg, currentTableName, historyTableName, ingestRunsTableName)
}

// createSCDTables creates the _current, _history, and optionally _ingest_runs tables
func createSCDTables(
	ctx context.Context,
	log *slog.Logger,
	conn Connection,
	cfg SCDTableConfig,
	currentTableName, historyTableName, ingestRunsTableName string,
) error {
	db := conn.DB()

	// Build column definitions for current table
	// We need to query the actual table schema or use a different approach
	// For now, assume we can infer types from an existing table or use VARCHAR for all
	// In practice, you'd want to query information_schema or have type info

	// Create _current table: PK columns + payload columns + as_of_ts + row_hash
	pkColsDef := make([]string, len(cfg.PrimaryKeyColumns))
	for i, pk := range cfg.PrimaryKeyColumns {
		pkColsDef[i] = fmt.Sprintf("%s VARCHAR", pk)
	}
	payloadColsDef := make([]string, len(cfg.PayloadColumns))
	for i, col := range cfg.PayloadColumns {
		payloadColsDef[i] = fmt.Sprintf("%s VARCHAR", col)
	}

	currentSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.%s.%s (
		%s,
		%s,
		as_of_ts TIMESTAMP NOT NULL,
		row_hash VARCHAR NOT NULL
	)`,
		db.Catalog(), db.Schema(), currentTableName,
		strings.Join(pkColsDef, ",\n\t\t"),
		strings.Join(payloadColsDef, ",\n\t\t"))

	// Create _history table: PK columns + payload columns + valid_from + valid_to + row_hash + op + run_id
	historySQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.%s.%s (
		%s,
		%s,
		valid_from TIMESTAMP NOT NULL,
		valid_to TIMESTAMP,
		row_hash VARCHAR NOT NULL,
		op VARCHAR,
		run_id VARCHAR
	)`,
		db.Catalog(), db.Schema(), historyTableName,
		strings.Join(pkColsDef, ",\n\t\t"),
		strings.Join(payloadColsDef, ",\n\t\t"))

	// Create _ingest_runs table (optional)
	var ingestRunsSQL string
	if cfg.TrackIngestRuns {
		ingestRunsSQL = fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.%s.%s (
			run_id VARCHAR NOT NULL,
			snapshot_ts TIMESTAMP NOT NULL,
			started_at TIMESTAMP NOT NULL,
			finished_at TIMESTAMP,
			rows_in_snapshot INTEGER,
			inserts INTEGER,
			updates INTEGER,
			deletes INTEGER
		)`,
			db.Catalog(), db.Schema(), ingestRunsTableName)
	}

	queries := []string{currentSQL, historySQL}
	if cfg.TrackIngestRuns {
		queries = append(queries, ingestRunsSQL)
	}

	for _, sql := range queries {
		if _, err := conn.ExecContext(ctx, sql); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}

// loadStageTable loads CSV into staging table and computes row_hash
func loadStageTable(
	ctx context.Context,
	tx *sql.Tx,
	log *slog.Logger,
	cfg SCDTableConfig,
	stageTableName, csvFilePath, catalog, schema string,
) error {
	// Create stage table with same structure as base table + snapshot_ts + row_hash
	pkColsDef := make([]string, len(cfg.PrimaryKeyColumns))
	for i, pk := range cfg.PrimaryKeyColumns {
		pkColsDef[i] = fmt.Sprintf("%s VARCHAR", pk)
	}
	payloadColsDef := make([]string, len(cfg.PayloadColumns))
	for i, col := range cfg.PayloadColumns {
		payloadColsDef[i] = fmt.Sprintf("%s VARCHAR", col)
	}

	createStageSQL := fmt.Sprintf(`CREATE TEMP TABLE %s (
		%s,
		%s,
		snapshot_ts TIMESTAMP NOT NULL,
		row_hash VARCHAR NOT NULL
	)`,
		stageTableName,
		strings.Join(pkColsDef, ",\n\t\t"),
		strings.Join(payloadColsDef, ",\n\t\t"))

	if _, err := tx.ExecContext(ctx, createStageSQL); err != nil {
		return fmt.Errorf("failed to create stage table: %w", err)
	}

	// Copy CSV into stage (without snapshot_ts and row_hash initially)
	// We'll add those in a separate step
	allColumns := append(cfg.PrimaryKeyColumns, cfg.PayloadColumns...)
	colList := strings.Join(allColumns, ", ")

	// Create a temp table for the raw CSV data
	rawStageName := stageTableName + "_raw"
	createRawSQL := fmt.Sprintf(`CREATE TEMP TABLE %s (
		%s
	)`,
		rawStageName,
		strings.Join(append(pkColsDef, payloadColsDef...), ",\n\t\t"))

	if _, err := tx.ExecContext(ctx, createRawSQL); err != nil {
		return fmt.Errorf("failed to create raw stage table: %w", err)
	}

	copySQL := fmt.Sprintf("COPY %s FROM '%s' (FORMAT CSV, HEADER false)", rawStageName, csvFilePath)
	if _, err := tx.ExecContext(ctx, copySQL); err != nil {
		return fmt.Errorf("failed to COPY FROM CSV: %w", err)
	}

	// Compute row_hash and insert into final stage table
	// DuckDB's md5() function can hash multiple columns
	// We'll concatenate payload columns for hashing
	payloadConcat := make([]string, len(cfg.PayloadColumns))
	for i, col := range cfg.PayloadColumns {
		// Handle NULL values
		payloadConcat[i] = fmt.Sprintf("COALESCE(CAST(%s AS VARCHAR), '')", col)
	}
	hashExpr := fmt.Sprintf("md5(%s)", strings.Join(payloadConcat, " || '|' || "))

	insertStageSQL := fmt.Sprintf(`INSERT INTO %s
		SELECT
			%s,
			? AS snapshot_ts,
			%s AS row_hash
		FROM %s`,
		stageTableName,
		colList,
		hashExpr,
		rawStageName)

	if _, err := tx.ExecContext(ctx, insertStageSQL, cfg.SnapshotTS); err != nil {
		return fmt.Errorf("failed to insert into stage table with hash: %w", err)
	}

	// Cleanup raw stage
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", rawStageName)); err != nil {
		log.Error("failed to drop raw stage table", "error", err)
	}

	return nil
}

// computeDeltas computes inserts, updates, and deletes by comparing stage with current
func computeDeltas(
	ctx context.Context,
	tx *sql.Tx,
	log *slog.Logger,
	cfg SCDTableConfig,
	stageTableName, currentTableName string,
) (inserts, updates, deletes int, err error) {
	// Build ON conditions for PK join
	onConditions := make([]string, len(cfg.PrimaryKeyColumns))
	for i, pkCol := range cfg.PrimaryKeyColumns {
		onConditions[i] = fmt.Sprintf("s.%s = c.%s", pkCol, pkCol)
	}
	onClause := fmt.Sprintf("(%s)", strings.Join(onConditions, " AND "))

	// Inserts: stage PK not in current
	insertSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %s s
		WHERE NOT EXISTS (
			SELECT 1 FROM %s c WHERE %s
		)`,
		stageTableName, currentTableName, onClause)
	if err := tx.QueryRowContext(ctx, insertSQL).Scan(&inserts); err != nil {
		return 0, 0, 0, fmt.Errorf("failed to count inserts: %w", err)
	}

	// Updates: stage PK in current, but row_hash differs
	updateSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %s s
		INNER JOIN %s c ON %s
		WHERE s.row_hash != c.row_hash`,
		stageTableName, currentTableName, onClause)
	if err := tx.QueryRowContext(ctx, updateSQL).Scan(&updates); err != nil {
		return 0, 0, 0, fmt.Errorf("failed to count updates: %w", err)
	}

	// Deletes: current PK not in stage (only if missing means deleted)
	if cfg.MissingMeansDeleted {
		deleteSQL := fmt.Sprintf(`SELECT COUNT(*) FROM %s c
			WHERE NOT EXISTS (
				SELECT 1 FROM %s s WHERE %s
			)`,
			currentTableName, stageTableName, onClause)
		if err := tx.QueryRowContext(ctx, deleteSQL).Scan(&deletes); err != nil {
			return 0, 0, 0, fmt.Errorf("failed to count deletes: %w", err)
		}
	}

	log.Debug("computed deltas",
		"table", cfg.TableBaseName,
		"inserts", inserts,
		"updates", updates,
		"deletes", deletes)

	return inserts, updates, deletes, nil
}

// updateHistory updates the history table with SCD2 semantics
func updateHistory(
	ctx context.Context,
	tx *sql.Tx,
	log *slog.Logger,
	cfg SCDTableConfig,
	stageTableName, historyTableName, currentTableName string,
	inserts, updates, deletes int,
) error {
	// Build ON conditions for PK join
	onConditions := make([]string, len(cfg.PrimaryKeyColumns))
	for i, pkCol := range cfg.PrimaryKeyColumns {
		onConditions[i] = fmt.Sprintf("h.%s = s.%s", pkCol, pkCol)
	}
	onClause := fmt.Sprintf("(%s)", strings.Join(onConditions, " AND "))

	// For updates and deletes: close previous open versions (set valid_to = snapshot_ts)
	// where valid_to IS NULL for those PKs
	if updates > 0 || deletes > 0 {
		// Build a temp table with PKs that need to be closed
		// This is more reliable than using WHERE EXISTS in UPDATE
		pkTempTable := historyTableName + "_pks_to_close"

		var pkSelectSQL string
		if updates > 0 && deletes > 0 {
			// Both updates and deletes: PKs that are updated OR deleted
			// Updated: stage PK in current with different hash
			// Deleted: current PK not in stage
			pkColsQualified1 := make([]string, len(cfg.PrimaryKeyColumns))
			pkColsQualified2 := make([]string, len(cfg.PrimaryKeyColumns))
			for i, pkCol := range cfg.PrimaryKeyColumns {
				pkColsQualified1[i] = fmt.Sprintf("s.%s", pkCol)
				pkColsQualified2[i] = fmt.Sprintf("c.%s", pkCol)
			}
			pkSelectSQL = fmt.Sprintf(`CREATE TEMP TABLE %s AS
				SELECT DISTINCT %s FROM (
					SELECT %s FROM %s s
					INNER JOIN %s c ON %s
					WHERE s.row_hash != c.row_hash
					UNION
					SELECT %s FROM %s c
					WHERE NOT EXISTS (SELECT 1 FROM %s s2 WHERE %s)
				)`,
				pkTempTable,
				strings.Join(cfg.PrimaryKeyColumns, ", "),
				strings.Join(pkColsQualified1, ", "),
				stageTableName,
				currentTableName,
				strings.Replace(onClause, "h.", "c.", -1),
				strings.Join(pkColsQualified2, ", "),
				currentTableName,
				stageTableName,
				strings.Replace(strings.Replace(onClause, "h.", "c.", -1), "s.", "s2.", -1))
		} else if updates > 0 {
			// Only updates: PKs in stage that have different hash in current
			pkColsQualified := make([]string, len(cfg.PrimaryKeyColumns))
			for i, pkCol := range cfg.PrimaryKeyColumns {
				pkColsQualified[i] = fmt.Sprintf("s.%s", pkCol)
			}
			pkSelectSQL = fmt.Sprintf(`CREATE TEMP TABLE %s AS
				SELECT DISTINCT %s FROM %s s
				INNER JOIN %s c ON %s
				WHERE s.row_hash != c.row_hash`,
				pkTempTable,
				strings.Join(pkColsQualified, ", "),
				stageTableName,
				currentTableName,
				strings.Replace(onClause, "h.", "c.", -1))
		} else {
			// Only deletes: PKs in current that are not in stage
			pkColsQualified := make([]string, len(cfg.PrimaryKeyColumns))
			for i, pkCol := range cfg.PrimaryKeyColumns {
				pkColsQualified[i] = fmt.Sprintf("c.%s", pkCol)
			}
			pkSelectSQL = fmt.Sprintf(`CREATE TEMP TABLE %s AS
				SELECT DISTINCT %s FROM %s c
				WHERE NOT EXISTS (SELECT 1 FROM %s s WHERE %s)`,
				pkTempTable,
				strings.Join(pkColsQualified, ", "),
				currentTableName,
				stageTableName,
				strings.Replace(onClause, "h.", "s.", -1))
		}

		if _, err := tx.ExecContext(ctx, pkSelectSQL); err != nil {
			return fmt.Errorf("failed to create temp table for PKs to close: %w", err)
		}

		// Build join conditions for UPDATE
		updateJoinConditions := make([]string, len(cfg.PrimaryKeyColumns))
		for i, pkCol := range cfg.PrimaryKeyColumns {
			updateJoinConditions[i] = fmt.Sprintf("h.%s = p.%s", pkCol, pkCol)
		}
		updateJoinClause := strings.Join(updateJoinConditions, " AND ")

		// Update using subquery (DuckDB supports UPDATE with subquery in WHERE)
		closeVersionsSQL := fmt.Sprintf(`UPDATE %s h
			SET valid_to = ?
			WHERE h.valid_to IS NULL
			AND EXISTS (
				SELECT 1 FROM %s p WHERE %s
			)`,
			historyTableName, pkTempTable, updateJoinClause)

		if _, err := tx.ExecContext(ctx, closeVersionsSQL, cfg.SnapshotTS); err != nil {
			return fmt.Errorf("failed to close old versions: %w", err)
		}

		// Cleanup temp table
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", pkTempTable)); err != nil {
			log.Error("failed to drop temp table for PKs", "error", err)
		}
	}

	// Append new versions for inserts and updates
	allColumns := append(cfg.PrimaryKeyColumns, cfg.PayloadColumns...)
	colList := strings.Join(allColumns, ", ")

	// Insert new history rows for inserts and updates
	insertHistorySQL := fmt.Sprintf(`INSERT INTO %s (
		%s,
		valid_from,
		valid_to,
		row_hash,
		op,
		run_id
	)
	SELECT
		%s,
		? AS valid_from,
		NULL AS valid_to,
		row_hash,
		CASE
			WHEN EXISTS (SELECT 1 FROM %s c WHERE %s) THEN 'U'
			ELSE 'I'
		END AS op,
		? AS run_id
	FROM %s s
	WHERE NOT EXISTS (
		SELECT 1 FROM %s c WHERE %s AND c.row_hash = s.row_hash
	)`,
		historyTableName,
		colList,
		colList,
		currentTableName,
		strings.Replace(onClause, "h.", "c.", -1),
		stageTableName,
		currentTableName,
		strings.Replace(onClause, "h.", "c.", -1))

	runID := cfg.RunID
	if runID == "" {
		runID = fmt.Sprintf("run_%d", cfg.SnapshotTS.Unix())
	}

	if _, err := tx.ExecContext(ctx, insertHistorySQL, cfg.SnapshotTS, runID); err != nil {
		return fmt.Errorf("failed to insert new history rows: %w", err)
	}

	// For deletes: optionally insert tombstone rows (op='D')
	if deletes > 0 {
		// Get deleted PKs from current that aren't in stage
		// Build join conditions for current -> stage
		deleteJoinConditions := make([]string, len(cfg.PrimaryKeyColumns))
		for i, pkCol := range cfg.PrimaryKeyColumns {
			deleteJoinConditions[i] = fmt.Sprintf("c.%s = s.%s", pkCol, pkCol)
		}
		deleteJoinClause := fmt.Sprintf("(%s)", strings.Join(deleteJoinConditions, " AND "))

		deleteHistorySQL := fmt.Sprintf(`INSERT INTO %s (
			%s,
			valid_from,
			valid_to,
			row_hash,
			op,
			run_id
		)
		SELECT
			%s,
			? AS valid_from,
			NULL AS valid_to,
			row_hash,
			'D' AS op,
			? AS run_id
		FROM %s c
		WHERE NOT EXISTS (
			SELECT 1 FROM %s s WHERE %s
		)`,
			historyTableName,
			colList,
			colList,
			currentTableName,
			stageTableName,
			deleteJoinClause)

		if _, err := tx.ExecContext(ctx, deleteHistorySQL, cfg.SnapshotTS, runID); err != nil {
			return fmt.Errorf("failed to insert delete tombstone rows: %w", err)
		}
	}

	return nil
}

// refreshCurrent refreshes the current table from stage
func refreshCurrent(
	ctx context.Context,
	tx *sql.Tx,
	log *slog.Logger,
	cfg SCDTableConfig,
	stageTableName, currentTableName string,
) error {
	// Build ON conditions for PK join
	onConditions := make([]string, len(cfg.PrimaryKeyColumns))
	for i, pkCol := range cfg.PrimaryKeyColumns {
		onConditions[i] = fmt.Sprintf("t.%s = s.%s", pkCol, pkCol)
	}
	onClause := fmt.Sprintf("(%s)", strings.Join(onConditions, " AND "))

	allColumns := append(cfg.PrimaryKeyColumns, cfg.PayloadColumns...)
	colList := strings.Join(allColumns, ", ")

	// Deduplicate stage table by primary key (in case of duplicates)
	// Use ROW_NUMBER to pick one row per primary key (latest by snapshot_ts)
	stageDeduped := fmt.Sprintf(`(
		SELECT %s, snapshot_ts, row_hash
		FROM (
			SELECT %s, snapshot_ts, row_hash,
				ROW_NUMBER() OVER (PARTITION BY %s ORDER BY snapshot_ts DESC) AS rn
			FROM %s
		) s
		WHERE rn = 1
	)`, colList, colList, strings.Join(cfg.PrimaryKeyColumns, ", "), stageTableName)

	// Update existing rows using MERGE (consistent with other functions)
	updateSetParts := make([]string, 0)
	for _, col := range cfg.PayloadColumns {
		updateSetParts = append(updateSetParts, fmt.Sprintf("%s = s.%s", col, col))
	}
	updateSetParts = append(updateSetParts, "as_of_ts = s.snapshot_ts", "row_hash = s.row_hash")

	updateSQL := fmt.Sprintf(`MERGE INTO %s t USING %s s ON %s
		WHEN MATCHED THEN UPDATE SET %s`,
		currentTableName,
		stageDeduped,
		onClause,
		strings.Join(updateSetParts, ", "))

	if _, err := tx.ExecContext(ctx, updateSQL); err != nil {
		return fmt.Errorf("failed to update current table: %w", err)
	}

	// Insert new rows (using the same deduplicated stage table)
	insertSQL := fmt.Sprintf(`INSERT INTO %s (
		%s,
		as_of_ts,
		row_hash
	)
	SELECT
		%s,
		snapshot_ts,
		row_hash
	FROM %s s
	WHERE NOT EXISTS (
		SELECT 1 FROM %s t WHERE %s
	)`,
		currentTableName,
		colList,
		colList,
		stageDeduped,
		currentTableName,
		onClause)

	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		return fmt.Errorf("failed to insert into current table: %w", err)
	}

	// Delete rows not in stage (if missing means deleted)
	if cfg.MissingMeansDeleted {
		deleteSQL := fmt.Sprintf(`DELETE FROM %s t
			WHERE NOT EXISTS (
				SELECT 1 FROM %s s WHERE %s
			)`,
			currentTableName, stageDeduped, onClause)

		if _, err := tx.ExecContext(ctx, deleteSQL); err != nil {
			return fmt.Errorf("failed to delete from current table: %w", err)
		}
	}

	return nil
}

// trackIngestRun records metadata about this ingestion run
func trackIngestRun(
	ctx context.Context,
	tx *sql.Tx,
	log *slog.Logger,
	cfg SCDTableConfig,
	ingestRunsTableName string,
	totalRows, inserts, updates, deletes int,
) error {
	runID := cfg.RunID
	if runID == "" {
		runID = fmt.Sprintf("run_%d", cfg.SnapshotTS.Unix())
	}

	startedAt := time.Now()

	// Upsert run record using MERGE (DuckDB doesn't support ON CONFLICT)
	upsertSQL := fmt.Sprintf(`MERGE INTO %s t USING (
		SELECT ? AS run_id, ? AS snapshot_ts, ? AS started_at, ? AS finished_at,
			? AS rows_in_snapshot, ? AS inserts, ? AS updates, ? AS deletes
	) s ON t.run_id = s.run_id
	WHEN MATCHED THEN UPDATE SET
		finished_at = s.finished_at,
		rows_in_snapshot = s.rows_in_snapshot,
		inserts = s.inserts,
		updates = s.updates,
		deletes = s.deletes
	WHEN NOT MATCHED THEN INSERT (
		run_id, snapshot_ts, started_at, finished_at,
		rows_in_snapshot, inserts, updates, deletes
	) VALUES (
		s.run_id, s.snapshot_ts, s.started_at, s.finished_at,
		s.rows_in_snapshot, s.inserts, s.updates, s.deletes
	)`,
		ingestRunsTableName)

	finishedAt := time.Now()
	if _, err := tx.ExecContext(ctx, upsertSQL,
		runID,
		cfg.SnapshotTS,
		startedAt,
		finishedAt,
		totalRows,
		inserts,
		updates,
		deletes,
	); err != nil {
		return fmt.Errorf("failed to upsert ingest run: %w", err)
	}

	return nil
}

// processEmptySnapshot handles the case when count is 0
func processEmptySnapshot(
	ctx context.Context,
	log *slog.Logger,
	conn Connection,
	cfg SCDTableConfig,
	currentTableName, historyTableName, ingestRunsTableName string,
) error {
	return retryWithBackoff(ctx, log, fmt.Sprintf("SCD2 empty snapshot %s", cfg.TableBaseName), func() error {
		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer func() {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				log.Error("failed to rollback transaction", "table", cfg.TableBaseName, "error", err)
			}
		}()

		if cfg.MissingMeansDeleted {
			// All current rows should be marked as deleted
			// Close all open history versions
			closeAllSQL := fmt.Sprintf(`UPDATE %s
				SET valid_to = ?
				WHERE valid_to IS NULL`,
				historyTableName)
			if _, err := tx.ExecContext(ctx, closeAllSQL, cfg.SnapshotTS); err != nil {
				return fmt.Errorf("failed to close all history versions: %w", err)
			}

			// Insert delete tombstones for all current rows
			allColumns := append(cfg.PrimaryKeyColumns, cfg.PayloadColumns...)
			colList := strings.Join(allColumns, ", ")

			runID := cfg.RunID
			if runID == "" {
				runID = fmt.Sprintf("run_%d", cfg.SnapshotTS.Unix())
			}

			deleteHistorySQL := fmt.Sprintf(`INSERT INTO %s (
				%s,
				valid_from,
				valid_to,
				row_hash,
				op,
				run_id
			)
			SELECT
				%s,
				? AS valid_from,
				NULL AS valid_to,
				row_hash,
				'D' AS op,
				? AS run_id
			FROM %s`,
				historyTableName,
				colList,
				colList,
				currentTableName)

			if _, err := tx.ExecContext(ctx, deleteHistorySQL, cfg.SnapshotTS, runID); err != nil {
				return fmt.Errorf("failed to insert delete tombstones: %w", err)
			}

			// Delete all from current
			deleteCurrentSQL := fmt.Sprintf(`DELETE FROM %s`, currentTableName)
			if _, err := tx.ExecContext(ctx, deleteCurrentSQL); err != nil {
				return fmt.Errorf("failed to delete from current: %w", err)
			}
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}
		return nil
	})
}
