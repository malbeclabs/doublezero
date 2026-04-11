package edge

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCSVFileSink_QuotesAndTrades(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.csv")

	sink, err := NewCSVFileSink(path)
	if err != nil {
		t.Fatalf("error creating sink: %v", err)
	}

	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	records := []Record{
		{
			Type:           "quote",
			Timestamp:      ts,
			ChannelID:      1,
			SequenceNumber: 100,
			InstrumentID:   42,
			Symbol:         "BTC-USDT",
			Fields: map[string]any{
				"source_id":        uint16(1),
				"bid_price":        67432.5,
				"bid_qty":          1.25,
				"ask_price":        67433.0,
				"ask_qty":          0.8,
				"bid_source_count": uint16(5),
				"ask_source_count": uint16(3),
				"update_flags":     uint8(3),
				"snapshot":         false,
			},
		},
		{
			Type:           "trade",
			Timestamp:      ts,
			ChannelID:      1,
			SequenceNumber: 101,
			InstrumentID:   42,
			Symbol:         "BTC-USDT",
			Fields: map[string]any{
				"source_id":         uint16(1),
				"trade_price":       67432.75,
				"trade_qty":         0.5,
				"aggressor_side":    "buy",
				"trade_id":          uint64(12345),
				"cumulative_volume": 100.0,
				"snapshot":          false,
			},
		},
	}

	if err := sink.Write(records); err != nil {
		t.Fatalf("error writing records: %v", err)
	}
	if err := sink.Close(); err != nil {
		t.Fatalf("error closing sink: %v", err)
	}

	// Read back and verify.
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("error opening output: %v", err)
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.FieldsPerRecord = -1 // quote and trade rows have different column counts
	allRows, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("error reading CSV: %v", err)
	}

	// Expect: quote header, quote row, trade header, trade row = 4 rows.
	if len(allRows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(allRows))
	}

	// Quote header.
	if allRows[0][0] != "type" {
		t.Errorf("expected quote header first column 'type', got %q", allRows[0][0])
	}
	if len(allRows[0]) != len(quoteCSVHeader) {
		t.Errorf("quote header has %d columns, expected %d", len(allRows[0]), len(quoteCSVHeader))
	}

	// Quote row.
	if allRows[1][0] != "quote" {
		t.Errorf("expected 'quote', got %q", allRows[1][0])
	}
	if allRows[1][5] != "BTC-USDT" {
		t.Errorf("expected symbol BTC-USDT, got %q", allRows[1][5])
	}
	if allRows[1][7] != "67432.5" {
		t.Errorf("expected bid_price 67432.5, got %q", allRows[1][7])
	}

	// Trade header.
	if allRows[2][0] != "type" {
		t.Errorf("expected trade header first column 'type', got %q", allRows[2][0])
	}
	if len(allRows[2]) != len(tradeCSVHeader) {
		t.Errorf("trade header has %d columns, expected %d", len(allRows[2]), len(tradeCSVHeader))
	}

	// Trade row.
	if allRows[3][0] != "trade" {
		t.Errorf("expected 'trade', got %q", allRows[3][0])
	}
	if allRows[3][9] != "buy" {
		t.Errorf("expected aggressor_side 'buy', got %q", allRows[3][9])
	}
}

func TestCSVFileSink_SkipsNonQuoteTrade(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.csv")

	sink, err := NewCSVFileSink(path)
	if err != nil {
		t.Fatalf("error creating sink: %v", err)
	}

	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	records := []Record{
		{Type: "heartbeat", Timestamp: ts, ChannelID: 1, SequenceNumber: 1},
		{Type: "instrument_definition", Timestamp: ts, ChannelID: 1, SequenceNumber: 2},
		{Type: "channel_reset", Timestamp: ts, ChannelID: 1, SequenceNumber: 3},
	}

	if err := sink.Write(records); err != nil {
		t.Fatalf("error writing records: %v", err)
	}
	sink.Close()

	// File should be empty — no quote or trade records.
	info, _ := os.Stat(path)
	if info.Size() != 0 {
		t.Errorf("expected empty file for non-quote/trade records, got %d bytes", info.Size())
	}
}

func TestCSVFileSink_HeaderWrittenOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.csv")

	sink, err := NewCSVFileSink(path)
	if err != nil {
		t.Fatalf("error creating sink: %v", err)
	}

	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	quote := Record{
		Type: "quote", Timestamp: ts, ChannelID: 1, SequenceNumber: 100,
		InstrumentID: 1, Symbol: "SOL-USDT",
		Fields: map[string]any{
			"source_id": uint16(1), "bid_price": 185.0, "bid_qty": 10.0,
			"ask_price": 186.0, "ask_qty": 5.0, "bid_source_count": uint16(1),
			"ask_source_count": uint16(1), "update_flags": uint8(3), "snapshot": false,
		},
	}

	// Write two batches of quotes.
	sink.Write([]Record{quote})
	quote.SequenceNumber = 101
	sink.Write([]Record{quote})
	sink.Close()

	f, _ := os.Open(path)
	defer f.Close()
	rows, _ := csv.NewReader(f).ReadAll()

	// 1 header + 2 data rows = 3.
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (1 header + 2 data), got %d", len(rows))
	}
	if rows[0][0] != "type" {
		t.Errorf("expected header row first, got %q", rows[0][0])
	}
}
