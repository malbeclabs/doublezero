package edge

import (
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"
)

func init() {
	RegisterParser("topofbook", func() Parser { return NewTopOfBookParser() })
}

// instrumentInfo holds refdata needed to interpret Quote/Trade messages.
type instrumentInfo struct {
	Symbol        string
	PriceExponent int8
	QtyExponent   int8
}

// bufferedMsg holds a raw datagram message that arrived before its
// instrument definition was available.
type bufferedMsg struct {
	channelID uint8
	seq       uint64
	sendTS    uint64
	msg       *topOfBookAppMessage
}

// TopOfBookParser decodes DoubleZero Top-of-Book v0.1.0 frames.
type TopOfBookParser struct {
	mu          sync.RWMutex
	instruments map[uint32]*instrumentInfo
	// buffer holds at most one pending message per unknown instrument_id.
	// Newer messages for the same instrument overwrite older ones, so we
	// only ever retain the most recent state per instrument. Capped at
	// maxBufferedInstruments to bound memory during the refdata gap.
	buffer map[uint32]bufferedMsg
	// bufferDropped counts messages dropped because buffer was full.
	bufferDropped uint64
	// bufferingLogged tracks instruments for which we've already emitted
	// an INFO log about first-time buffering, to avoid log spam.
	bufferingLogged map[uint32]bool
	// bufferFullLogged records whether we've already emitted an INFO log
	// about hitting the cap; further drops log at DEBUG.
	bufferFullLogged bool
}

func NewTopOfBookParser() *TopOfBookParser {
	return &TopOfBookParser{
		instruments:     make(map[uint32]*instrumentInfo),
		buffer:          make(map[uint32]bufferedMsg),
		bufferingLogged: make(map[uint32]bool),
	}
}

func (p *TopOfBookParser) Name() string { return "topofbook" }

func (p *TopOfBookParser) Buffered() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.buffer)
}

func (p *TopOfBookParser) InstrumentCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.instruments)
}

const (
	frameHeaderSize   = 24
	maxSchemaVersion  = 1
	maxReasonableMsgs = 200

	// maxBufferedInstruments caps the number of distinct instruments for
	// which we hold a pending marketdata message while awaiting refdata.
	// Exceeding this cap causes new unknown-instrument messages to be
	// dropped.
	maxBufferedInstruments = 1000
)

func (p *TopOfBookParser) Parse(data []byte) ([]Record, error) {
	frame, err := decodeTopOfBookFrame(data)
	if err != nil {
		return nil, fmt.Errorf("decoding frame: %w", err)
	}

	if err := p.validateHeader(frame, len(data)); err != nil {
		return nil, err
	}

	channelID := frame.Header.ChannelID
	seq := frame.Header.SequenceNumber
	sendTS := frame.Header.SendTimestamp

	var records []Record
	for i := range frame.Messages {
		msg := &frame.Messages[i]
		recs, err := p.processMessage(channelID, seq, sendTS, msg)
		if err != nil {
			return records, fmt.Errorf("processing message type 0x%02x: %w", msg.MsgType, err)
		}
		records = append(records, recs...)
	}

	// Try to flush buffered messages now that we may have new definitions.
	flushed := p.flushBuffer()
	records = append(records, flushed...)

	return records, nil
}

// validateHeader performs sanity checks on the frame header that go beyond
// what the wire-format decoder validates (magic bytes only).
func (p *TopOfBookParser) validateHeader(frame *topOfBookFrame, datagramLen int) error {
	h := frame.Header

	if h.SchemaVersion == 0 || h.SchemaVersion > maxSchemaVersion {
		return fmt.Errorf("unsupported schema version %d (expected 1..%d)", h.SchemaVersion, maxSchemaVersion)
	}

	if int(h.FrameLength) != datagramLen {
		return fmt.Errorf("frame length mismatch: header says %d, datagram is %d bytes", h.FrameLength, datagramLen)
	}

	if h.MsgCount == 0 {
		return fmt.Errorf("frame has zero messages")
	}
	if h.MsgCount > maxReasonableMsgs {
		return fmt.Errorf("frame claims %d messages (max %d)", h.MsgCount, maxReasonableMsgs)
	}

	return nil
}

