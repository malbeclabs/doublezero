package edge

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"sync"
)

// CSV column definitions for each supported record type.
var (
	quoteCSVHeader = []string{
		"type", "ts", "channel_id", "seq", "instrument_id", "symbol",
		"source_id", "bid_price", "bid_qty", "ask_price", "ask_qty",
		"bid_source_count", "ask_source_count", "update_flags", "snapshot",
	}
	tradeCSVHeader = []string{
		"type", "ts", "channel_id", "seq", "instrument_id", "symbol",
		"source_id", "trade_price", "trade_qty", "aggressor_side",
		"trade_id", "cumulative_volume", "snapshot",
	}
)

// CSVFileSink writes quote and trade records as CSV to a file.
// Each record type gets its own header row on first occurrence.
type CSVFileSink struct {
	mu            sync.Mutex
	file          *os.File
	w             *csv.Writer
	wroteQuoteHdr bool
	wroteTradeHdr bool
}

// NewCSVFileSink opens (or creates) the file at path for CSV output.
func NewCSVFileSink(path string) (*CSVFileSink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening output file: %w", err)
	}
	return &CSVFileSink{file: f, w: csv.NewWriter(f)}, nil
}

func (s *CSVFileSink) Write(records []Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range records {
		r := &records[i]
		switch r.Type {
		case "quote":
			if !s.wroteQuoteHdr {
				if err := s.w.Write(quoteCSVHeader); err != nil {
					return fmt.Errorf("writing quote header: %w", err)
				}
				s.wroteQuoteHdr = true
			}
			if err := s.w.Write(quoteToCSVRow(r)); err != nil {
				return fmt.Errorf("writing quote row: %w", err)
			}
		case "trade":
			if !s.wroteTradeHdr {
				if err := s.w.Write(tradeCSVHeader); err != nil {
					return fmt.Errorf("writing trade header: %w", err)
				}
				s.wroteTradeHdr = true
			}
			if err := s.w.Write(tradeToCSVRow(r)); err != nil {
				return fmt.Errorf("writing trade row: %w", err)
			}
		default:
			// CSV only outputs quotes and trades.
			continue
		}
	}
	s.w.Flush()
	return s.w.Error()
}

func (s *CSVFileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.w.Flush()
	return s.file.Close()
}

func quoteToCSVRow(r *Record) []string {
	return []string{
		r.Type,
		r.Timestamp.UTC().Format("2006-01-02T15:04:05.000000000Z"),
		strconv.FormatUint(uint64(r.ChannelID), 10),
		strconv.FormatUint(r.SequenceNumber, 10),
		strconv.FormatUint(uint64(r.InstrumentID), 10),
		r.Symbol,
		fmtFieldUint16(r.Fields, "source_id"),
		fmtFieldFloat(r.Fields, "bid_price"),
		fmtFieldFloat(r.Fields, "bid_qty"),
		fmtFieldFloat(r.Fields, "ask_price"),
		fmtFieldFloat(r.Fields, "ask_qty"),
		fmtFieldUint16(r.Fields, "bid_source_count"),
		fmtFieldUint16(r.Fields, "ask_source_count"),
		fmtFieldUint8(r.Fields, "update_flags"),
		fmtFieldBool(r.Fields, "snapshot"),
	}
}

func tradeToCSVRow(r *Record) []string {
	return []string{
		r.Type,
		r.Timestamp.UTC().Format("2006-01-02T15:04:05.000000000Z"),
		strconv.FormatUint(uint64(r.ChannelID), 10),
		strconv.FormatUint(r.SequenceNumber, 10),
		strconv.FormatUint(uint64(r.InstrumentID), 10),
		r.Symbol,
		fmtFieldUint16(r.Fields, "source_id"),
		fmtFieldFloat(r.Fields, "trade_price"),
		fmtFieldFloat(r.Fields, "trade_qty"),
		fmtFieldString(r.Fields, "aggressor_side"),
		fmtFieldUint64(r.Fields, "trade_id"),
		fmtFieldFloat(r.Fields, "cumulative_volume"),
		fmtFieldBool(r.Fields, "snapshot"),
	}
}

func fmtFieldFloat(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch f := v.(type) {
	case float64:
		return strconv.FormatFloat(f, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

func fmtFieldUint8(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch n := v.(type) {
	case uint8:
		return strconv.FormatUint(uint64(n), 10)
	default:
		return fmt.Sprint(v)
	}
}

func fmtFieldUint16(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch n := v.(type) {
	case uint16:
		return strconv.FormatUint(uint64(n), 10)
	default:
		return fmt.Sprint(v)
	}
}

func fmtFieldUint64(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	switch n := v.(type) {
	case uint64:
		return strconv.FormatUint(n, 10)
	default:
		return fmt.Sprint(v)
	}
}

func fmtFieldString(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	return fmt.Sprint(v)
}

func fmtFieldBool(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	if b, ok := v.(bool); ok {
		return strconv.FormatBool(b)
	}
	return fmt.Sprint(v)
}
