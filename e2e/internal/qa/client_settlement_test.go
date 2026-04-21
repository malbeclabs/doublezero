package qa

import (
	"testing"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/stretchr/testify/require"
)

func TestFirstSlotInEpoch(t *testing.T) {
	t.Run("no warmup", func(t *testing.T) {
		s := &rpc.GetEpochScheduleResult{
			SlotsPerEpoch:    432_000,
			Warmup:           false,
			FirstNormalEpoch: 0,
			FirstNormalSlot:  0,
		}
		require.Equal(t, uint64(0), firstSlotInEpoch(s, 0))
		require.Equal(t, uint64(432_000), firstSlotInEpoch(s, 1))
		require.Equal(t, uint64(100*432_000), firstSlotInEpoch(s, 100))
	})

	t.Run("warmup phase", func(t *testing.T) {
		s := &rpc.GetEpochScheduleResult{
			SlotsPerEpoch:    432_000,
			Warmup:           true,
			FirstNormalEpoch: 14,
			FirstNormalSlot:  ((uint64(1) << 14) - 1) * 32,
		}
		require.Equal(t, uint64(0), firstSlotInEpoch(s, 0))
		require.Equal(t, uint64(32), firstSlotInEpoch(s, 1))
		require.Equal(t, uint64(96), firstSlotInEpoch(s, 2))
		require.Equal(t, s.FirstNormalSlot+432_000,
			firstSlotInEpoch(s, s.FirstNormalEpoch+1))
	})
}

func TestProratedAmount(t *testing.T) {
	slotsPerEpoch := uint64(432_000)

	remaining := uint64(399_600)
	got := proratedAmount(43_000_000, remaining, slotsPerEpoch)
	require.InDelta(t, 39_775_000, got, 1_200,
		"matches observed refund within ~1ms slot granularity")

	require.Equal(t, uint64(0), proratedAmount(43_000_000, 0, slotsPerEpoch))
	require.Equal(t, uint64(0), proratedAmount(0, 1, slotsPerEpoch))
	require.Equal(t, uint64(43_000_000),
		proratedAmount(43_000_000, slotsPerEpoch, slotsPerEpoch))
}

func TestSaturatingSubU64(t *testing.T) {
	require.Equal(t, uint64(0), saturatingSubU64(5, 10))
	require.Equal(t, uint64(0), saturatingSubU64(0, 0))
	require.Equal(t, uint64(5), saturatingSubU64(10, 5))
}
