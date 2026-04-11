package edge

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestIntegration_PublisherToJSONL exercises the full pipeline:
// synthetic Top-of-Book frames → parser → JSONL file sink → read-back verification.
//
// This is the edge feed parser equivalent of the pcap fixture tests
// in telemetry/flow-enricher: we construct realistic wire-format data,
// feed it through the real parser and sink, and verify the output.
func TestIntegration_PublisherToJSONL(t *testing.T) {
	parser := NewTopOfBookParser()
	outputPath := filepath.Join(t.TempDir(), "feed.jsonl")
	sink, err := NewJSONFileSink(outputPath)
	if err != nil {
		t.Fatalf("creating sink: %v", err)
	}
	defer sink.Close()

	// Simulate a realistic publisher session:
	// 1. InstrumentDefinition for BTC-USDT
	// 2. InstrumentDefinition for ETH-USDT
	// 3. Several quotes for both instruments
	// 4. A trade
	// 5. A heartbeat

	ts := uint64(time.Date(2026, 4, 10, 14, 30, 0, 0, time.UTC).UnixNano())
	seq := uint64(0)

	// --- Refdata: instrument definitions ---
	btcDef := buildInstrumentDef(1, "BTC-USDT", "BTC", "USDT", -2, -8)
	ethDef := buildInstrumentDef(2, "ETH-USDT", "ETH", "USDT", -2, -6)
	seq++
	refFrame := buildFrame(1, seq, ts, btcDef, ethDef)
	processFrame(t, parser, sink, refFrame)

	// --- Hot path: quotes ---
	srcTS1 := uint64(time.Date(2026, 4, 10, 14, 30, 0, 100000000, time.UTC).UnixNano())
	q1 := buildQuote(1, 1, srcTS1, 6743250, 125000000, 6743300, 80000000, 0) // BTC
	seq++
	processFrame(t, parser, sink, buildFrame(1, seq, ts, q1))

	srcTS2 := uint64(time.Date(2026, 4, 10, 14, 30, 0, 200000000, time.UTC).UnixNano())
	q2 := buildQuote(2, 1, srcTS2, 350025, 1500000, 350075, 1000000, 0) // ETH
	seq++
	processFrame(t, parser, sink, buildFrame(1, seq, ts, q2))

	// Multiple quotes in one frame (batched).
	srcTS3 := uint64(time.Date(2026, 4, 10, 14, 30, 0, 300000000, time.UTC).UnixNano())
	q3 := buildQuote(1, 1, srcTS3, 6743200, 130000000, 6743350, 75000000, 0)
	q4 := buildQuote(2, 1, srcTS3, 350000, 1600000, 350100, 900000, 0)
	seq++
	processFrame(t, parser, sink, buildFrame(1, seq, ts, q3, q4))

	// --- Hot path: trade ---
	srcTS4 := uint64(time.Date(2026, 4, 10, 14, 30, 0, 400000000, time.UTC).UnixNano())
	trade := buildTrade(1, 1, srcTS4, 6743275, 50000000, 1) // buy
	seq++
	processFrame(t, parser, sink, buildFrame(1, seq, ts, trade))

	// --- Hot path: heartbeat ---
	hb := buildHeartbeat(1, ts)
	seq++
	processFrame(t, parser, sink, buildFrame(1, seq, ts, hb))

	// Close sink to flush.
	sink.Close()

	// --- Read back and verify ---
	records := readJSONL(t, outputPath)

	// Expected: 2 inst defs + 4 quotes + 1 trade + 1 heartbeat = 8 records.
	if len(records) != 8 {
		t.Fatalf("expected 8 records, got %d", len(records))
	}

	// Verify record types in order.
	expectedTypes := []string{
		"instrument_definition", "instrument_definition",
		"quote", "quote", "quote", "quote",
		"trade", "heartbeat",
	}
	for i, want := range expectedTypes {
		got := records[i]["type"].(string)
		if got != want {
			t.Errorf("record %d: expected type %q, got %q", i, want, got)
		}
	}

	// Verify BTC-USDT instrument definition.
	instDef := records[0]
	if instDef["symbol"] != "BTC-USDT" {
		t.Errorf("expected symbol BTC-USDT, got %v", instDef["symbol"])
	}

	// Verify first BTC quote has correct decoded prices.
	btcQuote := records[2]
	if btcQuote["symbol"] != "BTC-USDT" {
		t.Errorf("expected BTC-USDT quote, got symbol %v", btcQuote["symbol"])
	}
	fields := btcQuote["fields"].(map[string]any)
	bidPrice := fields["bid_price"].(float64)
	if math.Abs(bidPrice-67432.50) > 0.01 {
		t.Errorf("expected bid_price 67432.50, got %f", bidPrice)
	}
	bidQty := fields["bid_qty"].(float64)
	if math.Abs(bidQty-1.25) > 0.0001 {
		t.Errorf("expected bid_qty 1.25, got %f", bidQty)
	}

	// Verify ETH-USDT quote.
	ethQuote := records[3]
	if ethQuote["symbol"] != "ETH-USDT" {
		t.Errorf("expected ETH-USDT quote, got symbol %v", ethQuote["symbol"])
	}
	ethFields := ethQuote["fields"].(map[string]any)
	ethBid := ethFields["bid_price"].(float64)
	if math.Abs(ethBid-3500.25) > 0.01 {
		t.Errorf("expected ETH bid_price 3500.25, got %f", ethBid)
	}

	// Verify trade.
	tradeRec := records[6]
	if tradeRec["type"] != "trade" {
		t.Errorf("expected trade, got %v", tradeRec["type"])
	}
	tradeFields := tradeRec["fields"].(map[string]any)
	if tradeFields["aggressor_side"] != "buy" {
		t.Errorf("expected aggressor_side buy, got %v", tradeFields["aggressor_side"])
	}
	tradePrice := tradeFields["trade_price"].(float64)
	if math.Abs(tradePrice-67432.75) > 0.01 {
		t.Errorf("expected trade_price 67432.75, got %f", tradePrice)
	}
}

