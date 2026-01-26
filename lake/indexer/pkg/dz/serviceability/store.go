package dzsvc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse/dataset"
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

// GetClickHouse returns the ClickHouse DB connection
func (s *Store) GetClickHouse() clickhouse.Client {
	return s.cfg.ClickHouse
}

func (s *Store) ReplaceContributors(ctx context.Context, contributors []Contributor) error {
	s.log.Debug("serviceability/store: replacing contributors", "count", len(contributors))

	d, err := NewContributorDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create dataset: %w", err)
	}

	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}
	defer conn.Close()

	// Write to ClickHouse using new dataset API
	if err := d.WriteBatch(ctx, conn, len(contributors), func(i int) ([]any, error) {
		return contributorSchema.ToRow(contributors[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		MissingMeansDeleted: true,
	}); err != nil {
		return fmt.Errorf("failed to write contributors to ClickHouse: %w", err)
	}

	return nil
}

func (s *Store) ReplaceDevices(ctx context.Context, devices []Device) error {
	s.log.Debug("serviceability/store: replacing devices", "count", len(devices))

	d, err := NewDeviceDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create dataset: %w", err)
	}

	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}
	defer conn.Close()

	// Write to ClickHouse using new dataset API
	if err := d.WriteBatch(ctx, conn, len(devices), func(i int) ([]any, error) {
		return deviceSchema.ToRow(devices[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		MissingMeansDeleted: true,
	}); err != nil {
		return fmt.Errorf("failed to write devices to ClickHouse: %w", err)
	}

	return nil
}

func (s *Store) ReplaceUsers(ctx context.Context, users []User) error {
	s.log.Debug("serviceability/store: replacing users", "count", len(users))

	d, err := NewUserDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create dimension dataset: %w", err)
	}

	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}
	defer conn.Close()

	// Write to ClickHouse using new dataset API
	if err := d.WriteBatch(ctx, conn, len(users), func(i int) ([]any, error) {
		return userSchema.ToRow(users[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		MissingMeansDeleted: true,
	}); err != nil {
		return fmt.Errorf("failed to write users to ClickHouse: %w", err)
	}

	return nil
}

func (s *Store) ReplaceMetros(ctx context.Context, metros []Metro) error {
	s.log.Debug("serviceability/store: replacing metros", "count", len(metros))

	d, err := NewMetroDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create dimension dataset: %w", err)
	}

	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}
	defer conn.Close()

	// Write to ClickHouse using new dataset API
	if err := d.WriteBatch(ctx, conn, len(metros), func(i int) ([]any, error) {
		return metroSchema.ToRow(metros[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		MissingMeansDeleted: true,
	}); err != nil {
		return fmt.Errorf("failed to write metros to ClickHouse: %w", err)
	}

	return nil
}

func (s *Store) ReplaceLinks(ctx context.Context, links []Link) error {
	s.log.Debug("serviceability/store: replacing links", "count", len(links))

	d, err := NewLinkDataset(s.log)
	if err != nil {
		return fmt.Errorf("failed to create dimension dataset: %w", err)
	}

	conn, err := s.cfg.ClickHouse.Conn(ctx)
	if err != nil {
		return fmt.Errorf("failed to get ClickHouse connection: %w", err)
	}
	defer conn.Close()

	// Write to ClickHouse using new dataset API
	if err := d.WriteBatch(ctx, conn, len(links), func(i int) ([]any, error) {
		return linkSchema.ToRow(links[i]), nil
	}, &dataset.DimensionType2DatasetWriteConfig{
		MissingMeansDeleted: true,
	}); err != nil {
		return fmt.Errorf("failed to write links to ClickHouse: %w", err)
	}

	return nil
}