func (p *TopOfBookParser) processMessage(channelID uint8, seq uint64, sendTS uint64, msg *topOfBookAppMessage) ([]Record, error) {
	switch body := msg.Body.(type) {
	case *topOfBookInstrumentDef:
		return p.handleInstrumentDef(channelID, seq, sendTS, msg, body), nil
	case *topOfBookQuote:
		return p.handleQuote(channelID, seq, sendTS, msg, body), nil
	case *topOfBookTrade:
		return p.handleTrade(channelID, seq, sendTS, msg, body), nil
	case *topOfBookHeartbeat:
		return p.handleHeartbeat(channelID, seq, body), nil
	case *topOfBookChannelReset:
		return p.handleChannelReset(channelID, seq, body), nil
	case *topOfBookEndOfSession:
		return p.handleEndOfSession(channelID, seq, body), nil
	case *topOfBookManifestSummary:
		return p.handleManifestSummary(channelID, seq, body), nil
	default:
		// Unknown message type — skip per spec.
		return nil, nil
	}
}

func (p *TopOfBookParser) handleInstrumentDef(channelID uint8, seq uint64, sendTS uint64, msg *topOfBookAppMessage, body *topOfBookInstrumentDef) []Record {
	info := &instrumentInfo{
		Symbol:        trimNull(body.Symbol),
		PriceExponent: body.PriceExponent,
		QtyExponent:   body.QtyExponent,
	}

	p.mu.Lock()
	_, existed := p.instruments[body.InstrumentID]
	p.instruments[body.InstrumentID] = info
	// Clear the first-time-buffering flag so a future gap is logged again.
	delete(p.bufferingLogged, body.InstrumentID)
	p.mu.Unlock()

	if existed {
		slog.Debug("edge: instrument redefined",
			"parser", "topofbook",
			"instrument_id", body.InstrumentID,
			"symbol", info.Symbol)
	} else {
		slog.Info("edge: instrument defined",
			"parser", "topofbook",
			"instrument_id", body.InstrumentID,
			"symbol", info.Symbol)
	}

	return []Record{{
		Type:           "instrument_definition",
		Timestamp:      nsToTime(sendTS),
		ChannelID:      channelID,
		SequenceNumber: seq,
		InstrumentID:   body.InstrumentID,
		Symbol:         info.Symbol,
		Fields: map[string]any{
			"leg1":           trimNull(body.Leg1),
			"leg2":           trimNull(body.Leg2),
			"asset_class":    body.AssetClass,
			"price_exponent": body.PriceExponent,
			"qty_exponent":   body.QtyExponent,
			"market_model":   body.MarketModel,
			"tick_size":      body.TickSize,
			"lot_size":       body.LotSize,
			"contract_value": body.ContractValue,
			"settle_type":    body.SettleType,
			"price_bound":    body.PriceBound,
			"manifest_seq":   body.ManifestSeq,
		},
	}}
}

func (p *TopOfBookParser) handleQuote(channelID uint8, seq uint64, sendTS uint64, msg *topOfBookAppMessage, body *topOfBookQuote) []Record {
	p.mu.RLock()
	info, ok := p.instruments[body.InstrumentID]
	p.mu.RUnlock()

	if !ok {
		p.mu.Lock()
		ev := p.bufferPendingLocked(body.InstrumentID, "quote", bufferedMsg{
			channelID: channelID,
			seq:       seq,
			sendTS:    sendTS,
			msg:       msg,
		})
		p.mu.Unlock()
		ev.emit()
		return nil
	}

	return []Record{{
		Type:           "quote",
		Timestamp:      nsToTime(body.SourceTimestamp),
		ChannelID:      channelID,
		SequenceNumber: seq,
		InstrumentID:   body.InstrumentID,
		Symbol:         info.Symbol,
		Fields: map[string]any{
			"source_id":        body.SourceID,
			"bid_price":        applyExponent(body.BidPrice, info.PriceExponent),
			"bid_qty":          applyExponentUnsigned(body.BidQty, info.QtyExponent),
			"ask_price":        applyExponent(body.AskPrice, info.PriceExponent),
			"ask_qty":          applyExponentUnsigned(body.AskQty, info.QtyExponent),
			"bid_source_count": body.BidSourceCount,
			"ask_source_count": body.AskSourceCount,
			"update_flags":     body.UpdateFlags,
			"snapshot":         msg.Flags&1 == 1,
		},
	}}
}

