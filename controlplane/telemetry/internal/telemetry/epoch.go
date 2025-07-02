package telemetry

import "time"

// DeriveEpoch returns a monotonic counter that increments once every 2-day period
// since the Unix epoch (1970-01-01T00:00:00Z). The result is a stable epoch index
// that can be used to group time into fixed-duration intervals.
func DeriveEpoch(now time.Time) uint64 {
	return uint64(now.Unix() / (60 * 60 * 24 * 2))
}
