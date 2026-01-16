package gnmi

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
)

// RecordWriter defines the interface for writing Records to a destination.
type RecordWriter interface {
	WriteRecords(ctx context.Context, records []Record) error
}

// StdoutRecordWriter implements RecordWriter for writing Records as JSON lines to stdout.
// Each record is wrapped with a "_table" field indicating the destination table.
type StdoutRecordWriter struct {
	writer  io.Writer
	encoder *json.Encoder
}

// StdoutRecordWriterOption configures a StdoutRecordWriter.
type StdoutRecordWriterOption func(*StdoutRecordWriter)

// WithStdoutWriter sets a custom writer (defaults to os.Stdout).
func WithStdoutWriter(w io.Writer) StdoutRecordWriterOption {
	return func(s *StdoutRecordWriter) {
		s.writer = w
	}
}

// NewStdoutRecordWriter creates a new StdoutRecordWriter with the given options.
func NewStdoutRecordWriter(opts ...StdoutRecordWriterOption) *StdoutRecordWriter {
	s := &StdoutRecordWriter{
		writer: os.Stdout,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.encoder = json.NewEncoder(s.writer)
	return s
}

// recordWrapper wraps a record with table metadata for JSON output.
type recordWrapper struct {
	Table string `json:"_table"`
	Data  any    `json:"data"`
}

// WriteRecords writes each Record as a JSON line to the configured writer.
// Output format: {"_table": "table_name", "data": {...record fields...}}
func (s *StdoutRecordWriter) WriteRecords(ctx context.Context, records []Record) error {
	for _, record := range records {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		wrapper := recordWrapper{
			Table: record.TableName(),
			Data:  record,
		}
		if err := s.encoder.Encode(wrapper); err != nil {
			return err
		}
	}
	return nil
}

// FlatStdoutRecordWriter implements RecordWriter for writing Records as flat JSON lines.
// Each record is written directly without wrapper, including a "_table" field inline.
type FlatStdoutRecordWriter struct {
	writer  io.Writer
	encoder *json.Encoder
}

// FlatStdoutRecordWriterOption configures a FlatStdoutRecordWriter.
type FlatStdoutRecordWriterOption func(*FlatStdoutRecordWriter)

// WithFlatWriter sets a custom writer (defaults to os.Stdout).
func WithFlatWriter(w io.Writer) FlatStdoutRecordWriterOption {
	return func(s *FlatStdoutRecordWriter) {
		s.writer = w
	}
}

// NewFlatStdoutRecordWriter creates a new FlatStdoutRecordWriter with the given options.
func NewFlatStdoutRecordWriter(opts ...FlatStdoutRecordWriterOption) *FlatStdoutRecordWriter {
	s := &FlatStdoutRecordWriter{
		writer: os.Stdout,
	}
	for _, opt := range opts {
		opt(s)
	}
	s.encoder = json.NewEncoder(s.writer)
	return s
}

// WriteRecords writes each Record as a flat JSON line with _table field.
func (s *FlatStdoutRecordWriter) WriteRecords(ctx context.Context, records []Record) error {
	for _, record := range records {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Build map directly from struct using reflection (avoids marshal/unmarshal round-trip)
		m := structToJSONMap(record)
		m["_table"] = record.TableName()

		if err := s.encoder.Encode(m); err != nil {
			return err
		}
	}
	return nil
}

// structToJSONMap converts a struct to a map using json tags for field names.
func structToJSONMap(v any) map[string]any {
	result := make(map[string]any)
	val := reflect.ValueOf(v)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}
	if val.Kind() != reflect.Struct {
		return result
	}
	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		field := typ.Field(i)
		jsonTag := field.Tag.Get("json")
		if jsonTag == "-" {
			continue
		}

		// Handle json tag options like `json:"name,omitempty"`
		name := strings.Split(jsonTag, ",")[0]
		if name == "" {
			name = field.Name
		}

		result[name] = val.Field(i).Interface()
	}
	return result
}
