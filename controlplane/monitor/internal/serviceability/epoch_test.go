package serviceability

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServiceability_SolanaSlotTime(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		env  string
		want time.Duration
	}{
		{"mainnet-beta", "mainnet-beta", 350 * time.Millisecond},
		{"mainnet", "mainnet", 350 * time.Millisecond},
		{"testnet", "testnet", 200 * time.Millisecond},
		// DZ devnet dials Solana testnet.
		{"devnet", "devnet", 200 * time.Millisecond},
		{"localnet", "localnet", 400 * time.Millisecond},
		{"unknown env", "", 400 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, solanaSlotTime(tt.env))
		})
	}
}

func TestServiceability_CalculateEpochTimes(t *testing.T) {
	t.Parallel()

	const slotsInEpoch = 432_000
	const tolerance = 5 * time.Second

	tests := []struct {
		name      string
		slotIndex uint64
		slotTime  time.Duration
	}{
		{"dz ledger mid-epoch", slotsInEpoch / 2, dzLedgerSlotTime},
		{"solana mainnet mid-epoch", slotsInEpoch / 2, solanaMainnetSlotTime},
		{"solana testnet mid-epoch", slotsInEpoch / 2, solanaTestnetSlotTime},
		{"epoch start", 0, solanaMainnetSlotTime},
		{"epoch end", slotsInEpoch, solanaTestnetSlotTime},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			now := time.Now().UTC()
			start, next := CalculateEpochTimes(tt.slotIndex, slotsInEpoch, tt.slotTime)

			// The full epoch spans slotsInEpoch slots, exactly, since both times
			// derive from a single clock read inside the function.
			require.Equal(t, time.Duration(slotsInEpoch)*tt.slotTime, next.Sub(start))

			require.WithinDuration(t, now.Add(-time.Duration(tt.slotIndex)*tt.slotTime), start, tolerance)
			require.WithinDuration(t, now.Add(time.Duration(slotsInEpoch-tt.slotIndex)*tt.slotTime), next, tolerance)
		})
	}
}

func TestServiceability_CalculateEpochTimes_MalformedEpochInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		slotIndex    uint64
		slotsInEpoch uint64
	}{
		{"no slots in epoch", 0, 0},
		{"slot index past end of epoch", 432_001, 432_000},
		// Would otherwise overflow the duration multiplication.
		{"absurd slot index", 1 << 62, 432_000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			start, next := CalculateEpochTimes(tt.slotIndex, tt.slotsInEpoch, solanaMainnetSlotTime)
			require.True(t, start.IsZero())
			require.True(t, next.IsZero())
		})
	}
}
