package telemetry

import (
	"fmt"
	"time"
)

type Sample struct {
	// Timestamp is the time the sample was taken.
	Timestamp time.Time `json:"timestamp"`

	// Link is the link/tunnel used to probe the target device.
	Link string `json:"link"`

	// Device is the target device of the sample; the device that was probed.
	Device string `json:"device"`

	// RTT is the round-trip time of the probe.
	RTT time.Duration `json:"rtt"`

	// Loss is true if the probe was lost.
	Loss bool `json:"loss"`
}

func (s *Sample) PeerKey() string {
	return s.Link
}

func (s *Sample) Key() string {
	return fmt.Sprintf("%s-%s", s.PeerKey(), s.Timestamp.Format(time.RFC3339Nano))
}
