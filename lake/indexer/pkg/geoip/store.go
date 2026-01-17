package geoip

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
	"github.com/malbeclabs/doublezero/tools/maxmind/pkg/geoip"
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

func (s *Store) UpsertRecords(ctx context.Context, records []*geoip.Record) error {
	s.log.Debug("geoip/store: upserting records", "count", len(records))

	d, err := NewGeoIPRecordDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create dimension dataset: %w", err)
	}

	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}

	// Write to ClickHouse using new dataset API
	err = d.WriteBatch(ctx, conn, len(records), func(i int) ([]any, error) {
		r := records[i]
		if r == nil {
			return nil, fmt.Errorf("record at index %d is nil", i)
		}
		// schema.ToRow returns []any which is compatible with []any
		row := geoIPRecordSchema.ToRow(r)
		return row, nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		MissingMeansDeleted: false,
	})
	if err != nil {
		return fmt.Errorf("failed to write records to ClickHouse: %w", err)
	}
	return nil
}
