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

// agentVersionReport is the caller-supplied part of a version row. The
// controller build info columns are stamped by RecordVersion.
type agentVersionReport struct {
	DevicePubkey string
	UpdatedAt    time.Time
	AgentVersion string
	AgentCommit  string
	AgentDate    string
}

type versionEvent struct {
	agentVersionReport
	ControllerVersion string
	ControllerCommit  string
	ControllerDate    string
}

// Build identifies a controller build; it is stamped on every version row.
type Build struct {
	Version string
	Commit  string
	Date    string
}

type ClickhouseWriter struct {
	conn  clickhouse.Conn
	log   *slog.Logger
	db    string
	build Build

	// mu guards the buffers, the per-table enabled flags, and
	// consecutiveErrors: one writer may be shared by two Controller instances
	// (plaintext + TLS listeners), each running its own flush loop.
	mu                sync.Mutex
	events            []getConfigEvent
	versions          []versionEvent
	eventsEnabled     bool
	versionsEnabled   bool
	consecutiveErrors int
}

// consecutiveErrorThreshold is the number of consecutive flush failures
// before escalating from WARN to ERROR level logging.
const consecutiveErrorThreshold = 3

// maxAgentFieldLen caps agent-reported version strings. They come verbatim
// from an unauthenticated request and land in LowCardinality(String) columns,
// so unbounded values would bloat the in-memory buffer and the column
// dictionary.
const maxAgentFieldLen = 128

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

func NewClickhouseWriter(log *slog.Logger, addr, db, user, pass string, disableTLS bool, build Build) (*ClickhouseWriter, error) {
	chOpts := buildClickhouseOptions(addr, db, user, pass, disableTLS)
	conn, err := clickhouse.Open(chOpts)
	if err != nil {
		return nil, fmt.Errorf("error opening clickhouse connection: %w", err)
	}
	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("error pinging clickhouse: %w", err)
	}
	return &ClickhouseWriter{
		conn:  conn,
		log:   log,
		db:    db,
		build: build,
	}, nil
}

// CreateTables creates the tables the writer needs and enables writes
// per-table, so a single failing CREATE (e.g. a missing per-table GRANT on an
// upgraded deployment) disables only that table's writes instead of the whole
// writer. An error is returned only when no table is usable.
func (cw *ClickhouseWriter) CreateTables(ctx context.Context) error {
	eventsErr := cw.conn.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS "%s".controller_grpc_getconfig_success (
			timestamp DateTime64(3),
			device_pubkey LowCardinality(String)
		) ENGINE = MergeTree
		PARTITION BY toYYYYMM(timestamp)
		ORDER BY (timestamp, device_pubkey)
	`, cw.db))
	if eventsErr != nil {
		cw.log.Error("error creating getconfig table, disabling getconfig event writes", "error", eventsErr)
	}

	versionsErr := cw.conn.Exec(ctx, fmt.Sprintf(`
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
	if versionsErr != nil {
		cw.log.Error("error creating agent_versions table, disabling version writes", "error", versionsErr)
	}

	cw.mu.Lock()
	cw.eventsEnabled = eventsErr == nil
	cw.versionsEnabled = versionsErr == nil
	cw.mu.Unlock()

	if eventsErr != nil && versionsErr != nil {
		return fmt.Errorf("error creating tables: %w; %w", eventsErr, versionsErr)
	}
	return nil
}

func (cw *ClickhouseWriter) Record(event getConfigEvent) {
	cw.mu.Lock()
	defer cw.mu.Unlock()
	if !cw.eventsEnabled {
		return
	}
	cw.events = append(cw.events, event)
}

func (cw *ClickhouseWriter) RecordVersion(report agentVersionReport) {
	// A blank report (e.g. an old or restarting agent) must not overwrite the
	// last known good version row, since ReplacingMergeTree keeps the row with
	// the newest updated_at.
	if report.AgentVersion == "" && report.AgentCommit == "" && report.AgentDate == "" {
		return
	}
	report.AgentVersion = truncateAgentField(report.AgentVersion)
	report.AgentCommit = truncateAgentField(report.AgentCommit)
	report.AgentDate = truncateAgentField(report.AgentDate)
	cw.mu.Lock()
	defer cw.mu.Unlock()
	if !cw.versionsEnabled {
		return
	}
	cw.versions = append(cw.versions, versionEvent{
		agentVersionReport: report,
		ControllerVersion:  cw.build.Version,
		ControllerCommit:   cw.build.Commit,
		ControllerDate:     cw.build.Date,
	})
}

