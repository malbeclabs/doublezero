package gm

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type summaryKey struct {
	kind PlanKind
	path ProbePath
}

func buildSummaries(log *slog.Logger) map[summaryKey]*resultsSummary {
	return map[summaryKey]*resultsSummary{
		{PlanKindSolValICMP, ProbePathPublicInternet}: newResultsSummary(log, "icmp/validators/public"),
		{PlanKindSolValICMP, ProbePathDoubleZero}:     newResultsSummary(log, "icmp/validators/doublezero"),

		{PlanKindSolValTPUQUIC, ProbePathPublicInternet}: newResultsSummary(log, "tpuquic/validators/public"),
		{PlanKindSolValTPUQUIC, ProbePathDoubleZero}:     newResultsSummary(log, "tpuquic/validators/doublezero"),

		{PlanKindDZUserICMP, ProbePathPublicInternet}: newResultsSummary(log, "icmp/users/public"),
		{PlanKindDZUserICMP, ProbePathDoubleZero}:     newResultsSummary(log, "icmp/users/doublezero"),
	}
}

type resultsSummary struct {
	logger *slog.Logger
	name   string

	targets   uint64
	successes uint64
	failures  map[ProbeFailReason]uint64
	notReady  uint64
}

func newResultsSummary(log *slog.Logger, name string) *resultsSummary {
	failures := map[ProbeFailReason]uint64{
		ProbeFailReasonNoRoute:     0,
		ProbeFailReasonPacketsLost: 0,
		ProbeFailReasonNotReady:    0,
		ProbeFailReasonTimeout:     0,
		ProbeFailReasonOther:       0,
	}
	return &resultsSummary{
		logger:   log,
		name:     name,
		failures: failures,
	}
}

func (s *resultsSummary) add(res *ProbeResult) {
	s.targets++
	if res.OK {
		s.successes++
		return
	}
	switch res.FailReason {
	case ProbeFailReasonNotReady:
		s.notReady++
	default:
		s.failures[res.FailReason]++
	}
}

func (s *resultsSummary) log(duration time.Duration) {
	attrs := []slog.Attr{
		slog.Duration("duration", duration),
		slog.Uint64("successes", s.successes),
	}
	var totalFailures uint64
	for reason, count := range s.failures {
		if count > 0 {
			attrs = append(attrs, slog.Uint64(fmt.Sprintf("failures[%s]", reason), count))
		}
		totalFailures += count
	}
	attrs = append(attrs, slog.Uint64("failures", totalFailures))
	if s.notReady > 0 {
		attrs = append(attrs, slog.Uint64("notReady", s.notReady))
	}
	attrs = append(attrs, slog.Uint64("targets", s.targets))
	s.logger.LogAttrs(
		context.Background(),
		slog.LevelInfo,
		fmt.Sprintf("runner: tick summary (%s)", s.name),
		attrs...,
	)
}
