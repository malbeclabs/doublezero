package serviceability

import (
	"time"

	"github.com/malbeclabs/doublezero/config"
)

// The slot times below are *observed* rates, not protocol constants: actual slot
// time varies with network conditions, and a cluster that changes its slot
// cadence needs its value here updated.
const (
	dzLedgerSlotTime      = 400 * time.Millisecond
	solanaMainnetSlotTime = 350 * time.Millisecond
	solanaTestnetSlotTime = 200 * time.Millisecond
)

// solanaSlotTime returns the slot time of the Solana cluster a DoubleZero
// environment dials by default. A SOLANA_RPC_URL override repoints the cluster
// without changing the environment (see config.NetworkConfigForEnv), so pointing
// one environment's monitor at another cluster skews the estimate again.
func solanaSlotTime(env string) time.Duration {
	switch env {
	case config.EnvMainnetBeta, config.EnvMainnet:
		return solanaMainnetSlotTime
	// DoubleZero devnet dials Solana *testnet* (config.NetworkConfigForEnv gives it
	// TestnetSolanaRPC), so Solana devnet's slot rate never applies here.
	case config.EnvTestnet, config.EnvDevnet:
		return solanaTestnetSlotTime
	default:
		// Localnet runs a local test validator, which targets 400ms; so does any
		// environment we do not recognize, which keeps the previous behavior.
		return dzLedgerSlotTime
	}
}

// CalculateEpochTimes calculates the estimated start time of the previous and next epochs.
func CalculateEpochTimes(slotIndex, slotsInEpoch uint64, slotTime time.Duration) (currentEpochStartTime, nextEpochTime time.Time) {
	// A malformed getEpochInfo response would wrap the arithmetic below into a
	// plausible-looking timestamp; return zero times so the log shows it plainly.
	if slotsInEpoch == 0 || slotIndex > slotsInEpoch {
		return time.Time{}, time.Time{}
	}

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
