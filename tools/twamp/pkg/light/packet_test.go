package twamplight_test

import (
	"errors"
	"testing"

	twamplight "github.com/malbeclabs/doublezero/tools/twamp/pkg/light"
	"github.com/stretchr/testify/require"
)

func TestTWAMP_Packet(t *testing.T) {
	t.Run("NewPacket sets sequence and timestamps", func(t *testing.T) {
		seq := uint32(42)
		pkt := twamplight.NewPacket(seq)
		require.Equal(t, seq, pkt.Seq)
		if pkt.Seq != seq {
			t.Errorf("expected Seq=%d, got %d", seq, pkt.Seq)
		}
		if pkt.Sec == 0 && pkt.Frac == 0 {
			t.Errorf("expected non-zero timestamp")
		}
	})

	t.Run("MarshalBinary produces correct buffer", func(t *testing.T) {
		pkt := &twamplight.Packet{Seq: 1, Sec: 2, Frac: 3}
		buf, err := pkt.MarshalBinary()
		if err != nil {
			t.Fatalf("MarshalBinary failed: %v", err)
		}
		if len(buf) != 48 {
			t.Errorf("expected buffer length 48, got %d", len(buf))
		}
	})

	t.Run("UnmarshalBinary reconstructs Packet", func(t *testing.T) {
		original := &twamplight.Packet{Seq: 123, Sec: 456, Frac: 789}
		buf, _ := original.MarshalBinary()
		recovered, err := twamplight.UnmarshalBinary(buf)
		if err != nil {
			t.Fatalf("UnmarshalBinary failed: %v", err)
		}
		if *recovered != *original {
			t.Errorf("expected %+v, got %+v", original, recovered)
		}
	})

	t.Run("UnmarshalBinary rejects short buffer", func(t *testing.T) {
		buf := make([]byte, 20)
		_, err := twamplight.UnmarshalBinary(buf)
		if !errors.Is(err, twamplight.ErrInvalidPacket) {
			t.Errorf("expected ErrInvalidPacket, got %v", err)
		}
	})
}
