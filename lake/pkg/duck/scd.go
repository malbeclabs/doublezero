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
	// Each column is a name:type pair, e.g., "pk:VARCHAR", "id:BIGINT"
	PrimaryKeyColumns []string
	// PayloadColumns are all columns except primary keys (used for row_hash computation)
	// Each column is a name:type pair, e.g., "code:VARCHAR", "longitude:DOUBLE"
	PayloadColumns []string
	// MissingMeansDeleted if true, treats rows in current but not in stage as deletes
	MissingMeansDeleted bool
	// TrackIngestRuns if true, creates and updates the _ingest_runs table
	TrackIngestRuns bool
	// RunID is an optional identifier for this ingestion run (used in _ingest_runs)
	RunID string
}

// extractColumnName extracts the column name from a "name:type" format string
func extractColumnName(colDef string) (string, error) {
	parts := strings.SplitN(colDef, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid column definition %q: expected format 'name:type'", colDef)
	}
	return strings.TrimSpace(parts[0]), nil
}

// extractColumnNames extracts column names from a slice of "name:type" format strings
func extractColumnNames(colDefs []string) ([]string, error) {
	names := make([]string, 0, len(colDefs))
	for _, colDef := range colDefs {
		name, err := extractColumnName(colDef)
		if err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, nil
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
			if err := trackIngestRun(ctx, tx, log, cfg, ingestRunsTableName, ingestStart, count, inserts, updates, deletes); err != nil {
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

	// Parse columns into name:type pairs for current table
	pkColsDef := make([]string, 0, len(cfg.PrimaryKeyColumns))
	for _, pk := range cfg.PrimaryKeyColumns {
		parts := strings.SplitN(pk, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid primary key column definition %q: expected format 'name:type'", pk)
		}
		colName := strings.TrimSpace(parts[0])
		colType := strings.TrimSpace(parts[1])
		pkColsDef = append(pkColsDef, fmt.Sprintf("%s %s", colName, colType))
	}
	payloadColsDef := make([]string, 0, len(cfg.PayloadColumns))
	for _, col := range cfg.PayloadColumns {
		parts := strings.SplitN(col, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid payload column definition %q: expected format 'name:type'", col)
		}
		colName := strings.TrimSpace(parts[0])
		colType := strings.TrimSpace(parts[1])
		payloadColsDef = append(payloadColsDef, fmt.Sprintf("%s %s", colName, colType))
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

	// Add partitioning to history and ingest_runs tables if this is DuckLake
	if _, ok := db.(*Lake); ok {
		// Partition history table on valid_from
		partitionSQL := fmt.Sprintf(`ALTER TABLE %s.%s.%s SET PARTITIONED BY (year(valid_from), month(valid_from), day(valid_from))`,
			db.Catalog(), db.Schema(), historyTableName)
		if _, err := conn.ExecContext(ctx, partitionSQL); err != nil {
			// Partitioning may fail if:
			// - Table is already partitioned with a different scheme (error)
			// - Other constraints prevent partitioning (error)
			// If already partitioned with the same scheme, it typically succeeds (no-op or update)
			// Log but don't fail - this is idempotent for the common case
			log.Warn("failed to set partitioning (may already be partitioned)", "table", historyTableName, "error", err)
		}

		// Partition ingest_runs table on started_at if it exists
		if cfg.TrackIngestRuns {
			partitionSQL := fmt.Sprintf(`ALTER TABLE %s.%s.%s SET PARTITIONED BY (year(started_at), month(started_at), day(started_at))`,
				db.Catalog(), db.Schema(), ingestRunsTableName)
			if _, err := conn.ExecContext(ctx, partitionSQL); err != nil {
				// Partitioning may fail if:
				// - Table is already partitioned with a different scheme (error)
				// - Other constraints prevent partitioning (error)
				// If already partitioned with the same scheme, it typically succeeds (no-op or update)
				// Log but don't fail - this is idempotent for the common case
				log.Warn("failed to set partitioning (may already be partitioned)", "table", ingestRunsTableName, "error", err)
			}
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
	// For staging, use VARCHAR for all columns to simplify CSV loading
	// DuckDB will handle type conversion on INSERT
	pkColsDef := make([]string, 0, len(cfg.PrimaryKeyColumns))
	for _, pk := range cfg.PrimaryKeyColumns {
		parts := strings.SplitN(pk, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid primary key column definition %q: expected format 'name:type'", pk)
		}
		colName := strings.TrimSpace(parts[0])
		pkColsDef = append(pkColsDef, fmt.Sprintf("%s VARCHAR", colName))
	}
	payloadColsDef := make([]string, 0, len(cfg.PayloadColumns))
	for _, col := range cfg.PayloadColumns {
		parts := strings.SplitN(col, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid payload column definition %q: expected format 'name:type'", col)
		}
		colName := strings.TrimSpace(parts[0])
		payloadColsDef = append(payloadColsDef, fmt.Sprintf("%s VARCHAR", colName))
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
	pkColNames, err := extractColumnNames(cfg.PrimaryKeyColumns)
	if err != nil {
		return err
	}
	payloadColNames, err := extractColumnNames(cfg.PayloadColumns)
	if err != nil {
		return err
	}
	allColumns := append(pkColNames, payloadColNames...)
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
	payloadConcat := make([]string, len(payloadColNames))
	for i, col := range payloadColNames {
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
	// Extract column names
	pkColNames, err := extractColumnNames(cfg.PrimaryKeyColumns)
	if err != nil {
		return 0, 0, 0, err
	}
	// Build ON conditions for PK join
	// Use IS NOT DISTINCT FROM to handle NULL values correctly
	// (NULL = NULL evaluates to NULL in SQL, but IS NOT DISTINCT FROM treats NULL = NULL as true)
	onConditions := make([]string, len(pkColNames))
	for i, pkCol := range pkColNames {
		onConditions[i] = fmt.Sprintf("s.%s IS NOT DISTINCT FROM c.%s", pkCol, pkCol)
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

	// Diagnostic: check actual row counts to detect join issues
	var stageCount, currentCount int
	if err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", stageTableName)).Scan(&stageCount); err == nil {
		if err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", currentTableName)).Scan(&currentCount); err == nil {
			log.Debug("computed deltas with diagnostics",
				"table", cfg.TableBaseName,
				"stage_count", stageCount,
				"current_count", currentCount,
				"inserts", inserts,
				"updates", updates,
				"deletes", deletes)

			// Sanity check: deletes should never exceed current_count
			if deletes > currentCount {
				log.Error("computed deltas: delete count exceeds current table count - join condition may be broken",
					"table", cfg.TableBaseName,
					"deletes", deletes,
					"current_count", currentCount,
					"stage_count", stageCount)
			}

			// Check if stage is empty but we're reporting inserts
			if stageCount == 0 && inserts > 0 {
				log.Error("computed deltas: stage table is empty but inserts > 0",
					"table", cfg.TableBaseName,
					"inserts", inserts)
			}
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
	// Extract column names
	pkColNames, err := extractColumnNames(cfg.PrimaryKeyColumns)
	if err != nil {
		return err
	}
	payloadColNames, err := extractColumnNames(cfg.PayloadColumns)
	if err != nil {
		return err
	}
	// Build ON conditions for PK join
	// Use IS NOT DISTINCT FROM to handle NULL values correctly
	onConditions := make([]string, len(pkColNames))
	for i, pkCol := range pkColNames {
		onConditions[i] = fmt.Sprintf("h.%s IS NOT DISTINCT FROM s.%s", pkCol, pkCol)
	}
	onClause := fmt.Sprintf("(%s)", strings.Join(onConditions, " AND "))

	// For inserts, updates, and deletes: close previous open versions (set valid_to = snapshot_ts)
	// where valid_to IS NULL for those PKs. This includes closing delete tombstones when entities are re-inserted.
	if inserts > 0 || updates > 0 || deletes > 0 {
		// Build a temp table with PKs that need to be closed
		// This is more reliable than using WHERE EXISTS in UPDATE
		pkTempTable := historyTableName + "_pks_to_close"

		var pkSelectSQL string
		if inserts > 0 && updates > 0 && deletes > 0 {
			// All three: PKs that are inserted, updated, OR deleted
			// Inserted: stage PK not in current
			// Updated: stage PK in current with different hash
			// Deleted: current PK not in stage
			pkColsQualified1 := make([]string, len(pkColNames))
			pkColsQualified2 := make([]string, len(pkColNames))
			pkColsQualified3 := make([]string, len(pkColNames))
			for i, pkCol := range pkColNames {
				pkColsQualified1[i] = fmt.Sprintf("s.%s", pkCol)
				pkColsQualified2[i] = fmt.Sprintf("s2.%s", pkCol)
				pkColsQualified3[i] = fmt.Sprintf("c.%s", pkCol)
			}
			pkSelectSQL = fmt.Sprintf(`CREATE TEMP TABLE %s AS
				SELECT DISTINCT %s FROM (
					SELECT %s FROM %s s
					WHERE NOT EXISTS (SELECT 1 FROM %s c WHERE %s)
					UNION
					SELECT %s FROM %s s2
					INNER JOIN %s c ON %s
					WHERE s2.row_hash != c.row_hash
					UNION
					SELECT %s FROM %s c
					WHERE NOT EXISTS (SELECT 1 FROM %s s3 WHERE %s)
				)`,
				pkTempTable,
				strings.Join(pkColNames, ", "),
				strings.Join(pkColsQualified1, ", "),
				stageTableName,
				currentTableName,
				strings.Replace(onClause, "h.", "c.", -1),
				strings.Join(pkColsQualified2, ", "),
				stageTableName,
				currentTableName,
				strings.Replace(strings.Replace(onClause, "h.", "c.", -1), "s.", "s2.", -1),
				strings.Join(pkColsQualified3, ", "),
				currentTableName,
				stageTableName,
				strings.Replace(strings.Replace(onClause, "h.", "c.", -1), "s.", "s3.", -1))
		} else if inserts > 0 && updates > 0 {
			// Inserts and updates: PKs that are inserted OR updated
			pkColsQualified1 := make([]string, len(pkColNames))
			pkColsQualified2 := make([]string, len(pkColNames))
			for i, pkCol := range pkColNames {
				pkColsQualified1[i] = fmt.Sprintf("s.%s", pkCol)
				pkColsQualified2[i] = fmt.Sprintf("s2.%s", pkCol)
			}
			pkSelectSQL = fmt.Sprintf(`CREATE TEMP TABLE %s AS
				SELECT DISTINCT %s FROM (
					SELECT %s FROM %s s
					WHERE NOT EXISTS (SELECT 1 FROM %s c WHERE %s)
					UNION
					SELECT %s FROM %s s2
					INNER JOIN %s c ON %s
					WHERE s2.row_hash != c.row_hash
				)`,
				pkTempTable,
				strings.Join(pkColNames, ", "),
				strings.Join(pkColsQualified1, ", "),
				stageTableName,
				currentTableName,
				strings.Replace(onClause, "h.", "c.", -1),
				strings.Join(pkColsQualified2, ", "),
				stageTableName,
				currentTableName,
				strings.Replace(strings.Replace(onClause, "h.", "c.", -1), "s.", "s2.", -1))
		} else if inserts > 0 && deletes > 0 {
			// Inserts and deletes: PKs that are inserted OR deleted
			pkColsQualified1 := make([]string, len(pkColNames))
			pkColsQualified2 := make([]string, len(pkColNames))
			for i, pkCol := range pkColNames {
				pkColsQualified1[i] = fmt.Sprintf("s.%s", pkCol)
				pkColsQualified2[i] = fmt.Sprintf("c.%s", pkCol)
			}
			pkSelectSQL = fmt.Sprintf(`CREATE TEMP TABLE %s AS
				SELECT DISTINCT %s FROM (
					SELECT %s FROM %s s
					WHERE NOT EXISTS (SELECT 1 FROM %s c WHERE %s)
					UNION
					SELECT %s FROM %s c
					WHERE NOT EXISTS (SELECT 1 FROM %s s2 WHERE %s)
				)`,
				pkTempTable,
				strings.Join(pkColNames, ", "),
				strings.Join(pkColsQualified1, ", "),
				stageTableName,
				currentTableName,
				strings.Replace(onClause, "h.", "c.", -1),
				strings.Join(pkColsQualified2, ", "),
				currentTableName,
				stageTableName,
				strings.Replace(strings.Replace(onClause, "h.", "c.", -1), "s.", "s2.", -1))
		} else if updates > 0 && deletes > 0 {
			// Both updates and deletes: PKs that are updated OR deleted
			// Updated: stage PK in current with different hash
			// Deleted: current PK not in stage
			pkColsQualified1 := make([]string, len(pkColNames))
			pkColsQualified2 := make([]string, len(pkColNames))
			for i, pkCol := range pkColNames {
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
				strings.Join(pkColNames, ", "),
				strings.Join(pkColsQualified1, ", "),
				stageTableName,
				currentTableName,
				strings.Replace(onClause, "h.", "c.", -1),
				strings.Join(pkColsQualified2, ", "),
				currentTableName,
				stageTableName,
				strings.Replace(strings.Replace(onClause, "h.", "c.", -1), "s.", "s2.", -1))
		} else if inserts > 0 {
			// Only inserts: PKs in stage that are not in current (may have delete tombstones to close)
			pkColsQualified := make([]string, len(pkColNames))
			for i, pkCol := range pkColNames {
				pkColsQualified[i] = fmt.Sprintf("s.%s", pkCol)
			}
			pkSelectSQL = fmt.Sprintf(`CREATE TEMP TABLE %s AS
				SELECT DISTINCT %s FROM %s s
				WHERE NOT EXISTS (SELECT 1 FROM %s c WHERE %s)`,
				pkTempTable,
				strings.Join(pkColsQualified, ", "),
				stageTableName,
				currentTableName,
				strings.Replace(onClause, "h.", "c.", -1))
		} else if updates > 0 {
			// Only updates: PKs in stage that have different hash in current
			pkColsQualified := make([]string, len(pkColNames))
			for i, pkCol := range pkColNames {
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
			pkColsQualified := make([]string, len(pkColNames))
			for i, pkCol := range pkColNames {
				pkColsQualified[i] = fmt.Sprintf("c.%s", pkCol)
			}
			pkSelectSQL = fmt.Sprintf(`CREATE TEMP TABLE %s AS
				SELECT DISTINCT %s FROM %s c
				WHERE NOT EXISTS (SELECT 1 FROM %s s WHERE %s)`,
				pkTempTable,
				strings.Join(pkColsQualified, ", "),
				currentTableName,
				stageTableName,
				strings.Replace(onClause, "h.", "c.", -1))
		}

		if _, err := tx.ExecContext(ctx, pkSelectSQL); err != nil {
			return fmt.Errorf("failed to create temp table for PKs to close: %w", err)
		}

		// Build join conditions for UPDATE
		// Use IS NOT DISTINCT FROM to handle NULL values correctly
		updateJoinConditions := make([]string, len(pkColNames))
		for i, pkCol := range pkColNames {
			updateJoinConditions[i] = fmt.Sprintf("h.%s IS NOT DISTINCT FROM p.%s", pkCol, pkCol)
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
	allColumns := append(pkColNames, payloadColNames...)
	colList := strings.Join(allColumns, ", ")

	runID := cfg.RunID
	if runID == "" {
		runID = fmt.Sprintf("run_%d", cfg.SnapshotTS.Unix())
	}

	// Insert new history rows for inserts and updates
	// Only insert if there are actual changes (inserts or updates)
	// AND the row_hash differs from current (actual change), OR it's a new insert
	// AND there's no existing history row with the same PK and row_hash for this snapshot
	if inserts > 0 || updates > 0 {
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
		)
		AND NOT EXISTS (
			SELECT 1 FROM %s h WHERE %s AND h.row_hash = s.row_hash AND h.valid_from = ?
		)`,
			historyTableName,
			colList,
			colList,
			currentTableName,
			strings.Replace(onClause, "h.", "c.", -1),
			stageTableName,
			currentTableName,
			strings.Replace(onClause, "h.", "c.", -1),
			historyTableName,
			onClause)

		if _, err := tx.ExecContext(ctx, insertHistorySQL, cfg.SnapshotTS, runID, cfg.SnapshotTS); err != nil {
			return fmt.Errorf("failed to insert new history rows: %w", err)
		}
	}

	// For deletes: optionally insert tombstone rows (op='D')
	if deletes > 0 {
		// Get deleted PKs from current that aren't in stage
		// Build join conditions for current -> stage
		// Use IS NOT DISTINCT FROM to handle NULL values correctly
		deleteJoinConditions := make([]string, len(pkColNames))
		for i, pkCol := range pkColNames {
			deleteJoinConditions[i] = fmt.Sprintf("c.%s IS NOT DISTINCT FROM s.%s", pkCol, pkCol)
		}
		deleteJoinClause := fmt.Sprintf("(%s)", strings.Join(deleteJoinConditions, " AND "))

		// Build join conditions for history -> current
		// Use IS NOT DISTINCT FROM to handle NULL values correctly
		historyJoinConditions := make([]string, len(pkColNames))
		for i, pkCol := range pkColNames {
			historyJoinConditions[i] = fmt.Sprintf("h.%s IS NOT DISTINCT FROM c.%s", pkCol, pkCol)
		}
		historyJoinClause := fmt.Sprintf("(%s)", strings.Join(historyJoinConditions, " AND "))

		// Select from the history version we just closed (where valid_to = snapshot_ts)
		// This ensures we get the correct payload values that were active before deletion
		// We use a subquery to get the most recent version (MAX valid_from) to handle edge cases
		// where multiple versions might have the same valid_to timestamp
		historyColList := make([]string, len(allColumns))
		for i, col := range allColumns {
			historyColList[i] = fmt.Sprintf("h.%s", col)
		}
		historyColListStr := strings.Join(historyColList, ", ")

		// Build correlated subquery to get the most recent closed version for each PK
		// This ensures we get the correct version even if multiple rows have valid_to = snapshot_ts
		// The subquery correlates on PK from the outer query (c table)
		// Use IS NOT DISTINCT FROM to handle NULL values correctly
		pkJoinForSubquery := make([]string, len(pkColNames))
		for i, pkCol := range pkColNames {
			pkJoinForSubquery[i] = fmt.Sprintf("h2.%s IS NOT DISTINCT FROM c.%s", pkCol, pkCol)
		}
		pkJoinForSubqueryClause := strings.Join(pkJoinForSubquery, " AND ")
		maxValidFromSubquery := fmt.Sprintf(`(
			SELECT MAX(h2.valid_from)
			FROM %s h2
			WHERE %s
			AND h2.valid_to = ?
		)`,
			historyTableName,
			pkJoinForSubqueryClause)

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
			h.row_hash,
			'D' AS op,
			? AS run_id
		FROM %s c
		INNER JOIN %s h ON %s AND h.valid_to = ? AND h.valid_from = %s
		WHERE NOT EXISTS (
			SELECT 1 FROM %s s WHERE %s
		)`,
			historyTableName,
			colList,
			historyColListStr,
			currentTableName,
			historyTableName,
			historyJoinClause,
			maxValidFromSubquery,
			stageTableName,
			deleteJoinClause)

		if _, err := tx.ExecContext(ctx, deleteHistorySQL, cfg.SnapshotTS, runID, cfg.SnapshotTS, cfg.SnapshotTS); err != nil {
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
	// Extract column names
	pkColNames, err := extractColumnNames(cfg.PrimaryKeyColumns)
	if err != nil {
		return err
	}
	payloadColNames, err := extractColumnNames(cfg.PayloadColumns)
	if err != nil {
		return err
	}
	// Build ON conditions for PK join
	// Use IS NOT DISTINCT FROM to handle NULL values correctly
	onConditions := make([]string, len(pkColNames))
	for i, pkCol := range pkColNames {
		onConditions[i] = fmt.Sprintf("t.%s IS NOT DISTINCT FROM s.%s", pkCol, pkCol)
	}
	onClause := fmt.Sprintf("(%s)", strings.Join(onConditions, " AND "))

	allColumns := append(pkColNames, payloadColNames...)
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
	)`, colList, colList, strings.Join(pkColNames, ", "), stageTableName)

	// Update existing rows using MERGE (consistent with other functions)
	updateSetParts := make([]string, 0)
	for _, col := range payloadColNames {
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
	startedAt time.Time,
	totalRows, inserts, updates, deletes int,
) error {
	runID := cfg.RunID
	if runID == "" {
		runID = fmt.Sprintf("run_%d", cfg.SnapshotTS.Unix())
	}

	// Upsert run record using MERGE (DuckDB doesn't support ON CONFLICT)
	upsertSQL := fmt.Sprintf(`MERGE INTO %s t USING (
		SELECT ? AS run_id, ? AS snapshot_ts, ? AS started_at, ? AS finished_at,
			? AS rows_in_snapshot, ? AS inserts, ? AS updates, ? AS deletes
	) s ON t.run_id = s.run_id
	WHEN MATCHED THEN UPDATE SET
		started_at = s.started_at,
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
			pkColNames, err := extractColumnNames(cfg.PrimaryKeyColumns)
			if err != nil {
				return err
			}
			payloadColNames, err := extractColumnNames(cfg.PayloadColumns)
			if err != nil {
				return err
			}
			allColumns := append(pkColNames, payloadColNames...)
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

// BackfillValidToOnDeletes fixes rows where valid_to wasn't set when a delete occurred.
// This is a one-time backfill function to fix data affected by the bug where valid_to
// wasn't being set on delete.
//
// Algorithm:
//  1. Find all delete tombstones (op = 'D') in the history table
//  2. For each delete tombstone with valid_from = X, find previous open versions
//     (valid_to IS NULL) for the same PK where valid_from < X
//  3. Set valid_to = X for those previous open versions
//
// The delete tombstone's valid_from timestamp tells us exactly when the deletion
// occurred, so we can reliably backfill the valid_to field.
func BackfillValidToOnDeletes(
	ctx context.Context,
	log *slog.Logger,
	conn Connection,
	cfg SCDTableConfig,
	dryRun bool,
) (fixedCount int, err error) {
	historyTableName := cfg.TableBaseName + "_history"

	// Extract primary key column names
	pkColNames, err := extractColumnNames(cfg.PrimaryKeyColumns)
	if err != nil {
		return 0, fmt.Errorf("failed to extract primary key column names: %w", err)
	}

	// Build PK equality conditions for joining
	// Use IS NOT DISTINCT FROM to handle NULL values correctly
	pkConditions := make([]string, len(pkColNames))
	for i, pkCol := range pkColNames {
		pkConditions[i] = fmt.Sprintf("h1.%s IS NOT DISTINCT FROM h2.%s", pkCol, pkCol)
	}
	pkJoinClause := strings.Join(pkConditions, " AND ")

	if dryRun {
		// Count how many rows would be fixed
		countSQL := fmt.Sprintf(`SELECT COUNT(*)
			FROM %s h1
			INNER JOIN %s h2 ON %s
			WHERE h1.op = 'D'
			AND h2.valid_to IS NULL
			AND h2.valid_from < h1.valid_from`,
			historyTableName, historyTableName, pkJoinClause)

		if err := conn.QueryRowContext(ctx, countSQL).Scan(&fixedCount); err != nil {
			return 0, fmt.Errorf("failed to count rows to fix: %w", err)
		}

		log.Info("backfill valid_to on deletes (dry run)",
			"table", cfg.TableBaseName,
			"rows_to_fix", fixedCount)
		return fixedCount, nil
	}

	// Update previous open versions to set valid_to = delete tombstone's valid_from
	// We use a subquery to find the earliest delete tombstone after each open version
	updateSQL := fmt.Sprintf(`UPDATE %s h2
		SET valid_to = (
			SELECT h1.valid_from
			FROM %s h1
			WHERE %s
			AND h1.op = 'D'
			AND h1.valid_from > h2.valid_from
			ORDER BY h1.valid_from ASC
			LIMIT 1
		)
		WHERE h2.valid_to IS NULL
		AND EXISTS (
			SELECT 1
			FROM %s h1
			WHERE %s
			AND h1.op = 'D'
			AND h1.valid_from > h2.valid_from
		)`,
		historyTableName,
		historyTableName,
		pkJoinClause,
		historyTableName,
		pkJoinClause)

	result, err := conn.ExecContext(ctx, updateSQL)
	if err != nil {
		return 0, fmt.Errorf("failed to backfill valid_to on deletes: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// Some drivers don't support RowsAffected, so we'll query for the count
		var count int
		countSQL := fmt.Sprintf(`SELECT COUNT(*)
			FROM %s
			WHERE valid_to IS NOT NULL
			AND EXISTS (
				SELECT 1
				FROM %s h1
				WHERE %s
				AND h1.op = 'D'
				AND h1.valid_from = %s.valid_to
			)`,
			historyTableName,
			historyTableName,
			pkJoinClause,
			historyTableName)
		if err := conn.QueryRowContext(ctx, countSQL).Scan(&count); err != nil {
			log.Warn("failed to count fixed rows, using RowsAffected", "error", err)
			fixedCount = int(rowsAffected)
		} else {
			fixedCount = count
		}
	} else {
		fixedCount = int(rowsAffected)
	}

	log.Info("backfilled valid_to on deletes",
		"table", cfg.TableBaseName,
		"rows_fixed", fixedCount)

	return fixedCount, nil
}

// BackfillValidToOnReinserts fixes delete tombstones where valid_to wasn't set when an entity was re-inserted.
// This is a one-time backfill function to fix data affected by the bug where delete tombstones
// weren't being closed when entities were re-inserted.
//
// Algorithm:
//  1. Find all delete tombstones (op = 'D') with valid_to IS NULL
//  2. For each delete tombstone with valid_from = X, find subsequent insert/update versions
//     (op IN ('I', 'U')) for the same PK where valid_from > X
//  3. Set valid_to = earliest_insert_timestamp for the delete tombstone
//
// The re-insert's valid_from timestamp tells us exactly when the deletion period ended,
// so we can reliably backfill the valid_to field on the delete tombstone.
func BackfillValidToOnReinserts(
	ctx context.Context,
	log *slog.Logger,
	conn Connection,
	cfg SCDTableConfig,
	dryRun bool,
) (fixedCount int, err error) {
	historyTableName := cfg.TableBaseName + "_history"

	// Extract primary key column names
	pkColNames, err := extractColumnNames(cfg.PrimaryKeyColumns)
	if err != nil {
		return 0, fmt.Errorf("failed to extract primary key column names: %w", err)
	}

	// Build PK equality conditions for joining
	// Use IS NOT DISTINCT FROM to handle NULL values correctly
	pkConditions := make([]string, len(pkColNames))
	for i, pkCol := range pkColNames {
		pkConditions[i] = fmt.Sprintf("h1.%s IS NOT DISTINCT FROM h2.%s", pkCol, pkCol)
	}
	pkJoinClause := strings.Join(pkConditions, " AND ")

	if dryRun {
		// Count how many delete tombstones would be fixed
		countSQL := fmt.Sprintf(`SELECT COUNT(*)
			FROM %s h1
			WHERE h1.op = 'D'
			AND h1.valid_to IS NULL
			AND EXISTS (
				SELECT 1
				FROM %s h2
				WHERE %s
				AND h2.op IN ('I', 'U')
				AND h2.valid_from > h1.valid_from
			)`,
			historyTableName,
			historyTableName,
			pkJoinClause)

		if err := conn.QueryRowContext(ctx, countSQL).Scan(&fixedCount); err != nil {
			return 0, fmt.Errorf("failed to count rows to fix: %w", err)
		}

		log.Info("backfill valid_to on re-inserts (dry run)",
			"table", cfg.TableBaseName,
			"rows_to_fix", fixedCount)
		return fixedCount, nil
	}

	// Update delete tombstones to set valid_to = earliest re-insert timestamp
	// We use a subquery to find the earliest insert/update after each delete tombstone
	updateSQL := fmt.Sprintf(`UPDATE %s h1
		SET valid_to = (
			SELECT MIN(h2.valid_from)
			FROM %s h2
			WHERE %s
			AND h2.op IN ('I', 'U')
			AND h2.valid_from > h1.valid_from
		)
		WHERE h1.op = 'D'
		AND h1.valid_to IS NULL
		AND EXISTS (
			SELECT 1
			FROM %s h2
			WHERE %s
			AND h2.op IN ('I', 'U')
			AND h2.valid_from > h1.valid_from
		)`,
		historyTableName,
		historyTableName,
		pkJoinClause,
		historyTableName,
		pkJoinClause)

	result, err := conn.ExecContext(ctx, updateSQL)
	if err != nil {
		return 0, fmt.Errorf("failed to backfill valid_to on re-inserts: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		// Some drivers don't support RowsAffected, so we'll query for the count
		var count int
		countSQL := fmt.Sprintf(`SELECT COUNT(*)
			FROM %s
			WHERE op = 'D'
			AND valid_to IS NOT NULL
			AND EXISTS (
				SELECT 1
				FROM %s h2
				WHERE %s
				AND h2.op IN ('I', 'U')
				AND h2.valid_from = %s.valid_to
			)`,
			historyTableName,
			historyTableName,
			pkJoinClause,
			historyTableName)
		if err := conn.QueryRowContext(ctx, countSQL).Scan(&count); err != nil {
			log.Warn("failed to count fixed rows, using RowsAffected", "error", err)
			fixedCount = int(rowsAffected)
		} else {
			fixedCount = count
		}
	} else {
		fixedCount = int(rowsAffected)
	}

	log.Info("backfilled valid_to on re-inserts",
		"table", cfg.TableBaseName,
		"rows_fixed", fixedCount)

	return fixedCount, nil
}

// DeduplicateCurrentTable removes duplicate rows from SCD2 current tables, keeping only the most recent row per primary key.
// This fixes corruption from the broken join bug that created duplicates.
//
// Algorithm:
//  1. For each primary key, keep only the row with the latest as_of_ts
//  2. If multiple rows have the same as_of_ts, keep the one with the lexicographically largest row_hash
//  3. Delete all other duplicate rows
func DeduplicateCurrentTable(
	ctx context.Context,
	log *slog.Logger,
	conn Connection,
	cfg SCDTableConfig,
	dryRun bool,
) (deletedCount int, err error) {
	currentTableName := cfg.TableBaseName + "_current"

	// Extract primary key column names
	pkColNames, err := extractColumnNames(cfg.PrimaryKeyColumns)
	if err != nil {
		return 0, fmt.Errorf("failed to extract primary key column names: %w", err)
	}

	// Build ORDER BY clause for tiebreaking (as_of_ts DESC, then row_hash DESC)
	pkOrderBy := strings.Join(pkColNames, ", ")

	if dryRun {
		// Count how many rows would be deleted
		// Use ROW_NUMBER to identify duplicates - keep row_number = 1, delete the rest
		countSQL := fmt.Sprintf(`SELECT COUNT(*) FROM (
			SELECT %s,
				ROW_NUMBER() OVER (PARTITION BY %s ORDER BY as_of_ts DESC, row_hash DESC) AS rn
			FROM %s
		) t
		WHERE rn > 1`,
			strings.Join(pkColNames, ", "),
			pkOrderBy,
			currentTableName)

		if err := conn.QueryRowContext(ctx, countSQL).Scan(&deletedCount); err != nil {
			return 0, fmt.Errorf("failed to count rows to delete: %w", err)
		}

		log.Info("deduplicate current table (dry run)",
			"table", cfg.TableBaseName,
			"rows_to_delete", deletedCount)
		return deletedCount, nil
	}

	// Delete all but the most recent row per primary key
	// Strategy: Extract unique rows (one per primary key, keeping the latest),
	// delete all rows, then reinsert the unique rows. This is simpler and handles
	// the edge case of identical rows perfectly - we just keep one arbitrarily.
	payloadColNames, err := extractColumnNames(cfg.PayloadColumns)
	if err != nil {
		return 0, fmt.Errorf("failed to extract payload column names: %w", err)
	}

	// Build column lists
	allCols := append(pkColNames, payloadColNames...)
	allCols = append(allCols, "as_of_ts", "row_hash")
	allColsSelect := strings.Join(allCols, ", ")
	colList := strings.Join(allCols, ", ")

	// Use a transaction to ensure atomicity
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			log.Error("failed to rollback transaction", "table", cfg.TableBaseName, "error", err)
		}
	}()

	// Step 1: Count total rows (for reporting)
	var totalRows int
	if err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", currentTableName)).Scan(&totalRows); err != nil {
		return 0, fmt.Errorf("failed to count total rows: %w", err)
	}

	// Step 2: Create temp table with unique rows (one per primary key, keeping the latest)
	tempTableName := currentTableName + "_dedupe_temp"
	createTempSQL := fmt.Sprintf(`CREATE TEMP TABLE %s AS
		SELECT %s
		FROM (
			SELECT %s,
				ROW_NUMBER() OVER (PARTITION BY %s ORDER BY as_of_ts DESC, row_hash DESC) AS rn
			FROM %s
		) ranked
		WHERE rn = 1`,
		tempTableName,
		colList,
		allColsSelect,
		pkOrderBy,
		currentTableName)
	if _, err := tx.ExecContext(ctx, createTempSQL); err != nil {
		return 0, fmt.Errorf("failed to create temp table with unique rows: %w", err)
	}

	// Step 3: Count unique rows (for reporting)
	var uniqueRows int
	if err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", tempTableName)).Scan(&uniqueRows); err != nil {
		return 0, fmt.Errorf("failed to count unique rows: %w", err)
	}
	deletedCount = totalRows - uniqueRows

	// Step 4: Delete all rows from the original table
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s", currentTableName)); err != nil {
		return 0, fmt.Errorf("failed to delete all rows: %w", err)
	}

	// Step 5: Reinsert unique rows from temp table
	insertSQL := fmt.Sprintf(`INSERT INTO %s (%s)
		SELECT %s FROM %s`,
		currentTableName,
		colList,
		colList,
		tempTableName)
	if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
		return 0, fmt.Errorf("failed to reinsert unique rows: %w", err)
	}

	// Step 6: Clean up temp table
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTableName)); err != nil {
		log.Warn("failed to drop temp table", "table", tempTableName, "error", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	log.Info("deduplicated current table",
		"table", cfg.TableBaseName,
		"rows_deleted", deletedCount)

	return deletedCount, nil
}
