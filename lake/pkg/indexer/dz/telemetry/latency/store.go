package dztelemlatency

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/lake/pkg/duck"
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

// FactTableConfigDeviceLinkLatencySamples returns the fact table config for device link latency samples
func FactTableConfigDeviceLinkLatencySamples() duck.FactTableConfig {
	return duck.FactTableConfig{
		TableName:       "dz_device_link_latency_samples_raw",
		PartitionByTime: true,
		TimeColumn:      "time",
		Columns: []string{
			"time:TIMESTAMP",
			"epoch:BIGINT",
			"sample_index:INTEGER",
			"origin_device_pk:VARCHAR",
			"target_device_pk:VARCHAR",
			"link_pk:VARCHAR",
			"rtt_us:BIGINT",
			"loss:BOOLEAN",
			"ipdv_us:BIGINT",
		},
	}
}

// FactTableConfigInternetMetroLatencySamples returns the fact table config for internet metro latency samples
func FactTableConfigInternetMetroLatencySamples() duck.FactTableConfig {
	return duck.FactTableConfig{
		TableName:       "dz_internet_metro_latency_samples_raw",
		PartitionByTime: true,
		TimeColumn:      "time",
		Columns: []string{
			"time:TIMESTAMP",
			"epoch:BIGINT",
			"sample_index:INTEGER",
			"origin_metro_pk:VARCHAR",
			"target_metro_pk:VARCHAR",
			"data_provider:VARCHAR",
			"rtt_us:BIGINT",
			"ipdv_us:BIGINT",
		},
	}
}

func (s *Store) CreateTablesIfNotExists() error {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	// Create device link latency samples table
	deviceLinkCfg := FactTableConfigDeviceLinkLatencySamples()
	if err := duck.CreateFactTable(ctx, s.log, conn, deviceLinkCfg); err != nil {
		return fmt.Errorf("failed to create device link latency samples table: %w", err)
	}

	// Create internet metro latency samples table
	internetMetroCfg := FactTableConfigInternetMetroLatencySamples()
	if err := duck.CreateFactTable(ctx, s.log, conn, internetMetroCfg); err != nil {
		return fmt.Errorf("failed to create internet metro latency samples table: %w", err)
	}

	return nil
}

// getPreviousDeviceLinkRTTBatch gets the most recent RTT for multiple device link circuits in one query
func (s *Store) getPreviousDeviceLinkRTTBatch(ctx context.Context, conn duck.Connection, circuits []struct {
	originDevicePK, targetDevicePK, linkPK string
}) (map[string]uint32, error) {
	if len(circuits) == 0 {
		return make(map[string]uint32), nil
	}

	// Build query with IN clauses for each circuit
	// Use DISTINCT ON to get the latest RTT for each circuit
	query := `SELECT DISTINCT ON (origin_device_pk, target_device_pk, link_pk)
		origin_device_pk, target_device_pk, link_pk, rtt_us
		FROM dz_device_link_latency_samples_raw
		WHERE rtt_us > 0
		AND (`

	conditions := make([]string, 0, len(circuits))
	args := make([]any, 0, len(circuits)*3)
	argIdx := 1
	for _, circuit := range circuits {
		conditions = append(conditions, fmt.Sprintf("(origin_device_pk = $%d AND target_device_pk = $%d AND link_pk = $%d)", argIdx, argIdx+1, argIdx+2))
		args = append(args, circuit.originDevicePK, circuit.targetDevicePK, circuit.linkPK)
		argIdx += 3
	}
	query += strings.Join(conditions, " OR ")
	query += `)
		ORDER BY origin_device_pk, target_device_pk, link_pk, epoch DESC, sample_index DESC`

	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query previous RTTs: %w", err)
	}
	defer rows.Close()

	result := make(map[string]uint32)
	for rows.Next() {
		var originDevicePK, targetDevicePK, linkPK string
		var rtt sql.NullInt64
		if err := rows.Scan(&originDevicePK, &targetDevicePK, &linkPK, &rtt); err != nil {
			return nil, fmt.Errorf("failed to scan previous RTT: %w", err)
		}
		if rtt.Valid && rtt.Int64 > 0 {
			key := fmt.Sprintf("%s:%s:%s", originDevicePK, targetDevicePK, linkPK)
			result[key] = uint32(rtt.Int64)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating previous RTTs: %w", err)
	}

	return result, nil
}