// TestIntegration_BufferingThenFlush verifies that the full pipeline
// correctly buffers quotes arriving before instrument definitions,
// then flushes them once the definition arrives.
func TestIntegration_BufferingThenFlush(t *testing.T) {
	parser := NewTopOfBookParser()
	outputPath := filepath.Join(t.TempDir(), "feed.jsonl")
	sink, err := NewJSONFileSink(outputPath)
	if err != nil {
		t.Fatalf("creating sink: %v", err)
	}
	defer sink.Close()

	ts := uint64(time.Date(2026, 4, 10, 14, 30, 0, 0, time.UTC).UnixNano())

	// Send quotes BEFORE instrument definitions (cold-start scenario).
	// Use two different instruments so both slots are occupied; the
	// buffer now holds at most one pending message per instrument_id.
	srcTS := uint64(time.Date(2026, 4, 10, 14, 30, 0, 100000000, time.UTC).UnixNano())
	q1 := buildQuote(1, 1, srcTS, 6743250, 125000000, 6743300, 80000000, 0) // instrument 1
	q2 := buildQuote(2, 1, srcTS, 350025, 1500000, 350075, 1000000, 0)      // instrument 2
	processFrame(t, parser, sink, buildFrame(1, 1, ts, q1))
	processFrame(t, parser, sink, buildFrame(1, 2, ts, q2))

	// No records should have been written yet.
	sink.Close()
	records := readJSONL(t, outputPath)
	if len(records) != 0 {
		t.Fatalf("expected 0 records before instrument def, got %d", len(records))
	}
	if parser.Buffered() != 2 {
		t.Fatalf("expected 2 buffered messages, got %d", parser.Buffered())
	}

	// Re-open sink (simulating continued operation).
	sink2, err := NewJSONFileSink(outputPath)
	if err != nil {
		t.Fatalf("creating sink2: %v", err)
	}
	defer sink2.Close()

	// Send first instrument definition — only instrument 1's buffered
	// quote should flush; instrument 2 remains pending.
	instDef := buildInstrumentDef(1, "BTC-USDT", "BTC", "USDT", -2, -8)
	processFrame2(t, parser, sink2, buildFrame(1, 3, ts, instDef))

	if parser.Buffered() != 1 {
		t.Fatalf("expected 1 buffered message after partial flush, got %d", parser.Buffered())
	}

	// Send second instrument definition — the last buffered quote flushes.
	instDef2 := buildInstrumentDef(2, "ETH-USDT", "ETH", "USDT", -2, -6)
	processFrame2(t, parser, sink2, buildFrame(1, 4, ts, instDef2))

	if parser.Buffered() != 0 {
		t.Fatalf("expected empty buffer after full flush, got %d", parser.Buffered())
	}

	sink2.Close()

	records = readJSONL(t, outputPath)
	// Should have: 2 instrument_definitions + 2 flushed quotes = 4 records.
	if len(records) != 4 {
		t.Fatalf("expected 4 records after flush, got %d", len(records))
	}
	// After the two instrument definitions land, the expected output
	// is: instrument_def(1), quote(1), instrument_def(2), quote(2).
	expectedTypes := []string{"instrument_definition", "quote", "instrument_definition", "quote"}
	for i, want := range expectedTypes {
		if got := records[i]["type"]; got != want {
			t.Errorf("record %d: expected %q, got %v", i, want, got)
		}
	}
	if parser.Buffered() != 0 {
		t.Errorf("expected 0 buffered after flush, got %d", parser.Buffered())
	}
}

