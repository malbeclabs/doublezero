package serviceability

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestServiceability_SlotTimeForChain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		chain string
		env   string
		want  time.Duration
	}{
		{"solana mainnet-beta", chainSolana, "mainnet-beta", 350 * time.Millisecond},
		{"solana mainnet", chainSolana, "mainnet", 350 * time.Millisecond},
		{"solana testnet", chainSolana, "testnet", 200 * time.Millisecond},
		// DZ devnet dials Solana testnet.
		{"solana devnet", chainSolana, "devnet", 200 * time.Millisecond},
		{"solana localnet", chainSolana, "localnet", 400 * time.Millisecond},
		{"solana unknown env", chainSolana, "", 400 * time.Millisecond},
		{"doublezero mainnet-beta", chainDoubleZero, "mainnet-beta", 400 * time.Millisecond},
		{"doublezero testnet", chainDoubleZero, "testnet", 400 * time.Millisecond},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, slotTimeForChain(tt.chain, tt.env))
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
