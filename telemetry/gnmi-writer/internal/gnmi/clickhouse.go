package gnmi

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/prometheus/client_golang/prometheus"
)

// ClickHouse error codes
const (
	chErrCodeUnknownTable = 60 // Table does not exist
)

// IsRetryableClickhouseError returns true if the error is transient and
// the operation should be retried, false if it's a permanent error.
func IsRetryableClickhouseError(err error) bool {
	if err == nil {
		return false
	}

	var exception *clickhouse.Exception
	if errors.As(err, &exception) {
		switch exception.Code {
		case chErrCodeUnknownTable:
			return false
		}
	}

	// Default: assume transient/retryable
	return true
}

// structMetadata holds cached reflection data for a struct type.
type structMetadata struct {
	columns    []string       // Column names from ch tags
	tagToIndex map[string]int // Map of column name to field index
}

// structMetadataCache caches reflection metadata per type to avoid repeated reflection.
var structMetadataCache sync.Map // map[reflect.Type]*structMetadata

// ClickhouseRecordWriter implements RecordWriter for writing Records to ClickHouse.
// Records are routed to tables based on their TableName() method.
// It uses reflection to dynamically build INSERT statements from struct tags.
type ClickhouseRecordWriter struct {
	addr       string
	db         string
	user       string
	pass       string
	disableTLS bool
	conn       clickhouse.Conn
	logger     *slog.Logger
	metrics    *ClickhouseMetrics
}

// ClickhouseWriterOption configures a ClickhouseRecordWriter.
type ClickhouseWriterOption func(*ClickhouseRecordWriter)

// WithClickhouseAddr sets the ClickHouse server address.
func WithClickhouseAddr(addr string) ClickhouseWriterOption {
	return func(cw *ClickhouseRecordWriter) {
		cw.addr = addr
	}
}

// WithClickhouseDB sets the database name.
func WithClickhouseDB(db string) ClickhouseWriterOption {
	return func(cw *ClickhouseRecordWriter) {
		cw.db = db
	}
}

// WithClickhouseUser sets the username.
func WithClickhouseUser(user string) ClickhouseWriterOption {
	return func(cw *ClickhouseRecordWriter) {
		cw.user = user
	}
}

// WithClickhousePassword sets the password.
func WithClickhousePassword(pass string) ClickhouseWriterOption {
	return func(cw *ClickhouseRecordWriter) {
		cw.pass = pass
	}
}

// WithClickhouseTLSDisabled disables TLS for the connection.
func WithClickhouseTLSDisabled(disabled bool) ClickhouseWriterOption {
	return func(cw *ClickhouseRecordWriter) {
		cw.disableTLS = disabled
	}
}

// WithClickhouseLogger sets the logger.
func WithClickhouseLogger(logger *slog.Logger) ClickhouseWriterOption {
	return func(cw *ClickhouseRecordWriter) {
		cw.logger = logger
	}
}

// WithClickhouseMetrics sets the metrics.
func WithClickhouseMetrics(metrics *ClickhouseMetrics) ClickhouseWriterOption {
	return func(cw *ClickhouseRecordWriter) {
		cw.metrics = metrics
	}
}

// NewClickhouseRecordWriter creates a new ClickhouseRecordWriter with the given options.
// The ClickHouse address must be configured via WithClickhouseAddr.
func NewClickhouseRecordWriter(opts ...ClickhouseWriterOption) (*ClickhouseRecordWriter, error) {
	cw := &ClickhouseRecordWriter{
		db:      "default",
		user:    "default",
		metrics: NewClickhouseMetrics(nil), // Always set, unregistered by default
	}

	for _, opt := range opts {
		opt(cw)
	}

	if cw.addr == "" {
		return nil, fmt.Errorf("clickhouse address is required: use WithClickhouseAddr")
	}

	if cw.logger == nil {
		cw.logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}

	chOpts := &clickhouse.Options{
		Addr: []string{cw.addr},
		Auth: clickhouse.Auth{
			Database: cw.db,
			Username: cw.user,
			Password: cw.pass,
		},
	}

	if !cw.disableTLS {
		chOpts.TLS = &tls.Config{}
	}

	conn, err := clickhouse.Open(chOpts)
	if err != nil {
		return nil, fmt.Errorf("error opening clickhouse connection: %w", err)
	}

	cw.conn = conn
	return cw, nil
}