func (s *Store) AppendDeviceLinkLatencySamples(ctx context.Context, samples []DeviceLinkLatencySample) error {
	if len(samples) == 0 {
		return nil
	}

	// Sort samples by circuit, then by epoch, then by sample_index
	sortedSamples := make([]DeviceLinkLatencySample, len(samples))
	copy(sortedSamples, samples)
	sort.Slice(sortedSamples, func(i, j int) bool {
		keyI := fmt.Sprintf("%s:%s:%s", sortedSamples[i].OriginDevicePK, sortedSamples[i].TargetDevicePK, sortedSamples[i].LinkPK)
		keyJ := fmt.Sprintf("%s:%s:%s", sortedSamples[j].OriginDevicePK, sortedSamples[j].TargetDevicePK, sortedSamples[j].LinkPK)
		if keyI != keyJ {
			return keyI < keyJ
		}
		if sortedSamples[i].Epoch != sortedSamples[j].Epoch {
			return sortedSamples[i].Epoch < sortedSamples[j].Epoch
		}
		return sortedSamples[i].SampleIndex < sortedSamples[j].SampleIndex
	})

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	// Collect unique circuits from the batch
	circuitSet := make(map[string]struct {
		originDevicePK, targetDevicePK, linkPK string
	})
	for _, sample := range sortedSamples {
		key := fmt.Sprintf("%s:%s:%s", sample.OriginDevicePK, sample.TargetDevicePK, sample.LinkPK)
		if _, ok := circuitSet[key]; !ok {
			circuitSet[key] = struct {
				originDevicePK, targetDevicePK, linkPK string
			}{sample.OriginDevicePK, sample.TargetDevicePK, sample.LinkPK}
		}
	}

	// Batch query previous RTTs for all circuits
	circuits := make([]struct {
		originDevicePK, targetDevicePK, linkPK string
	}, 0, len(circuitSet))
	for _, circuit := range circuitSet {
		circuits = append(circuits, circuit)
	}

	prevRTTsFromDB, err := s.getPreviousDeviceLinkRTTBatch(ctx, conn, circuits)
	if err != nil {
		// Log but don't fail - we'll just have NULL IPDV for first samples
		s.log.Debug("could not batch query previous RTTs (will use NULL, can backfill later)", "error", err)
		prevRTTsFromDB = make(map[string]uint32)
	}

	// Track previous RTT for each circuit (from DB + within batch)
	prevRTTs := make(map[string]uint32)
	// Initialize with values from DB
	for key, rtt := range prevRTTsFromDB {
		prevRTTs[key] = rtt
	}

	cfg := FactTableConfigDeviceLinkLatencySamples()
	return duck.InsertFactsViaCSV(
		ctx,
		s.log,
		conn,
		cfg,
		len(sortedSamples),
		func(w *csv.Writer, i int) error {
			sample := sortedSamples[i]
			loss := "false"
			if sample.RTTMicroseconds == 0 {
				loss = "true"
			}

			// Calculate IPDV: absolute difference from previous RTT
			// Previous RTT can come from DB (batch queried) or from within this batch
			var ipdvStr string
			key := fmt.Sprintf("%s:%s:%s", sample.OriginDevicePK, sample.TargetDevicePK, sample.LinkPK)
			if sample.RTTMicroseconds > 0 {
				// Check if we have a previous RTT (from DB or earlier in batch)
				if prevRTT, ok := prevRTTs[key]; ok && prevRTT > 0 {
					var ipdv uint32
					if sample.RTTMicroseconds > prevRTT {
						ipdv = sample.RTTMicroseconds - prevRTT
					} else {
						ipdv = prevRTT - sample.RTTMicroseconds
					}
					ipdvStr = fmt.Sprintf("%d", ipdv)
				} else {
					ipdvStr = "" // NULL for first sample of circuit (no previous RTT found)
				}
				// Update previous RTT for next sample in same circuit
				prevRTTs[key] = sample.RTTMicroseconds
			} else {
				ipdvStr = "" // NULL for loss
			}

			return w.Write([]string{
				sample.Time.UTC().Format(time.RFC3339Nano),
				fmt.Sprintf("%d", sample.Epoch),
				fmt.Sprintf("%d", sample.SampleIndex),
				sample.OriginDevicePK,
				sample.TargetDevicePK,
				sample.LinkPK,
				fmt.Sprintf("%d", sample.RTTMicroseconds),
				loss,
				ipdvStr,
			})
		},
	)
}

