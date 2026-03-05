package signed_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"testing"

	"github.com/malbeclabs/doublezero/tools/twamp/pkg/signed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestSigner(t *testing.T) (ed25519.PublicKey, signed.Signer) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return pub, signed.NewEd25519Signer(priv)
}

func makeTestOffset(t *testing.T) []byte {
	t.Helper()
	blob := make([]byte, signed.LocationOffsetSize)
	_, err := rand.Read(blob)
	require.NoError(t, err)
	return blob
}

func TestProbePacket_MarshalUnmarshal(t *testing.T) {
	t.Parallel()

	pub, signer := newTestSigner(t)

	probe := signed.NewProbePacket(42, signer)

	buf := make([]byte, signed.ProbePacketSize)
	err := probe.Marshal(buf)
	require.NoError(t, err)

	got, err := signed.UnmarshalProbePacket(buf)
	require.NoError(t, err)

	assert.Equal(t, probe.Seq, got.Seq)
	assert.Equal(t, probe.Sec, got.Sec)
	assert.Equal(t, probe.Frac, got.Frac)
	assert.Equal(t, probe.SenderPubkey, got.SenderPubkey)
	assert.Equal(t, probe.Signature, got.Signature)

	assert.Equal(t, [32]byte(pub), got.SenderPubkey)
}

func TestReplyPacket_MarshalUnmarshal(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	reflectorPub, reflectorSigner := newTestSigner(t)
	geoprobePub, _ := newTestSigner(t)
	var geoprobePubkey [32]byte
	copy(geoprobePubkey[:], geoprobePub)

	probe := signed.NewProbePacket(7, senderSigner)
	reply, err := signed.NewReplyPacket(probe, reflectorSigner, geoprobePubkey, nil)
	require.NoError(t, err)

	buf := make([]byte, reply.Size())
	n, err := reply.Marshal(buf)
	require.NoError(t, err)
	assert.Equal(t, signed.MinReplyPacketSize, n)

	got, err := signed.UnmarshalReplyPacket(buf[:n])
	require.NoError(t, err)

	assert.Equal(t, reply.Probe.Seq, got.Probe.Seq)
	assert.Equal(t, reply.Probe.Sec, got.Probe.Sec)
	assert.Equal(t, reply.Probe.Frac, got.Probe.Frac)
	assert.Equal(t, reply.Probe.SenderPubkey, got.Probe.SenderPubkey)
	assert.Equal(t, reply.Probe.Signature, got.Probe.Signature)
	assert.Equal(t, reply.AuthorityPubkey, got.AuthorityPubkey)
	assert.Equal(t, reply.GeoprobePubkey, got.GeoprobePubkey)
	assert.Equal(t, reply.Signature, got.Signature)
	assert.Equal(t, [32]byte(reflectorPub), got.AuthorityPubkey)
	assert.Equal(t, geoprobePubkey, got.GeoprobePubkey)
	assert.Empty(t, got.Offsets)
}

func TestReplyPacket_MarshalUnmarshal_WithOffsets(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)

	offsets := [][]byte{makeTestOffset(t), makeTestOffset(t)}

	probe := signed.NewProbePacket(1, senderSigner)
	reply, err := signed.NewReplyPacket(probe, reflectorSigner, [32]byte{}, offsets)
	require.NoError(t, err)
	assert.Equal(t, signed.MinReplyPacketSize+2*signed.LocationOffsetSize, reply.Size())

	buf := make([]byte, reply.Size())
	n, err := reply.Marshal(buf)
	require.NoError(t, err)
	assert.Equal(t, reply.Size(), n)

	got, err := signed.UnmarshalReplyPacket(buf[:n])
	require.NoError(t, err)
	require.Len(t, got.Offsets, 2)
	assert.Equal(t, offsets[0], got.Offsets[0])
	assert.Equal(t, offsets[1], got.Offsets[1])
	assert.Equal(t, reply.Signature, got.Signature)
}

func TestReplyPacket_MarshalUnmarshal_MaxOffsets(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)

	offsets := make([][]byte, signed.MaxOffsets)
	for i := range offsets {
		offsets[i] = makeTestOffset(t)
	}

	probe := signed.NewProbePacket(1, senderSigner)
	reply, err := signed.NewReplyPacket(probe, reflectorSigner, [32]byte{}, offsets)
	require.NoError(t, err)
	assert.Equal(t, signed.MaxReplyPacketSize, reply.Size())

	buf := make([]byte, reply.Size())
	n, err := reply.Marshal(buf)
	require.NoError(t, err)
	assert.Equal(t, signed.MaxReplyPacketSize, n)

	got, err := signed.UnmarshalReplyPacket(buf[:n])
	require.NoError(t, err)
	require.Len(t, got.Offsets, signed.MaxOffsets)
	for i := range offsets {
		assert.Equal(t, offsets[i], got.Offsets[i])
	}
}

func TestProbePacket_Verify(t *testing.T) {
	t.Parallel()

	_, signer := newTestSigner(t)

	probe := signed.NewProbePacket(1, signer)
	assert.True(t, probe.Verify())
}

