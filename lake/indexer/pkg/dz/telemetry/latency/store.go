package dztelemlatency

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
)

type StoreConfig struct {
	Logger     *slog.Logger
	ClickHouse clickhouse.Client
}

func (cfg *StoreConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.ClickHouse == nil {
		return errors.New("clickhouse connection is required")
	}
	return nil
}

type Store struct {
	log *slog.Logger
	cfg StoreConfig
}

func NewStore(cfg StoreConfig) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Store{
		log: cfg.Logger,
		cfg: cfg,
	}, nil
}

// getPreviousDeviceLinkRTTBatch gets the most recent RTT for multiple device link circuits in one query
func (s *Store) getPreviousDeviceLinkRTTBatch(ctx context.Context, circuits []struct {
	originDevicePK, targetDevicePK, linkPK string
}) (map[string]uint32, error) {
	if len(circuits) == 0 {
		return make(map[string]uint32), nil
	}

	// Build query with IN clauses for each circuit
	// Use argMax to get the latest RTT for each circuit
	conditions := make([]string, 0, len(circuits))
	args := make([]any, 0, len(circuits)*3)
	for _, circuit := range circuits {
		conditions = append(conditions, "(origin_device_pk = ? AND target_device_pk = ? AND link_pk = ?)")
		args = append(args, circuit.originDevicePK, circuit.targetDevicePK, circuit.linkPK)
	}

	query := fmt.Sprintf(`SELECT
		origin_device_pk,
		target_device_pk,
		link_pk,
		argMax(rtt_us, (epoch, sample_index, event_ts)) as rtt_us
		FROM (
			SELECT origin_device_pk, target_device_pk, link_pk, rtt_us, epoch, sample_index, event_ts
			FROM fact_dz_device_link_latency
			WHERE rtt_us > 0
			AND (%s)
		)
		GROUP BY origin_device_pk, target_device_pk, link_pk`,
		strings.Join(conditions, " OR "))

	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query previous RTTs: %w", err)
	}
	defer rows.Close()

	result := make(map[string]uint32)
	for rows.Next() {
		var originDevicePK, targetDevicePK, linkPK string
		var rtt *int64
		if err := rows.Scan(&originDevicePK, &targetDevicePK, &linkPK, &rtt); err != nil {
			return nil, fmt.Errorf("failed to scan previous RTT: %w", err)
		}
		if rtt != nil && *rtt > 0 {
			key := fmt.Sprintf("%s:%s:%s", originDevicePK, targetDevicePK, linkPK)
			result[key] = uint32(*rtt)
		}
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

	prevRTTsFromDB, err := s.getPreviousDeviceLinkRTTBatch(ctx, circuits)
	if err != nil {
		// Query errors should fail - no data is handled by empty result
		return fmt.Errorf("failed to query previous RTTs: %w", err)
	}

	// Track previous RTT for each circuit (from DB + within batch)
	prevRTTs := make(map[string]uint32)
	// Initialize with values from DB
	for key, rtt := range prevRTTsFromDB {
		prevRTTs[key] = rtt
	}

	ds, err := NewDeviceLinkLatencyDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create dataset: %w", err)
	}

	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	// Write to ClickHouse
	ingestedAt := time.Now().UTC()
	if err := ds.WriteBatch(ctx, conn, len(sortedSamples), func(idx int) ([]any, error) {
		sample := sortedSamples[idx]
		loss := sample.RTTMicroseconds == 0

		// Calculate IPDV: absolute difference from previous RTT
		var ipdv *int64
		key := fmt.Sprintf("%s:%s:%s", sample.OriginDevicePK, sample.TargetDevicePK, sample.LinkPK)
		if sample.RTTMicroseconds > 0 {
			if prevRTT, ok := prevRTTs[key]; ok && prevRTT > 0 {
				var ipdvVal uint32
				if sample.RTTMicroseconds > prevRTT {
					ipdvVal = sample.RTTMicroseconds - prevRTT
				} else {
					ipdvVal = prevRTT - sample.RTTMicroseconds
				}
				ipdvValInt64 := int64(ipdvVal)
				ipdv = &ipdvValInt64
			}
			prevRTTs[key] = sample.RTTMicroseconds
		}

		return []any{
			sample.Time.UTC(), // event_ts
			ingestedAt,        // ingested_at
			sample.Epoch,
			sample.SampleIndex,
			sample.OriginDevicePK,
			sample.TargetDevicePK,
			sample.LinkPK,
			sample.RTTMicroseconds,
			loss,
			ipdv,
		}, nil
	}); err != nil {
		return fmt.Errorf("failed to write device link latency samples to ClickHouse: %w", err)
	}

	return nil
}

