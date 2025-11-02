package probing_test

import (
	"testing"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/probing"
	"github.com/stretchr/testify/require"
)

func TestProbing_Liveness_HysteresisTracker_Behavior(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		up, down uint
		probes   []bool
		wantStat probing.LivenessStatus
		wantTran []probing.LivenessTransition
	}{
		{
			name: "remains down with intermittent success below threshold",
			up:   3, down: 2,
			probes:   []bool{true, false, true, false},
			wantStat: probing.LivenessStatusDown,
			wantTran: []probing.LivenessTransition{
				probing.LivenessTransitionNoChange,
				probing.LivenessTransitionNoChange,
				probing.LivenessTransitionNoChange,
				probing.LivenessTransitionNoChange,
			},
		},
		{
			name: "transitions to up after reaching up threshold",
			up:   2, down: 2,
			probes:   []bool{true, true},
			wantStat: probing.LivenessStatusUp,
			wantTran: []probing.LivenessTransition{
				probing.LivenessTransitionNoChange,
				probing.LivenessTransitionToUp,
			},
		},
		{
			name: "transitions down after consecutive failures",
			up:   2, down: 2,
			probes:   []bool{true, true, false, false},
			wantStat: probing.LivenessStatusDown,
			wantTran: []probing.LivenessTransition{
				probing.LivenessTransitionNoChange,
				probing.LivenessTransitionToUp,
				probing.LivenessTransitionNoChange,
				probing.LivenessTransitionToDown,
			},
		},
		{
			name: "stays up with more successes than threshold",
			up:   2, down: 3,
			probes:   []bool{true, true, true, true},
			wantStat: probing.LivenessStatusUp,
			wantTran: []probing.LivenessTransition{
				probing.LivenessTransitionNoChange,
				probing.LivenessTransitionToUp,
				probing.LivenessTransitionNoChange,
				probing.LivenessTransitionNoChange,
			},
		},
		{
			name: "requires multiple failures before going down",
			up:   2, down: 3,
			probes:   []bool{true, true, false, false, false},
			wantStat: probing.LivenessStatusDown,
			wantTran: []probing.LivenessTransition{
				probing.LivenessTransitionNoChange,
				probing.LivenessTransitionToUp,
				probing.LivenessTransitionNoChange,
				probing.LivenessTransitionNoChange,
				probing.LivenessTransitionToDown,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			policy, err := probing.NewHysteresisLivenessPolicy(tt.up, tt.down)
			require.NoError(t, err)
			tracker := policy.NewTracker()

			var gotTran []probing.LivenessTransition
			for _, ok := range tt.probes {
				gotTran = append(gotTran, tracker.OnProbe(ok))
			}

			require.Equal(t, tt.wantTran, gotTran, "transition mismatch")
			require.Equal(t, tt.wantStat, tracker.Status(), "final status mismatch")
		})
	}
}

func TestProbing_Liveness_HysteresisTracker_Counters(t *testing.T) {
	t.Parallel()

	policy, err := probing.NewHysteresisLivenessPolicy(2, 2)
	require.NoError(t, err)
	tracker := policy.NewTracker()

	require.Equal(t, uint(0), tracker.ConsecutiveOK())
	require.Equal(t, uint(0), tracker.ConsecutiveFail())
	require.Equal(t, probing.LivenessStatusDown, tracker.Status())

	tracker.OnProbe(true)
	require.Equal(t, uint(1), tracker.ConsecutiveOK())
	require.Equal(t, uint(0), tracker.ConsecutiveFail())

	tracker.OnProbe(true)
	require.Equal(t, uint(2), tracker.ConsecutiveOK())
	require.Equal(t, probing.LivenessStatusUp, tracker.Status())

	tracker.OnProbe(false)
	require.Equal(t, uint(1), tracker.ConsecutiveFail())
	require.Equal(t, uint(0), tracker.ConsecutiveOK())
}

func TestProbing_Liveness_HysteresisPolicy_NewTracker(t *testing.T) {
	t.Parallel()

	p, err := probing.NewHysteresisLivenessPolicy(3, 2)
	require.NoError(t, err)
	require.Equal(t, uint(3), p.UpThreshold)
	require.Equal(t, uint(2), p.DownThreshold)

	tr := p.NewTracker()
	require.Equal(t, probing.LivenessStatusDown, tr.Status())

	tr2 := p.NewTracker()
	require.NotSame(t, tr, tr2, "NewTracker should produce independent instances")
}