func TestProbePacket_Verify_TamperedData(t *testing.T) {
	t.Parallel()

	_, signer := newTestSigner(t)

	probe := signed.NewProbePacket(1, signer)

	probe.Seq = 999
	assert.False(t, probe.Verify())
}

func TestReplyPacket_Verify(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)
	geoprobePub, _ := newTestSigner(t)
	var geoprobePubkey [32]byte
	copy(geoprobePubkey[:], geoprobePub)

	probe := signed.NewProbePacket(1, senderSigner)
	reply, err := signed.NewReplyPacket(probe, reflectorSigner, geoprobePubkey, nil)
	require.NoError(t, err)

	assert.True(t, reply.Probe.Verify())
	assert.True(t, reply.Verify())
}

func TestReplyPacket_Verify_WithOffsets(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)

	offsets := [][]byte{makeTestOffset(t), makeTestOffset(t), makeTestOffset(t)}

	probe := signed.NewProbePacket(1, senderSigner)
	reply, err := signed.NewReplyPacket(probe, reflectorSigner, [32]byte{}, offsets)
	require.NoError(t, err)

	assert.True(t, reply.Verify())
}

func TestReplyPacket_Verify_TamperedOffset(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)

	offsets := [][]byte{makeTestOffset(t)}

	probe := signed.NewProbePacket(1, senderSigner)
	reply, err := signed.NewReplyPacket(probe, reflectorSigner, [32]byte{}, offsets)
	require.NoError(t, err)

	reply.Offsets[0][0] ^= 0xff
	assert.False(t, reply.Verify())
}

func TestReplyPacket_Verify_TamperedAuthorityPubkey(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)

	probe := signed.NewProbePacket(1, senderSigner)
	reply, err := signed.NewReplyPacket(probe, reflectorSigner, [32]byte{}, nil)
	require.NoError(t, err)

	reply.AuthorityPubkey[0] ^= 0xff
	assert.False(t, reply.Verify())
}

func TestReplyPacket_Verify_TamperedGeoprobePubkey(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)
	geoprobePub, _ := newTestSigner(t)
	var geoprobePubkey [32]byte
	copy(geoprobePubkey[:], geoprobePub)

	probe := signed.NewProbePacket(1, senderSigner)
	reply, err := signed.NewReplyPacket(probe, reflectorSigner, geoprobePubkey, nil)
	require.NoError(t, err)

	reply.GeoprobePubkey[0] ^= 0xff
	assert.False(t, reply.Verify())
}

func TestProbePacket_UnmarshalRejectsWrongSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		size int
	}{
		{"too small", signed.ProbePacketSize - 1},
		{"too large", signed.ProbePacketSize + 1},
		{"empty", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.size)
			_, err := signed.UnmarshalProbePacket(buf)
			assert.Error(t, err)
		})
	}
}

func TestReplyPacket_UnmarshalRejectsWrongSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		size int
	}{
		{"too small", signed.MinReplyPacketSize - 1},
		{"too large for 0 offsets", signed.MinReplyPacketSize + 1},
		{"empty", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.size)
			_, err := signed.UnmarshalReplyPacket(buf)
			assert.Error(t, err)
		})
	}
}

func TestReplyPacket_UnmarshalRejectsTooManyOffsets(t *testing.T) {
	t.Parallel()

	// Construct a buffer with NumOffsets = MaxOffsets+1 but otherwise valid size.
	numOffsets := signed.MaxOffsets + 1
	size := 173 + numOffsets*signed.LocationOffsetSize + 64
	buf := make([]byte, size)
	buf[172] = uint8(numOffsets)

	_, err := signed.UnmarshalReplyPacket(buf)
	assert.Error(t, err)
}

func TestNewReplyPacket_RejectsTooManyOffsets(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)

	offsets := make([][]byte, signed.MaxOffsets+1)
	for i := range offsets {
		offsets[i] = make([]byte, signed.LocationOffsetSize)
	}

	probe := signed.NewProbePacket(1, senderSigner)
	_, err := signed.NewReplyPacket(probe, reflectorSigner, [32]byte{}, offsets)
	assert.Error(t, err)
}

func TestNewReplyPacket_RejectsWrongSizeOffset(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)

	offsets := [][]byte{make([]byte, signed.LocationOffsetSize-1)}

	probe := signed.NewProbePacket(1, senderSigner)
	_, err := signed.NewReplyPacket(probe, reflectorSigner, [32]byte{}, offsets)
	assert.Error(t, err)
}

func TestProbePacket_Marshal_BufferTooSmall(t *testing.T) {
	t.Parallel()

	_, signer := newTestSigner(t)

	probe := signed.NewProbePacket(1, signer)
	buf := make([]byte, signed.ProbePacketSize-1)
	err := probe.Marshal(buf)
	assert.Error(t, err)
}

func TestReplyPacket_Marshal_BufferTooSmall(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)

	probe := signed.NewProbePacket(1, senderSigner)
	reply, err := signed.NewReplyPacket(probe, reflectorSigner, [32]byte{}, nil)
	require.NoError(t, err)
	buf := make([]byte, reply.Size()-1)
	_, err = reply.Marshal(buf)
	assert.Error(t, err)
}

