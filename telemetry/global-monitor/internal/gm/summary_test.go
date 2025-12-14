package gm

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGlobalMonitor_TargetSet_buildSummaries_HasAllKeysAndNames(t *testing.T) {
	t.Parallel()

	log := slog.New(slog.NewJSONHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	got := buildSummaries(log)

	require.Len(t, got, 6)

	type exp struct {
		k summaryKey
		n string
	}
	exps := []exp{
		{summaryKey{PlanKindSolValICMP, ProbePathPublicInternet}, "icmp/validators/public"},
		{summaryKey{PlanKindSolValICMP, ProbePathDoubleZero}, "icmp/validators/doublezero"},
		{summaryKey{PlanKindSolValTPUQUIC, ProbePathPublicInternet}, "tpuquic/validators/public"},
		{summaryKey{PlanKindSolValTPUQUIC, ProbePathDoubleZero}, "tpuquic/validators/doublezero"},
		{summaryKey{PlanKindDZUserICMP, ProbePathPublicInternet}, "icmp/users/public"},
		{summaryKey{PlanKindDZUserICMP, ProbePathDoubleZero}, "icmp/users/doublezero"},
	}

	for _, e := range exps {
		s, ok := got[e.k]
		require.True(t, ok)
		require.NotNil(t, s)
		require.Equal(t, e.n, s.name)
		require.Same(t, log, s.logger)

		require.EqualValues(t, 0, s.targets)
		require.EqualValues(t, 0, s.successes)
		require.EqualValues(t, 0, s.notReady)
		require.NotNil(t, s.failures)
		require.Len(t, s.failures, 5)
		require.EqualValues(t, 0, s.failures[ProbeFailReasonNoRoute])
		require.EqualValues(t, 0, s.failures[ProbeFailReasonPacketsLost])
		require.EqualValues(t, 0, s.failures[ProbeFailReasonNotReady])
		require.EqualValues(t, 0, s.failures[ProbeFailReasonTimeout])
		require.EqualValues(t, 0, s.failures[ProbeFailReasonOther])
	}
}

func TestGlobalMonitor_TargetSet_resultsSummary_add_SuccessIncrementsSuccesses(t *testing.T) {
	t.Parallel()

	s := newResultsSummary(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), "x")

	s.add(&ProbeResult{OK: true})
	s.add(&ProbeResult{OK: true})

	require.EqualValues(t, 2, s.targets)
	require.EqualValues(t, 2, s.successes)
	require.EqualValues(t, 0, s.notReady)
	for _, v := range s.failures {
		require.EqualValues(t, 0, v)
	}
}

func TestGlobalMonitor_TargetSet_resultsSummary_add_NotReadyTracksSeparately(t *testing.T) {
	t.Parallel()

	s := newResultsSummary(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), "x")

	s.add(&ProbeResult{OK: false, FailReason: ProbeFailReasonNotReady})
	s.add(&ProbeResult{OK: false, FailReason: ProbeFailReasonNotReady})
	s.add(&ProbeResult{OK: true})

	require.EqualValues(t, 3, s.targets)
	require.EqualValues(t, 1, s.successes)
	require.EqualValues(t, 2, s.notReady)
	require.EqualValues(t, 0, s.failures[ProbeFailReasonNotReady])
	require.EqualValues(t, 0, s.failures[ProbeFailReasonTimeout])
	require.EqualValues(t, 0, s.failures[ProbeFailReasonNoRoute])
	require.EqualValues(t, 0, s.failures[ProbeFailReasonPacketsLost])
	require.EqualValues(t, 0, s.failures[ProbeFailReasonOther])
}

func TestGlobalMonitor_TargetSet_resultsSummary_add_FailuresBucketed(t *testing.T) {
	t.Parallel()

	s := newResultsSummary(slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)), "x")

	s.add(&ProbeResult{OK: false, FailReason: ProbeFailReasonTimeout})
	s.add(&ProbeResult{OK: false, FailReason: ProbeFailReasonTimeout})
	s.add(&ProbeResult{OK: false, FailReason: ProbeFailReasonNoRoute})
	s.add(&ProbeResult{OK: false, FailReason: ProbeFailReasonOther})
	s.add(&ProbeResult{OK: true})

	require.EqualValues(t, 5, s.targets)
	require.EqualValues(t, 1, s.successes)
	require.EqualValues(t, 0, s.notReady)
	require.EqualValues(t, 2, s.failures[ProbeFailReasonTimeout])
	require.EqualValues(t, 1, s.failures[ProbeFailReasonNoRoute])
	require.EqualValues(t, 1, s.failures[ProbeFailReasonOther])
	require.EqualValues(t, 0, s.failures[ProbeFailReasonPacketsLost])
	require.EqualValues(t, 0, s.failures[ProbeFailReasonNotReady])
}

func TestGlobalMonitor_TargetSet_resultsSummary_log_EmitsExpectedFields(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	s := newResultsSummary(log, "icmp/validators/public")

	s.add(&ProbeResult{OK: true})
	s.add(&ProbeResult{OK: false, FailReason: ProbeFailReasonTimeout})
	s.add(&ProbeResult{OK: false, FailReason: ProbeFailReasonTimeout})
	s.add(&ProbeResult{OK: false, FailReason: ProbeFailReasonNotReady})

	d := 1500 * time.Millisecond
	s.log(d)

	line := bytes.TrimSpace(buf.Bytes())
	require.NotEmpty(t, line)

	var rec map[string]any
	require.NoError(t, json.Unmarshal(line, &rec))

	require.Equal(t, "INFO", rec["level"])
	require.Equal(t, "runner: tick summary (icmp/validators/public)", rec["msg"])

	require.Equal(t, float64(d.Nanoseconds()), rec["duration"])
	require.Equal(t, float64(1), rec["successes"])
	require.Equal(t, float64(2), rec["failures"])
	require.Equal(t, float64(1), rec["notReady"])
	require.Equal(t, float64(4), rec["targets"])

	require.Equal(t, float64(2), rec["failures[timeout]"])
	_, ok := rec["failures[no-route]"]
	require.False(t, ok)
	_, ok = rec["failures[packets-lost]"]
	require.False(t, ok)
	_, ok = rec["failures[other]"]
	require.False(t, ok)
}
