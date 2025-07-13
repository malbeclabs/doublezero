package twamplight

import (
	"encoding/binary"
	"fmt"
	"time"
)

const (
	PacketSize = 48
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

func (p *Packet) Marshal(buf []byte) error {
	if len(buf) < PacketSize {
		return fmt.Errorf("buffer too small: %d < %d", len(buf), PacketSize)
	}

	binary.BigEndian.PutUint32(buf[0:4], p.Seq)
	binary.BigEndian.PutUint32(buf[4:8], p.Sec)
	binary.BigEndian.PutUint32(buf[8:12], p.Frac)
	copy(buf[12:], p.Pad[:])
	return nil
}

func UnmarshalPacket(buf []byte) (*Packet, error) {
	// Validate packet size.
	if len(buf) != PacketSize {
		return nil, ErrInvalidPacket
	}

	// Validate padding.
	for i := 12; i < PacketSize; i++ {
		if buf[i] != 0 {
			return nil, ErrInvalidPacket
		}
	}

	// Read packet fields.
	seq := binary.BigEndian.Uint32(buf[0:4])
	sec := binary.BigEndian.Uint32(buf[4:8])
	frac := binary.BigEndian.Uint32(buf[8:12])

	return &Packet{
		Seq:  seq,
		Sec:  sec,
		Frac: frac,
	}, nil
}

// ntpTimestamp converts a time.Time to an NTP timestamp.
func ntpTimestamp(t time.Time) (uint32, uint32) {
	const ntpEpochOffset = 2208988800
	secs := uint32(t.Unix()) + ntpEpochOffset
	nanos := uint64(t.Nanosecond())
	frac := uint32((nanos * (1 << 32)) / 1e9)
	return secs, frac
}