func TestReplyPacket_Marshal_BufferTooSmallWithOffsets(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)

	offsets := [][]byte{makeTestOffset(t)}

	probe := signed.NewProbePacket(1, senderSigner)
	reply, err := signed.NewReplyPacket(probe, reflectorSigner, [32]byte{}, offsets)
	require.NoError(t, err)
	buf := make([]byte, reply.Size()-1)
	_, err = reply.Marshal(buf)
	assert.Error(t, err)
}

func TestProbePacket_ByteLayout(t *testing.T) {
	t.Parallel()

	pub, signer := newTestSigner(t)

	probe := signed.NewProbePacket(0x01020304, signer)

	buf := make([]byte, signed.ProbePacketSize)
	require.NoError(t, probe.Marshal(buf))

	assert.Equal(t, uint32(0x01020304), binary.BigEndian.Uint32(buf[0:4]), "seq at bytes 0-3")
	assert.Equal(t, probe.Sec, binary.BigEndian.Uint32(buf[4:8]), "sec at bytes 4-7")
	assert.Equal(t, probe.Frac, binary.BigEndian.Uint32(buf[8:12]), "frac at bytes 8-11")
	assert.Equal(t, []byte(pub), buf[12:44], "sender pubkey at bytes 12-43")
	assert.Equal(t, probe.Signature[:], buf[44:108], "signature at bytes 44-107")
}

func TestReplyPacket_ByteLayout(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	reflectorPub, reflectorSigner := newTestSigner(t)
	geoprobePub, _ := newTestSigner(t)
	var geoprobePubkey [32]byte
	copy(geoprobePubkey[:], geoprobePub)

	probe := signed.NewProbePacket(1, senderSigner)
	reply, err := signed.NewReplyPacket(probe, reflectorSigner, geoprobePubkey, nil)
	require.NoError(t, err)

	buf := make([]byte, reply.Size())
	n, err := reply.Marshal(buf)
	require.NoError(t, err)

	probeBuf := make([]byte, signed.ProbePacketSize)
	require.NoError(t, probe.Marshal(probeBuf))
	assert.Equal(t, probeBuf, buf[0:108], "embedded probe at bytes 0-107")
	assert.Equal(t, []byte(reflectorPub), buf[108:140], "authority pubkey at bytes 108-139")
	assert.Equal(t, geoprobePubkey[:], buf[140:172], "geoprobe pubkey at bytes 140-171")
	assert.Equal(t, byte(0), buf[172], "num offsets at byte 172")
	assert.Equal(t, reply.Signature[:], buf[173:n], "signature at bytes 173-end")
}

func TestReplyPacket_ByteLayout_WithOffsets(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)

	offsets := [][]byte{makeTestOffset(t), makeTestOffset(t)}

	probe := signed.NewProbePacket(1, senderSigner)
	reply, err := signed.NewReplyPacket(probe, reflectorSigner, [32]byte{}, offsets)
	require.NoError(t, err)

	buf := make([]byte, reply.Size())
	n, err := reply.Marshal(buf)
	require.NoError(t, err)

	assert.Equal(t, byte(2), buf[172], "num offsets at byte 172")
	assert.Equal(t, offsets[0], buf[173:173+signed.LocationOffsetSize], "offset 0")
	assert.Equal(t, offsets[1], buf[173+signed.LocationOffsetSize:173+2*signed.LocationOffsetSize], "offset 1")
	sigStart := n - 64
	assert.Equal(t, reply.Signature[:], buf[sigStart:n], "signature at end")
}

func TestProbePacket_Verify_InvalidSignatureBytes(t *testing.T) {
	t.Parallel()

	_, signer := newTestSigner(t)

	probe := signed.NewProbePacket(1, signer)

	probe.Signature = [64]byte{}
	assert.False(t, probe.Verify())
}

func TestReplyPacket_Verify_TamperedEmbeddedProbe(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)

	probe := signed.NewProbePacket(1, senderSigner)
	reply, err := signed.NewReplyPacket(probe, reflectorSigner, [32]byte{}, nil)
	require.NoError(t, err)

	reply.Probe.Seq = 999
	assert.False(t, reply.Verify())
}

func TestReplyPacket_Size(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)
	probe := signed.NewProbePacket(1, senderSigner)

	tests := []struct {
		name         string
		numOffsets   int
		expectedSize int
	}{
		{"0 offsets", 0, signed.MinReplyPacketSize},
		{"1 offset", 1, signed.MinReplyPacketSize + signed.LocationOffsetSize},
		{"5 offsets", 5, signed.MaxReplyPacketSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			offsets := make([][]byte, tt.numOffsets)
			for i := range offsets {
				offsets[i] = makeTestOffset(t)
			}
			reply, err := signed.NewReplyPacket(probe, reflectorSigner, [32]byte{}, offsets)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedSize, reply.Size())
		})
	}
}