func (p *TopOfBookParser) handleTrade(channelID uint8, seq uint64, sendTS uint64, msg *topOfBookAppMessage, body *topOfBookTrade) []Record {
	p.mu.RLock()
	info, ok := p.instruments[body.InstrumentID]
	p.mu.RUnlock()

	if !ok {
		p.mu.Lock()
		ev := p.bufferPendingLocked(body.InstrumentID, "trade", bufferedMsg{
			channelID: channelID,
			seq:       seq,
			sendTS:    sendTS,
			msg:       msg,
		})
		p.mu.Unlock()
		ev.emit()
		return nil
	}

	side := "unknown"
	switch body.AggressorSide {
	case 1:
		side = "buy"
	case 2:
		side = "sell"
	}

	return []Record{{
		Type:           "trade",
		Timestamp:      nsToTime(body.SourceTimestamp),
		ChannelID:      channelID,
		SequenceNumber: seq,
		InstrumentID:   body.InstrumentID,
		Symbol:         info.Symbol,
		Fields: map[string]any{
			"source_id":         body.SourceID,
			"trade_price":       applyExponent(body.TradePrice, info.PriceExponent),
			"trade_qty":         applyExponentUnsigned(body.TradeQty, info.QtyExponent),
			"aggressor_side":    side,
			"trade_id":          body.TradeID,
			"cumulative_volume": applyExponentUnsigned(body.CumulativeVolume, info.QtyExponent),
			"snapshot":          msg.Flags&1 == 1,
		},
	}}
}

func (p *TopOfBookParser) handleHeartbeat(channelID uint8, seq uint64, body *topOfBookHeartbeat) []Record {
	return []Record{{
		Type:           "heartbeat",
		Timestamp:      nsToTime(body.Timestamp),
		ChannelID:      channelID,
		SequenceNumber: seq,
	}}
}

func (p *TopOfBookParser) handleChannelReset(channelID uint8, seq uint64, body *topOfBookChannelReset) []Record {
	p.mu.Lock()
	p.instruments = make(map[uint32]*instrumentInfo)
	p.buffer = make(map[uint32]bufferedMsg)
	p.bufferingLogged = make(map[uint32]bool)
	p.bufferFullLogged = false
	p.mu.Unlock()

	return []Record{{
		Type:           "channel_reset",
		Timestamp:      nsToTime(body.Timestamp),
		ChannelID:      channelID,
		SequenceNumber: seq,
	}}
}

func (p *TopOfBookParser) handleEndOfSession(channelID uint8, seq uint64, body *topOfBookEndOfSession) []Record {
	return []Record{{
		Type:           "end_of_session",
		Timestamp:      nsToTime(body.Timestamp),
		ChannelID:      channelID,
		SequenceNumber: seq,
	}}
}

func (p *TopOfBookParser) handleManifestSummary(channelID uint8, seq uint64, body *topOfBookManifestSummary) []Record {
	return []Record{{
		Type:           "manifest_summary",
		Timestamp:      nsToTime(body.Timestamp),
		ChannelID:      channelID,
		SequenceNumber: seq,
		Fields: map[string]any{
			"manifest_seq":     body.ManifestSeq,
			"instrument_count": body.InstrumentCount,
		},
	}}
}

