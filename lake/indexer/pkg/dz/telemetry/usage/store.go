package dztelemusage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

// InterfaceUsage represents a single interface usage measurement
type InterfaceUsage struct {
	Time               time.Time
	DevicePK           *string
	Host               *string
	Intf               *string
	UserTunnelID       *int64
	LinkPK             *string
	LinkSide           *string // "A" or "Z"
	ModelName          *string
	SerialNumber       *string
	CarrierTransitions *int64
	InBroadcastPkts    *int64
	InDiscards         *int64
	InErrors           *int64
	InFCSErrors        *int64
	InMulticastPkts    *int64
	InOctets           *int64
	InPkts             *int64
	InUnicastPkts      *int64
	OutBroadcastPkts   *int64
	OutDiscards        *int64
	OutErrors          *int64
	OutMulticastPkts   *int64
	OutOctets          *int64
	OutPkts            *int64
	OutUnicastPkts     *int64
	// Delta fields (change from previous value)
	CarrierTransitionsDelta *int64
	InBroadcastPktsDelta    *int64
	InDiscardsDelta         *int64
	InErrorsDelta           *int64
	InFCSErrorsDelta        *int64
	InMulticastPktsDelta    *int64
	InOctetsDelta           *int64
	InPktsDelta             *int64
	InUnicastPktsDelta      *int64
	OutBroadcastPktsDelta   *int64
	OutDiscardsDelta        *int64
	OutErrorsDelta          *int64
	OutMulticastPktsDelta   *int64
	OutOctetsDelta          *int64
	OutPktsDelta            *int64
	OutUnicastPktsDelta     *int64
	// DeltaDuration is the time difference in seconds between this measurement and the previous one
	DeltaDuration *float64
}

// GetMaxTimestamp returns the maximum timestamp in the table, or nil if the table is empty
func (s *Store) GetMaxTimestamp(ctx context.Context) (*time.Time, error) {
	// Check for context cancellation before querying
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var maxTime *time.Time
	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	rows, err := conn.Query(ctx, "SELECT max(event_ts) FROM fact_dz_device_interface_counters")
	if err != nil {
		return nil, fmt.Errorf("failed to query max timestamp: %w", err)
	}
	defer rows.Close()

	if rows.Next() {
		var scannedTime time.Time
		if err := rows.Scan(&scannedTime); err != nil {
			return nil, fmt.Errorf("failed to scan max timestamp: %w", err)
		}
		// ClickHouse max() returns zero time (1970-01-01 00:00:00 UTC) for empty tables, not NULL
		// Check if it's the Unix epoch zero time to determine if table is empty
		zeroTime := time.Unix(0, 0).UTC()
		if scannedTime.After(zeroTime) {
			maxTime = &scannedTime
		}
	}

	return maxTime, nil
}

func (s *Store) InsertInterfaceUsage(ctx context.Context, usage []InterfaceUsage) error {
	ds, err := NewDeviceInterfaceCountersDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create dataset: %w", err)
	}

	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	// Write to ClickHouse
	ingestedAt := time.Now().UTC()
	if err := ds.WriteBatch(ctx, conn, len(usage), func(i int) ([]any, error) {
		return deviceInterfaceCountersSchema.ToRow(usage[i], ingestedAt), nil
	}); err != nil {
		return fmt.Errorf("failed to write interface usage to ClickHouse: %w", err)
	}

	return nil
}

// MaxTimestampsByKey maps "device_pk:intf" to the max event_ts already written for that key
type MaxTimestampsByKey map[string]time.Time

// GetMaxTimestampsByKey returns the max event_ts per (device_pk, intf) for rows at or after startTime
// This is used to skip re-writing duplicate rows during refresh with overlap
func (s *Store) GetMaxTimestampsByKey(ctx context.Context, startTime time.Time) (MaxTimestampsByKey, error) {
	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	query := `
		SELECT
			device_pk,
			intf,
			max(event_ts) as max_ts
		FROM fact_dz_device_interface_counters
		WHERE event_ts >= ?
		GROUP BY device_pk, intf
	`

	rows, err := conn.Query(ctx, query, startTime)
	if err != nil {
		return nil, fmt.Errorf("failed to query max timestamps by key: %w", err)
	}
	defer rows.Close()

	result := make(MaxTimestampsByKey)
	for rows.Next() {
		var devicePK, intf string
		var maxTS time.Time
		if err := rows.Scan(&devicePK, &intf, &maxTS); err != nil {
			return nil, fmt.Errorf("failed to scan max timestamp row: %w", err)
		}
		key := fmt.Sprintf("%s:%s", devicePK, intf)
		result[key] = maxTS
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating max timestamp rows: %w", err)
	}

	return result, nil
}

// DataBoundaries contains min/max timestamps for a fact table
type DataBoundaries struct {
	MinTime  *time.Time
	MaxTime  *time.Time
	RowCount uint64
}

// GetDataBoundaries returns the data boundaries for the device interface counters fact table
func (s *Store) GetDataBoundaries(ctx context.Context) (*DataBoundaries, error) {
	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	query := `SELECT
		min(event_ts) as min_ts,
		max(event_ts) as max_ts,
		count() as row_count
	FROM fact_dz_device_interface_counters`

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query data boundaries: %w", err)
	}
	defer rows.Close()

	bounds := &DataBoundaries{}
	if rows.Next() {
		var minTS, maxTS time.Time
		var rowCount uint64
		if err := rows.Scan(&minTS, &maxTS, &rowCount); err != nil {
			return nil, fmt.Errorf("failed to scan data boundaries: %w", err)
		}
		bounds.RowCount = rowCount
		// ClickHouse returns zero time for empty tables
		zeroTime := time.Unix(0, 0).UTC()
		if minTS.After(zeroTime) {
			bounds.MinTime = &minTS
			bounds.MaxTime = &maxTS
		}
	}

	return bounds, nil
}
