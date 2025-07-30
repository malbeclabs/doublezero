package data

import "time"

func DeriveEpoch(now time.Time) uint64 {
	return uint64(now.Unix() / (60 * 60 * 24 * 2))
}