// getPreviousInternetMetroRTTBatch gets the most recent RTT for multiple internet metro circuits in one query
func (s *Store) getPreviousInternetMetroRTTBatch(ctx context.Context, circuits []struct {
	originMetroPK, targetMetroPK, dataProvider string
}) (map[string]uint32, error) {
	if len(circuits) == 0 {
		return make(map[string]uint32), nil
	}

	// Build query with IN clauses for each circuit
	conditions := make([]string, 0, len(circuits))
	args := make([]any, 0, len(circuits)*3)
	for _, circuit := range circuits {
		conditions = append(conditions, "(origin_metro_pk = ? AND target_metro_pk = ? AND data_provider = ?)")
		args = append(args, circuit.originMetroPK, circuit.targetMetroPK, circuit.dataProvider)
	}

	query := fmt.Sprintf(`SELECT
		origin_metro_pk,
		target_metro_pk,
		data_provider,
		argMax(rtt_us, (epoch, sample_index, event_ts)) as rtt_us
		FROM (
			SELECT origin_metro_pk, target_metro_pk, data_provider, rtt_us, epoch, sample_index, event_ts
			FROM fact_dz_internet_metro_latency
			WHERE rtt_us > 0
			AND (%s)
		)
		GROUP BY origin_metro_pk, target_metro_pk, data_provider`,
		strings.Join(conditions, " OR "))

	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	rows, err := conn.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query previous RTTs: %w", err)
	}
	defer rows.Close()

	result := make(map[string]uint32)
	for rows.Next() {
		var originMetroPK, targetMetroPK, dataProvider string
		var rtt *int64
		if err := rows.Scan(&originMetroPK, &targetMetroPK, &dataProvider, &rtt); err != nil {
			return nil, fmt.Errorf("failed to scan previous RTT: %w", err)
		}
		if rtt != nil && *rtt > 0 {
			key := fmt.Sprintf("%s:%s:%s", originMetroPK, targetMetroPK, dataProvider)
			result[key] = uint32(*rtt)
		}
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

	prevRTTsFromDB, err := s.getPreviousInternetMetroRTTBatch(ctx, circuits)
	if err != nil {
		// Query errors should fail - no data is handled by empty result
		return fmt.Errorf("failed to query previous RTTs: %w", err)
	}

	// Track previous RTT for each circuit (from DB + within batch)
	prevRTTs := make(map[string]uint32)
	// Initialize with values from DB
	for key, rtt := range prevRTTsFromDB {
		prevRTTs[key] = rtt
	}

	ds, err := NewInternetMetroLatencyDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create dataset: %w", err)
	}

	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	// Write to ClickHouse
	ingestedAt := time.Now().UTC()
	if err := ds.WriteBatch(ctx, conn, len(sortedSamples), func(idx int) ([]any, error) {
		sample := sortedSamples[idx]

		// Calculate IPDV: absolute difference from previous RTT
		var ipdv *int64
		key := fmt.Sprintf("%s:%s:%s", sample.OriginMetroPK, sample.TargetMetroPK, sample.DataProvider)
		if sample.RTTMicroseconds > 0 {
			if prevRTT, ok := prevRTTs[key]; ok && prevRTT > 0 {
				var ipdvVal uint32
				if sample.RTTMicroseconds > prevRTT {
					ipdvVal = sample.RTTMicroseconds - prevRTT
				} else {
					ipdvVal = prevRTT - sample.RTTMicroseconds
				}
				ipdvValInt64 := int64(ipdvVal)
				ipdv = &ipdvValInt64
			}
			prevRTTs[key] = sample.RTTMicroseconds
		}

		return []any{
			sample.Time.UTC(), // event_ts
			ingestedAt,        // ingested_at
			sample.Epoch,
			sample.SampleIndex,
			sample.OriginMetroPK,
			sample.TargetMetroPK,
			sample.DataProvider,
			sample.RTTMicroseconds,
			ipdv,
		}, nil
	}); err != nil {
		return fmt.Errorf("failed to write internet metro latency samples to ClickHouse: %w", err)
	}

	return nil
}

func (s *Store) GetExistingMaxSampleIndices() (map[string]int, error) {
	ctx := context.Background()
	// Filter to last 4 days to enable partition pruning - we only need current and previous epoch
	// Solana epochs are ~2-3 days, so 4 days covers both epochs we fetch
	query := `SELECT origin_device_pk, target_device_pk, link_pk, epoch, max(sample_index) as max_idx
	          FROM fact_dz_device_link_latency
	          GROUP BY origin_device_pk, target_device_pk, link_pk, epoch`
	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing max indices: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var originDevicePK, targetDevicePK, linkPK string
		var epoch int64
		var maxIdx *int32
		if err := rows.Scan(&originDevicePK, &targetDevicePK, &linkPK, &epoch, &maxIdx); err != nil {
			return nil, fmt.Errorf("failed to scan max index: %w", err)
		}
		if maxIdx != nil {
			key := fmt.Sprintf("%s:%s:%s:%d", originDevicePK, targetDevicePK, linkPK, epoch)
			result[key] = int(*maxIdx)
		}
	}
	return result, nil
}

