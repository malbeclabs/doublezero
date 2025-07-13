package twamplight_test

import (
	"testing"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/require"
)

func TestTWAMP_Packet(t *testing.T) {
	t.Run("NewPacket sets sequence and timestamps", func(t *testing.T) {
		t.Parallel()

		seq := uint32(42)
		pkt := twamplight.NewPacket(seq)
		require.Equal(t, seq, pkt.Seq)
		require.NotZero(t, pkt.Sec)
		require.NotZero(t, pkt.Frac)
	})

	t.Run("Marshal produces correct buffer", func(t *testing.T) {
		t.Parallel()

		pkt := &twamplight.Packet{Seq: 1, Sec: 2, Frac: 3}
		buf := make([]byte, twamplight.PacketSize)
		require.NoError(t, pkt.Marshal(buf))
		require.Equal(t, twamplight.PacketSize, len(buf))
	})

	t.Run("UnmarshalPacket reconstructs Packet", func(t *testing.T) {
		t.Parallel()

		original := &twamplight.Packet{Seq: 123, Sec: 456, Frac: 789}
		buf := make([]byte, twamplight.PacketSize)
		require.NoError(t, original.Marshal(buf))
		recovered, err := twamplight.UnmarshalPacket(buf)
		require.NoError(t, err)
		require.Equal(t, original, recovered)
	})

	t.Run("UnmarshalPacket rejects short buffer", func(t *testing.T) {
		t.Parallel()

		buf := make([]byte, 20)
		_, err := twamplight.UnmarshalPacket(buf)
		require.ErrorIs(t, err, twamplight.ErrInvalidPacket)
	})

	t.Run("UnmarshalPacket rejects packet with non-zero padding", func(t *testing.T) {
		t.Parallel()

		buf := make([]byte, 48)
		buf[12] = 1
		_, err := twamplight.UnmarshalPacket(buf)
		require.ErrorIs(t, err, twamplight.ErrInvalidPacket)
	})
}
