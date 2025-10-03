package serviceability

import (
	"time"
)

// slotTimeSeconds is the *approximate* time for a Solana slot (400ms).
// NOTE: This is a target. The actual slot time varies with network conditions.
// For more accurate calculations, one would need to use other RPC methods
// like `getBlockTime` for specific slots, which is beyond the scope of this tool.
const slotTimeSeconds = 0.4

// CalculateEpochTimes calculates the estimated start time of the previous and next epochs.
func CalculateEpochTimes(slotIndex, slotsInEpoch uint64) (currentEpochStartTime, nextEpochTime time.Time) {
	nowUTC := time.Now().UTC()

	// calculate epoch start
	secondsSinceEpochStart := float64(slotIndex) * slotTimeSeconds
	durationSinceEpochStart := time.Duration(secondsSinceEpochStart * float64(time.Second))
	currentEpochStartTime = nowUTC.Add(-durationSinceEpochStart)

	// calculate next epoch start
	slotsUntilNextEpoch := slotsInEpoch - slotIndex
	secondsUntilNextEpoch := float64(slotsUntilNextEpoch) * slotTimeSeconds
	durationUntilNextEpoch := time.Duration(secondsUntilNextEpoch * float64(time.Second))
	nextEpochTime = nowUTC.Add(durationUntilNextEpoch)

	return currentEpochStartTime, nextEpochTime
}
