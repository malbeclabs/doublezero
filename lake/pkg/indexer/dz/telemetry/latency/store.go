package dztelemlatency

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"
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

func (s *Store) AppendDeviceLinkLatencySamples(ctx context.Context, samples []DeviceLinkLatencySample) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	cfg := FactTableConfigDeviceLinkLatencySamples()
	return duck.InsertFactsViaCSV(
		ctx,
		s.log,
		conn,
		cfg,
		len(samples),
		func(w *csv.Writer, i int) error {
			sample := samples[i]
			loss := "false"
			if sample.RTTMicroseconds == 0 {
				loss = "true"
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
			})
		},
	)
}

func (s *Store) AppendInternetMetroLatencySamples(ctx context.Context, samples []InternetMetroLatencySample) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()

	cfg := FactTableConfigInternetMetroLatencySamples()
	return duck.InsertFactsViaCSV(
		ctx,
		s.log,
		conn,
		cfg,
		len(samples),
		func(w *csv.Writer, i int) error {
			sample := samples[i]
			return w.Write([]string{
				sample.Time.UTC().Format(time.RFC3339Nano),
				fmt.Sprintf("%d", sample.Epoch),
				fmt.Sprintf("%d", sample.SampleIndex),
				sample.OriginMetroPK,
				sample.TargetMetroPK,
				sample.DataProvider,
				fmt.Sprintf("%d", sample.RTTMicroseconds),
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
