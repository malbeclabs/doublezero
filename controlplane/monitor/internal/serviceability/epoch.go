package serviceability

import (
	"time"

	"github.com/malbeclabs/doublezero/config"
)

// The slot times below are *observed* rates, not protocol constants: actual slot
// time varies with network conditions.
//
// Solana is mid-rollout to 200ms slots. Testnet is already there; mainnet still
// produces slots in ~350ms, and solanaMainnetSlotTime must be changed to 200ms
// once mainnet completes the migration.
//
// The DoubleZero ledger targets 400ms and runs slightly ahead of it.
const (
	dzLedgerSlotTime      = 400 * time.Millisecond
	solanaMainnetSlotTime = 350 * time.Millisecond
	solanaTestnetSlotTime = 200 * time.Millisecond
)

// slotTimeForChain returns the slot time to use for a chain in a given DoubleZero
// environment. Anything that is not Solana L1 — and any environment not named
// below — gets the DoubleZero ledger's slot time.
func slotTimeForChain(chainName, env string) time.Duration {
	if chainName != chainSolana {
		return dzLedgerSlotTime
	}
	switch env {
	case config.EnvMainnetBeta, config.EnvMainnet:
		return solanaMainnetSlotTime
	// DoubleZero devnet dials Solana *testnet* (config.NetworkConfigForEnv gives it
	// TestnetSolanaRPC), so Solana devnet's slot rate never applies here.
	case config.EnvTestnet, config.EnvDevnet:
		return solanaTestnetSlotTime
	default:
		return dzLedgerSlotTime
	}
}

// CalculateEpochTimes calculates the estimated start time of the previous and next epochs.
func CalculateEpochTimes(slotIndex, slotsInEpoch uint64, slotTime time.Duration) (currentEpochStartTime, nextEpochTime time.Time) {
	nowUTC := time.Now().UTC()

	// calculate epoch start
	durationSinceEpochStart := time.Duration(slotIndex) * slotTime
	currentEpochStartTime = nowUTC.Add(-durationSinceEpochStart)

	// calculate next epoch start
	slotsUntilNextEpoch := slotsInEpoch - slotIndex
	durationUntilNextEpoch := time.Duration(slotsUntilNextEpoch) * slotTime
	nextEpochTime = nowUTC.Add(durationUntilNextEpoch)

	return currentEpochStartTime, nextEpochTime
}
