package dztelemusage

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/mcp/duck"
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
	schema := `CREATE TABLE IF NOT EXISTS dz_device_iface_usage (
		time TIMESTAMP NOT NULL,
		device_pk VARCHAR,
		intf VARCHAR,
		user_tunnel_id BIGINT,
		link_pk VARCHAR,
		link_side VARCHAR,
		model_name VARCHAR,
		serial_number VARCHAR,
		carrier_transitions BIGINT,
		in_broadcast_pkts BIGINT,
		in_discards BIGINT,
		in_errors BIGINT,
		in_fcs_errors BIGINT,
		in_multicast_pkts BIGINT,
		in_octets BIGINT,
		in_pkts BIGINT,
		in_unicast_pkts BIGINT,
		out_broadcast_pkts BIGINT,
		out_discards BIGINT,
		out_errors BIGINT,
		out_multicast_pkts BIGINT,
		out_octets BIGINT,
		out_pkts BIGINT,
		out_unicast_pkts BIGINT,
		carrier_transitions_delta BIGINT,
		in_broadcast_pkts_delta BIGINT,
		in_discards_delta BIGINT,
		in_errors_delta BIGINT,
		in_fcs_errors_delta BIGINT,
		in_multicast_pkts_delta BIGINT,
		in_octets_delta BIGINT,
		in_pkts_delta BIGINT,
		in_unicast_pkts_delta BIGINT,
		out_broadcast_pkts_delta BIGINT,
		out_discards_delta BIGINT,
		out_errors_delta BIGINT,
		out_multicast_pkts_delta BIGINT,
		out_octets_delta BIGINT,
		out_pkts_delta BIGINT,
		out_unicast_pkts_delta BIGINT,
		delta_duration DOUBLE,
		PRIMARY KEY (time, device_pk, intf)
	)`

	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	return nil
}

// InterfaceUsage represents a single interface usage measurement
type InterfaceUsage struct {
	Time               time.Time
	DevicePK           *string
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

	var maxTime sql.NullTime
	err := s.db.QueryRow("SELECT MAX(time) FROM dz_device_iface_usage").Scan(&maxTime)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to query max timestamp: %w", err)
	}
	if !maxTime.Valid {
		return nil, nil // Table is empty
	}
	return &maxTime.Time, nil
}

func (s *Store) UpsertInterfaceUsage(ctx context.Context, usage []InterfaceUsage) error {
	return duck.UpsertTableViaCSV(ctx, s.log, s.db, "dz_device_iface_usage", len(usage), func(w *csv.Writer, i int) error {
		u := usage[i]
		record := make([]string, 41)

		// Time (required)
		record[0] = u.Time.Format(time.RFC3339Nano)

		// String fields (nullable)
		record[1] = formatNullableString(u.DevicePK)
		record[2] = formatNullableString(u.Intf)
		record[3] = formatNullableInt64(u.UserTunnelID)
		record[4] = formatNullableString(u.LinkPK)
		record[5] = formatNullableString(u.LinkSide)
		record[6] = formatNullableString(u.ModelName)
		record[7] = formatNullableString(u.SerialNumber)

		// Numeric fields (nullable) - raw values
		record[8] = formatNullableInt64(u.CarrierTransitions)
		record[9] = formatNullableInt64(u.InBroadcastPkts)
		record[10] = formatNullableInt64(u.InDiscards)
		record[11] = formatNullableInt64(u.InErrors)
		record[12] = formatNullableInt64(u.InFCSErrors)
		record[13] = formatNullableInt64(u.InMulticastPkts)
		record[14] = formatNullableInt64(u.InOctets)
		record[15] = formatNullableInt64(u.InPkts)
		record[16] = formatNullableInt64(u.InUnicastPkts)
		record[17] = formatNullableInt64(u.OutBroadcastPkts)
		record[18] = formatNullableInt64(u.OutDiscards)
		record[19] = formatNullableInt64(u.OutErrors)
		record[20] = formatNullableInt64(u.OutMulticastPkts)
		record[21] = formatNullableInt64(u.OutOctets)
		record[22] = formatNullableInt64(u.OutPkts)
		record[23] = formatNullableInt64(u.OutUnicastPkts)

		// Delta fields (nullable)
		record[24] = formatNullableInt64(u.CarrierTransitionsDelta)
		record[25] = formatNullableInt64(u.InBroadcastPktsDelta)
		record[26] = formatNullableInt64(u.InDiscardsDelta)
		record[27] = formatNullableInt64(u.InErrorsDelta)
		record[28] = formatNullableInt64(u.InFCSErrorsDelta)
		record[29] = formatNullableInt64(u.InMulticastPktsDelta)
		record[30] = formatNullableInt64(u.InOctetsDelta)
		record[31] = formatNullableInt64(u.InPktsDelta)
		record[32] = formatNullableInt64(u.InUnicastPktsDelta)
		record[33] = formatNullableInt64(u.OutBroadcastPktsDelta)
		record[34] = formatNullableInt64(u.OutDiscardsDelta)
		record[35] = formatNullableInt64(u.OutErrorsDelta)
		record[36] = formatNullableInt64(u.OutMulticastPktsDelta)
		record[37] = formatNullableInt64(u.OutOctetsDelta)
		record[38] = formatNullableInt64(u.OutPktsDelta)
		record[39] = formatNullableInt64(u.OutUnicastPktsDelta)
		record[40] = formatNullableFloat64(u.DeltaDuration)

		return w.Write(record)
	})
}

func formatNullableFloat64(f *float64) string {
	if f == nil {
		return ""
	}
	return fmt.Sprintf("%f", *f)
}

func formatNullableString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func formatNullableInt64(i *int64) string {
	if i == nil {
		return ""
	}
	return fmt.Sprintf("%d", *i)
}
