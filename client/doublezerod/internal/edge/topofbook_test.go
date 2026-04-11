package edge

import (
	"encoding/binary"
	"math"
	"testing"
	"time"
)

// buildFrame constructs a raw Top-of-Book frame from a header and message payloads.
func buildFrame(channelID uint8, seq uint64, sendTS uint64, msgs ...[]byte) []byte {
	headerSize := 24
	bodySize := 0
	for _, m := range msgs {
		bodySize += len(m)
	}
	frameLen := headerSize + bodySize

	buf := make([]byte, frameLen)
	// Magic "DZ" = 0x445A little-endian → bytes 0x5A, 0x44
	buf[0] = 0x5A
	buf[1] = 0x44
	buf[2] = 1 // schema version
	buf[3] = channelID
	binary.LittleEndian.PutUint64(buf[4:], seq)
	binary.LittleEndian.PutUint64(buf[12:], sendTS)
	buf[20] = uint8(len(msgs)) // msg count
	buf[21] = 0                // reserved
	binary.LittleEndian.PutUint16(buf[22:], uint16(frameLen))

	off := headerSize
	for _, m := range msgs {
		copy(buf[off:], m)
		off += len(m)
	}
	return buf
}

// buildInstrumentDef constructs a 80-byte InstrumentDefinition message.
func buildInstrumentDef(instID uint32, symbol string, leg1, leg2 string, priceExp, qtyExp int8) []byte {
	buf := make([]byte, 80)
	buf[0] = 0x02                             // type
	buf[1] = 80                               // length
	binary.LittleEndian.PutUint16(buf[2:], 0) // flags

	binary.LittleEndian.PutUint32(buf[4:], instID)
	copy(buf[8:24], padNull(symbol, 16))
	copy(buf[24:32], padNull(leg1, 8))
	copy(buf[32:40], padNull(leg2, 8))
	buf[40] = 1                                // asset class: crypto spot
	buf[41] = byte(priceExp)                   // price exponent (signed)
	buf[42] = byte(qtyExp)                     // qty exponent (signed)
	buf[43] = 1                                // market model: CLOB
	binary.LittleEndian.PutUint64(buf[44:], 1) // tick size
	binary.LittleEndian.PutUint64(buf[52:], 1) // lot size
	binary.LittleEndian.PutUint64(buf[60:], 0) // contract value
	binary.LittleEndian.PutUint64(buf[68:], 0) // expiry
	buf[76] = 0                                // settle type
	buf[77] = 0                                // price bound
	binary.LittleEndian.PutUint16(buf[78:], 1) // manifest seq
	return buf
}

// buildQuote constructs a 60-byte Quote message.
func buildQuote(instID uint32, sourceID uint16, srcTS uint64, bidPrice int64, bidQty uint64, askPrice int64, askQty uint64, flags uint16) []byte {
	buf := make([]byte, 60)
	buf[0] = 0x03 // type
	buf[1] = 60   // length
	binary.LittleEndian.PutUint16(buf[2:], flags)

	binary.LittleEndian.PutUint32(buf[4:], instID)
	binary.LittleEndian.PutUint16(buf[8:], sourceID)
	buf[10] = 0x03 // update flags: bid + ask updated
	buf[11] = 0    // reserved

	binary.LittleEndian.PutUint64(buf[12:], srcTS)
	putInt64LE(buf[20:], bidPrice)
	binary.LittleEndian.PutUint64(buf[28:], bidQty)
	putInt64LE(buf[36:], askPrice)
	binary.LittleEndian.PutUint64(buf[44:], askQty)
	binary.LittleEndian.PutUint16(buf[52:], 5) // bid source count
	binary.LittleEndian.PutUint16(buf[54:], 3) // ask source count
	// 4 bytes reserved at 56-59
	return buf
}

