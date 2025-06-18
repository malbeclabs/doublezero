package twamplight

import "time"

// ntpTimestamp converts a time.Time to an NTP timestamp.
func ntpTimestamp(t time.Time) (uint32, uint32) {
	const ntpEpochOffset = 2208988800
	secs := uint32(t.Unix()) + ntpEpochOffset
	nanos := uint64(t.Nanosecond())
	frac := uint32((nanos * (1 << 32)) / 1e9)
	return secs, frac
}
