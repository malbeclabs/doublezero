package duck

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"
)

func UpsertTableViaCSV(ctx context.Context, log *slog.Logger, db DB, tableName string, count int, writeCSVFn func(*csv.Writer, int) error) error {
	upsertStart := time.Now()
	defer func() {
		duration := time.Since(upsertStart)
		log.Debug("upserting to table completed", "table", tableName, "rows", count, "duration", duration.String())
	}()

	if count == 0 {
		return nil
	}

	// Create a temporary CSV file for COPY FROM (much faster than individual INSERTs)
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("%s_upsert_*.csv", tableName))
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Write CSV data
	csvWriter := csv.NewWriter(tmpFile)
	csvWriter.Comma = ','

	// Log progress every 5 seconds for long-running operations
	progressLogInterval := 5 * time.Second
	lastProgressLog := time.Now()

	for i := range count {
		// Check for context cancellation during long-running write operations
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while writing CSV for %s: %w", tableName, ctx.Err())
		default:
		}

		if err := writeCSVFn(csvWriter, i); err != nil {
			log.Error("failed to write CSV record", "table", tableName, "row", i, "total", count, "error", err)
			return fmt.Errorf("failed to write CSV record for %s: %w", tableName, err)
		}

		// Log progress periodically for large operations
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

	// Close file before COPY (DuckDB needs to open it)
	tmpFile.Close()

	// Check for context cancellation before starting transaction
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled before transaction for %s: %w", tableName, ctx.Err())
	default:
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction for %s: %w", tableName, err)
	}
	defer func() {
		if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			log.Error("failed to rollback transaction", "table", tableName, "error", err)
		}
	}()

	// Create temporary table with same structure
	// Use DROP IF EXISTS to handle case where temp table already exists from previous failed transaction
	tempTableName := fmt.Sprintf("%s_temp_upsert", tableName)
	dropTempSQL := fmt.Sprintf(`DROP TABLE IF EXISTS %s`, tempTableName)
	if _, err := tx.Exec(dropTempSQL); err != nil {
		return fmt.Errorf("failed to drop temp table for %s: %w", tableName, err)
	}
	createTempSQL := fmt.Sprintf(`CREATE TEMP TABLE %s AS SELECT * FROM %s WHERE 1=0`, tempTableName, tableName)
	if _, err := tx.Exec(createTempSQL); err != nil {
		return fmt.Errorf("failed to create temp table for %s: %w", tableName, err)
	}

	// Load CSV into temporary table
	copySQL := fmt.Sprintf("COPY %s FROM '%s' (FORMAT CSV, HEADER false)", tempTableName, tmpFile.Name())
	if _, err := tx.Exec(copySQL); err != nil {
		return fmt.Errorf("failed to COPY FROM CSV for %s: %w", tableName, err)
	}

	// Use INSERT OR REPLACE to merge temp table into main table
	upsertSQL := fmt.Sprintf(`INSERT OR REPLACE INTO %s SELECT * FROM %s`, tableName, tempTableName)
	if _, err := tx.Exec(upsertSQL); err != nil {
		return fmt.Errorf("failed to upsert from temp table for %s: %w", tableName, err)
	}

	if err := tx.Commit(); err != nil {
		log.Error("transaction commit failed", "table", tableName, "error", err)
		return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
	}
	return nil
}
