package exporter

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/malbeclabs/doublezero/controlplane/internet-latency-collector/internal/metrics"
)

const (
	DefaultClickHouseTable = "cloud_region_latency"

	defaultClickHouseBatchSize     = 500
	defaultClickHouseFlushInterval = 30 * time.Second
	closeFlushTimeout              = 10 * time.Second

	// 136 region pairs sampled every 10 minutes produce 19,584 records a day, so this holds
	// about two days of them.
	defaultClickHouseMaxBufferedRecords = 50_000
)

// clickHouseColumns lists the target table's columns in the order values are appended.
var clickHouseColumns = []string{
	"event_ts",
	"ingested_at",
	"origin_cloud",
	"origin_region",
	"target_cloud",
	"target_region",
	"data_provider",
	"probe_id",
	"rtt_us",
	"packets_sent",
	"packets_received",
}

type ClickHouseBatchInserter interface {
	Insert(ctx context.Context, records []Record) error
	Close() error
}

type ClickHouseExporterConfig struct {
	Logger      *slog.Logger
	Addr        string
	Database    string
	Username    string
	Password    string
	TLSDisabled bool
	Table       string

	BatchSize          int
	FlushInterval      time.Duration
	MaxBufferedRecords int

	// Inserter replaces the ClickHouse connection. When nil, a connection is opened from Addr,
	// Database, Username and Password.
	Inserter ClickHouseBatchInserter
}

func (c *ClickHouseExporterConfig) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Inserter == nil && c.Addr == "" {
		return errors.New("clickhouse address is required")
	}
	if c.Inserter == nil && c.Database == "" {
		return errors.New("clickhouse database is required")
	}
	return nil
}

type ClickHouseExporter struct {
	log                *slog.Logger
	inserter           ClickHouseBatchInserter
	table              string
	batchSize          int
	flushInterval      time.Duration
	maxBufferedRecords int

	// flushMu serialises Flush so a failed batch requeues in take order and the oldest stays first.
	flushMu sync.Mutex

	mu        sync.Mutex
	pending   []Record
	lastFlush time.Time
}

func NewClickHouseExporter(cfg ClickHouseExporterConfig) (*ClickHouseExporter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate clickhouse exporter config: %w", err)
	}

	table := cfg.Table
	if table == "" {
		table = DefaultClickHouseTable
	}
	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = defaultClickHouseBatchSize
	}
	flushInterval := cfg.FlushInterval
	if flushInterval <= 0 {
		flushInterval = defaultClickHouseFlushInterval
	}
	maxBufferedRecords := cfg.MaxBufferedRecords
	if maxBufferedRecords <= 0 {
		maxBufferedRecords = defaultClickHouseMaxBufferedRecords
	}

	inserter := cfg.Inserter
	if inserter == nil {
		chOpts := &clickhouse.Options{
			Addr: []string{cfg.Addr},
			Auth: clickhouse.Auth{
				Database: cfg.Database,
				Username: cfg.Username,
				Password: cfg.Password,
			},
		}
		if !cfg.TLSDisabled {
			chOpts.TLS = &tls.Config{}
		}

		conn, err := clickhouse.Open(chOpts)
		if err != nil {
			return nil, fmt.Errorf("failed to open clickhouse connection: %w", err)
		}
		// Open does not dial, so ping to settle the address, credentials and database here. A
		// bad setting then fails at startup instead of warning on every flush.
		if err := conn.Ping(context.Background()); err != nil {
			return nil, fmt.Errorf("clickhouse ping: %w", err)
		}
		inserter = &clickHouseConnInserter{
			conn:     conn,
			database: cfg.Database,
			table:    table,
		}
	}

	cfg.Logger.Info("Created ClickHouse exporter",
		slog.String("table", table),
		slog.Int("batch_size", batchSize),
		slog.Int("max_buffered_records", maxBufferedRecords))

	return &ClickHouseExporter{
		log:                cfg.Logger,
		inserter:           inserter,
		table:              table,
		batchSize:          batchSize,
		flushInterval:      flushInterval,
		maxBufferedRecords: maxBufferedRecords,
		lastFlush:          time.Now(),
	}, nil
}