// TestBufferOverwriteAndCap verifies the bounded per-instrument buffer:
// repeated messages for the same instrument overwrite each other (keeping
// only the most recent), and new instruments are dropped once the cap is
// reached.
func TestBufferOverwriteAndCap(t *testing.T) {
	parser := NewTopOfBookParser()

	ts := uint64(time.Date(2026, 4, 10, 14, 30, 0, 0, time.UTC).UnixNano())
	srcTS := ts

	// Flood the buffer with maxBufferedInstruments distinct instruments.
	// The +1 beyond the cap should be dropped (not buffered, not errored).
	seq := uint64(0)
	for i := 0; i < maxBufferedInstruments+1; i++ {
		seq++
		q := buildQuote(uint32(i+1), 1, srcTS, 100, 1, 101, 1, 0)
		_, err := parser.Parse(buildFrame(1, seq, ts, q))
		if err != nil {
			t.Fatalf("parse iter %d: %v", i, err)
		}
	}

	if got := parser.Buffered(); got != maxBufferedInstruments {
		t.Errorf("expected buffered = %d (cap), got %d", maxBufferedInstruments, got)
	}

	// Sending another quote for an instrument already in the buffer must
	// overwrite in place — buffer size stays at the cap.
	seq++
	q := buildQuote(1, 1, srcTS, 999, 999, 999, 999, 0)
	if _, err := parser.Parse(buildFrame(1, seq, ts, q)); err != nil {
		t.Fatalf("overwrite parse: %v", err)
	}
	if got := parser.Buffered(); got != maxBufferedInstruments {
		t.Errorf("buffer should stay at cap after overwrite, got %d", got)
	}

	// Defining instrument 1 flushes exactly one record with the most
	// recent (overwritten) values, confirming the overwrite took effect.
	seq++
	instDef := buildInstrumentDef(1, "SYM-1", "A", "B", -2, -8)
	records, err := parser.Parse(buildFrame(1, seq, ts, instDef))
	if err != nil {
		t.Fatalf("instrument def parse: %v", err)
	}

	var flushedQuote *Record
	for i := range records {
		if records[i].Type == "quote" {
			flushedQuote = &records[i]
			break
		}
	}
	if flushedQuote == nil {
		t.Fatal("expected a flushed quote record after instrument def")
	}
	// The overwrite set bid_price to 9.99 (999 with price_exponent -2).
	bidPrice, ok := flushedQuote.Fields["bid_price"].(float64)
	if !ok {
		t.Fatalf("bid_price missing or wrong type: %v", flushedQuote.Fields["bid_price"])
	}
	if math.Abs(bidPrice-9.99) > 0.001 {
		t.Errorf("expected overwritten bid_price 9.99, got %v", bidPrice)
	}
}

