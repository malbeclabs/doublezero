package gm

import (
	"context"
	"fmt"
	"time"
)

type ProbeFailReason string

const (
	ProbeFailReasonNoRoute     ProbeFailReason = "no-route"
	ProbeFailReasonPacketsLost ProbeFailReason = "packets-lost"
	ProbeFailReasonNotReady    ProbeFailReason = "not-ready"
	ProbeFailReasonTimeout     ProbeFailReason = "timeout"
	ProbeFailReasonOther       ProbeFailReason = "other"
)

type ProbeResult struct {
	Timestamp  time.Time
	OK         bool
	Stats      *ProbeStats
	FailReason ProbeFailReason
	FailError  error
}

type ProbeTargetID string

type ProbeTarget interface {
	ID() ProbeTargetID
	Probe(ctx context.Context) (*ProbeResult, error)
	Close()
}

type ProbeStats struct {
	PacketsSent uint64
	PacketsRecv uint64
	PacketsLost uint64
	LossRatio   float64
	RTTMin      time.Duration
	RTTMax      time.Duration
	RTTAvg      time.Duration
	RTTStdDev   time.Duration
}

func (s *ProbeStats) String() string {
	return fmt.Sprintf(
		"packetsSent: %d, packetsRecv: %d, packetsLost: %d, lossRatio: %f, rttMin: %s, rttMax: %s, rttAvg: %s, rttStdDev: %s",
		s.PacketsSent,
		s.PacketsRecv,
		s.PacketsLost,
		s.LossRatio,
		s.RTTMin.String(),
		s.RTTMax.String(),
		s.RTTAvg.String(),
		s.RTTStdDev.String(),
	)
}

type Number interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 | ~uintptr |
		~float32 | ~float64
}

func safeDivide(num, den float64) float64 {
	if den == 0 {
		return 0
	}
	return num / den
}