// bufferPendingLocked stores a pending marketdata message for an unknown
// instrument. If a message for that instrument is already buffered, it is
// overwritten (keeping only the most recent). If the buffer is at capacity
// and this is a new instrument, the message is dropped.
//
// Caller must hold p.mu.
//
// Returns a pendingLogEvent describing what should be logged once the
// lock is released. Callers must emit the log outside the critical
// section to keep slog off the hot path while the mutex is held.
func (p *TopOfBookParser) bufferPendingLocked(instrumentID uint32, msgType string, bm bufferedMsg) pendingLogEvent {
	if _, exists := p.buffer[instrumentID]; exists {
		p.buffer[instrumentID] = bm
		return pendingLogEvent{
			kind:         logBufferReplaced,
			instrumentID: instrumentID,
			msgType:      msgType,
			bufferDepth:  len(p.buffer),
		}
	}
	if len(p.buffer) >= maxBufferedInstruments {
		p.bufferDropped++
		first := !p.bufferFullLogged
		p.bufferFullLogged = true
		return pendingLogEvent{
			kind:         logBufferFull,
			instrumentID: instrumentID,
			msgType:      msgType,
			bufferDepth:  len(p.buffer),
			dropCount:    p.bufferDropped,
			firstTime:    first,
		}
	}
	p.buffer[instrumentID] = bm
	firstTime := !p.bufferingLogged[instrumentID]
	if firstTime {
		p.bufferingLogged[instrumentID] = true
	}
	return pendingLogEvent{
		kind:         logBufferInserted,
		instrumentID: instrumentID,
		msgType:      msgType,
		bufferDepth:  len(p.buffer),
		firstTime:    firstTime,
	}
}

// pendingLogEvent carries data about a buffer-transition event that the
// caller of bufferPendingLocked should log after releasing p.mu.
type pendingLogEvent struct {
	kind         pendingLogKind
	instrumentID uint32
	msgType      string
	bufferDepth  int
	dropCount    uint64
	firstTime    bool
}

type pendingLogKind int

const (
	logBufferInserted pendingLogKind = iota
	logBufferReplaced
	logBufferFull
)

func (e pendingLogEvent) emit() {
	switch e.kind {
	case logBufferInserted:
		if e.firstTime {
			slog.Info("edge: buffering messages, awaiting instrument definition",
				"parser", "topofbook",
				"msg_type", e.msgType,
				"instrument_id", e.instrumentID,
				"buffer_depth", e.bufferDepth)
		} else {
			slog.Debug("edge: buffering message",
				"parser", "topofbook",
				"msg_type", e.msgType,
				"instrument_id", e.instrumentID,
				"buffer_depth", e.bufferDepth)
		}
	case logBufferReplaced:
		slog.Debug("edge: buffered message replaced",
			"parser", "topofbook",
			"msg_type", e.msgType,
			"instrument_id", e.instrumentID,
			"buffer_depth", e.bufferDepth)
	case logBufferFull:
		if e.firstTime {
			slog.Warn("edge: buffer full, dropping message",
				"parser", "topofbook",
				"msg_type", e.msgType,
				"instrument_id", e.instrumentID,
				"buffer_depth", e.bufferDepth,
				"cap", maxBufferedInstruments,
				"total_dropped", e.dropCount)
		} else {
			slog.Debug("edge: buffer full, dropping message",
				"parser", "topofbook",
				"msg_type", e.msgType,
				"instrument_id", e.instrumentID,
				"total_dropped", e.dropCount)
		}
	}
}

func (p *TopOfBookParser) flushBuffer() []Record {
	p.mu.Lock()
	defer p.mu.Unlock()

	var records []Record
	for instrumentID, bm := range p.buffer {
		recs, err := p.processBufferedMessage(bm)
		if err != nil || recs == nil {
			continue
		}
		records = append(records, recs...)
		delete(p.buffer, instrumentID)
	}

	if len(records) > 0 {
		slog.Info("edge: flushed buffered messages",
			"parser", "topofbook",
			"flushed", len(records),
			"remaining", len(p.buffer))
	}

	return records
}

