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

func UpsertTableViaCSV(ctx context.Context, log *slog.Logger, conn Connection, tableName string, count int, writeCSVFn func(*csv.Writer, int) error, primaryKeyColumns []string) error {
	upsertStart := time.Now()
	defer func() {
		duration := time.Since(upsertStart)
		log.Debug("upserting to table completed", "table", tableName, "rows", count, "duration", duration.String())
	}()

	if count == 0 {
		return nil
	}

	// Create CSV file once before retry loop - this ensures idempotency:
	// - Same data is used on each retry attempt
	// - File is only deleted after all retries complete (via defer)
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("%s_upsert_*.csv", tableName))
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
			return fmt.Errorf("context cancelled while writing CSV for %s: %w", tableName, ctx.Err())
		default:
		}

		if err := writeCSVFn(csvWriter, i); err != nil {
			log.Error("failed to write CSV record", "table", tableName, "row", i, "total", count, "error", err)
			return fmt.Errorf("failed to write CSV record for %s: %w", tableName, err)
		}

		if count > 1000 {
			now := time.Now()
			if now.Sub(lastProgressLog) >= progressLogInterval {
				log.Debug("write progress", "table", tableName, "written", i+1, "total", count)
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

	// Verify file still exists before retry (should always be true, but defensive)
	if _, err := os.Stat(tmpFile.Name()); err != nil {
		return fmt.Errorf("CSV file no longer exists for %s: %w", tableName, err)
	}

	// Retry the transaction with the same CSV file - operations are idempotent:
	// - MERGE operations are idempotent (based on primary keys)
	// - TEMP tables are transaction-scoped (cleaned up on rollback)
	// - Each retry uses a unique temp table name (random suffix)
	return retryWithBackoff(ctx, log, fmt.Sprintf("upsert table %s", tableName), func() error {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled before transaction for %s: %w", tableName, ctx.Err())
		default:
		}

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for %s: %w", tableName, err)
		}
		defer func() {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				log.Error("failed to rollback transaction", "table", tableName, "error", err)
			}
		}()

		// Generate unique suffix for temp table to avoid collisions in concurrent operations
		// Each retry gets a new unique name, ensuring idempotency
		suffix := make([]byte, 7)
		if _, err := rand.Read(suffix); err != nil {
			return fmt.Errorf("failed to generate unique suffix: %w", err)
		}
		tempTableName := fmt.Sprintf("%s_temp_%s", tableName, hex.EncodeToString(suffix))
		// DROP IF EXISTS is idempotent - safe to run multiple times
		dropTempSQL := fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tempTableName)
		if _, err := tx.ExecContext(ctx, dropTempSQL); err != nil {
			return fmt.Errorf("failed to drop temp table for %s: %w", tableName, err)
		}
		// Create fresh temp table for this retry attempt
		createTempSQL := fmt.Sprintf(`CREATE TEMP TABLE %s AS SELECT * FROM %s WHERE 1=0`, tempTableName, tableName)
		if _, err := tx.ExecContext(ctx, createTempSQL); err != nil {
			return fmt.Errorf("failed to create temp table for %s: %w", tableName, err)
		}

		// COPY same CSV file - idempotent because MERGE operation below is idempotent
		copySQL := fmt.Sprintf("COPY %s FROM '%s' (FORMAT CSV, HEADER false)", tempTableName, tmpFile.Name())
		if _, err := tx.ExecContext(ctx, copySQL); err != nil {
			return fmt.Errorf("failed to COPY FROM CSV for %s: %w", tableName, err)
		}

		db := conn.DB()
		rows, err := tx.QueryContext(ctx, `
			SELECT column_name
			FROM information_schema.columns
			WHERE table_catalog = ? AND table_schema = ? AND table_name = ?
			ORDER BY ordinal_position
		`, db.Catalog(), db.Schema(), tableName)
		if err != nil {
			return fmt.Errorf("failed to query table schema for %s: %w", tableName, err)
		}
		defer rows.Close()

		var allColumns []string
		for rows.Next() {
			var colName string
			if err := rows.Scan(&colName); err != nil {
				return fmt.Errorf("failed to scan column name for %s: %w", tableName, err)
			}
			allColumns = append(allColumns, colName)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("error iterating schema rows for %s: %w", tableName, err)
		}

		if len(allColumns) == 0 {
			return fmt.Errorf("table %s has no columns", tableName)
		}

		if len(primaryKeyColumns) == 0 {
			return fmt.Errorf("primary key columns cannot be empty")
		}

		// Deduplicate temp table by primary key to avoid "same row updated multiple times" error
		dedupedTableName := fmt.Sprintf("%s_deduped", tempTableName)
		dropDedupSQL := fmt.Sprintf(`DROP TABLE IF EXISTS %s`, dedupedTableName)
		if _, err := tx.ExecContext(ctx, dropDedupSQL); err != nil {
			return fmt.Errorf("failed to drop existing deduplicated temp table for %s: %w", tableName, err)
		}
		pkColsStr := strings.Join(primaryKeyColumns, ", ")
		allColsStr := strings.Join(allColumns, ", ")
		dedupSQL := fmt.Sprintf(`CREATE TEMP TABLE %s AS SELECT %s FROM (SELECT %s, ROW_NUMBER() OVER (PARTITION BY %s) AS rn FROM %s) WHERE rn = 1`,
			dedupedTableName, allColsStr, allColsStr, pkColsStr, tempTableName)
		if _, err := tx.ExecContext(ctx, dedupSQL); err != nil {
			return fmt.Errorf("failed to deduplicate temp table for %s: %w", tableName, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE %s", tempTableName)); err != nil {
			return fmt.Errorf("failed to drop original temp table for %s: %w", tableName, err)
		}
		tempTableName = dedupedTableName

		onConditions := make([]string, len(primaryKeyColumns))
		for i, pkCol := range primaryKeyColumns {
			onConditions[i] = fmt.Sprintf("t.%s = s.%s", pkCol, pkCol)
		}
		onClause := fmt.Sprintf("(%s)", strings.Join(onConditions, " AND "))

		updateSetParts := make([]string, 0)
		pkSet := make(map[string]bool)
		for _, pkCol := range primaryKeyColumns {
			pkSet[pkCol] = true
		}
		for _, col := range allColumns {
			if !pkSet[col] {
				updateSetParts = append(updateSetParts, fmt.Sprintf("%s = s.%s", col, col))
			}
		}

		insertColumns := fmt.Sprintf("(%s)", strings.Join(allColumns, ", "))
		insertValueRefs := make([]string, len(allColumns))
		for i, col := range allColumns {
			insertValueRefs[i] = fmt.Sprintf("s.%s", col)
		}
		insertValues := fmt.Sprintf("(%s)", strings.Join(insertValueRefs, ", "))

		// MERGE operation is idempotent - running multiple times with the same data
		// produces the same result (based on primary key matching)
		var mergeSQL string
		if len(updateSetParts) > 0 {
			updateClause := fmt.Sprintf("WHEN MATCHED THEN UPDATE SET %s", strings.Join(updateSetParts, ", "))
			mergeSQL = fmt.Sprintf(`MERGE INTO %s t USING %s s ON %s %s WHEN NOT MATCHED THEN INSERT %s VALUES %s`,
				tableName, tempTableName, onClause, updateClause, insertColumns, insertValues)
		} else {
			// If all columns are primary keys, only do INSERT
			mergeSQL = fmt.Sprintf(`MERGE INTO %s t USING %s s ON %s WHEN NOT MATCHED THEN INSERT %s VALUES %s`,
				tableName, tempTableName, onClause, insertColumns, insertValues)
		}

		if _, err := tx.ExecContext(ctx, mergeSQL); err != nil {
			return fmt.Errorf("failed to MERGE INTO for %s: %w", tableName, err)
		}

		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTableName)); err != nil {
			log.Error("failed to drop temp table", "table", tableName, "temp_table", tempTableName, "error", err)
			// Don't fail the operation if cleanup fails
		}

		if err := tx.Commit(); err != nil {
			log.Error("transaction commit failed", "table", tableName, "error", err)
			return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
		}
		return nil
	})
}
