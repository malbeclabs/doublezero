package liveness

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClient_Liveness_Packet_MarshalEncodesHeaderAndFields(t *testing.T) {
	t.Parallel()
	cp := &ControlPacket{
		Version:         5,
		State:           StateUp,
		DetectMult:      3,
		LocalDiscr:      0x11223344,
		PeerDiscr:       0x55667788,
		DesiredMinTxUs:  0x01020304,
		RequiredMinRxUs: 0x0A0B0C0D,
	}

	b := cp.Marshal()
	require.Len(t, b, 40)
	require.Equal(t, uint8(40), b[3])
	require.Equal(t, uint8((5&0x7)<<5), b[0])
	require.Equal(t, uint8((uint8(StateUp)&0x3)<<6), b[1])
	require.Equal(t, uint8(3), b[2])

	require.Equal(t, uint32(0x11223344), binary.BigEndian.Uint32(b[4:8]))
	require.Equal(t, uint32(0x55667788), binary.BigEndian.Uint32(b[8:12]))
	require.Equal(t, uint32(0x01020304), binary.BigEndian.Uint32(b[12:16]))
	require.Equal(t, uint32(0x0A0B0C0D), binary.BigEndian.Uint32(b[16:20]))

	require.True(t, bytes.Equal(b[20:40], make([]byte, 20)))
}

func TestClient_Liveness_Packet_UnmarshalRoundTrip(t *testing.T) {
	t.Parallel()
	orig := &ControlPacket{
		Version:         1,
		State:           StateInit,
		DetectMult:      7,
		LocalDiscr:      1,
		PeerDiscr:       2,
		DesiredMinTxUs:  3,
		RequiredMinRxUs: 4,
	}
	b := orig.Marshal()
	got, err := UnmarshalControlPacket(b)
	require.NoError(t, err)

	require.Equal(t, uint8(1), got.Version)
	require.Equal(t, StateInit, got.State)
	require.Equal(t, uint8(7), got.DetectMult)
	require.Equal(t, uint8(40), got.Length)
	require.Equal(t, uint32(1), got.LocalDiscr)
	require.Equal(t, uint32(2), got.PeerDiscr)
	require.Equal(t, uint32(3), got.DesiredMinTxUs)
	require.Equal(t, uint32(4), got.RequiredMinRxUs)
}

func TestClient_Liveness_Packet_UnmarshalShort(t *testing.T) {
	t.Parallel()
	_, err := UnmarshalControlPacket(make([]byte, 39))
	require.EqualError(t, err, "short packet")
}

func TestClient_Liveness_Packet_UnmarshalBadLength(t *testing.T) {
	t.Parallel()
	cp := (&ControlPacket{Version: 1}).Marshal()
	cp[3] = 99
	_, err := UnmarshalControlPacket(cp)
	require.EqualError(t, err, "invalid length")
}

func TestClient_Liveness_Packet_BitMaskingVersionAndState_MarshalOnly(t *testing.T) {
	t.Parallel()
	cp := &ControlPacket{
		Version:    0xFF,
		State:      State(7),
		DetectMult: 1,
	}
	b := cp.Marshal()
	require.Equal(t, uint8(0xE0), b[0])
	require.Equal(t, uint8(0xC0), b[1])
}

func TestClient_Liveness_Packet_UnmarshalUnsupportedVersion(t *testing.T) {
	t.Parallel()
	cp := (&ControlPacket{Version: 7, State: StateUp, DetectMult: 1}).Marshal()
	_, err := UnmarshalControlPacket(cp)
	require.EqualError(t, err, "unsupported version: 7")
}

func TestClient_Liveness_Packet_UnmarshalStateMaskWithV1(t *testing.T) {
	t.Parallel()
	cp := (&ControlPacket{Version: 1, State: State(7), DetectMult: 1}).Marshal()
	got, err := UnmarshalControlPacket(cp)
	require.NoError(t, err)
	require.Equal(t, uint8(1), got.Version)
	require.Equal(t, StateUp, got.State) // state masked to 2 bits
}

func TestClient_Liveness_Packet_PaddingRemainsZero(t *testing.T) {
	t.Parallel()
	cp := &ControlPacket{Version: 3, State: StateDown, DetectMult: 5}
	b := cp.Marshal()
	require.True(t, bytes.Equal(b[20:], make([]byte, 20)))
}

func TestClient_Liveness_Packet_PassiveFlagRoundTrip(t *testing.T) {
	t.Parallel()

	cp := &ControlPacket{
		Version:         1,
		State:           StateUp,
		DetectMult:      3,
		LocalDiscr:      0x11223344,
		PeerDiscr:       0x55667788,
		DesiredMinTxUs:  0x01020304,
		RequiredMinRxUs: 0x0A0B0C0D,
	}
	require.False(t, cp.IsPassive())
	require.Equal(t, "none", cp.FlagsString())

	cp.SetPassive()
	require.True(t, cp.IsPassive())
	require.Equal(t, "passive", cp.FlagsString())

	b := cp.Marshal()
	require.Len(t, b, 40)
	require.Equal(t, uint8(FlagPassive), b[20], "passive flag should be encoded at byte 20")

	got, err := UnmarshalControlPacket(b)
	require.NoError(t, err)
	require.True(t, got.IsPassive())
	require.Equal(t, "passive", got.FlagsString())
}

func TestClient_Liveness_Packet_SetPassiveIsIdempotent(t *testing.T) {
	t.Parallel()

	cp := &ControlPacket{Version: 1, State: StateUp, DetectMult: 1}
	require.False(t, cp.IsPassive())

	cp.SetPassive()
	require.True(t, cp.IsPassive())
	firstFlags := cp.Flags

	cp.SetPassive()
	require.True(t, cp.IsPassive())
	require.Equal(t, firstFlags, cp.Flags, "SetPassive should not flip other bits")
}
