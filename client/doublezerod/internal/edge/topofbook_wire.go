package edge

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Wire format for the DoubleZero Top-of-Book feed, v0.1.0.
//
// One UDP datagram carries one frame. A frame is a fixed 24-byte header
// followed by a sequence of application messages. Every multi-byte
// integer is little-endian. The layout is fixed-size (no varints, no
// length-prefixed strings beyond fixed arrays), making a straight
// positional decode sufficient.
//
// Message types:
//
//   0x01 heartbeat
//   0x02 instrument definition (refdata)
//   0x03 quote (marketdata)
//   0x04 trade (marketdata)
//   0x05 channel reset
//   0x06 end of session
//   0x07 manifest summary

const (
	frameHeaderBytes = 24

	// Magic bytes at the start of every frame: "DZ".
	frameMagic0 = 0x5A
	frameMagic1 = 0x44
)

const (
	msgHeartbeat            uint8 = 0x01
	msgInstrumentDefinition uint8 = 0x02
	msgQuote                uint8 = 0x03
	msgTrade                uint8 = 0x04
	msgChannelReset         uint8 = 0x05
	msgEndOfSession         uint8 = 0x06
	msgManifestSummary      uint8 = 0x07
)

// topOfBookFrame is one decoded UDP datagram.
type topOfBookFrame struct {
	Header   topOfBookHeader
	Messages []topOfBookAppMessage
}

type topOfBookHeader struct {
	Magic          [2]byte
	SchemaVersion  uint8
	ChannelID      uint8
	SequenceNumber uint64
	SendTimestamp  uint64 // nanoseconds since Unix epoch
	MsgCount       uint8
	Reserved       uint8
	FrameLength    uint16
}

// topOfBookAppMessage is a single message inside a frame. Body is one
// of the *topOfBook* body types below, selected by MsgType; unknown
// message types leave Body nil so the parser can skip them.
type topOfBookAppMessage struct {
	MsgType   uint8
	MsgLength uint8
	Flags     uint16
	Body      any
}

type topOfBookHeartbeat struct {
	ChannelID uint8
	Timestamp uint64
}

type topOfBookInstrumentDef struct {
	InstrumentID  uint32
	Symbol        string // fixed 16 bytes, null-padded ASCII
	Leg1          string // fixed 8 bytes
	Leg2          string // fixed 8 bytes
	AssetClass    uint8
	PriceExponent int8
	QtyExponent   int8
	MarketModel   uint8
	TickSize      int64
	LotSize       uint64
	ContractValue uint64
	Expiry        uint64
	SettleType    uint8
	PriceBound    uint8
	ManifestSeq   uint16
}

type topOfBookQuote struct {
	InstrumentID    uint32
	SourceID        uint16
	UpdateFlags     uint8
	SourceTimestamp uint64
	BidPrice        int64
	BidQty          uint64
	AskPrice        int64
	AskQty          uint64
	BidSourceCount  uint16
	AskSourceCount  uint16
}

type topOfBookTrade struct {
	InstrumentID     uint32
	SourceID         uint16
	AggressorSide    uint8
	TradeFlags       uint8
	SourceTimestamp  uint64
	TradePrice       int64
	TradeQty         uint64
	TradeID          uint64
	CumulativeVolume uint64
}

type topOfBookChannelReset struct {
	Timestamp uint64
}

type topOfBookEndOfSession struct {
	Timestamp uint64
}

type topOfBookManifestSummary struct {
	ChannelID       uint8
	ManifestSeq     uint16
	InstrumentCount uint32
	Timestamp       uint64
}

// wireReader is a trivial positional reader for fixed-layout
// little-endian wire formats. Errors are sticky: once a read fails,
// subsequent reads are no-ops and err stays set, so callers can do a
// block of reads and check err once.
type wireReader struct {
	buf []byte
	off int
	err error
}

func (r *wireReader) need(n int) bool {
	if r.err != nil {
		return false
	}
	if r.off+n > len(r.buf) {
		r.err = io.ErrUnexpectedEOF
		return false
	}
	return true
}

func (r *wireReader) u8() uint8 {
	if !r.need(1) {
		return 0
	}
	v := r.buf[r.off]
	r.off++
	return v
}

func (r *wireReader) i8() int8 { return int8(r.u8()) }

func (r *wireReader) u16() uint16 {
	if !r.need(2) {
		return 0
	}
	v := binary.LittleEndian.Uint16(r.buf[r.off:])
	r.off += 2
	return v
}

func (r *wireReader) u32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.buf[r.off:])
	r.off += 4
	return v
}

func (r *wireReader) u64() uint64 {
	if !r.need(8) {
		return 0
	}
	v := binary.LittleEndian.Uint64(r.buf[r.off:])
	r.off += 8
	return v
}

func (r *wireReader) i64() int64 { return int64(r.u64()) }

func (r *wireReader) bytes(n int) []byte {
	if !r.need(n) {
		return nil
	}
	v := r.buf[r.off : r.off+n]
	r.off += n
	return v
}

func (r *wireReader) skip(n int) {
	if r.need(n) {
		r.off += n
	}
}