// WriteRecords writes Records to ClickHouse, routing to tables based on record type.
// Uses reflection to dynamically build INSERT statements from struct `ch` tags.
func (cw *ClickhouseRecordWriter) WriteRecords(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}

	// Group records by table
	byTable := make(map[string][]Record)
	for _, r := range records {
		table := r.TableName()
		byTable[table] = append(byTable[table], r)
	}

	// Write each table's records
	for table, tableRecords := range byTable {
		if err := cw.writeGeneric(ctx, table, tableRecords); err != nil {
			return fmt.Errorf("error writing to table %s: %w", table, err)
		}
	}

	return nil
}

// Close closes the ClickHouse connection.
func (cw *ClickhouseRecordWriter) Close() error {
	return cw.conn.Close()
}

// writeGeneric writes records to a specific table using reflection.
// It uses fail-fast semantics: if any record fails to serialize or append,
// the entire batch is aborted to ensure atomicity. This prevents partial writes
// and allows the caller to retry the full batch.
func (cw *ClickhouseRecordWriter) writeGeneric(ctx context.Context, table string, records []Record) error {
	if len(records) == 0 {
		return nil
	}

	// Get column info from first record
	columns, err := getStructColumns(records[0])
	if err != nil {
		return fmt.Errorf("error getting columns: %w", err)
	}

	// Build INSERT query
	query := fmt.Sprintf("INSERT INTO %s.%s (%s)", cw.db, table, strings.Join(columns, ", "))

	batch, err := cw.conn.PrepareBatch(ctx, query)
	if err != nil {
		return fmt.Errorf("error preparing batch: %w", err)
	}

	// Append each record - fail fast on any error to ensure atomicity
	for i, r := range records {
		values, err := getStructValues(r, columns)
		if err != nil {
			_ = batch.Close()
			return fmt.Errorf("error getting values from record %d: %w", i, err)
		}

		if err := batch.Append(values...); err != nil {
			_ = batch.Close()
			return fmt.Errorf("error appending record %d to batch: %w", i, err)
		}
	}

	return cw.sendBatch(batch, len(records))
}

// sendBatch sends a batch and records metrics.
func (cw *ClickhouseRecordWriter) sendBatch(batch interface {
	Send() error
	Close() error
}, count int) error {
	timer := prometheus.NewTimer(cw.metrics.InsertDuration)

	if err := batch.Send(); err != nil {
		_ = batch.Close()
		cw.metrics.InsertErrors.Inc()
		return fmt.Errorf("error sending batch: %w", err)
	}

	timer.ObserveDuration()

	if err := batch.Close(); err != nil {
		return fmt.Errorf("error closing batch: %w", err)
	}

	cw.metrics.RecordsWritten.Add(float64(count))

	cw.logger.Debug("wrote records to clickhouse", "count", count)
	return nil
}

// getOrComputeMetadata returns cached struct metadata or computes and caches it.
func getOrComputeMetadata(t reflect.Type) (*structMetadata, error) {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("expected struct, got %s", t.Kind())
	}

	// Check cache first
	if cached, ok := structMetadataCache.Load(t); ok {
		return cached.(*structMetadata), nil
	}

	// Compute metadata
	var columns []string
	tagToIndex := make(map[string]int)

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("ch")
		if tag == "" {
			continue
		}
		// Handle tag options like `ch:"column_name,omitempty"`
		colName := strings.Split(tag, ",")[0]
		if colName != "" && colName != "-" {
			columns = append(columns, colName)
			tagToIndex[colName] = i
		}
	}

	if len(columns) == 0 {
		return nil, fmt.Errorf("no columns found with 'ch' tags")
	}

	meta := &structMetadata{
		columns:    columns,
		tagToIndex: tagToIndex,
	}

	// Store in cache (if another goroutine stored first, that's fine)
	structMetadataCache.Store(t, meta)
	return meta, nil
}

// getStructColumns extracts column names from a struct's `ch` tags.
// Results are cached per type for performance.
func getStructColumns(r Record) ([]string, error) {
	t := reflect.TypeOf(r)
	meta, err := getOrComputeMetadata(t)
	if err != nil {
		return nil, err
	}
	return meta.columns, nil
}

// getStructValues extracts values from a struct in the order matching the columns.
// Uses cached field index mapping for performance.
func getStructValues(r Record, columns []string) ([]any, error) {
	v := reflect.ValueOf(r)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	meta, err := getOrComputeMetadata(v.Type())
	if err != nil {
		return nil, err
	}

	// Extract values in column order using cached index mapping
	values := make([]any, len(columns))
	for i, col := range columns {
		idx, ok := meta.tagToIndex[col]
		if !ok {
			return nil, fmt.Errorf("column %q not found in struct", col)
		}
		values[i] = v.Field(idx).Interface()
	}

	return values, nil
}
