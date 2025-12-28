package duck

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// FactTableConfig holds configuration for fact table ingestion
type FactTableConfig struct {
	// TableName is the name of the fact table
	TableName string
	// Columns defines all columns in the fact table (in order)
	// Each column is a name:type pair, e.g., "time:TIMESTAMP", "device_pk:VARCHAR"
	Columns []string
	// PartitionByTime if true, partitions the table by year(time), month(time), day(time) in DuckLake
	PartitionByTime bool
	// TimeColumn is the name of the timestamp column (required if PartitionByTime is true)
	TimeColumn string
}

// InsertFactsViaCSV performs append-only fact table ingestion:
// - Creates the table if it doesn't exist
// - Loads data from CSV into staging table
// - Inserts all rows into the fact table (append-only, no updates/deletes)
//
// This is designed for time-series fact tables like telemetry data.
func InsertFactsViaCSV(
	ctx context.Context,
	log *slog.Logger,
	conn Connection,
	cfg FactTableConfig,
	count int,
	writeCSVFn func(*csv.Writer, int) error,
) error {
	ingestStart := time.Now()
	defer func() {
		duration := time.Since(ingestStart)
		log.Debug("fact table ingestion completed",
			"table", cfg.TableName,
			"rows", count,
			"duration", duration.String())
	}()

	if len(cfg.Columns) == 0 {
		return fmt.Errorf("columns cannot be empty")
	}

	if cfg.PartitionByTime && cfg.TimeColumn == "" {
		return fmt.Errorf("time_column is required when partition_by_time is true")
	}

	// Create table if it doesn't exist
	if err := CreateFactTable(ctx, log, conn, cfg); err != nil {
		return fmt.Errorf("failed to create fact table: %w", err)
	}

	if count == 0 {
		return nil
	}

	// Create CSV file
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("%s_facts_*.csv", cfg.TableName))
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	csvWriter := csv.NewWriter(tmpFile)
	csvWriter.Comma = ','

	// Write CSV data
	for i := range count {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled during CSV writing: %w", ctx.Err())
		default:
		}

		if err := writeCSVFn(csvWriter, i); err != nil {
			return fmt.Errorf("failed to write CSV row %d: %w", i, err)
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return fmt.Errorf("failed to flush CSV: %w", err)
	}

	// Retry the transaction with the same CSV file
	return retryWithBackoff(ctx, log, fmt.Sprintf("fact table %s", cfg.TableName), func() error {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled before transaction for %s: %w", cfg.TableName, ctx.Err())
		default:
		}

		tx, err := conn.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for %s: %w", cfg.TableName, err)
		}
		defer func() {
			if err := tx.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
				log.Error("failed to rollback transaction", "table", cfg.TableName, "error", err)
			}
		}()

		db := conn.DB()

		// Create staging table
		stageTableName := fmt.Sprintf("%s_stage", cfg.TableName)
		if err := createStageTableForFacts(ctx, tx, cfg, stageTableName); err != nil {
			return fmt.Errorf("failed to create stage table: %w", err)
		}

		// Load CSV into staging table
		copySQL := fmt.Sprintf("COPY %s FROM '%s' (FORMAT CSV, HEADER false)", stageTableName, tmpFile.Name())
		if _, err := tx.ExecContext(ctx, copySQL); err != nil {
			return fmt.Errorf("failed to COPY FROM CSV: %w", err)
		}

		// Insert from stage into fact table
		// Extract column names from column definitions (name:type format)
		colNames := make([]string, 0, len(cfg.Columns))
		for _, col := range cfg.Columns {
			parts := strings.SplitN(col, ":", 2)
			if len(parts) != 2 {
				return fmt.Errorf("invalid column definition %q: expected format 'name:type'", col)
			}
			colNames = append(colNames, strings.TrimSpace(parts[0]))
		}
		colList := strings.Join(colNames, ", ")
		insertSQL := fmt.Sprintf(`INSERT INTO %s.%s.%s (%s)
			SELECT %s FROM %s`,
			db.Catalog(), db.Schema(), cfg.TableName,
			colList,
			colList,
			stageTableName)

		if _, err := tx.ExecContext(ctx, insertSQL); err != nil {
			return fmt.Errorf("failed to insert into fact table: %w", err)
		}

		// Cleanup stage table
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s", stageTableName)); err != nil {
			log.Error("failed to drop stage table", "error", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction: %w", err)
		}

		return nil
	})
}

// CreateFactTable creates the fact table if it doesn't exist
// This is a public function that can be called to create tables before validation
func CreateFactTable(
	ctx context.Context,
	log *slog.Logger,
	conn Connection,
	cfg FactTableConfig,
) error {
	db := conn.DB()

	// Parse columns into name:type pairs
	colDefs := make([]string, 0, len(cfg.Columns))
	for _, col := range cfg.Columns {
		parts := strings.SplitN(col, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid column definition %q: expected format 'name:type'", col)
		}
		colName := strings.TrimSpace(parts[0])
		colType := strings.TrimSpace(parts[1])
		colDefs = append(colDefs, fmt.Sprintf("%s %s", colName, colType))
	}

	createSQL := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.%s.%s (
		%s
	)`,
		db.Catalog(), db.Schema(), cfg.TableName,
		strings.Join(colDefs, ",\n\t\t"))

	if _, err := conn.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create fact table: %w", err)
	}

	// Add partitioning if requested and this is DuckLake
	if cfg.PartitionByTime {
		if _, ok := db.(*Lake); ok {
			partitionSQL := fmt.Sprintf(`ALTER TABLE %s.%s.%s SET PARTITIONED BY (year(%s), month(%s), day(%s))`,
				db.Catalog(), db.Schema(), cfg.TableName,
				cfg.TimeColumn, cfg.TimeColumn, cfg.TimeColumn)
			if _, err := conn.ExecContext(ctx, partitionSQL); err != nil {
				// Partitioning might fail if table already exists and is partitioned differently
				// Log but don't fail - this is idempotent
				log.Debug("failed to set partitioning (may already be partitioned)", "error", err)
			}
		}
	}

	return nil
}

// createStageTableForFacts creates a temporary staging table for fact data
func createStageTableForFacts(
	ctx context.Context,
	tx *sql.Tx,
	cfg FactTableConfig,
	stageTableName string,
) error {
	// Parse columns into name:type pairs
	colDefs := make([]string, 0, len(cfg.Columns))
	for _, col := range cfg.Columns {
		parts := strings.SplitN(col, ":", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid column definition %q: expected format 'name:type'", col)
		}
		colName := strings.TrimSpace(parts[0])
		// For staging, use VARCHAR for all columns to simplify CSV loading
		// DuckDB will handle type conversion on INSERT
		colDefs = append(colDefs, fmt.Sprintf("%s VARCHAR", colName))
	}

	createSQL := fmt.Sprintf(`CREATE TEMP TABLE %s (
		%s
	)`,
		stageTableName,
		strings.Join(colDefs, ",\n\t\t"))

	if _, err := tx.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create stage table: %w", err)
	}

	return nil
}