// WriteRecords errors only when the buffer is full, so a failing insert does not stall its caller.
func (e *ClickHouseExporter) WriteRecords(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}

	for _, record := range records {
		if err := record.Validate(); err != nil {
			return fmt.Errorf("invalid record: %w", err)
		}
		if err := record.Cloud.Validate(); err != nil {
			return fmt.Errorf("invalid record: %w", err)
		}
	}

	e.enqueue(records)

	if e.shouldFlush() {
		if err := e.Flush(ctx); err != nil {
			e.log.Warn("Failed to flush records to ClickHouse", slog.String("error", err.Error()))
		}
	}

	if e.Buffered() >= e.maxBufferedRecords {
		return fmt.Errorf("clickhouse exporter buffer is full at %d records", e.maxBufferedRecords)
	}
	return nil
}

func (e *ClickHouseExporter) Flush(ctx context.Context) error {
	e.flushMu.Lock()
	defer e.flushMu.Unlock()

	for {
		batch := e.takeBatch()
		if len(batch) == 0 {
			return nil
		}

		if err := e.inserter.Insert(ctx, batch); err != nil {
			e.requeue(batch)
			metrics.ExporterClickHouseInsertErrorsTotal.Inc()
			return fmt.Errorf("failed to insert %d records into %s: %w", len(batch), e.table, err)
		}

		metrics.ExporterClickHouseRecordsWrittenTotal.Add(float64(len(batch)))
		e.log.Debug("Wrote records to ClickHouse",
			slog.String("table", e.table),
			slog.Int("records", len(batch)))
	}
}

func (e *ClickHouseExporter) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), closeFlushTimeout)
	defer cancel()

	flushErr := e.Flush(ctx)
	closeErr := e.inserter.Close()
	return errors.Join(flushErr, closeErr)
}

func (e *ClickHouseExporter) Buffered() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.pending)
}

func (e *ClickHouseExporter) enqueue(records []Record) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.pending = append(e.pending, records...)
	e.dropOldestLocked()
	metrics.ExporterClickHouseBufferedRecords.Set(float64(len(e.pending)))
}

func (e *ClickHouseExporter) requeue(batch []Record) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.pending = append(batch, e.pending...)
	e.dropOldestLocked()
	metrics.ExporterClickHouseBufferedRecords.Set(float64(len(e.pending)))
}

// dropOldestLocked trims the buffer back to its cap, oldest first. The caller holds e.mu.
func (e *ClickHouseExporter) dropOldestLocked() {
	overflow := len(e.pending) - e.maxBufferedRecords
	if overflow <= 0 {
		return
	}

	e.pending = append([]Record(nil), e.pending[overflow:]...)
	metrics.ExporterClickHouseRecordsDroppedTotal.Add(float64(overflow))
	e.log.Warn("ClickHouse exporter buffer full, dropped oldest records",
		slog.Int("dropped", overflow),
		slog.Int("max_buffered_records", e.maxBufferedRecords))
}

func (e *ClickHouseExporter) shouldFlush() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.pending) >= e.batchSize || time.Since(e.lastFlush) >= e.flushInterval
}

func (e *ClickHouseExporter) takeBatch() []Record {
	e.mu.Lock()
	defer e.mu.Unlock()

	if len(e.pending) == 0 {
		return nil
	}

	n := min(e.batchSize, len(e.pending))
	batch := append([]Record(nil), e.pending[:n]...)
	e.pending = append([]Record(nil), e.pending[n:]...)
	e.lastFlush = time.Now()
	metrics.ExporterClickHouseBufferedRecords.Set(float64(len(e.pending)))
	return batch
}

type clickHouseConnInserter struct {
	conn     clickhouse.Conn
	database string
	table    string
}

func (i *clickHouseConnInserter) Insert(ctx context.Context, records []Record) error {
	query := fmt.Sprintf("INSERT INTO %s.%s (%s)", i.database, i.table, strings.Join(clickHouseColumns, ", "))

	batch, err := i.conn.PrepareBatch(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to prepare batch: %w", err)
	}

	ingestedAt := time.Now().UTC()
	for n, record := range records {
		err := batch.Append(
			record.Timestamp.UTC(),
			ingestedAt,
			record.Cloud.SourceCloud,
			record.SourceExchangeCode,
			record.Cloud.TargetCloud,
			record.TargetExchangeCode,
			string(record.DataProvider),
			record.Cloud.ProbeID,
			uint32(record.RTT.Microseconds()),
			record.Cloud.PacketsSent,
			record.Cloud.PacketsReceived,
		)
		if err != nil {
			_ = batch.Abort()
			return fmt.Errorf("failed to append record %d to batch: %w", n, err)
		}
	}

	if err := batch.Send(); err != nil {
		return fmt.Errorf("failed to send batch: %w", err)
	}
	return nil
}

func (i *clickHouseConnInserter) Close() error {
	return i.conn.Close()
}