// processBufferedMessage processes a buffered message without acquiring locks
// (caller must hold p.mu).
func (p *TopOfBookParser) processBufferedMessage(bm bufferedMsg) ([]Record, error) {
	switch body := bm.msg.Body.(type) {
	case *topOfBookQuote:
		info, ok := p.instruments[body.InstrumentID]
		if !ok {
			return nil, nil
		}
		return []Record{{
			Type:           "quote",
			Timestamp:      nsToTime(body.SourceTimestamp),
			ChannelID:      bm.channelID,
			SequenceNumber: bm.seq,
			InstrumentID:   body.InstrumentID,
			Symbol:         info.Symbol,
			Fields: map[string]any{
				"source_id":        body.SourceID,
				"bid_price":        applyExponent(body.BidPrice, info.PriceExponent),
				"bid_qty":          applyExponentUnsigned(body.BidQty, info.QtyExponent),
				"ask_price":        applyExponent(body.AskPrice, info.PriceExponent),
				"ask_qty":          applyExponentUnsigned(body.AskQty, info.QtyExponent),
				"bid_source_count": body.BidSourceCount,
				"ask_source_count": body.AskSourceCount,
				"update_flags":     body.UpdateFlags,
				"snapshot":         bm.msg.Flags&1 == 1,
			},
		}}, nil
	case *topOfBookTrade:
		info, ok := p.instruments[body.InstrumentID]
		if !ok {
			return nil, nil
		}
		side := "unknown"
		switch body.AggressorSide {
		case 1:
			side = "buy"
		case 2:
			side = "sell"
		}
		return []Record{{
			Type:           "trade",
			Timestamp:      nsToTime(body.SourceTimestamp),
			ChannelID:      bm.channelID,
			SequenceNumber: bm.seq,
			InstrumentID:   body.InstrumentID,
			Symbol:         info.Symbol,
			Fields: map[string]any{
				"source_id":         body.SourceID,
				"trade_price":       applyExponent(body.TradePrice, info.PriceExponent),
				"trade_qty":         applyExponentUnsigned(body.TradeQty, info.QtyExponent),
				"aggressor_side":    side,
				"trade_id":          body.TradeID,
				"cumulative_volume": applyExponentUnsigned(body.CumulativeVolume, info.QtyExponent),
				"snapshot":          bm.msg.Flags&1 == 1,
			},
		}}, nil
	default:
		return nil, nil
	}
}

// trimNull removes trailing null bytes from a fixed-size ASCII string.
func trimNull(s string) string {
	return strings.TrimRight(s, "\x00")
}

// nsToTime converts nanoseconds since Unix epoch to time.Time.
func nsToTime(ns uint64) time.Time {
	return time.Unix(0, int64(ns)).UTC()
}

// maxExponent is the largest exponent we accept. Financial instruments
// use small exponents (typically -8 to +2). Anything outside [-18, 18]
// is almost certainly garbage and would produce unreasonable float values.
const maxExponent = 18

// applyExponent converts a raw signed integer with an implied decimal exponent
// to a float64. For example, raw=6743250 with exponent=-2 yields 67432.50.
// Returns 0 if the exponent is out of the reasonable range or the result
// would be Inf or NaN.
func applyExponent(raw int64, exp int8) float64 {
	if exp > maxExponent || exp < -maxExponent {
		return 0
	}
	v := float64(raw) * math.Pow(10, float64(exp))
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return 0
	}
	return v
}

// applyExponentUnsigned is the unsigned variant of applyExponent.
func applyExponentUnsigned(raw uint64, exp int8) float64 {
	if exp > maxExponent || exp < -maxExponent {
		return 0
	}
	v := float64(raw) * math.Pow(10, float64(exp))
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return 0
	}
	return v
}
