package telemetry

import (
	"time"
)

type Sample struct {
	// Timestamp is the time the sample was taken.
	Timestamp time.Time `json:"timestamp"`

	// RTT is the round-trip time of the probe.
	RTT time.Duration `json:"rtt"`

	// Loss is true if the probe was lost.
	Loss bool `json:"loss"`
}