// buildTrade constructs a 52-byte Trade message.
func buildTrade(instID uint32, sourceID uint16, srcTS uint64, price int64, qty uint64, side uint8) []byte {
	buf := make([]byte, 52)
	buf[0] = 0x04                             // type
	buf[1] = 52                               // length
	binary.LittleEndian.PutUint16(buf[2:], 0) // flags

	binary.LittleEndian.PutUint32(buf[4:], instID)
	binary.LittleEndian.PutUint16(buf[8:], sourceID)
	buf[10] = side // aggressor side
	buf[11] = 0    // trade flags

	binary.LittleEndian.PutUint64(buf[12:], srcTS)
	putInt64LE(buf[20:], price)
	binary.LittleEndian.PutUint64(buf[28:], qty)
	binary.LittleEndian.PutUint64(buf[36:], 12345) // trade ID
	binary.LittleEndian.PutUint64(buf[44:], qty)   // cumulative volume
	return buf
}

// buildHeartbeat constructs a 16-byte Heartbeat message.
func buildHeartbeat(channelID uint8, ts uint64) []byte {
	buf := make([]byte, 16)
	buf[0] = 0x01                             // type
	buf[1] = 16                               // length
	binary.LittleEndian.PutUint16(buf[2:], 0) // flags
	buf[4] = channelID
	// 3 bytes reserved
	binary.LittleEndian.PutUint64(buf[8:], ts)
	return buf
}

// buildChannelReset constructs a 12-byte ChannelReset message.
func buildChannelReset(ts uint64) []byte {
	buf := make([]byte, 12)
	buf[0] = 0x05 // type
	buf[1] = 12   // length
	binary.LittleEndian.PutUint16(buf[2:], 0)
	binary.LittleEndian.PutUint64(buf[4:], ts)
	return buf
}

func padNull(s string, n int) []byte {
	buf := make([]byte, n)
	copy(buf, s)
	return buf
}

func putInt64LE(buf []byte, v int64) {
	binary.LittleEndian.PutUint64(buf, uint64(v))
}

func TestTopOfBookParser_InstrumentDefinition(t *testing.T) {
	p := NewTopOfBookParser()

	ts := uint64(time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC).UnixNano())
	instDef := buildInstrumentDef(42, "BTC-USDT", "BTC", "USDT", -2, -8)
	frame := buildFrame(1, 100, ts, instDef)

	records, err := p.Parse(frame)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.Type != "instrument_definition" {
		t.Errorf("expected type instrument_definition, got %s", r.Type)
	}
	if r.InstrumentID != 42 {
		t.Errorf("expected instrument ID 42, got %d", r.InstrumentID)
	}
	if r.Symbol != "BTC-USDT" {
		t.Errorf("expected symbol BTC-USDT, got %q", r.Symbol)
	}
	if r.ChannelID != 1 {
		t.Errorf("expected channel ID 1, got %d", r.ChannelID)
	}
	if r.SequenceNumber != 100 {
		t.Errorf("expected sequence number 100, got %d", r.SequenceNumber)
	}
	if r.Fields["price_exponent"] != int8(-2) {
		t.Errorf("expected price_exponent -2, got %v", r.Fields["price_exponent"])
	}
	if r.Fields["leg1"] != "BTC" {
		t.Errorf("expected leg1 BTC, got %v", r.Fields["leg1"])
	}
}

func TestTopOfBookParser_QuoteWithDefinition(t *testing.T) {
	p := NewTopOfBookParser()

	ts := uint64(time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC).UnixNano())

	// First send instrument definition.
	instDef := buildInstrumentDef(42, "BTC-USDT", "BTC", "USDT", -2, -8)
	frame1 := buildFrame(1, 100, ts, instDef)
	_, err := p.Parse(frame1)
	if err != nil {
		t.Fatalf("error parsing instrument def: %v", err)
	}

	// Now send a quote.
	srcTS := uint64(time.Date(2026, 4, 10, 12, 0, 1, 0, time.UTC).UnixNano())
	quote := buildQuote(42, 1, srcTS, 6743250, 125000000, 6743300, 80000000, 0)
	frame2 := buildFrame(1, 101, ts, quote)

	records, err := p.Parse(frame2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.Type != "quote" {
		t.Errorf("expected type quote, got %s", r.Type)
	}
	if r.Symbol != "BTC-USDT" {
		t.Errorf("expected symbol BTC-USDT, got %q", r.Symbol)
	}

	// With price exponent -2: 6743250 * 10^-2 = 67432.50
	bidPrice := r.Fields["bid_price"].(float64)
	if math.Abs(bidPrice-67432.50) > 0.001 {
		t.Errorf("expected bid_price 67432.50, got %f", bidPrice)
	}

	// With qty exponent -8: 125000000 * 10^-8 = 1.25
	bidQty := r.Fields["bid_qty"].(float64)
	if math.Abs(bidQty-1.25) > 0.0001 {
		t.Errorf("expected bid_qty 1.25, got %f", bidQty)
	}
}

