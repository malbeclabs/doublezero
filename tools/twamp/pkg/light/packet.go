package twamplight

import (
	"encoding/binary"
	"time"
)

const (
	packetSize = 48
)

type Packet struct {
	Seq  uint32
	Sec  uint32
	Frac uint32
	Pad  [36]byte
}

func NewPacket(seq uint32) *Packet {
	sec, frac := ntpTimestamp(time.Now())
	return &Packet{
		Seq:  seq,
		Sec:  sec,
		Frac: frac,
	}
}

func (p *Packet) MarshalBinary() ([]byte, error) {
	buf := make([]byte, packetSize)
	binary.BigEndian.PutUint32(buf[0:4], p.Seq)
	binary.BigEndian.PutUint32(buf[4:8], p.Sec)
	binary.BigEndian.PutUint32(buf[8:12], p.Frac)
	return buf, nil
}

func UnmarshalBinary(buf []byte) (*Packet, error) {
	if len(buf) != packetSize {
		return nil, ErrInvalidPacket
	}
	seq := binary.BigEndian.Uint32(buf[0:4])
	sec := binary.BigEndian.Uint32(buf[4:8])
	frac := binary.BigEndian.Uint32(buf[8:12])
	p := &Packet{
		Seq:  seq,
		Sec:  sec,
		Frac: frac,
	}
	return p, nil
}
