package edge

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJSONFileSink_Write(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.jsonl")

	sink, err := NewJSONFileSink(path)
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
				"bid_price": 67432.50,
				"ask_price": 67433.00,
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
				"trade_price":    67432.75,
				"aggressor_side": "buy",
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
		t.Fatalf("error opening output file: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var decoded []Record
	for scanner.Scan() {
		var r Record
		if err := json.Unmarshal(scanner.Bytes(), &r); err != nil {
			t.Fatalf("error decoding line: %v", err)
		}
		decoded = append(decoded, r)
	}

	if len(decoded) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(decoded))
	}
	if decoded[0].Type != "quote" {
		t.Errorf("expected first record type quote, got %s", decoded[0].Type)
	}
	if decoded[0].Symbol != "BTC-USDT" {
		t.Errorf("expected symbol BTC-USDT, got %s", decoded[0].Symbol)
	}
	if decoded[1].Type != "trade" {
		t.Errorf("expected second record type trade, got %s", decoded[1].Type)
	}
}

func TestJSONFileSink_Append(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "output.jsonl")

	ts := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)

	// Write first batch.
	sink1, err := NewJSONFileSink(path)
	if err != nil {
		t.Fatalf("error creating sink: %v", err)
	}
	sink1.Write([]Record{{Type: "heartbeat", Timestamp: ts, ChannelID: 1, SequenceNumber: 1}})
	sink1.Close()

	// Write second batch (should append).
	sink2, err := NewJSONFileSink(path)
	if err != nil {
		t.Fatalf("error creating sink: %v", err)
	}
	sink2.Write([]Record{{Type: "heartbeat", Timestamp: ts, ChannelID: 1, SequenceNumber: 2}})
	sink2.Close()

	// Count lines.
	f, _ := os.Open(path)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	count := 0
	for scanner.Scan() {
		count++
	}
	if count != 2 {
		t.Errorf("expected 2 lines after append, got %d", count)
	}
}