func truncateAgentField(s string) string {
	if len(s) > maxAgentFieldLen {
		return s[:maxAgentFieldLen]
	}
	return s
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

	if len(events) == 0 && len(versions) == 0 {
		return
	}
	ok := true
	if len(events) > 0 {
		ok = cw.flushEvents(ctx, events) && ok
	}
	if len(versions) > 0 {
		ok = cw.flushVersions(ctx, versions) && ok
	}
	// The counter advances at most once per flush pass so that
	// consecutiveErrorThreshold counts failed ticks, not failed sub-flushes.
	cw.mu.Lock()
	if ok {
		cw.consecutiveErrors = 0
	} else {
		cw.consecutiveErrors++
	}
	cw.mu.Unlock()
}

func (cw *ClickhouseWriter) flushEvents(ctx context.Context, events []getConfigEvent) bool {
	batch, err := cw.conn.PrepareBatch(ctx, fmt.Sprintf(
		`INSERT INTO "%s".controller_grpc_getconfig_success (timestamp, device_pubkey)`, cw.db,
	))
	if err != nil {
		cw.logFlushError("error preparing clickhouse batch", err)
		return false
	}
	for _, e := range events {
		if err := batch.Append(e.Timestamp, e.DevicePubkey); err != nil {
			// An Append error is systematic (wrong arity or types), so don't
			// send a partial batch.
			_ = batch.Close()
			cw.logFlushError("error appending to clickhouse batch", err)
			return false
		}
	}
	if err := batch.Send(); err != nil {
		_ = batch.Close()
		cw.logFlushError("error sending clickhouse batch", err)
		return false
	}
	if err := batch.Close(); err != nil {
		cw.logFlushError("error closing clickhouse batch", err)
		return false
	}
	cw.log.Debug("flushed getconfig events to clickhouse", "count", len(events))
	return true
}

func (cw *ClickhouseWriter) flushVersions(ctx context.Context, versions []versionEvent) bool {
	// Agents poll faster than the flush interval, so a tick usually buffers
	// several rows per device; only the newest per device would survive the
	// ReplacingMergeTree anyway, so skip inserting the rest.
	newest := make(map[string]versionEvent, len(versions))
	for _, v := range versions {
		if cur, seen := newest[v.DevicePubkey]; !seen || v.UpdatedAt.After(cur.UpdatedAt) {
			newest[v.DevicePubkey] = v
		}
	}

	batch, err := cw.conn.PrepareBatch(ctx, fmt.Sprintf(
		`INSERT INTO "%s".controller_agent_versions (device_pubkey, updated_at, agent_version, agent_commit, agent_date, controller_version, controller_commit, controller_date)`, cw.db,
	))
	if err != nil {
		cw.logFlushError("error preparing clickhouse versions batch", err)
		return false
	}
	for _, v := range newest {
		if err := batch.Append(v.DevicePubkey, v.UpdatedAt, v.AgentVersion, v.AgentCommit, v.AgentDate, v.ControllerVersion, v.ControllerCommit, v.ControllerDate); err != nil {
			// An Append error is systematic (wrong arity or types), so don't
			// send a partial batch.
			_ = batch.Close()
			cw.logFlushError("error appending to clickhouse versions batch", err)
			return false
		}
	}
	if err := batch.Send(); err != nil {
		_ = batch.Close()
		cw.logFlushError("error sending clickhouse versions batch", err)
		return false
	}
	if err := batch.Close(); err != nil {
		cw.logFlushError("error closing clickhouse versions batch", err)
		return false
	}
	cw.log.Debug("flushed version events to clickhouse", "count", len(newest))
	return true
}

// logFlushError logs a flush error at WARN, escalating to ERROR when the
// failure being logged belongs to at least the consecutiveErrorThreshold-th
// consecutive failed flush. The counter itself advances in flush().
func (cw *ClickhouseWriter) logFlushError(msg string, err error) {
	cw.mu.Lock()
	failures := cw.consecutiveErrors + 1 // include the flush currently failing
	cw.mu.Unlock()
	if failures >= consecutiveErrorThreshold {
		cw.log.Error(msg, "error", err, "consecutive_errors", failures)
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
