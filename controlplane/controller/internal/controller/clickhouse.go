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

type versionEvent struct {
	DevicePubkey      string
	UpdatedAt         time.Time
	AgentVersion      string
	AgentCommit       string
	AgentDate         string
	ControllerVersion string
	ControllerCommit  string
	ControllerDate    string
}

type ClickhouseWriter struct {
	conn              clickhouse.Conn
	log               *slog.Logger
	db                string
	mu                sync.Mutex
	events            []getConfigEvent
	versions          []versionEvent
	consecutiveErrors int

	// Controller build info, stamped on every version event.
	controllerVersion string
	controllerCommit  string
	controllerDate    string
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

func NewClickhouseWriter(log *slog.Logger, addr, db, user, pass string, disableTLS bool, controllerVersion, controllerCommit, controllerDate string) (*ClickhouseWriter, error) {
	chOpts := buildClickhouseOptions(addr, db, user, pass, disableTLS)
	conn, err := clickhouse.Open(chOpts)
	if err != nil {
		return nil, fmt.Errorf("error opening clickhouse connection: %w", err)
	}
	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("error pinging clickhouse: %w", err)
	}
	return &ClickhouseWriter{
		conn:              conn,
		log:               log,
		db:                db,
		controllerVersion: controllerVersion,
		controllerCommit:  controllerCommit,
		controllerDate:    controllerDate,
	}, nil
}

func (cw *ClickhouseWriter) CreateTables(ctx context.Context) error {
	err := cw.conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS "%s".controller_grpc_getconfig_success (
			timestamp DateTime64(3),
			device_pubkey LowCardinality(String)
		) ENGINE = MergeTree
		PARTITION BY toYYYYMM(timestamp)
		ORDER BY (timestamp, device_pubkey)
	`, cw.db))
	if err != nil {
		return fmt.Errorf("error creating getconfig table: %w", err)
	}

	err = cw.conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS "%s".controller_agent_versions (
			device_pubkey LowCardinality(String),
			updated_at DateTime64(3),
			agent_version LowCardinality(String) DEFAULT '',
			agent_commit LowCardinality(String) DEFAULT '',
			agent_date LowCardinality(String) DEFAULT '',
			controller_version LowCardinality(String) DEFAULT '',
			controller_commit LowCardinality(String) DEFAULT '',
			controller_date LowCardinality(String) DEFAULT ''
		) ENGINE = ReplacingMergeTree(updated_at)
		ORDER BY device_pubkey
	`, cw.db))
	if err != nil {
		return fmt.Errorf("error creating agent_versions table: %w", err)
	}

	return nil
}

func (cw *ClickhouseWriter) Record(event getConfigEvent) {
	cw.mu.Lock()
	cw.events = append(cw.events, event)
	cw.mu.Unlock()
}

func (cw *ClickhouseWriter) RecordVersion(event versionEvent) {
	event.ControllerVersion = cw.controllerVersion
	event.ControllerCommit = cw.controllerCommit
	event.ControllerDate = cw.controllerDate
	cw.mu.Lock()
	cw.versions = append(cw.versions, event)
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
	events := cw.events
	cw.events = nil
	versions := cw.versions
	cw.versions = nil
	cw.mu.Unlock()

	if len(events) > 0 {
		cw.flushEvents(ctx, events)
	}
	if len(versions) > 0 {
		cw.flushVersions(ctx, versions)
	}
}

func (cw *ClickhouseWriter) flushEvents(ctx context.Context, events []getConfigEvent) {
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

func (cw *ClickhouseWriter) flushVersions(ctx context.Context, versions []versionEvent) {
	batch, err := cw.conn.PrepareBatch(ctx, fmt.Sprintf(
		`INSERT INTO "%s".controller_agent_versions (device_pubkey, updated_at, agent_version, agent_commit, agent_date, controller_version, controller_commit, controller_date)`, cw.db,
	))
	if err != nil {
		cw.recordFlushError("error preparing clickhouse versions batch", err)
		return
	}
	for _, v := range versions {
		if err := batch.Append(v.DevicePubkey, v.UpdatedAt, v.AgentVersion, v.AgentCommit, v.AgentDate, v.ControllerVersion, v.ControllerCommit, v.ControllerDate); err != nil {
			cw.logFlushError("error appending to clickhouse versions batch", err)
		}
	}
	if err := batch.Send(); err != nil {
		_ = batch.Close()
		cw.recordFlushError("error sending clickhouse versions batch", err)
		return
	}
	if err := batch.Close(); err != nil {
		cw.recordFlushError("error closing clickhouse versions batch", err)
		return
	}
	cw.log.Debug("flushed version events to clickhouse", "count", len(versions))
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