// decodeTopOfBookFrame parses one UDP datagram into a topOfBookFrame.
// Unknown message types are still counted and skipped so their bytes
// are consumed; their Body field is left nil and callers should ignore
// them.
func decodeTopOfBookFrame(data []byte) (*topOfBookFrame, error) {
	if len(data) < frameHeaderBytes {
		return nil, fmt.Errorf("datagram too short: %d bytes (minimum %d)", len(data), frameHeaderBytes)
	}

	r := &wireReader{buf: data}

	var f topOfBookFrame
	f.Header.Magic[0] = r.u8()
	f.Header.Magic[1] = r.u8()
	if f.Header.Magic[0] != frameMagic0 || f.Header.Magic[1] != frameMagic1 {
		return nil, fmt.Errorf("bad magic: 0x%02x 0x%02x (expected 0x5A 0x44)",
			f.Header.Magic[0], f.Header.Magic[1])
	}
	f.Header.SchemaVersion = r.u8()
	f.Header.ChannelID = r.u8()
	f.Header.SequenceNumber = r.u64()
	f.Header.SendTimestamp = r.u64()
	f.Header.MsgCount = r.u8()
	f.Header.Reserved = r.u8()
	f.Header.FrameLength = r.u16()
	if r.err != nil {
		return nil, fmt.Errorf("decoding frame header: %w", r.err)
	}

	f.Messages = make([]topOfBookAppMessage, 0, f.Header.MsgCount)
	for i := 0; i < int(f.Header.MsgCount); i++ {
		var msg topOfBookAppMessage
		msg.MsgType = r.u8()
		msg.MsgLength = r.u8()
		msg.Flags = r.u16()
		if r.err != nil {
			return nil, fmt.Errorf("decoding message %d header: %w", i, r.err)
		}

		if msg.MsgLength < 4 {
			return nil, fmt.Errorf("message %d: msg_length %d too small (min 4)", i, msg.MsgLength)
		}
		bodyLen := int(msg.MsgLength) - 4
		bodyBuf := r.bytes(bodyLen)
		if r.err != nil {
			return nil, fmt.Errorf("decoding message %d body (len=%d): %w", i, bodyLen, r.err)
		}

		body, err := decodeTopOfBookBody(msg.MsgType, bodyBuf)
		if err != nil {
			return nil, fmt.Errorf("decoding message %d (type=0x%02x): %w", i, msg.MsgType, err)
		}
		msg.Body = body
		f.Messages = append(f.Messages, msg)
	}

	return &f, nil
}

// decodeTopOfBookBody dispatches on msg type to decode a message body.
// Returns (nil, nil) for unknown types so the parser skips them.
func decodeTopOfBookBody(msgType uint8, buf []byte) (any, error) {
	br := &wireReader{buf: buf}

	switch msgType {
	case msgHeartbeat:
		var b topOfBookHeartbeat
		b.ChannelID = br.u8()
		br.skip(3) // reserved
		b.Timestamp = br.u64()
		if br.err != nil {
			return nil, br.err
		}
		return &b, nil

	case msgInstrumentDefinition:
		var b topOfBookInstrumentDef
		b.InstrumentID = br.u32()
		b.Symbol = string(br.bytes(16))
		b.Leg1 = string(br.bytes(8))
		b.Leg2 = string(br.bytes(8))
		b.AssetClass = br.u8()
		b.PriceExponent = br.i8()
		b.QtyExponent = br.i8()
		b.MarketModel = br.u8()
		b.TickSize = br.i64()
		b.LotSize = br.u64()
		b.ContractValue = br.u64()
		b.Expiry = br.u64()
		b.SettleType = br.u8()
		b.PriceBound = br.u8()
		b.ManifestSeq = br.u16()
		if br.err != nil {
			return nil, br.err
		}
		return &b, nil

	case msgQuote:
		var b topOfBookQuote
		b.InstrumentID = br.u32()
		b.SourceID = br.u16()
		b.UpdateFlags = br.u8()
		br.skip(1) // reserved
		b.SourceTimestamp = br.u64()
		b.BidPrice = br.i64()
		b.BidQty = br.u64()
		b.AskPrice = br.i64()
		b.AskQty = br.u64()
		b.BidSourceCount = br.u16()
		b.AskSourceCount = br.u16()
		br.skip(4) // reserved
		if br.err != nil {
			return nil, br.err
		}
		return &b, nil

	case msgTrade:
		var b topOfBookTrade
		b.InstrumentID = br.u32()
		b.SourceID = br.u16()
		b.AggressorSide = br.u8()
		b.TradeFlags = br.u8()
		b.SourceTimestamp = br.u64()
		b.TradePrice = br.i64()
		b.TradeQty = br.u64()
		b.TradeID = br.u64()
		b.CumulativeVolume = br.u64()
		if br.err != nil {
			return nil, br.err
		}
		return &b, nil

	case msgChannelReset:
		var b topOfBookChannelReset
		b.Timestamp = br.u64()
		if br.err != nil {
			return nil, br.err
		}
		return &b, nil

	case msgEndOfSession:
		var b topOfBookEndOfSession
		b.Timestamp = br.u64()
		if br.err != nil {
			return nil, br.err
		}
		return &b, nil

	case msgManifestSummary:
		var b topOfBookManifestSummary
		b.ChannelID = br.u8()
		br.skip(3) // reserved
		b.ManifestSeq = br.u16()
		br.skip(2) // reserved
		b.InstrumentCount = br.u32()
		b.Timestamp = br.u64()
		if br.err != nil {
			return nil, br.err
		}
		return &b, nil

	default:
		// Unknown message type — body bytes already consumed by the
		// caller. Return a nil body so the parser skips it.
		return nil, nil
	}
}