func TestTopOfBookParser_QuoteBuffering(t *testing.T) {
	p := NewTopOfBookParser()

	ts := uint64(time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC).UnixNano())

	// Send a quote BEFORE the instrument definition — should be buffered.
	srcTS := uint64(time.Date(2026, 4, 10, 12, 0, 1, 0, time.UTC).UnixNano())
	quote := buildQuote(42, 1, srcTS, 6743250, 125000000, 6743300, 80000000, 0)
	frame1 := buildFrame(1, 101, ts, quote)

	records, err := p.Parse(frame1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records (buffered), got %d", len(records))
	}
	if p.Buffered() != 1 {
		t.Fatalf("expected 1 buffered message, got %d", p.Buffered())
	}

	// Now send the instrument definition — buffered quote should flush.
	instDef := buildInstrumentDef(42, "BTC-USDT", "BTC", "USDT", -2, -8)
	frame2 := buildFrame(1, 102, ts, instDef)

	records, err = p.Parse(frame2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should get the instrument def + the flushed quote.
	if len(records) != 2 {
		t.Fatalf("expected 2 records (def + flushed quote), got %d", len(records))
	}
	if records[0].Type != "instrument_definition" {
		t.Errorf("expected first record to be instrument_definition, got %s", records[0].Type)
	}
	if records[1].Type != "quote" {
		t.Errorf("expected second record to be quote, got %s", records[1].Type)
	}
	if p.Buffered() != 0 {
		t.Errorf("expected 0 buffered after flush, got %d", p.Buffered())
	}
}

func TestTopOfBookParser_Trade(t *testing.T) {
	p := NewTopOfBookParser()

	ts := uint64(time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC).UnixNano())

	// Register instrument first.
	instDef := buildInstrumentDef(42, "ETH-USDT", "ETH", "USDT", -2, -6)
	frame1 := buildFrame(1, 100, ts, instDef)
	p.Parse(frame1)

	// Send a trade (sell aggressor).
	srcTS := uint64(time.Date(2026, 4, 10, 12, 0, 5, 0, time.UTC).UnixNano())
	trade := buildTrade(42, 1, srcTS, 350025, 1500000, 2)
	frame2 := buildFrame(1, 101, ts, trade)

	records, err := p.Parse(frame2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	r := records[0]
	if r.Type != "trade" {
		t.Errorf("expected type trade, got %s", r.Type)
	}
	if r.Fields["aggressor_side"] != "sell" {
		t.Errorf("expected aggressor_side sell, got %v", r.Fields["aggressor_side"])
	}

	// 350025 * 10^-2 = 3500.25
	price := r.Fields["trade_price"].(float64)
	if math.Abs(price-3500.25) > 0.001 {
		t.Errorf("expected trade_price 3500.25, got %f", price)
	}
}

func TestTopOfBookParser_Heartbeat(t *testing.T) {
	p := NewTopOfBookParser()

	ts := uint64(time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC).UnixNano())
	hb := buildHeartbeat(3, ts)
	frame := buildFrame(3, 50, ts, hb)

	records, err := p.Parse(frame)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Type != "heartbeat" {
		t.Errorf("expected type heartbeat, got %s", records[0].Type)
	}
	if records[0].ChannelID != 3 {
		t.Errorf("expected channel ID 3, got %d", records[0].ChannelID)
	}
}

