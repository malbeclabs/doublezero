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

func AppendTableViaCSV(ctx context.Context, log *slog.Logger, conn Connection, tableName string, count int, writeCSVFn func(*csv.Writer, int) error) error {
	tableAppendStart := time.Now()
	defer func() {
		duration := time.Since(tableAppendStart)
		log.Debug("appending to table completed", "table", tableName, "rows", count, "duration", duration.String())
	}()

	if count == 0 {
		return nil
	}

	// Create CSV file once before retry loop - this ensures idempotency:
	// - Same data is used on each retry attempt
	// - File is only deleted after all retries complete (via defer)
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

	// Verify file still exists before retry (should always be true, but defensive)
	if _, err := os.Stat(tmpFile.Name()); err != nil {
		return fmt.Errorf("CSV file no longer exists for %s: %w", tableName, err)
	}

	// Retry the transaction with the same CSV file - operations are idempotent:
	// - COPY operations append data, but if the transaction fails and retries,
	//   the rollback undoes the append, so retrying is safe
	// - TEMP tables are transaction-scoped (cleaned up on rollback)
	return retryWithBackoff(ctx, log, fmt.Sprintf("append table %s", tableName), func() error {
		// Check for context cancellation before starting transaction
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

		// Use COPY FROM CSV to append (no TRUNCATE - we're appending)
		copySQL := fmt.Sprintf("COPY %s FROM '%s' (FORMAT CSV, HEADER false)", tableName, tmpFile.Name())
		if _, err := tx.ExecContext(ctx, copySQL); err != nil {
			return fmt.Errorf("failed to COPY FROM CSV for %s: %w", tableName, err)
		}

		if err := tx.Commit(); err != nil {
			log.Error("transaction commit failed", "table", tableName, "error", err)
			return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
		}
		return nil
	})
}
