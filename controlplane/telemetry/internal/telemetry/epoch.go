package telemetry

import "time"

// EpochInfo is the slice of the ledger's epoch info the agent uses. The epoch itself keys the
// sample buffer's partitions; the slot position is what tells us when that epoch ends, which
// bounds how long a cached epoch may be probed with while the ledger RPC is unreachable.
type EpochInfo struct {
	Epoch        uint64
	SlotIndex    uint64
	SlotsInEpoch uint64
	AbsoluteSlot uint64
}

const (
	// fallbackSlotDuration is used to project an epoch's end until the slot rate has been measured.
	// It is Solana's 400ms slot target less 15%: the DoubleZero ledger runs slots faster than the
	// target (a 432k-slot epoch lands in ~44h, i.e. ~367ms per slot), so projecting with 400ms puts
	// the boundary up to ~4h later than it really is, right after a rollover.
	fallbackSlotDuration = 340 * time.Millisecond

	// measuredSlotMargin trims the measured slot duration so the projection lands before the real
	// boundary rather than after it, in case the ledger speeds up during an outage — while the
	// fetch is failing we have no way to notice.
	measuredSlotMargin = 0.95

	// minSlotRateBaseline is how much wall time the slot-rate baseline needs to span before it is
	// trusted. At the default refresh cadence a single delta is a few dozen slots, where RPC
	// latency and commitment jitter are a large fraction of the reading.
	minSlotRateBaseline = 5 * time.Minute

	// Bounds on a plausible measured slot duration. Outside these the reading is treated as noise
	// (a paused ledger, a clock step, a rewound slot counter) and the fallback is used.
	minSlotDuration = 200 * time.Millisecond
	maxSlotDuration = time.Second
)

// slotRateEstimator measures the ledger's real slot duration from AbsoluteSlot deltas. The
// baseline is the first observation rather than the previous one: a long baseline averages out the
// per-reading jitter that makes consecutive deltas useless at a 10s cadence.
//
// Not safe for concurrent use; the caller serializes access.
type slotRateEstimator struct {
	have     bool
	baseSlot uint64
	baseAt   time.Time
}

// observe folds a fresh AbsoluteSlot reading into the baseline and returns the slot duration to
// project with.
func (e *slotRateEstimator) observe(absoluteSlot uint64, now time.Time) time.Duration {
	if !e.have || absoluteSlot < e.baseSlot || now.Before(e.baseAt) {
		// First reading, or the slot counter or the clock moved backwards under us. Rebase.
		e.have = true
		e.baseSlot = absoluteSlot
		e.baseAt = now
		return fallbackSlotDuration
	}

	slots := absoluteSlot - e.baseSlot
	elapsed := now.Sub(e.baseAt)
	if slots == 0 || elapsed < minSlotRateBaseline {
		return fallbackSlotDuration
	}

	measured := time.Duration(int64(elapsed) / int64(slots))
	if measured < minSlotDuration || measured > maxSlotDuration {
		return fallbackSlotDuration
	}
	return time.Duration(float64(measured) * measuredSlotMargin)
}

// projectEpochEnd estimates when the epoch a fetch reported will end. A zero time means the ledger
// did not report a usable slot position, in which case only MaxEpochStaleness bounds the cache.
func projectEpochEnd(now time.Time, info EpochInfo, slot time.Duration) time.Time {
	if info.SlotsInEpoch == 0 || info.SlotIndex > info.SlotsInEpoch {
		return time.Time{}
	}
	remaining := info.SlotsInEpoch - info.SlotIndex
	return now.Add(time.Duration(remaining) * slot)
}
