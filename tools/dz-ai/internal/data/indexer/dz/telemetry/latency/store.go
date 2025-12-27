package dztelemlatency

import (
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log/slog"

	"github.com/malbeclabs/doublezero/tools/dz-ai/internal/data/duck"
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
	tablePrefix := s.db.Catalog() + "." + s.db.Schema() + "."
	schemas := []string{
		`CREATE TABLE IF NOT EXISTS ` + tablePrefix + `dz_device_link_circuits (
			code VARCHAR,
			origin_device_pk VARCHAR,
			target_device_pk VARCHAR,
			link_pk VARCHAR,
			link_code VARCHAR,
			link_type VARCHAR,
			contributor_code VARCHAR,
			committed_rtt DOUBLE,
			committed_jitter DOUBLE
		)`,
		`CREATE TABLE IF NOT EXISTS ` + tablePrefix + `dz_device_link_latency_samples (
			circuit_code VARCHAR,
			epoch BIGINT,
			sample_index INTEGER,
			timestamp_us BIGINT,
			rtt_us BIGINT
		)`,
		`CREATE TABLE IF NOT EXISTS ` + tablePrefix + `dz_internet_metro_latency_samples (
			circuit_code VARCHAR,
			data_provider VARCHAR,
			epoch BIGINT,
			sample_index INTEGER,
			timestamp_us BIGINT,
			rtt_us BIGINT
		)`,
	}

	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	for _, schema := range schemas {
		if _, err := conn.ExecContext(ctx, schema); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	return nil
}

func (s *Store) ReplaceDeviceLinkCircuits(ctx context.Context, circuits []DeviceLinkCircuit) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	return duck.ReplaceTableViaCSV(ctx, s.log, conn, "dz_device_link_circuits", len(circuits), func(w *csv.Writer, i int) error {
		c := circuits[i]
		return w.Write([]string{
			c.Code, c.OriginDevicePK, c.TargetDevicePK, c.LinkPK, c.LinkCode, c.LinkType, c.ContributorCode,
			fmt.Sprintf("%.6f", c.CommittedRTT), fmt.Sprintf("%.6f", c.CommittedJitter),
		})
	}, []string{"code"})
}

func (s *Store) AppendDeviceLinkLatencySamples(ctx context.Context, samples []DeviceLinkLatencySample) error {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	return duck.AppendTableViaCSV(
		ctx,
		s.log,
		conn,
		"dz_device_link_latency_samples",
		len(samples),
		func(w *csv.Writer, i int) error {
			sample := samples[i]
			return w.Write([]string{
				sample.CircuitCode,
				fmt.Sprintf("%d", sample.Epoch),
				fmt.Sprintf("%d", sample.SampleIndex),
				fmt.Sprintf("%d", sample.TimestampMicroseconds),
				fmt.Sprintf("%d", sample.RTTMicroseconds),
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
	return duck.AppendTableViaCSV(
		ctx,
		s.log,
		conn,
		"dz_internet_metro_latency_samples",
		len(samples),
		func(w *csv.Writer, i int) error {
			sample := samples[i]
			return w.Write([]string{
				sample.CircuitCode,
				sample.DataProvider,
				fmt.Sprintf("%d", sample.Epoch),
				fmt.Sprintf("%d", sample.SampleIndex),
				fmt.Sprintf("%d", sample.TimestampMicroseconds),
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
	query := `SELECT circuit_code, epoch, MAX(sample_index) as max_idx
	          FROM dz_device_link_latency_samples
	          GROUP BY circuit_code, epoch`
	rows, err := conn.QueryContext(ctx, query)
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
	ctx := context.Background()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get connection: %w", err)
	}
	defer conn.Close()
	query := `SELECT circuit_code, data_provider, epoch, MAX(sample_index) as max_idx
	          FROM dz_internet_metro_latency_samples
	          GROUP BY circuit_code, data_provider, epoch`
	rows, err := conn.QueryContext(ctx, query)
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
