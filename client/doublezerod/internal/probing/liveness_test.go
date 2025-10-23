package probing

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProbing_Liveness(t *testing.T) {
	t.Parallel()

	t.Run("UpTransitionFromDown_WithThreshold", func(t *testing.T) {
		t.Parallel()
		p := livenessPolicy{UpThreshold: 3, DownThreshold: 2}
		s := newLivenessStateDown()

		var tr livenessTransition
		s, tr = p.Next(s, true)
		require.Equal(t, livenessTransitionNoChange, tr)
		require.Equal(t, uint(1), s.consecOK)
		require.Equal(t, uint(0), s.consecFail)
		require.Equal(t, livenessStatusDown, s.status)

		s, tr = p.Next(s, true)
		require.Equal(t, livenessTransitionNoChange, tr)
		require.Equal(t, uint(2), s.consecOK)
		require.Equal(t, uint(0), s.consecFail)
		require.Equal(t, livenessStatusDown, s.status)

		s, tr = p.Next(s, true)
		require.Equal(t, livenessTransitionToUp, tr)
		require.Equal(t, uint(3), s.consecOK)
		require.Equal(t, uint(0), s.consecFail)
		require.Equal(t, livenessStatusUp, s.status)
	})

	t.Run("DownTransitionFromUp_WithThreshold", func(t *testing.T) {
		t.Parallel()
		p := livenessPolicy{UpThreshold: 3, DownThreshold: 2}
		s := livenessState{status: livenessStatusUp}

		var tr livenessTransition
		s, tr = p.Next(s, false)
		require.Equal(t, livenessTransitionNoChange, tr)
		require.Equal(t, uint(0), s.consecOK)
		require.Equal(t, uint(1), s.consecFail)
		require.Equal(t, livenessStatusUp, s.status)

		s, tr = p.Next(s, false)
		require.Equal(t, livenessTransitionToDown, tr)
		require.Equal(t, uint(0), s.consecOK)
		require.Equal(t, uint(2), s.consecFail)
		require.Equal(t, livenessStatusDown, s.status)
	})

	t.Run("OppositeCounterResets_OnSuccessAfterFail", func(t *testing.T) {
		t.Parallel()
		p := livenessPolicy{UpThreshold: 3, DownThreshold: 2}
		s := newLivenessStateDown()

		// one failure increments consecFail
		s, _ = p.Next(s, false)
		require.Equal(t, uint(1), s.consecFail)
		require.Equal(t, uint(0), s.consecOK)

		// success resets consecFail and increments consecOK
		s, tr := p.Next(s, true)
		require.Equal(t, livenessTransitionNoChange, tr)
		require.Equal(t, uint(0), s.consecFail)
		require.Equal(t, uint(1), s.consecOK)
		require.Equal(t, livenessStatusDown, s.status)
	})

	t.Run("OppositeCounterResets_OnFailAfterSuccess", func(t *testing.T) {
		t.Parallel()
		p := livenessPolicy{UpThreshold: 3, DownThreshold: 2}
		s := newLivenessStateDown()

		// two successes, still down (threshold 3)
		s, _ = p.Next(s, true)
		s, _ = p.Next(s, true)
		require.Equal(t, uint(2), s.consecOK)
		require.Equal(t, uint(0), s.consecFail)
		require.Equal(t, livenessStatusDown, s.status)

		// failure resets consecOK and increments consecFail
		s, tr := p.Next(s, false)
		require.Equal(t, livenessTransitionNoChange, tr)
		require.Equal(t, uint(0), s.consecOK)
		require.Equal(t, uint(1), s.consecFail)
		require.Equal(t, livenessStatusDown, s.status)
	})

	t.Run("ImmediateThresholds_UpAndDown", func(t *testing.T) {
		t.Parallel()
		p := livenessPolicy{UpThreshold: 1, DownThreshold: 1}

		// from Down -> one success flips to Up
		s := newLivenessStateDown()
		var tr livenessTransition
		s, tr = p.Next(s, true)
		require.Equal(t, livenessTransitionToUp, tr)
		require.Equal(t, livenessStatusUp, s.status)
		require.Equal(t, uint(1), s.consecOK)
		require.Equal(t, uint(0), s.consecFail)

		// staying Up with further success: no change
		s, tr = p.Next(s, true)
		require.Equal(t, livenessTransitionNoChange, tr)
		require.Equal(t, livenessStatusUp, s.status)

		// one failure flips immediately to Down
		s, tr = p.Next(s, false)
		require.Equal(t, livenessTransitionToDown, tr)
		require.Equal(t, livenessStatusDown, s.status)

		// staying Down with further failure: no change
		s, tr = p.Next(s, false)
		require.Equal(t, livenessTransitionNoChange, tr)
		require.Equal(t, livenessStatusDown, s.status)
	})

	t.Run("UnknownStart_ReachesUpOrDownByThresholds", func(t *testing.T) {
		t.Parallel()
		p := livenessPolicy{UpThreshold: 2, DownThreshold: 2}

		// Unknown -> two successes => Up
		s := livenessState{status: livenessStatusUnknown}
		var tr livenessTransition
		s, tr = p.Next(s, true)
		require.Equal(t, livenessTransitionNoChange, tr)
		require.Equal(t, livenessStatusUnknown, s.status)
		s, tr = p.Next(s, true)
		require.Equal(t, livenessTransitionToUp, tr)
		require.Equal(t, livenessStatusUp, s.status)

		// Reset to Unknown -> two failures => Down
		s = livenessState{status: livenessStatusUnknown}
		s, tr = p.Next(s, false)
		require.Equal(t, livenessTransitionNoChange, tr)
		require.Equal(t, livenessStatusUnknown, s.status)
		s, tr = p.Next(s, false)
		require.Equal(t, livenessTransitionToDown, tr)
		require.Equal(t, livenessStatusDown, s.status)
	})

	t.Run("MaintainState_NoChangeOnSameDirectionBeyondThreshold", func(t *testing.T) {
		t.Parallel()
		p := livenessPolicy{UpThreshold: 2, DownThreshold: 2}

		// climb to Up
		s := newLivenessStateDown()
		s, _ = p.Next(s, true)
		s, tr := p.Next(s, true)
		require.Equal(t, livenessTransitionToUp, tr)
		require.Equal(t, livenessStatusUp, s.status)

		// extra successes keep status Up with NoChange
		s, tr = p.Next(s, true)
		require.Equal(t, livenessTransitionNoChange, tr)
		require.Equal(t, livenessStatusUp, s.status)

		// drop to Down
		s, _ = p.Next(s, false)
		s, tr = p.Next(s, false)
		require.Equal(t, livenessTransitionToDown, tr)
		require.Equal(t, livenessStatusDown, s.status)

		// extra failures keep status Down with NoChange
		s, tr = p.Next(s, false)
		require.Equal(t, livenessTransitionNoChange, tr)
		require.Equal(t, livenessStatusDown, s.status)
	})
}