func TestTopOfBookParser_ChannelReset(t *testing.T) {
	p := NewTopOfBookParser()

	ts := uint64(time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC).UnixNano())

	// Add an instrument definition.
	instDef := buildInstrumentDef(42, "BTC-USDT", "BTC", "USDT", -2, -8)
	frame1 := buildFrame(1, 100, ts, instDef)
	p.Parse(frame1)

	// Channel reset should clear instruments.
	reset := buildChannelReset(ts)
	frame2 := buildFrame(1, 101, ts, reset)
	records, err := p.Parse(frame2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Type != "channel_reset" {
		t.Errorf("expected type channel_reset, got %s", records[0].Type)
	}

	// A quote for the same instrument should now be buffered (definition was cleared).
	srcTS := uint64(time.Date(2026, 4, 10, 12, 0, 1, 0, time.UTC).UnixNano())
	quote := buildQuote(42, 1, srcTS, 100, 200, 300, 400, 0)
	frame3 := buildFrame(1, 102, ts, quote)
	records, err = p.Parse(frame3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 0 {
		t.Errorf("expected 0 records (buffered after reset), got %d", len(records))
	}
	if p.Buffered() != 1 {
		t.Errorf("expected 1 buffered, got %d", p.Buffered())
	}
}

func TestTopOfBookParser_MultipleMessagesInFrame(t *testing.T) {
	p := NewTopOfBookParser()

	ts := uint64(time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC).UnixNano())

	// Build a frame with an instrument definition + a quote for that instrument.
	instDef := buildInstrumentDef(1, "SOL-USDT", "SOL", "USDT", -4, -6)
	srcTS := uint64(time.Date(2026, 4, 10, 12, 0, 1, 0, time.UTC).UnixNano())
	quote := buildQuote(1, 1, srcTS, 1850000, 5000000, 1860000, 3000000, 0)

	frame := buildFrame(1, 200, ts, instDef, quote)

	records, err := p.Parse(frame)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Instrument def is processed first, so the quote should be decoded immediately
	// (either directly or via buffer flush).
	if len(records) < 2 {
		t.Fatalf("expected at least 2 records, got %d", len(records))
	}

	var foundDef, foundQuote bool
	for _, r := range records {
		switch r.Type {
		case "instrument_definition":
			foundDef = true
			if r.Symbol != "SOL-USDT" {
				t.Errorf("expected symbol SOL-USDT, got %q", r.Symbol)
			}
		case "quote":
			foundQuote = true
			// 1850000 * 10^-4 = 185.0000
			bidPrice := r.Fields["bid_price"].(float64)
			if math.Abs(bidPrice-185.0) > 0.001 {
				t.Errorf("expected bid_price 185.0, got %f", bidPrice)
			}
		}
	}
	if !foundDef {
		t.Error("expected instrument_definition record")
	}
	if !foundQuote {
		t.Error("expected quote record")
	}
}

func TestTopOfBookParser_SnapshotFlag(t *testing.T) {
	p := NewTopOfBookParser()

	ts := uint64(time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC).UnixNano())

	// Register instrument.
	instDef := buildInstrumentDef(1, "BTC-USDT", "BTC", "USDT", -2, -8)
	frame1 := buildFrame(1, 100, ts, instDef)
	p.Parse(frame1)

	// Send a quote with snapshot flag set (flags bit 0 = 1).
	srcTS := uint64(time.Date(2026, 4, 10, 12, 0, 1, 0, time.UTC).UnixNano())
	quote := buildQuote(1, 1, srcTS, 100, 200, 300, 400, 1) // flags=1 → snapshot
	frame2 := buildFrame(1, 101, ts, quote)

	records, err := p.Parse(frame2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Fields["snapshot"] != true {
		t.Errorf("expected snapshot=true, got %v", records[0].Fields["snapshot"])
	}
}

