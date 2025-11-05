package liveness

import (
	"encoding/binary"
	"fmt"
)

type State uint8

const (
	AdminDown State = iota
	Down
	Init
	Up
)

type ControlPacket struct {
	Version    uint8
	Diag       uint8
	State      State
	Flags      uint8 // bit1=Poll, bit0=Final
	DetectMult uint8
	Length     uint8

	MyDiscr         uint32
	YourDiscr       uint32
	DesiredMinTxUs  uint32
	RequiredMinRxUs uint32
	RouteHash       uint32
}

func (c *ControlPacket) Marshal() []byte {
	b := make([]byte, 40)
	vd := (c.Version&0x7)<<5 | (c.Diag & 0x1f)
	sf := (uint8(c.State)&0x3)<<6 | (c.Flags & 0x3f)
	b[0], b[1], b[2], b[3] = vd, sf, c.DetectMult, 40
	be := binary.BigEndian
	be.PutUint32(b[4:8], c.MyDiscr)
	be.PutUint32(b[8:12], c.YourDiscr)
	be.PutUint32(b[12:16], c.DesiredMinTxUs)
	be.PutUint32(b[16:20], c.RequiredMinRxUs)
	be.PutUint32(b[20:24], c.RouteHash)
	// padding [24:40] left zero
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
	c := &ControlPacket{
		Version:    (vd >> 5) & 0x7,
		Diag:       vd & 0x1f,
		State:      State((sf >> 6) & 0x3),
		Flags:      sf & 0x3f,
		DetectMult: b[2],
		Length:     b[3],
	}
	rd := func(off int) uint32 { return binary.BigEndian.Uint32(b[off : off+4]) }
	c.MyDiscr = rd(4)
	c.YourDiscr = rd(8)
	c.DesiredMinTxUs = rd(12)
	c.RequiredMinRxUs = rd(16)
	c.RouteHash = rd(20)
	return c, nil
}
