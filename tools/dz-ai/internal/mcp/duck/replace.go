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

func ReplaceTableViaCSV(ctx context.Context, log *slog.Logger, db DB, tableName string, count int, writeCSVFn func(*csv.Writer, int) error) error {
	tableRefreshStart := time.Now()
	defer func() {
		duration := time.Since(tableRefreshStart)
		log.Debug("refreshing table completed", "table", tableName, "rows", count, "duration", duration.String())
	}()

	if count == 0 {
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

		// Use TRUNCATE to clear the table (faster than DELETE)
		truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", tableName)
		if _, err := tx.Exec(truncateSQL); err != nil {
			return fmt.Errorf("failed to truncate %s: %w", tableName, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
		}
		return nil
	}

	// Create a temporary CSV file for COPY FROM (much faster than INSERT)
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("%s_*.csv", tableName))
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

	// Use COPY FROM for bulk load (much faster than INSERT)
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

	// Use TRUNCATE to clear the table (faster than DELETE)
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", tableName)
	if _, err := tx.Exec(truncateSQL); err != nil {
		return fmt.Errorf("failed to truncate %s: %w", tableName, err)
	}

	// Use COPY FROM CSV to load data
	copySQL := fmt.Sprintf("COPY %s FROM '%s' (FORMAT CSV, HEADER false)", tableName, tmpFile.Name())
	if _, err := tx.Exec(copySQL); err != nil {
		return fmt.Errorf("failed to COPY FROM CSV for %s: %w", tableName, err)
	}

	if err := tx.Commit(); err != nil {
		log.Error("transaction commit failed", "table", tableName, "error", err)
		return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
	}
	return nil
}
