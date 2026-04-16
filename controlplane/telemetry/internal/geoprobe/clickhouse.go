package geoprobe

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/gagliardetto/solana-go"
)

type ClickhouseConfig struct {
	Addr     string
	Database string
	Username string
	Password string
	Secure   bool
}

func ClickhouseConfigFromEnv() *ClickhouseConfig {
	addr := os.Getenv("CLICKHOUSE_ADDR")
	if addr == "" {
		return nil
	}
	addr = strings.TrimPrefix(addr, "https://")
	addr = strings.TrimPrefix(addr, "http://")
	db := os.Getenv("CLICKHOUSE_DB")
	if db == "" {
		db = "default"
	}
	user := os.Getenv("CLICKHOUSE_USER")
	if user == "" {
		user = "default"
	}
	return &ClickhouseConfig{
		Addr:     addr,
		Database: db,
		Username: user,
		Password: os.Getenv("CLICKHOUSE_PASS"),
		Secure:   os.Getenv("CLICKHOUSE_TLS_DISABLED") != "true",
	}
}

func NewClickhouseConn(cfg ClickhouseConfig) (driver.Conn, error) {
	opts := &clickhouse.Options{
		Protocol: clickhouse.HTTP,
		Addr:     []string{cfg.Addr},
		Auth: clickhouse.Auth{
			Database: cfg.Database,
			Username: cfg.Username,
			Password: cfg.Password,
		},
		MaxOpenConns: 5,
		DialTimeout:  30 * time.Second,
	}
	if cfg.Secure {
		opts.TLS = &tls.Config{}
	}

	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("clickhouse open: %w", err)
	}
	if err := conn.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("clickhouse ping: %w", err)
	}
	return conn, nil
}

type OffsetRow struct {
	ReceivedAt          time.Time
	SourceAddr          string
	AuthorityPubkey     string
	SenderPubkey        string
	MeasurementSlot     uint64
	Lat                 float64
	Lng                 float64
	MeasuredRttNs       uint64
	RttNs               uint64
	TargetIP            string
	NumReferences       uint8
	SignatureValid      bool
	SignatureError      string
	RawOffset           string
	RefAuthorityPubkeys []string
	RefSenderPubkeys    []string
	RefMeasuredRttNs    []uint64
	RefRttNs            []uint64
}

func OffsetRowFromLocationOffset(offset *LocationOffset, sourceAddr string, sigValid bool, sigError string, rawBytes []byte) OffsetRow {
	row := OffsetRow{
		ReceivedAt:      time.Now(),
		SourceAddr:      sourceAddr,
		AuthorityPubkey: solana.PublicKeyFromBytes(offset.AuthorityPubkey[:]).String(),
		SenderPubkey:    solana.PublicKeyFromBytes(offset.SenderPubkey[:]).String(),
		MeasurementSlot: offset.MeasurementSlot,
		Lat:             offset.Lat,
		Lng:             offset.Lng,
		MeasuredRttNs:   offset.MeasuredRttNs,
		RttNs:           offset.RttNs,
		TargetIP:        FormatTargetIP(offset.TargetIP),
		NumReferences:   offset.NumReferences,
		SignatureValid:  sigValid,
		SignatureError:  sigError,
		RawOffset:       hex.EncodeToString(rawBytes),
	}

	for _, ref := range offset.References {
		row.RefAuthorityPubkeys = append(row.RefAuthorityPubkeys, solana.PublicKeyFromBytes(ref.AuthorityPubkey[:]).String())
		row.RefSenderPubkeys = append(row.RefSenderPubkeys, solana.PublicKeyFromBytes(ref.SenderPubkey[:]).String())
		row.RefMeasuredRttNs = append(row.RefMeasuredRttNs, ref.MeasuredRttNs)
		row.RefRttNs = append(row.RefRttNs, ref.RttNs)
	}

	return row
}

const maxBufferedRows = 10_000

type ClickhouseWriter struct {
	cfg  ClickhouseConfig
	conn driver.Conn
	buf  []OffsetRow
	mu   sync.Mutex
	log  *slog.Logger
}

func NewClickhouseWriter(cfg ClickhouseConfig, log *slog.Logger) *ClickhouseWriter {
	return &ClickhouseWriter{
		cfg: cfg,
		buf: make([]OffsetRow, 0, 64),
		log: log,
	}
}

func (w *ClickhouseWriter) Record(row OffsetRow) {
	w.mu.Lock()
	if len(w.buf) >= maxBufferedRows {
		w.mu.Unlock()
		w.log.Warn("clickhouse buffer full, dropping row", "max", maxBufferedRows)
		return
	}
	w.buf = append(w.buf, row)
	w.mu.Unlock()
}

func (w *ClickhouseWriter) connect(ctx context.Context) error {
	if err := RunMigrations(w.cfg, w.log); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}
	conn, err := NewClickhouseConn(w.cfg)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	w.conn = conn
	w.log.Info("clickhouse connected", "addr", w.cfg.Addr, "db", w.cfg.Database)
	return nil
}

func (w *ClickhouseWriter) Close() {
	if w.conn != nil {
		_ = w.conn.Close()
		w.conn = nil
	}
}

func (w *ClickhouseWriter) Run(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	defer w.Close()

	for {
		select {
		case <-ctx.Done():
			if w.conn != nil {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				w.flush(shutdownCtx)
				cancel()
			}
			return
		case <-ticker.C:
			if w.conn == nil {
				if err := w.connect(ctx); err != nil {
					w.log.Error("clickhouse connection failed, will retry", "error", err)
					continue
				}
			}
			w.flush(ctx)
		}
	}
}

func (w *ClickhouseWriter) flush(ctx context.Context) {
	w.mu.Lock()
	if len(w.buf) == 0 {
		w.mu.Unlock()
		return
	}
	rows := w.buf
	w.buf = make([]OffsetRow, 0, 64)
	w.mu.Unlock()

	batch, err := w.conn.PrepareBatch(ctx, fmt.Sprintf(
		`INSERT INTO "%s".location_offsets`, w.cfg.Database,
	))
	if err != nil {
		w.log.Error("failed to prepare batch", "error", err, "dropped_rows", len(rows))
		w.Close()
		return
	}

	for _, r := range rows {
		if err := batch.Append(
			r.ReceivedAt,
			r.SourceAddr,
			r.AuthorityPubkey,
			r.SenderPubkey,
			r.MeasurementSlot,
			r.Lat,
			r.Lng,
			r.MeasuredRttNs,
			r.RttNs,
			r.TargetIP,
			r.NumReferences,
			r.SignatureValid,
			r.SignatureError,
			r.RawOffset,
			r.RefAuthorityPubkeys,
			r.RefSenderPubkeys,
			r.RefMeasuredRttNs,
			r.RefRttNs,
		); err != nil {
			w.log.Error("failed to append row", "error", err, "dropped_rows", len(rows))
			_ = batch.Abort()
			w.Close()
			return
		}
	}

	if err := batch.Send(); err != nil {
		w.log.Error("failed to send batch", "error", err, "dropped_rows", len(rows))
		_ = batch.Close()
		w.Close()
		return
	}
	_ = batch.Close()

	w.log.Debug("flushed offsets to clickhouse", "count", len(rows))
}
