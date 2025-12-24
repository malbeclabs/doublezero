package dztelem

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/malbeclabs/doublezero/tools/mcp/internal/duck"
)

type StoreConfig struct {
	Logger *slog.Logger
	DB     duck.DB
}

func (cfg *StoreConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.DB == nil {
		return errors.New("db is required")
	}
	return nil
}

type Store struct {
	log *slog.Logger
	cfg StoreConfig
	db  duck.DB
}

func NewStore(cfg StoreConfig) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Store{
		log: cfg.Logger,
		cfg: cfg,
		db:  cfg.DB,
	}, nil
}

func (s *Store) CreateTablesIfNotExists() error {
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS dz_device_link_circuits (
			code VARCHAR PRIMARY KEY,
			origin_device_pk VARCHAR,
			target_device_pk VARCHAR,
			link_pk VARCHAR,
			link_code VARCHAR,
			link_type VARCHAR,
			contributor_code VARCHAR,
			committed_rtt DOUBLE,
			committed_jitter DOUBLE
		)`,
		`CREATE TABLE IF NOT EXISTS dz_device_link_latency_samples (
			circuit_code VARCHAR,
			epoch BIGINT,
			sample_index INTEGER,
			timestamp_us BIGINT,
			rtt_us BIGINT,
			PRIMARY KEY (circuit_code, epoch, sample_index)
		)`,
		`CREATE TABLE IF NOT EXISTS dz_internet_metro_latency_samples (
			circuit_code VARCHAR,
			data_provider VARCHAR,
			epoch BIGINT,
			sample_index INTEGER,
			timestamp_us BIGINT,
			rtt_us BIGINT,
			PRIMARY KEY (circuit_code, data_provider, epoch, sample_index)
		)`,
	}

	for _, schema := range schemas {
		if _, err := s.db.Exec(schema); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}

func (s *Store) ReplaceDeviceLinkCircuits(circuits []DeviceLinkCircuit) error {
	s.log.Debug("telemetry/store: replacing device-link circuits", "count", len(circuits))
	return s.replaceTable("dz_device_link_circuits", "DELETE FROM dz_device_link_circuits", "INSERT INTO dz_device_link_circuits (code, origin_device_pk, target_device_pk, link_pk, link_code, link_type, contributor_code, committed_rtt, committed_jitter) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)", len(circuits), func(stmt *sql.Stmt, i int) error {
		c := circuits[i]
		_, err := stmt.Exec(c.Code, c.OriginDevicePK, c.TargetDevicePK, c.LinkPK, c.LinkCode, c.LinkType, c.ContributorCode, c.CommittedRTT, c.CommittedJitter)
		return err
	})
}

func (s *Store) AppendDeviceLinkLatencySamples(samples []DeviceLinkLatencySample) error {
	tableRefreshStart := time.Now()
	s.log.Info("telemetry: appending to table started", "table", "dz_device_link_latency_samples", "rows", len(samples), "start_time", tableRefreshStart)
	defer func() {
		duration := time.Since(tableRefreshStart)
		s.log.Info("telemetry: appending to table completed", "table", "dz_device_link_latency_samples", "duration", duration.String())
	}()

	if len(samples) == 0 {
		return nil
	}

	s.log.Debug("telemetry/device-link: starting bulk append using COPY FROM", "samples", len(samples))
	startTime := time.Now()

	// Create a temporary CSV file for COPY FROM (much faster than INSERT)
	tmpFile, err := os.CreateTemp("", "dz_device_link_latency_samples_*.csv")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Write CSV data
	s.log.Debug("telemetry/device-link: writing CSV file", "samples", len(samples))
	csvWriter := csv.NewWriter(tmpFile)
	csvWriter.Comma = ','

	writeStart := time.Now()
	for _, s := range samples {
		record := []string{
			s.CircuitCode,
			fmt.Sprintf("%d", s.Epoch),
			fmt.Sprintf("%d", s.SampleIndex),
			fmt.Sprintf("%d", s.TimestampMicroseconds),
			fmt.Sprintf("%d", s.RTTMicroseconds),
		}
		if err := csvWriter.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
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
	s.log.Debug("telemetry/device-link: CSV file written", "duration_ms", writeDuration.Milliseconds(), "file_size_mb", float64(getFileSize(tmpFile))/1024/1024)

	// Get file info for COPY
	fileInfo, err := tmpFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat temp file: %w", err)
	}
	s.log.Debug("telemetry/device-link: file ready for COPY", "size_bytes", fileInfo.Size())

	// Close file before COPY (DuckDB needs to open it)
	tmpFile.Close()

	// Use COPY FROM for bulk load (much faster than INSERT)
	txStart := time.Now()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	s.log.Debug("telemetry: transaction begun", "table", "dz_device_link_latency_samples", "tx_start_time", txStart)
	defer tx.Rollback()

	// Use COPY FROM CSV to append (no TRUNCATE - we're appending)
	copyStart := time.Now()
	copySQL := fmt.Sprintf("COPY dz_device_link_latency_samples FROM '%s' (FORMAT CSV, HEADER false)", tmpFile.Name())
	if _, err := tx.Exec(copySQL); err != nil {
		return fmt.Errorf("failed to COPY FROM CSV: %w", err)
	}
	copyDuration := time.Since(copyStart)
	s.log.Debug("telemetry/device-link: COPY FROM completed", "duration", copyDuration.String())

	commitStart := time.Now()
	s.log.Info("telemetry: committing transaction", "table", "dz_device_link_latency_samples", "rows", len(samples), "tx_duration", time.Since(txStart).String(), "commit_start_time", commitStart)
	if err := tx.Commit(); err != nil {
		txDuration := time.Since(txStart)
		s.log.Error("telemetry: transaction commit failed", "table", "dz_device_link_latency_samples", "error", err, "tx_duration", txDuration.String())
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	commitDuration := time.Since(commitStart)
	s.log.Info("telemetry: transaction committed", "table", "dz_device_link_latency_samples", "commit_duration", commitDuration.String(), "total_tx_duration", time.Since(txStart).String())

	totalDuration := time.Since(startTime)
	rate := float64(len(samples)) / totalDuration.Seconds()
	s.log.Debug("telemetry/device-link: bulk append completed", "samples", len(samples), "total_duration_ms", totalDuration.Milliseconds(), "rate_rows_per_sec", int(rate))
	return nil
}

func (s *Store) AppendInternetMetroLatencySamples(samples []InternetMetroLatencySample) error {
	tableRefreshStart := time.Now()
	s.log.Info("telemetry: appending to table started", "table", "dz_internet_metro_latency_samples", "rows", len(samples), "start_time", tableRefreshStart)
	defer func() {
		duration := time.Since(tableRefreshStart)
		s.log.Info("telemetry: appending to table completed", "table", "dz_internet_metro_latency_samples", "duration", duration.String())
	}()

	if len(samples) == 0 {
		return nil
	}

	s.log.Debug("telemetry/internet-metro: starting bulk append using COPY FROM", "samples", len(samples))
	startTime := time.Now()

	// Create a temporary CSV file for COPY FROM (much faster than INSERT)
	tmpFile, err := os.CreateTemp("", "internet_metro_latency_samples_*.csv")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	// Write CSV data
	s.log.Debug("telemetry/internet-metro: writing CSV file", "samples", len(samples))
	csvWriter := csv.NewWriter(tmpFile)
	csvWriter.Comma = ','

	writeStart := time.Now()
	for _, s := range samples {
		record := []string{
			s.CircuitCode,
			s.DataProvider,
			fmt.Sprintf("%d", s.Epoch),
			fmt.Sprintf("%d", s.SampleIndex),
			fmt.Sprintf("%d", s.TimestampMicroseconds),
			fmt.Sprintf("%d", s.RTTMicroseconds),
		}
		if err := csvWriter.Write(record); err != nil {
			return fmt.Errorf("failed to write CSV record: %w", err)
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
	s.log.Debug("telemetry/internet-metro: CSV file written", "duration_ms", writeDuration.Milliseconds(), "file_size_mb", float64(getFileSize(tmpFile))/1024/1024)

	// Close file before COPY (DuckDB needs to open it)
	tmpFile.Close()

	// Use COPY FROM for bulk load (much faster than INSERT)
	txStart := time.Now()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	s.log.Debug("telemetry: transaction begun", "table", "dz_internet_metro_latency_samples", "tx_start_time", txStart)
	defer tx.Rollback()

	// Use COPY FROM CSV to append (no TRUNCATE - we're appending)
	copyStart := time.Now()
	copySQL := fmt.Sprintf("COPY dz_internet_metro_latency_samples FROM '%s' (FORMAT CSV, HEADER false)", tmpFile.Name())
	if _, err := tx.Exec(copySQL); err != nil {
		return fmt.Errorf("failed to COPY FROM CSV: %w", err)
	}
	copyDuration := time.Since(copyStart)
	s.log.Debug("telemetry/internet-metro: COPY FROM completed", "duration", copyDuration.String())

	commitStart := time.Now()
	s.log.Info("telemetry: committing transaction", "table", "dz_internet_metro_latency_samples", "rows", len(samples), "tx_duration", time.Since(txStart).String(), "commit_start_time", commitStart)
	if err := tx.Commit(); err != nil {
		txDuration := time.Since(txStart)
		s.log.Error("telemetry: transaction commit failed", "table", "dz_internet_metro_latency_samples", "error", err, "tx_duration", txDuration.String())
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	commitDuration := time.Since(commitStart)
	s.log.Info("telemetry: transaction committed", "table", "dz_internet_metro_latency_samples", "commit_duration", commitDuration.String(), "total_tx_duration", time.Since(txStart).String())

	totalDuration := time.Since(startTime)
	rate := float64(len(samples)) / totalDuration.Seconds()
	s.log.Debug("telemetry/internet-metro: bulk append completed", "samples", len(samples), "total_duration_ms", totalDuration.Milliseconds(), "rate_rows_per_sec", int(rate))
	return nil
}

func (s *Store) GetExistingMaxSampleIndices() (map[string]int, error) {
	query := `SELECT circuit_code, epoch, MAX(sample_index) as max_idx
	          FROM dz_device_link_latency_samples
	          GROUP BY circuit_code, epoch`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing max indices: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var circuitCode string
		var epoch uint64
		var maxIdx sql.NullInt64
		if err := rows.Scan(&circuitCode, &epoch, &maxIdx); err != nil {
			return nil, fmt.Errorf("failed to scan max index: %w", err)
		}
		if maxIdx.Valid {
			key := fmt.Sprintf("%s:%d", circuitCode, epoch)
			result[key] = int(maxIdx.Int64)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating max indices: %w", err)
	}
	return result, nil
}

func (s *Store) GetExistingInternetMaxSampleIndices() (map[string]int, error) {
	query := `SELECT circuit_code, data_provider, epoch, MAX(sample_index) as max_idx
	          FROM dz_internet_metro_latency_samples
	          GROUP BY circuit_code, data_provider, epoch`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing max indices: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var circuitCode, dataProvider string
		var epoch uint64
		var maxIdx sql.NullInt64
		if err := rows.Scan(&circuitCode, &dataProvider, &epoch, &maxIdx); err != nil {
			return nil, fmt.Errorf("failed to scan max index: %w", err)
		}
		if maxIdx.Valid {
			key := fmt.Sprintf("%s:%s:%d", circuitCode, dataProvider, epoch)
			result[key] = int(maxIdx.Int64)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating max indices: %w", err)
	}
	return result, nil
}

func (s *Store) replaceTable(tableName, deleteSQL, insertSQL string, count int, insertFn func(*sql.Stmt, int) error) error {
	tableRefreshStart := time.Now()
	s.log.Info("telemetry: refreshing table started", "table", tableName, "rows", count, "start_time", tableRefreshStart)
	defer func() {
		duration := time.Since(tableRefreshStart)
		s.log.Info("telemetry: refreshing table completed", "table", tableName, "duration", duration.String())
	}()

	s.log.Debug("telemetry: refreshing table", "table", tableName, "rows", count)

	txStart := time.Now()
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction for %s: %w", tableName, err)
	}
	s.log.Debug("telemetry: transaction begun", "table", tableName, "tx_start_time", txStart)
	defer tx.Rollback()

	if _, err := tx.Exec(deleteSQL); err != nil {
		return fmt.Errorf("failed to clear %s: %w", tableName, err)
	}

	if count == 0 {
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
		}
		s.log.Debug("telemetry: table refreshed (empty)", "table", tableName)
		return nil
	}

	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		return fmt.Errorf("failed to prepare statement for %s: %w", tableName, err)
	}
	defer stmt.Close()

	// Log progress for large inserts
	logInterval := min(max(count/10, 1000), 100000)

	for i := range count {
		if err := insertFn(stmt, i); err != nil {
			s.log.Error("failed to insert row", "table", tableName, "row", i, "total", count, "error", err)
			return fmt.Errorf("failed to insert into %s: %w", tableName, err)
		}
		if (i+1)%logInterval == 0 || i == count-1 {
			s.log.Debug("insert progress", "table", tableName, "inserted", i+1, "total", count, "percent", float64(i+1)*100.0/float64(count))
		}
	}

	commitStart := time.Now()
	s.log.Info("telemetry: committing transaction", "table", tableName, "rows", count, "tx_duration", time.Since(txStart).String(), "commit_start_time", commitStart)
	if err := tx.Commit(); err != nil {
		txDuration := time.Since(txStart)
		s.log.Error("telemetry: transaction commit failed", "table", tableName, "error", err, "tx_duration", txDuration.String())
		return fmt.Errorf("failed to commit transaction for %s: %w", tableName, err)
	}
	commitDuration := time.Since(commitStart)
	s.log.Info("telemetry: transaction committed", "table", tableName, "commit_duration", commitDuration.String(), "total_tx_duration", time.Since(txStart).String())

	s.log.Debug("telemetry: table refreshed", "table", tableName, "rows", count)
	return nil
}

func getFileSize(f *os.File) int64 {
	if info, err := f.Stat(); err == nil {
		return info.Size()
	}
	return 0
}
