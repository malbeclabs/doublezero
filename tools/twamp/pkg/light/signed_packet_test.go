package twamplight_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"testing"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSignedProbePacket_MarshalUnmarshal(t *testing.T) {
	t.Parallel()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	probe := twamplight.NewSignedProbePacket(42, priv)

	buf := make([]byte, twamplight.SignedProbePacketSize)
	err = probe.Marshal(buf)
	require.NoError(t, err)

	got, err := twamplight.UnmarshalSignedProbePacket(buf)
	require.NoError(t, err)

	assert.Equal(t, probe.Seq, got.Seq)
	assert.Equal(t, probe.Sec, got.Sec)
	assert.Equal(t, probe.Frac, got.Frac)
	assert.Equal(t, probe.SenderPubkey, got.SenderPubkey)
	assert.Equal(t, probe.Signature, got.Signature)

	// Verify pubkey in packet matches generated key.
	assert.Equal(t, [32]byte(pub), got.SenderPubkey)
}

func TestSignedReplyPacket_MarshalUnmarshal(t *testing.T) {
	t.Parallel()

	_, senderPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	reflectorPub, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	probe := twamplight.NewSignedProbePacket(7, senderPriv)
	reply := twamplight.NewSignedReplyPacket(probe, reflectorPriv)

	buf := make([]byte, twamplight.SignedReplyPacketSize)
	err = reply.Marshal(buf)
	require.NoError(t, err)

	got, err := twamplight.UnmarshalSignedReplyPacket(buf)
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

func TestSignedProbePacket_VerifyProbe(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	probe := twamplight.NewSignedProbePacket(1, priv)
	assert.True(t, twamplight.VerifyProbe(probe))
}

func TestSignedProbePacket_VerifyProbe_TamperedData(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	probe := twamplight.NewSignedProbePacket(1, priv)

	// Tamper with sequence number.
	probe.Seq = 999
	assert.False(t, twamplight.VerifyProbe(probe))
}

func TestSignedReplyPacket_VerifyReply(t *testing.T) {
	t.Parallel()

	_, senderPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	_, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	probe := twamplight.NewSignedProbePacket(1, senderPriv)
	reply := twamplight.NewSignedReplyPacket(probe, reflectorPriv)

	assert.True(t, twamplight.VerifyProbe(&reply.Probe))
	assert.True(t, twamplight.VerifyReply(reply))
}

func TestSignedReplyPacket_VerifyReply_TamperedReflectorPubkey(t *testing.T) {
	t.Parallel()

	_, senderPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	_, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	probe := twamplight.NewSignedProbePacket(1, senderPriv)
	reply := twamplight.NewSignedReplyPacket(probe, reflectorPriv)

	// Tamper with reflector pubkey.
	reply.ReflectorPubkey[0] ^= 0xff
	assert.False(t, twamplight.VerifyReply(reply))
}

func TestSignedProbePacket_UnmarshalRejectsWrongSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		size int
	}{
		{"too small", twamplight.SignedProbePacketSize - 1},
		{"too large", twamplight.SignedProbePacketSize + 1},
		{"empty", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.size)
			_, err := twamplight.UnmarshalSignedProbePacket(buf)
			assert.ErrorIs(t, err, twamplight.ErrInvalidPacket)
		})
	}
}

func TestSignedReplyPacket_UnmarshalRejectsWrongSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		size int
	}{
		{"too small", twamplight.SignedReplyPacketSize - 1},
		{"too large", twamplight.SignedReplyPacketSize + 1},
		{"empty", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := make([]byte, tt.size)
			_, err := twamplight.UnmarshalSignedReplyPacket(buf)
			assert.ErrorIs(t, err, twamplight.ErrInvalidPacket)
		})
	}
}

func TestSignedProbePacket_Marshal_BufferTooSmall(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	probe := twamplight.NewSignedProbePacket(1, priv)
	buf := make([]byte, twamplight.SignedProbePacketSize-1)
	err = probe.Marshal(buf)
	assert.Error(t, err)
}

func TestSignedReplyPacket_Marshal_BufferTooSmall(t *testing.T) {
	t.Parallel()

	_, senderPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	_, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	probe := twamplight.NewSignedProbePacket(1, senderPriv)
	reply := twamplight.NewSignedReplyPacket(probe, reflectorPriv)
	buf := make([]byte, twamplight.SignedReplyPacketSize-1)
	err = reply.Marshal(buf)
	assert.Error(t, err)
}

func TestSignedProbePacket_ByteLayout(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pub := priv.Public().(ed25519.PublicKey)

	probe := twamplight.NewSignedProbePacket(0x01020304, priv)

	buf := make([]byte, twamplight.SignedProbePacketSize)
	require.NoError(t, probe.Marshal(buf))

	// Verify byte-level layout.
	assert.Equal(t, uint32(0x01020304), binary.BigEndian.Uint32(buf[0:4]), "seq at bytes 0-3")
	assert.Equal(t, probe.Sec, binary.BigEndian.Uint32(buf[4:8]), "sec at bytes 4-7")
	assert.Equal(t, probe.Frac, binary.BigEndian.Uint32(buf[8:12]), "frac at bytes 8-11")
	assert.Equal(t, []byte(pub), buf[12:44], "sender pubkey at bytes 12-43")
	assert.Equal(t, probe.Signature[:], buf[44:108], "signature at bytes 44-107")
}

func TestSignedReplyPacket_ByteLayout(t *testing.T) {
	t.Parallel()

	_, senderPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	reflectorPub, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	probe := twamplight.NewSignedProbePacket(1, senderPriv)
	reply := twamplight.NewSignedReplyPacket(probe, reflectorPriv)

	buf := make([]byte, twamplight.SignedReplyPacketSize)
	require.NoError(t, reply.Marshal(buf))

	// Verify probe is embedded at bytes 0-107.
	probeBuf := make([]byte, twamplight.SignedProbePacketSize)
	require.NoError(t, probe.Marshal(probeBuf))
	assert.Equal(t, probeBuf, buf[0:108], "embedded probe at bytes 0-107")

	// Verify reflector pubkey at bytes 108-139.
	assert.Equal(t, []byte(reflectorPub), buf[108:140], "reflector pubkey at bytes 108-139")

	// Verify signature at bytes 140-203.
	assert.Equal(t, reply.Signature[:], buf[140:204], "signature at bytes 140-203")
}

func TestSignedProbePacket_VerifyProbe_InvalidSignatureBytes(t *testing.T) {
	t.Parallel()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	probe := twamplight.NewSignedProbePacket(1, priv)

	// Zero out the signature.
	probe.Signature = [64]byte{}
	assert.False(t, twamplight.VerifyProbe(probe))
}

func TestSignedReplyPacket_VerifyReply_TamperedEmbeddedProbe(t *testing.T) {
	t.Parallel()

	_, senderPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	_, reflectorPriv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	probe := twamplight.NewSignedProbePacket(1, senderPriv)
	reply := twamplight.NewSignedReplyPacket(probe, reflectorPriv)

	// Tamper with the embedded probe's sequence.
	reply.Probe.Seq = 999
	assert.False(t, twamplight.VerifyReply(reply))
}
