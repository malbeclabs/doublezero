package liveness

import (
	"encoding/binary"
	"fmt"
)

type State uint8

const (
	StateAdminDown State = iota
	StateDown
	StateInit
	StateUp
)

type ControlPacket struct {
	Version         uint8
	State           State
	DetectMult      uint8
	Length          uint8
	MyDiscr         uint32
	YourDiscr       uint32
	DesiredMinTxUs  uint32
	RequiredMinRxUs uint32
}

func (c *ControlPacket) Marshal() []byte {
	b := make([]byte, 40)
	vd := (c.Version & 0x7) << 5
	sf := (uint8(c.State) & 0x3) << 6
	b[0], b[1], b[2], b[3] = vd, sf, c.DetectMult, 40
	be := binary.BigEndian
	be.PutUint32(b[4:8], c.MyDiscr)
	be.PutUint32(b[8:12], c.YourDiscr)
	be.PutUint32(b[12:16], c.DesiredMinTxUs)
	be.PutUint32(b[16:20], c.RequiredMinRxUs)
	// padding [20:40] left zero
	return b
}

func UnmarshalControlPacket(b []byte) (*ControlPacket, error) {
	if len(b) < 40 {
		return nil, fmt.Errorf("short")
	}
	if b[3] != 40 {
		return nil, fmt.Errorf("bad length")
	}
	vd, sf := b[0], b[1]
	ver := (vd >> 5) & 0x7
	if ver != 1 {
		return nil, fmt.Errorf("unsupported version: %d", ver)
	}
	c := &ControlPacket{
		Version:    ver,
		State:      State((sf >> 6) & 0x3),
		DetectMult: b[2],
		Length:     b[3],
	}
	rd := func(off int) uint32 { return binary.BigEndian.Uint32(b[off : off+4]) }
	c.MyDiscr = rd(4)
	c.YourDiscr = rd(8)
	c.DesiredMinTxUs = rd(12)
	c.RequiredMinRxUs = rd(16)
	return c, nil
}
