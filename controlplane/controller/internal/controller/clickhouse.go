package controller

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
)

type getConfigEvent struct {
	Timestamp    time.Time
	DevicePubkey string
}

type ClickhouseWriter struct {
	conn              clickhouse.Conn
	log               *slog.Logger
	db                string
	mu                sync.Mutex
	events            []getConfigEvent
	consecutiveErrors int
}

// consecutiveErrorThreshold is the number of consecutive flush failures
// before escalating from WARN to ERROR level logging.
const consecutiveErrorThreshold = 3

// buildClickhouseOptions constructs clickhouse.Options for the HTTP protocol.
// When disableTLS is false (default), TLS is enabled (HTTPS).
// When disableTLS is true, TLS is not set (plain HTTP, for local dev only).
func buildClickhouseOptions(addr, db, user, pass string, disableTLS bool) *clickhouse.Options {
	// Strip URL scheme if present - clickhouse-go expects host:port only
	addr = strings.TrimPrefix(addr, "https://")
	addr = strings.TrimPrefix(addr, "http://")

	opts := &clickhouse.Options{
		Protocol: clickhouse.HTTP,
		Addr:     []string{addr},
		Auth: clickhouse.Auth{
			Database: db,
			Username: user,
			Password: pass,
		},
	}
	if !disableTLS {
		opts.TLS = &tls.Config{}
	}
	return opts
}

func NewClickhouseWriter(log *slog.Logger, addr, db, user, pass string, disableTLS bool) (*ClickhouseWriter, error) {
	chOpts := buildClickhouseOptions(addr, db, user, pass, disableTLS)
	conn, err := clickhouse.Open(chOpts)
	if err != nil {
		return nil, fmt.Errorf("error opening clickhouse connection: %w", err)
	}
	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("error pinging clickhouse: %w", err)
	}
	return &ClickhouseWriter{
		conn: conn,
		log:  log,
		db:   db,
	}, nil
}

func (cw *ClickhouseWriter) CreateTable(ctx context.Context) error {
	err := cw.conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS "%s".controller_grpc_getconfig_success (
			timestamp DateTime64(3),
			device_pubkey LowCardinality(String)
		) ENGINE = MergeTree
		PARTITION BY toYYYYMM(timestamp)
		ORDER BY (timestamp, device_pubkey)
	`, cw.db))
	if err != nil {
		return fmt.Errorf("error creating table: %w", err)
	}
	return nil
}

func (cw *ClickhouseWriter) Record(event getConfigEvent) {
	cw.mu.Lock()
	cw.events = append(cw.events, event)
	cw.mu.Unlock()
}

func (cw *ClickhouseWriter) Run(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cw.flush(context.Background())
			return
		case <-ticker.C:
			cw.flush(ctx)
		}
	}
}

func (cw *ClickhouseWriter) flush(ctx context.Context) {
	cw.mu.Lock()
	if len(cw.events) == 0 {
		cw.mu.Unlock()
		return
	}
	events := cw.events
	cw.events = nil
	cw.mu.Unlock()

	batch, err := cw.conn.PrepareBatch(ctx, fmt.Sprintf(
		`INSERT INTO "%s".controller_grpc_getconfig_success (timestamp, device_pubkey)`, cw.db,
	))
	if err != nil {
		cw.recordFlushError("error preparing clickhouse batch", err)
		return
	}
	for _, e := range events {
		if err := batch.Append(e.Timestamp, e.DevicePubkey); err != nil {
			cw.logFlushError("error appending to clickhouse batch", err)
		}
	}
	if err := batch.Send(); err != nil {
		_ = batch.Close()
		cw.recordFlushError("error sending clickhouse batch", err)
		return
	}
	if err := batch.Close(); err != nil {
		cw.recordFlushError("error closing clickhouse batch", err)
		return
	}
	cw.consecutiveErrors = 0
	cw.log.Debug("flushed getconfig events to clickhouse", "count", len(events))
}

// recordFlushError increments the consecutive error counter and logs at the
// appropriate level. Transient errors are logged as WARN; persistent errors
// (exceeding consecutiveErrorThreshold) are logged as ERROR.
func (cw *ClickhouseWriter) recordFlushError(msg string, err error) {
	cw.consecutiveErrors++
	cw.logFlushError(msg, err)
}

func (cw *ClickhouseWriter) logFlushError(msg string, err error) {
	if cw.consecutiveErrors >= consecutiveErrorThreshold {
		cw.log.Error(msg, "error", err, "consecutive_errors", cw.consecutiveErrors)
	} else {
		cw.log.Warn(msg, "error", err)
	}
}

func (cw *ClickhouseWriter) Close() {
	cw.flush(context.Background())
	if err := cw.conn.Close(); err != nil {
		cw.log.Error("error closing clickhouse connection", "error", err)
	}
}