// getPreviousInternetMetroRTTBatch gets the most recent RTT for multiple internet metro circuits in one query
func (s *Store) getPreviousInternetMetroRTTBatch(ctx context.Context, conn duck.Connection, circuits []struct {
	originMetroPK, targetMetroPK, dataProvider string
}) (map[string]uint32, error) {
	if len(circuits) == 0 {
		return make(map[string]uint32), nil
	}

	// Build query with IN clauses for each circuit
	// Use DISTINCT ON to get the latest RTT for each circuit
	query := `SELECT DISTINCT ON (origin_metro_pk, target_metro_pk, data_provider)
		origin_metro_pk, target_metro_pk, data_provider, rtt_us
		FROM dz_internet_metro_latency_samples_raw
		WHERE rtt_us > 0
		AND (`

	conditions := make([]string, 0, len(circuits))
	args := make([]any, 0, len(circuits)*3)
	argIdx := 1
	for _, circuit := range circuits {
		conditions = append(conditions, fmt.Sprintf("(origin_metro_pk = $%d AND target_metro_pk = $%d AND data_provider = $%d)", argIdx, argIdx+1, argIdx+2))
		args = append(args, circuit.originMetroPK, circuit.targetMetroPK, circuit.dataProvider)
		argIdx += 3
	}
	query += strings.Join(conditions, " OR ")
	query += `)
		ORDER BY origin_metro_pk, target_metro_pk, data_provider, epoch DESC, sample_index DESC`

	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query previous RTTs: %w", err)
	}
	defer rows.Close()

	result := make(map[string]uint32)
	for rows.Next() {
		var originMetroPK, targetMetroPK, dataProvider string
		var rtt sql.NullInt64
		if err := rows.Scan(&originMetroPK, &targetMetroPK, &dataProvider, &rtt); err != nil {
			return nil, fmt.Errorf("failed to scan previous RTT: %w", err)
		}
		if rtt.Valid && rtt.Int64 > 0 {
			key := fmt.Sprintf("%s:%s:%s", originMetroPK, targetMetroPK, dataProvider)
			result[key] = uint32(rtt.Int64)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating previous RTTs: %w", err)
	}

	return result, nil
}

