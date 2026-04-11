// Package testutil provides a synthetic Top-of-Book publisher for testing
// the edge feed parser pipeline end-to-end.
package testutil

import (
	"encoding/binary"
	"net"
	"time"
)

// Publisher sends synthetic Top-of-Book v0.1.0 frames to a multicast group.
type Publisher struct {
	conn    *net.UDPConn
	groupIP net.IP
	port    int
	seq     uint64
}

// NewPublisher creates a publisher that sends to the given multicast group and port.
func NewPublisher(groupIP net.IP, port int) (*Publisher, error) {
	addr := &net.UDPAddr{
		IP:   groupIP,
		Port: port,
	}
	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		return nil, err
	}
	return &Publisher{conn: conn, groupIP: groupIP, port: port}, nil
}

// Close closes the publisher connection.
func (p *Publisher) Close() error {
	return p.conn.Close()
}

// SendInstrumentDefinition sends a frame containing a single InstrumentDefinition message.
func (p *Publisher) SendInstrumentDefinition(channelID uint8, instID uint32, symbol, leg1, leg2 string, priceExp, qtyExp int8) error {
	msg := buildInstrumentDef(instID, symbol, leg1, leg2, priceExp, qtyExp)
	frame := p.buildFrame(channelID, msg)
	_, err := p.conn.Write(frame)
	return err
}

// SendQuote sends a frame containing a single Quote message.
func (p *Publisher) SendQuote(channelID uint8, instID uint32, sourceID uint16, bidPrice int64, bidQty uint64, askPrice int64, askQty uint64) error {
	msg := buildQuote(instID, sourceID, bidPrice, bidQty, askPrice, askQty)
	frame := p.buildFrame(channelID, msg)
	_, err := p.conn.Write(frame)
	return err
}

// SendTrade sends a frame containing a single Trade message.
func (p *Publisher) SendTrade(channelID uint8, instID uint32, sourceID uint16, price int64, qty uint64, side uint8) error {
	msg := buildTrade(instID, sourceID, price, qty, side)
	frame := p.buildFrame(channelID, msg)
	_, err := p.conn.Write(frame)
	return err
}

// SendHeartbeat sends a frame containing a single Heartbeat message.
func (p *Publisher) SendHeartbeat(channelID uint8) error {
	msg := buildHeartbeat(channelID)
	frame := p.buildFrame(channelID, msg)
	_, err := p.conn.Write(frame)
	return err
}

func (p *Publisher) buildFrame(channelID uint8, msgs ...[]byte) []byte {
	headerSize := 24
	bodySize := 0
	for _, m := range msgs {
		bodySize += len(m)
	}
	frameLen := headerSize + bodySize

	buf := make([]byte, frameLen)
	// Magic "DZ" = 0x445A little-endian
	buf[0] = 0x5A
	buf[1] = 0x44
	buf[2] = 1 // schema version
	buf[3] = channelID
	p.seq++
	binary.LittleEndian.PutUint64(buf[4:], p.seq)
	binary.LittleEndian.PutUint64(buf[12:], uint64(time.Now().UnixNano()))
	buf[20] = uint8(len(msgs))
	buf[21] = 0
	binary.LittleEndian.PutUint16(buf[22:], uint16(frameLen))

	off := headerSize
	for _, m := range msgs {
		copy(buf[off:], m)
		off += len(m)
	}
	return buf
}

func buildInstrumentDef(instID uint32, symbol, leg1, leg2 string, priceExp, qtyExp int8) []byte {
	buf := make([]byte, 80)
	buf[0] = 0x02
	buf[1] = 80
	binary.LittleEndian.PutUint32(buf[4:], instID)
	copy(buf[8:24], padNull(symbol, 16))
	copy(buf[24:32], padNull(leg1, 8))
	copy(buf[32:40], padNull(leg2, 8))
	buf[40] = 1 // crypto spot
	buf[41] = byte(priceExp)
	buf[42] = byte(qtyExp)
	buf[43] = 1 // CLOB
	binary.LittleEndian.PutUint64(buf[44:], 1)
	binary.LittleEndian.PutUint64(buf[52:], 1)
	binary.LittleEndian.PutUint16(buf[78:], 1)
	return buf
}

func buildQuote(instID uint32, sourceID uint16, bidPrice int64, bidQty uint64, askPrice int64, askQty uint64) []byte {
	buf := make([]byte, 60)
	buf[0] = 0x03
	buf[1] = 60
	binary.LittleEndian.PutUint32(buf[4:], instID)
	binary.LittleEndian.PutUint16(buf[8:], sourceID)
	buf[10] = 0x03 // bid + ask updated
	binary.LittleEndian.PutUint64(buf[12:], uint64(time.Now().UnixNano()))
	binary.LittleEndian.PutUint64(buf[20:], uint64(bidPrice))
	binary.LittleEndian.PutUint64(buf[28:], bidQty)
	binary.LittleEndian.PutUint64(buf[36:], uint64(askPrice))
	binary.LittleEndian.PutUint64(buf[44:], askQty)
	binary.LittleEndian.PutUint16(buf[52:], 1)
	binary.LittleEndian.PutUint16(buf[54:], 1)
	return buf
}

func buildTrade(instID uint32, sourceID uint16, price int64, qty uint64, side uint8) []byte {
	buf := make([]byte, 52)
	buf[0] = 0x04
	buf[1] = 52
	binary.LittleEndian.PutUint32(buf[4:], instID)
	binary.LittleEndian.PutUint16(buf[8:], sourceID)
	buf[10] = side
	binary.LittleEndian.PutUint64(buf[12:], uint64(time.Now().UnixNano()))
	binary.LittleEndian.PutUint64(buf[20:], uint64(price))
	binary.LittleEndian.PutUint64(buf[28:], qty)
	binary.LittleEndian.PutUint64(buf[36:], 12345)
	binary.LittleEndian.PutUint64(buf[44:], qty)
	return buf
}

func buildHeartbeat(channelID uint8) []byte {
	buf := make([]byte, 16)
	buf[0] = 0x01
	buf[1] = 16
	buf[4] = channelID
	binary.LittleEndian.PutUint64(buf[8:], uint64(time.Now().UnixNano()))
	return buf
}

func padNull(s string, n int) []byte {
	buf := make([]byte, n)
	copy(buf, s)
	return buf
}
