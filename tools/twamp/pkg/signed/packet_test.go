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

	probe := signed.NewProbePacket(7, senderSigner)
	reply := signed.NewReplyPacket(probe, reflectorSigner)

	buf := make([]byte, signed.ReplyPacketSize)
	err := reply.Marshal(buf)
	require.NoError(t, err)

	got, err := signed.UnmarshalReplyPacket(buf)
	require.NoError(t, err)

	assert.Equal(t, reply.Probe.Seq, got.Probe.Seq)
	assert.Equal(t, reply.Probe.Sec, got.Probe.Sec)
	assert.Equal(t, reply.Probe.Frac, got.Probe.Frac)
	assert.Equal(t, reply.Probe.SenderPubkey, got.Probe.SenderPubkey)
	assert.Equal(t, reply.Probe.Signature, got.Probe.Signature)
	assert.Equal(t, reply.ReflectorPubkey, got.ReflectorPubkey)
	assert.Equal(t, reply.Signature, got.Signature)
	assert.Equal(t, [32]byte(reflectorPub), got.ReflectorPubkey)
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

	probe := signed.NewProbePacket(1, senderSigner)
	reply := signed.NewReplyPacket(probe, reflectorSigner)

	assert.True(t, reply.Probe.Verify())
	assert.True(t, reply.Verify())
}

func TestReplyPacket_Verify_TamperedReflectorPubkey(t *testing.T) {
	t.Parallel()

	_, senderSigner := newTestSigner(t)
	_, reflectorSigner := newTestSigner(t)

	probe := signed.NewProbePacket(1, senderSigner)
	reply := signed.NewReplyPacket(probe, reflectorSigner)

	reply.ReflectorPubkey[0] ^= 0xff
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
		{"too small", signed.ReplyPacketSize - 1},
		{"too large", signed.ReplyPacketSize + 1},
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
	reply := signed.NewReplyPacket(probe, reflectorSigner)
	buf := make([]byte, signed.ReplyPacketSize-1)
	err := reply.Marshal(buf)
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

	probe := signed.NewProbePacket(1, senderSigner)
	reply := signed.NewReplyPacket(probe, reflectorSigner)

	buf := make([]byte, signed.ReplyPacketSize)
	require.NoError(t, reply.Marshal(buf))

	probeBuf := make([]byte, signed.ProbePacketSize)
	require.NoError(t, probe.Marshal(probeBuf))
	assert.Equal(t, probeBuf, buf[0:108], "embedded probe at bytes 0-107")

	assert.Equal(t, []byte(reflectorPub), buf[108:140], "reflector pubkey at bytes 108-139")

	assert.Equal(t, reply.Signature[:], buf[140:204], "signature at bytes 140-203")
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
	reply := signed.NewReplyPacket(probe, reflectorSigner)

	reply.Probe.Seq = 999
	assert.False(t, reply.Verify())
}