func (s *Store) AppendInternetMetroLatencySamples(ctx context.Context, samples []InternetMetroLatencySample) error {
	if len(samples) == 0 {
		return nil
	}

	// Sort samples by circuit, then by epoch, then by sample_index
	sortedSamples := make([]InternetMetroLatencySample, len(samples))
	copy(sortedSamples, samples)
	sort.Slice(sortedSamples, func(i, j int) bool {
		keyI := fmt.Sprintf("%s:%s:%s", sortedSamples[i].OriginMetroPK, sortedSamples[i].TargetMetroPK, sortedSamples[i].DataProvider)
		keyJ := fmt.Sprintf("%s:%s:%s", sortedSamples[j].OriginMetroPK, sortedSamples[j].TargetMetroPK, sortedSamples[j].DataProvider)
		if keyI != keyJ {
			return keyI < keyJ
		}
		if sortedSamples[i].Epoch != sortedSamples[j].Epoch {
			return sortedSamples[i].Epoch < sortedSamples[j].Epoch
		}
		return sortedSamples[i].SampleIndex < sortedSamples[j].SampleIndex
	})

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	// Collect unique circuits from the batch
	circuitSet := make(map[string]struct {
		originMetroPK, targetMetroPK, dataProvider string
	})
	for _, sample := range sortedSamples {
		key := fmt.Sprintf("%s:%s:%s", sample.OriginMetroPK, sample.TargetMetroPK, sample.DataProvider)
		if _, ok := circuitSet[key]; !ok {
			circuitSet[key] = struct {
				originMetroPK, targetMetroPK, dataProvider string
			}{sample.OriginMetroPK, sample.TargetMetroPK, sample.DataProvider}
		}
	}

	// Batch query previous RTTs for all circuits
	circuits := make([]struct {
		originMetroPK, targetMetroPK, dataProvider string
	}, 0, len(circuitSet))
	for _, circuit := range circuitSet {
		circuits = append(circuits, circuit)
	}

	prevRTTsFromDB, err := s.getPreviousInternetMetroRTTBatch(ctx, conn, circuits)
	if err != nil {
		// Log but don't fail - we'll just have NULL IPDV for first samples
		s.log.Debug("could not batch query previous RTTs (will use NULL, can backfill later)", "error", err)
		prevRTTsFromDB = make(map[string]uint32)
	}

	// Track previous RTT for each circuit (from DB + within batch)
	prevRTTs := make(map[string]uint32)
	// Initialize with values from DB
	for key, rtt := range prevRTTsFromDB {
		prevRTTs[key] = rtt
	}

	cfg := FactTableConfigInternetMetroLatencySamples()
	return duck.InsertFactsViaCSV(
		ctx,
		s.log,
		conn,
		cfg,
		len(sortedSamples),
		func(w *csv.Writer, i int) error {
			sample := sortedSamples[i]

			// Calculate IPDV: absolute difference from previous RTT
			// Previous RTT can come from DB (batch queried) or from within this batch
			var ipdvStr string
			key := fmt.Sprintf("%s:%s:%s", sample.OriginMetroPK, sample.TargetMetroPK, sample.DataProvider)
			if sample.RTTMicroseconds > 0 {
				// Check if we have a previous RTT (from DB or earlier in batch)
				if prevRTT, ok := prevRTTs[key]; ok && prevRTT > 0 {
					var ipdv uint32
					if sample.RTTMicroseconds > prevRTT {
						ipdv = sample.RTTMicroseconds - prevRTT
					} else {
						ipdv = prevRTT - sample.RTTMicroseconds
					}
					ipdvStr = fmt.Sprintf("%d", ipdv)
				} else {
					ipdvStr = "" // NULL for first sample of circuit (no previous RTT found)
				}
				// Update previous RTT for next sample in same circuit
				prevRTTs[key] = sample.RTTMicroseconds
			} else {
				ipdvStr = "" // NULL for loss
			}

			return w.Write([]string{
				sample.Time.UTC().Format(time.RFC3339Nano),
				fmt.Sprintf("%d", sample.Epoch),
				fmt.Sprintf("%d", sample.SampleIndex),
				sample.OriginMetroPK,
				sample.TargetMetroPK,
				sample.DataProvider,
				fmt.Sprintf("%d", sample.RTTMicroseconds),
				ipdvStr,
			})
		},
	)
}

func (s *Store) GetExistingMaxSampleIndices() (map[string]int, error) {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	query := `SELECT origin_device_pk, target_device_pk, link_pk, epoch, MAX(sample_index) as max_idx
	          FROM dz_device_link_latency_samples_raw
	          GROUP BY origin_device_pk, target_device_pk, link_pk, epoch`
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing max indices: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var originDevicePK, targetDevicePK, linkPK string
		var epoch uint64
		var maxIdx sql.NullInt64
		if err := rows.Scan(&originDevicePK, &targetDevicePK, &linkPK, &epoch, &maxIdx); err != nil {
			return nil, fmt.Errorf("failed to scan max index: %w", err)
		}
		if maxIdx.Valid {
			key := fmt.Sprintf("%s:%s:%s:%d", originDevicePK, targetDevicePK, linkPK, epoch)
			result[key] = int(maxIdx.Int64)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating max indices: %w", err)
	}
	return result, nil
}

func (s *Store) GetExistingInternetMaxSampleIndices() (map[string]int, error) {
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	query := `SELECT origin_metro_pk, target_metro_pk, data_provider, epoch, MAX(sample_index) as max_idx
	          FROM dz_internet_metro_latency_samples_raw
	          GROUP BY origin_metro_pk, target_metro_pk, data_provider, epoch`
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing max indices: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var originMetroPK, targetMetroPK, dataProvider string
		var epoch uint64
		var maxIdx sql.NullInt64
		if err := rows.Scan(&originMetroPK, &targetMetroPK, &dataProvider, &epoch, &maxIdx); err != nil {
			return nil, fmt.Errorf("failed to scan max index: %w", err)
		}
		if maxIdx.Valid {
			key := fmt.Sprintf("%s:%s:%s:%d", originMetroPK, targetMetroPK, dataProvider, epoch)
			result[key] = int(maxIdx.Int64)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating max indices: %w", err)
	}
	return result, nil
}

