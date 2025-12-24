package duck

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"time"
)

func ReplaceTableViaCSV(ctx context.Context, log *slog.Logger, db DB, tableName string, count int, writeCSVFn func(*csv.Writer, int) error) error {
	tableRefreshStart := time.Now()
	log.Info("serviceability: refreshing table started", "table", tableName, "rows", count, "start_time", tableRefreshStart)
	defer func() {
		duration := time.Since(tableRefreshStart)
		log.Info("serviceability: refreshing table completed", "table", tableName, "duration", duration.String())
	}()

	log.Debug("serviceability: refreshing table", "table", tableName, "rows", count)

	if count == 0 {
		// Check for context cancellation before starting transaction
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled before transaction for %s: %w", tableName, ctx.Err())
		default:
		}

		txStart := time.Now()
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("failed to begin transaction for %s: %w", tableName, err)
		}
		log.Debug("serviceability: transaction begun", "table", tableName, "tx_start_time", txStart)
		defer tx.Rollback()

		// Use TRUNCATE to clear the table (faster than DELETE)
		truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", tableName)
		if _, err := tx.Exec(truncateSQL); err != nil {
			return fmt.Errorf("failed to truncate %s: %w", tableName, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
		}
		log.Debug("serviceability: table refreshed (empty)", "table", tableName)
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
	log.Debug("serviceability: writing CSV file", "table", tableName, "rows", count)
	csvWriter := csv.NewWriter(tmpFile)
	csvWriter.Comma = ','

	writeStart := time.Now()
	logInterval := min(max(count/10, 1000), 100000)

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
		if (i+1)%logInterval == 0 || i == count-1 {
			log.Debug("write progress", "table", tableName, "written", i+1, "total", count, "percent", float64(i+1)*100.0/float64(count))
		}
	}

	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("CSV writer error: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	writeDuration := time.Since(writeStart)
	fileSize := getFileSize(tmpFile)
	log.Debug("serviceability: CSV file written", "table", tableName, "duration_ms", writeDuration.Milliseconds(), "file_size_mb", float64(fileSize)/1024/1024)

	// Close file before COPY (DuckDB needs to open it)
	tmpFile.Close()

	// Use COPY FROM for bulk load (much faster than INSERT)
	// Check for context cancellation before starting transaction
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled before transaction for %s: %w", tableName, ctx.Err())
	default:
	}

	txStart := time.Now()
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction for %s: %w", tableName, err)
	}
	log.Debug("serviceability: transaction begun", "table", tableName, "tx_start_time", txStart)
	defer tx.Rollback()

	// Use TRUNCATE to clear the table (faster than DELETE)
	truncateSQL := fmt.Sprintf("TRUNCATE TABLE %s", tableName)
	if _, err := tx.Exec(truncateSQL); err != nil {
		return fmt.Errorf("failed to truncate %s: %w", tableName, err)
	}

	// Use COPY FROM CSV to load data
	copyStart := time.Now()
	copySQL := fmt.Sprintf("COPY %s FROM '%s' (FORMAT CSV, HEADER false)", tableName, tmpFile.Name())
	if _, err := tx.Exec(copySQL); err != nil {
		return fmt.Errorf("failed to COPY FROM CSV for %s: %w", tableName, err)
	}
	copyDuration := time.Since(copyStart)
	log.Debug("serviceability: COPY FROM completed", "table", tableName, "duration", copyDuration.String())

	commitStart := time.Now()
	log.Info("serviceability: committing transaction", "table", tableName, "rows", count, "tx_duration", time.Since(txStart).String(), "commit_start_time", commitStart)
	if err := tx.Commit(); err != nil {
		txDuration := time.Since(txStart)
		log.Error("serviceability: transaction commit failed", "table", tableName, "error", err, "tx_duration", txDuration.String())
		return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
	}
	commitDuration := time.Since(commitStart)
	log.Info("serviceability: transaction committed", "table", tableName, "commit_duration", commitDuration.String(), "total_tx_duration", time.Since(txStart).String())

	totalDuration := time.Since(tableRefreshStart)
	rate := float64(count) / totalDuration.Seconds()
	log.Debug("serviceability: table refreshed", "table", tableName, "rows", count, "total_duration_ms", totalDuration.Milliseconds(), "rate_rows_per_sec", int(rate))
	return nil
}

func getFileSize(f *os.File) int64 {
	if info, err := f.Stat(); err == nil {
		return info.Size()
	}
	return 0
}