func (s *Store) GetExistingInternetMaxSampleIndices() (map[string]int, error) {
	ctx := context.Background()
	// Query all existing samples grouped by circuit and epoch to determine what's already been inserted
	// This enables incremental appends by only inserting new samples (sample_index > existing max)
	query := `SELECT origin_metro_pk, target_metro_pk, data_provider, epoch, max(sample_index) as max_idx
	          FROM fact_dz_internet_metro_latency
	          GROUP BY origin_metro_pk, target_metro_pk, data_provider, epoch`
	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing max indices: %w", err)
	}
	defer rows.Close()

	result := make(map[string]int)
	for rows.Next() {
		var originMetroPK, targetMetroPK, dataProvider string
		var epoch int64
		var maxIdx *int32
		if err := rows.Scan(&originMetroPK, &targetMetroPK, &dataProvider, &epoch, &maxIdx); err != nil {
			return nil, fmt.Errorf("failed to scan max index: %w", err)
		}
		if maxIdx != nil {
			// Convert int64 epoch to uint64 for key consistency (epoch is stored as int64 in DB but used as uint64 elsewhere)
			key := fmt.Sprintf("%s:%s:%s:%d", originMetroPK, targetMetroPK, dataProvider, uint64(epoch))
			result[key] = int(*maxIdx)
		}
	}
	return result, nil
}

// DataBoundaries contains min/max timestamps and epochs for a fact table
type DataBoundaries struct {
	MinTime  *time.Time
	MaxTime  *time.Time
	MinEpoch *int64
	MaxEpoch *int64
	RowCount uint64
}

// GetDeviceLinkLatencyBoundaries returns the data boundaries for the device link latency fact table
func (s *Store) GetDeviceLinkLatencyBoundaries(ctx context.Context) (*DataBoundaries, error) {
	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	query := `SELECT
		min(event_ts) as min_ts,
		max(event_ts) as max_ts,
		min(epoch) as min_epoch,
		max(epoch) as max_epoch,
		count() as row_count
	FROM fact_dz_device_link_latency`

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query data boundaries: %w", err)
	}
	defer rows.Close()

	bounds := &DataBoundaries{}
	if rows.Next() {
		var minTS, maxTS time.Time
		var minEpoch, maxEpoch int64
		var rowCount uint64
		if err := rows.Scan(&minTS, &maxTS, &minEpoch, &maxEpoch, &rowCount); err != nil {
			return nil, fmt.Errorf("failed to scan data boundaries: %w", err)
		}
		bounds.RowCount = rowCount
		// ClickHouse returns zero time for empty tables
		zeroTime := time.Unix(0, 0).UTC()
		if minTS.After(zeroTime) {
			bounds.MinTime = &minTS
			bounds.MaxTime = &maxTS
			bounds.MinEpoch = &minEpoch
			bounds.MaxEpoch = &maxEpoch
		}
	}

	return bounds, nil
}

// GetInternetMetroLatencyBoundaries returns the data boundaries for the internet metro latency fact table
func (s *Store) GetInternetMetroLatencyBoundaries(ctx context.Context) (*DataBoundaries, error) {
	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	query := `SELECT
		min(event_ts) as min_ts,
		max(event_ts) as max_ts,
		min(epoch) as min_epoch,
		max(epoch) as max_epoch,
		count() as row_count
	FROM fact_dz_internet_metro_latency`

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query data boundaries: %w", err)
	}
	defer rows.Close()

	bounds := &DataBoundaries{}
	if rows.Next() {
		var minTS, maxTS time.Time
		var minEpoch, maxEpoch int64
		var rowCount uint64
		if err := rows.Scan(&minTS, &maxTS, &minEpoch, &maxEpoch, &rowCount); err != nil {
			return nil, fmt.Errorf("failed to scan data boundaries: %w", err)
		}
		bounds.RowCount = rowCount
		// ClickHouse returns zero time for empty tables
		zeroTime := time.Unix(0, 0).UTC()
		if minTS.After(zeroTime) {
			bounds.MinTime = &minTS
			bounds.MaxTime = &maxTS
			bounds.MinEpoch = &minEpoch
			bounds.MaxEpoch = &maxEpoch
		}
	}

	return bounds, nil
}