// BackfillIPDVDeviceLink backfills the ipdv_us column for device link latency samples.
// IPDV is calculated as the absolute difference between consecutive RTT measurements
// for the same circuit (origin_device_pk, target_device_pk, link_pk).
// Processes data day-by-day to work efficiently with partitioned tables.
func BackfillIPDVDeviceLink(ctx context.Context, log *slog.Logger, conn duck.Connection, dryRun bool) (int64, error) {
	// Get distinct days from the table
	daysSQL := `SELECT DISTINCT DATE(time) AS day
		FROM dz_device_link_latency_samples_raw
		WHERE rtt_us > 0 AND ipdv_us IS NULL
		ORDER BY day`
	rows, err := conn.QueryContext(ctx, daysSQL)
	if err != nil {
		return 0, fmt.Errorf("failed to get distinct days: %w", err)
	}
	defer rows.Close()

	var days []time.Time
	for rows.Next() {
		var day time.Time
		if err := rows.Scan(&day); err != nil {
			return 0, fmt.Errorf("failed to scan day: %w", err)
		}
		days = append(days, day)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("error iterating days: %w", err)
	}

	if len(days) == 0 {
		log.Info("no days to process for IPDV backfill")
		return 0, nil
	}

	log.Info("processing IPDV backfill day-by-day", "total_days", len(days), "dry_run", dryRun)

	var totalUpdated int64
	for i, day := range days {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return totalUpdated, ctx.Err()
		default:
		}

		dayStart := day.Format("2006-01-02")

		log.Info("processing day", "day", dayStart, "progress", fmt.Sprintf("%d/%d", i+1, len(days)))

		if dryRun {
			// Count rows for this day
			countSQL := `SELECT COUNT(*)
				FROM dz_device_link_latency_samples_raw
				WHERE DATE(time) = $1
				AND rtt_us > 0
				AND ipdv_us IS NULL`
			var count int64
			if err := conn.QueryRowContext(ctx, countSQL, dayStart).Scan(&count); err != nil {
				log.Warn("failed to count rows for day", "day", dayStart, "error", err)
				continue
			}
			totalUpdated += count
			log.Info("day dry run", "day", dayStart, "rows_to_update", count)
			continue
		}

		// Use MERGE for the specific day partition
		// Include previous day in window function to handle first sample of each circuit
		updateSQL := `MERGE INTO dz_device_link_latency_samples_raw AS target
		USING (
			SELECT
				time,
				epoch,
				sample_index,
				origin_device_pk,
				target_device_pk,
				link_pk,
				ABS(rtt_us - LAG(rtt_us) OVER (
					PARTITION BY origin_device_pk, target_device_pk, link_pk
					ORDER BY epoch, sample_index
				)) AS ipdv
			FROM dz_device_link_latency_samples_raw
			WHERE DATE(time) >= CAST($1 AS DATE) - INTERVAL '1 day'
			AND DATE(time) <= CAST($1 AS DATE)
			AND rtt_us > 0
		) AS source
		ON target.time = source.time
		AND target.epoch = source.epoch
		AND target.sample_index = source.sample_index
		AND target.origin_device_pk = source.origin_device_pk
		AND target.target_device_pk = source.target_device_pk
		AND target.link_pk = source.link_pk
		AND DATE(target.time) = CAST($1 AS DATE)
		WHEN MATCHED AND target.ipdv_us IS NULL AND source.ipdv IS NOT NULL THEN
			UPDATE SET ipdv_us = source.ipdv`

		result, err := conn.ExecContext(ctx, updateSQL, dayStart)
		if err != nil {
			log.Warn("failed to update IPDV for day", "day", dayStart, "error", err)
			continue
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			// Count updated rows for this day
			var count int64
			countSQL := `SELECT COUNT(*)
				FROM dz_device_link_latency_samples_raw
				WHERE DATE(time) = $1
				AND ipdv_us IS NOT NULL`
			if err := conn.QueryRowContext(ctx, countSQL, dayStart).Scan(&count); err != nil {
				log.Warn("failed to count updated rows for day", "day", dayStart, "error", err)
			} else {
				rowsAffected = count
			}
		}

		totalUpdated += rowsAffected
		log.Info("completed day", "day", dayStart, "rows_updated", rowsAffected, "total_updated", totalUpdated)
	}

	log.Info("backfilled IPDV device link", "total_rows_updated", totalUpdated, "days_processed", len(days))
	return totalUpdated, nil
}

