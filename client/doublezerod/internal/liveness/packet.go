package liveness

import (
	"encoding/binary"
	"fmt"
)

// State encodes the finite-state machine for a BFD-like session.
// The progression follows AdminDown → Down → Init → Up, with
// transitions driven by control messages and detect timeouts.
type State uint8

const (
	StateAdminDown State = iota // administratively disabled, no detection
	StateDown                   // no session detected or timed out
	StateInit                   // attempting to establish connectivity
	StateUp                     // session fully established
)

// ControlPacket represents the wire format of a minimal BFD control packet.
// Fields mirror RFC 5880 §4.1 in a compact form using microsecond units for timers.
type ControlPacket struct {
	Version         uint8  // protocol version; expected to be 1
	State           State  // sender's current session state
	DetectMult      uint8  // detection multiplier (used by peer for detect timeout)
	Length          uint8  // total length, always 40 for this fixed-size implementation
	MyDiscr         uint32 // sender's discriminator (unique session ID)
	YourDiscr       uint32 // discriminator of the remote session (echo back)
	DesiredMinTxUs  uint32 // minimum TX interval desired by sender (microseconds)
	RequiredMinRxUs uint32 // minimum RX interval the sender can handle (microseconds)
}

// Marshal serializes a ControlPacket into its fixed 40-byte wire format.
//
// Field layout (Big Endian):
//
//	0: Version (3 high bits) | 5 bits unused (zero)
//	1: State (2 high bits)   | 6 bits unused (zero)
//	2: DetectMult
//	3: Length (always 40)
//	4–7:  MyDiscr
//	8–11: YourDiscr
//
// 12–15: DesiredMinTxUs
// 16–19: RequiredMinRxUs
// 20–39: zero padding (unused / reserved)
//
// Only a subset of the full BFD header is implemented; authentication and
// optional fields are omitted for simplicity.
func (c *ControlPacket) Marshal() []byte {
	b := make([]byte, 40)
	// Version (3 bits) and State (2 bits in high order of next byte)
	vd := (c.Version & 0x7) << 5
	sf := (uint8(c.State) & 0x3) << 6
	b[0], b[1], b[2], b[3] = vd, sf, c.DetectMult, 40
	be := binary.BigEndian
	be.PutUint32(b[4:8], c.MyDiscr)
	be.PutUint32(b[8:12], c.YourDiscr)
	be.PutUint32(b[12:16], c.DesiredMinTxUs)
	be.PutUint32(b[16:20], c.RequiredMinRxUs)
	// Remaining bytes [20:40] are reserved/padding → left zeroed
	return b
}

// UnmarshalControlPacket parses a 40-byte control message from the wire
// into a ControlPacket. It validates the version and length fields and
// extracts all header values using big-endian order.
func UnmarshalControlPacket(b []byte) (*ControlPacket, error) {
	if len(b) < 40 {
		return nil, fmt.Errorf("short packet")
	}
	if b[3] != 40 {
		return nil, fmt.Errorf("invalid length")
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
