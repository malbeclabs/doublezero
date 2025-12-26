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

func ReplaceTableViaCSV(ctx context.Context, log *slog.Logger, conn Connection, tableName string, count int, writeCSVFn func(*csv.Writer, int) error, primaryKeyColumns []string) error {
	tableRefreshStart := time.Now()
	defer func() {
		duration := time.Since(tableRefreshStart)
		log.Debug("refreshing table completed", "table", tableName, "rows", count, "duration", duration.String())
	}()

	if count == 0 {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled before transaction for %s: %w", tableName, ctx.Err())
		default:
		}

		if len(primaryKeyColumns) == 0 {
			return fmt.Errorf("primary key columns cannot be empty")
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
		suffix := make([]byte, 7)
		if _, err := rand.Read(suffix); err != nil {
			return fmt.Errorf("failed to generate unique suffix: %w", err)
		}
		tempTableName := fmt.Sprintf("%s_temp_%s", tableName, hex.EncodeToString(suffix))
		dropTempSQL := fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tempTableName)
		if _, err := tx.ExecContext(ctx, dropTempSQL); err != nil {
			return fmt.Errorf("failed to drop temp table for %s: %w", tableName, err)
		}
		createTempSQL := fmt.Sprintf(`CREATE TEMP TABLE %s AS SELECT * FROM %s WHERE 1=0`, tempTableName, tableName)
		if _, err := tx.ExecContext(ctx, createTempSQL); err != nil {
			return fmt.Errorf("failed to create temp table for %s: %w", tableName, err)
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

		onConditions := make([]string, len(primaryKeyColumns))
		for i, pkCol := range primaryKeyColumns {
			onConditions[i] = fmt.Sprintf("t.%s = s.%s", pkCol, pkCol)
		}
		onClause := fmt.Sprintf("(%s)", strings.Join(onConditions, " AND "))

		mergeSQL := fmt.Sprintf(`MERGE INTO %s t USING %s s ON %s WHEN NOT MATCHED BY SOURCE THEN DELETE`,
			tableName, tempTableName, onClause)

		if _, err := tx.ExecContext(ctx, mergeSQL); err != nil {
			return fmt.Errorf("failed to MERGE INTO for %s: %w", tableName, err)
		}

		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", tempTableName)); err != nil {
			log.Error("failed to drop temp table", "table", tableName, "temp_table", tempTableName, "error", err)
			// Don't fail the operation if cleanup fails
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
		}
		return nil
	}

	tmpFile, err := os.CreateTemp("", fmt.Sprintf("%s_*.csv", tableName))
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

	if len(primaryKeyColumns) == 0 {
		return fmt.Errorf("primary key columns cannot be empty")
	}

	// Generate unique suffix for temp table to avoid collisions in concurrent operations
	suffix := make([]byte, 7)
	if _, err := rand.Read(suffix); err != nil {
		return fmt.Errorf("failed to generate unique suffix: %w", err)
	}
	tempTableName := fmt.Sprintf("%s_temp_%s", tableName, hex.EncodeToString(suffix))
	dropTempSQL := fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tempTableName)
	if _, err := tx.ExecContext(ctx, dropTempSQL); err != nil {
		return fmt.Errorf("failed to drop temp table for %s: %w", tableName, err)
	}
	createTempSQL := fmt.Sprintf(`CREATE TEMP TABLE %s AS SELECT * FROM %s WHERE 1=0`, tempTableName, tableName)
	if _, err := tx.ExecContext(ctx, createTempSQL); err != nil {
		return fmt.Errorf("failed to create temp table for %s: %w", tableName, err)
	}

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

	deleteSQL := fmt.Sprintf(`MERGE INTO %s t USING %s s ON %s WHEN NOT MATCHED BY SOURCE THEN DELETE`,
		tableName, tempTableName, onClause)
	if _, err := tx.ExecContext(ctx, deleteSQL); err != nil {
		return fmt.Errorf("failed to delete rows not in source for %s: %w", tableName, err)
	}

	var upsertSQL string
	if len(updateSetParts) > 0 {
		updateClause := fmt.Sprintf("WHEN MATCHED THEN UPDATE SET %s", strings.Join(updateSetParts, ", "))
		upsertSQL = fmt.Sprintf(`MERGE INTO %s t USING %s s ON %s %s WHEN NOT MATCHED THEN INSERT %s VALUES %s`,
			tableName, tempTableName, onClause, updateClause, insertColumns, insertValues)
	} else {
		// If all columns are primary keys, only do INSERT
		upsertSQL = fmt.Sprintf(`MERGE INTO %s t USING %s s ON %s WHEN NOT MATCHED THEN INSERT %s VALUES %s`,
			tableName, tempTableName, onClause, insertColumns, insertValues)
	}

	if _, err := tx.ExecContext(ctx, upsertSQL); err != nil {
		return fmt.Errorf("failed to upsert rows from source for %s: %w", tableName, err)
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
}