// TestIntegration_CSVOutput verifies the full pipeline with CSV output.
func TestIntegration_CSVOutput(t *testing.T) {
	parser := NewTopOfBookParser()
	outputPath := filepath.Join(t.TempDir(), "feed.csv")
	sink, err := NewCSVFileSink(outputPath)
	if err != nil {
		t.Fatalf("creating sink: %v", err)
	}
	defer sink.Close()

	ts := uint64(time.Date(2026, 4, 10, 14, 30, 0, 0, time.UTC).UnixNano())

	// Send instrument def, then a quote, then a trade.
	instDef := buildInstrumentDef(1, "SOL-USDT", "SOL", "USDT", -4, -6)
	processFrame(t, parser, sink, buildFrame(1, 1, ts, instDef))

	srcTS := uint64(time.Date(2026, 4, 10, 14, 30, 1, 0, time.UTC).UnixNano())
	q := buildQuote(1, 1, srcTS, 1850000, 5000000, 1860000, 3000000, 0)
	processFrame(t, parser, sink, buildFrame(1, 2, ts, q))

	trade := buildTrade(1, 1, srcTS, 1855000, 2000000, 2) // sell
	processFrame(t, parser, sink, buildFrame(1, 3, ts, trade))

	sink.Close()

	// CSV should contain: quote header + quote row + trade header + trade row.
	// Instrument definitions and heartbeats are filtered out by CSV sink.
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}

	lines := splitNonEmpty(string(data))
	// 1 quote header + 1 quote row + 1 trade header + 1 trade row = 4 lines.
	if len(lines) != 4 {
		t.Fatalf("expected 4 CSV lines, got %d: %v", len(lines), lines)
	}

	// First non-header line should be the quote.
	if lines[1][:5] != "quote" {
		t.Errorf("expected quote row, got %q", lines[1][:10])
	}
	// Third line is trade header, fourth is trade data.
	if lines[3][:5] != "trade" {
		t.Errorf("expected trade row, got %q", lines[3][:10])
	}
}

// processFrame parses a frame and writes any records to the sink.
func processFrame(t *testing.T, parser Parser, sink OutputSink, frame []byte) {
	t.Helper()
	records, err := parser.Parse(frame)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(records) > 0 {
		if err := sink.Write(records); err != nil {
			t.Fatalf("sink write error: %v", err)
		}
	}
}

// processFrame2 is identical to processFrame but doesn't use t.Fatalf
// for the sink write (allows caller to handle).
func processFrame2(t *testing.T, parser Parser, sink OutputSink, frame []byte) {
	t.Helper()
	records, err := parser.Parse(frame)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(records) > 0 {
		if err := sink.Write(records); err != nil {
			t.Fatalf("sink write error: %v", err)
		}
	}
}

// readJSONL reads a JSONL file and returns parsed records.
func readJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening %s: %v", path, err)
	}
	defer f.Close()

	var records []map[string]any
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var r map[string]any
		if err := json.Unmarshal(line, &r); err != nil {
			t.Fatalf("parsing JSONL line: %v", err)
		}
		records = append(records, r)
	}
	return records
}

func splitNonEmpty(s string) []string {
	var out []string
	for _, line := range split(s) {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func split(s string) []string {
	return splitBy(s, '\n')
}

func splitBy(s string, sep byte) []string {
	var result []string
	start := 0
	for i := range len(s) {
		if s[i] == sep {
			result = append(result, s[start:i])
			start = i + 1
		}
	}
	result = append(result, s[start:])
	return result
}

// buildFrame and message builders are reused from topofbook_test.go
// (they are in the same package).
// The following are only needed if the test needs additional frame types
// not covered by the existing test helpers.

func putInt64LE_integration(buf []byte, v int64) {
	binary.LittleEndian.PutUint64(buf, uint64(v))
}