// BackfillIPDVInternetMetro backfills the ipdv_us column for internet metro latency samples.
// IPDV is calculated as the absolute difference between consecutive RTT measurements
// for the same circuit (origin_metro_pk, target_metro_pk, data_provider).
// Processes data day-by-day to work efficiently with partitioned tables.
func BackfillIPDVInternetMetro(ctx context.Context, log *slog.Logger, conn duck.Connection, dryRun bool) (int64, error) {
	// Get distinct days from the table
	daysSQL := `SELECT DISTINCT DATE(time) AS day
		FROM dz_internet_metro_latency_samples_raw
		WHERE rtt_us > 0 AND ipdv_us IS NULL
		ORDER BY day`
	rows, err := conn.QueryContext(ctx, daysSQL)
	if err != nil {
		return 0, fmt.Errorf("failed to get distinct days: %w", err)
	}
	defer rows.Close()

	var days []time.Time
	for rows.Next() {
		var day time.Time
		if err := rows.Scan(&day); err != nil {
			return 0, fmt.Errorf("failed to scan day: %w", err)
		}
		days = append(days, day)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("error iterating days: %w", err)
	}

	if len(days) == 0 {
		log.Info("no days to process for IPDV backfill")
		return 0, nil
	}

	log.Info("processing IPDV backfill day-by-day", "total_days", len(days), "dry_run", dryRun)

	var totalUpdated int64
	for i, day := range days {
		// Check for context cancellation
		select {
		case <-ctx.Done():
			return totalUpdated, ctx.Err()
		default:
		}

		dayStart := day.Format("2006-01-02")

		log.Info("processing day", "day", dayStart, "progress", fmt.Sprintf("%d/%d", i+1, len(days)))

		if dryRun {
			// Count rows for this day
			countSQL := `SELECT COUNT(*)
				FROM dz_internet_metro_latency_samples_raw
				WHERE DATE(time) = $1
				AND rtt_us > 0
				AND ipdv_us IS NULL`
			var count int64
			if err := conn.QueryRowContext(ctx, countSQL, dayStart).Scan(&count); err != nil {
				log.Warn("failed to count rows for day", "day", dayStart, "error", err)
				continue
			}
			totalUpdated += count
			log.Info("day dry run", "day", dayStart, "rows_to_update", count)
			continue
		}

		// Use MERGE for the specific day partition
		// Include previous day in window function to handle first sample of each circuit
		updateSQL := `MERGE INTO dz_internet_metro_latency_samples_raw AS target
		USING (
			SELECT
				time,
				epoch,
				sample_index,
				origin_metro_pk,
				target_metro_pk,
				data_provider,
				ABS(rtt_us - LAG(rtt_us) OVER (
					PARTITION BY origin_metro_pk, target_metro_pk, data_provider
					ORDER BY epoch, sample_index
				)) AS ipdv
			FROM dz_internet_metro_latency_samples_raw
			WHERE DATE(time) >= CAST($1 AS DATE) - INTERVAL '1 day'
			AND DATE(time) <= CAST($1 AS DATE)
			AND rtt_us > 0
		) AS source
		ON target.time = source.time
		AND target.epoch = source.epoch
		AND target.sample_index = source.sample_index
		AND target.origin_metro_pk = source.origin_metro_pk
		AND target.target_metro_pk = source.target_metro_pk
		AND target.data_provider = source.data_provider
		AND DATE(target.time) = CAST($1 AS DATE)
		WHEN MATCHED AND target.ipdv_us IS NULL AND source.ipdv IS NOT NULL THEN
			UPDATE SET ipdv_us = source.ipdv`

		result, err := conn.ExecContext(ctx, updateSQL, dayStart)
		if err != nil {
			log.Warn("failed to update IPDV for day", "day", dayStart, "error", err)
			continue
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			// Count updated rows for this day
			var count int64
			countSQL := `SELECT COUNT(*)
				FROM dz_internet_metro_latency_samples_raw
				WHERE DATE(time) = $1
				AND ipdv_us IS NOT NULL`
			if err := conn.QueryRowContext(ctx, countSQL, dayStart).Scan(&count); err != nil {
				log.Warn("failed to count updated rows for day", "day", dayStart, "error", err)
			} else {
				rowsAffected = count
			}
		}

		totalUpdated += rowsAffected
		log.Info("completed day", "day", dayStart, "rows_updated", rowsAffected, "total_updated", totalUpdated)
	}

	log.Info("backfilled IPDV internet metro", "total_rows_updated", totalUpdated, "days_processed", len(days))
	return totalUpdated, nil
}