func TestTopOfBookParser_UnknownMessageTypeSkipped(t *testing.T) {
	p := NewTopOfBookParser()

	ts := uint64(time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC).UnixNano())

	// Build an unknown message type (0xFF) with 12-byte body.
	unknownMsg := make([]byte, 12)
	unknownMsg[0] = 0xFF // unknown type
	unknownMsg[1] = 12   // length
	// rest is zeros

	hb := buildHeartbeat(1, ts)
	frame := buildFrame(1, 100, ts, unknownMsg, hb)

	records, err := p.Parse(frame)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the heartbeat should produce a record; unknown type is skipped.
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Type != "heartbeat" {
		t.Errorf("expected heartbeat, got %s", records[0].Type)
	}
}

func TestTopOfBookParser_GarbageData(t *testing.T) {
	p := NewTopOfBookParser()

	tests := []struct {
		name string
		data []byte
	}{
		{"empty", []byte{}},
		{"too short", []byte{0x5A, 0x44, 0x01}},
		{"wrong magic", append(make([]byte, 24), 0x00)},
		{"zero messages", func() []byte {
			buf := make([]byte, 24)
			buf[0] = 0x5A
			buf[1] = 0x44
			buf[2] = 1  // schema version
			buf[20] = 0 // msg count = 0
			binary.LittleEndian.PutUint16(buf[22:], 24)
			return buf
		}()},
		{"bad schema version", func() []byte {
			buf := make([]byte, 24)
			buf[0] = 0x5A
			buf[1] = 0x44
			buf[2] = 99 // unsupported version
			buf[20] = 1
			binary.LittleEndian.PutUint16(buf[22:], 24)
			return buf
		}()},
		{"frame length mismatch", func() []byte {
			ts := uint64(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixNano())
			hb := buildHeartbeat(1, ts)
			frame := buildFrame(1, 1, ts, hb)
			// Corrupt frame length to be larger than datagram.
			binary.LittleEndian.PutUint16(frame[22:], uint16(len(frame)+100))
			return frame
		}()},
		{"random bytes", []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x11, 0x22, 0x33,
			0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA, 0xBB,
			0xCC, 0xDD, 0xEE, 0xFF, 0x01, 0x02, 0x03, 0x04}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			records, err := p.Parse(tt.data)
			if err == nil {
				t.Errorf("expected error for %s input, got %d records", tt.name, len(records))
			}
		})
	}
}

func TestApplyExponent_ExtremeValues(t *testing.T) {
	// Extreme exponents that would produce Inf should return 0.
	v := applyExponent(math.MaxInt64, 127)
	if v != 0 {
		t.Errorf("expected 0 for overflow, got %f", v)
	}

	// Normal exponents should still work.
	v = applyExponent(100, -2)
	if math.Abs(v-1.0) > 0.001 {
		t.Errorf("expected 1.0, got %f", v)
	}

	// Unsigned variant.
	v = applyExponentUnsigned(math.MaxUint64, 127)
	if v != 0 {
		t.Errorf("expected 0 for unsigned overflow, got %f", v)
	}
}

func TestApplyExponent(t *testing.T) {
	tests := []struct {
		raw    int64
		exp    int8
		expect float64
	}{
		{6743250, -2, 67432.50},
		{100, 0, 100.0},
		{5, 3, 5000.0},
		{-100, -1, -10.0},
	}
	for _, tt := range tests {
		got := applyExponent(tt.raw, tt.exp)
		if math.Abs(got-tt.expect) > 0.001 {
			t.Errorf("applyExponent(%d, %d) = %f, want %f", tt.raw, tt.exp, got, tt.expect)
		}
	}
}

func TestTrimNull(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"BTC-USDT\x00\x00\x00\x00\x00\x00\x00\x00", "BTC-USDT"},
		{"SOL\x00\x00\x00\x00\x00", "SOL"},
		{"ABCDEFGHIJKLMNOP", "ABCDEFGHIJKLMNOP"},
		{"\x00\x00\x00\x00", ""},
	}
	for _, tt := range tests {
		got := trimNull(tt.input)
		if got != tt.expect {
			t.Errorf("trimNull(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}
