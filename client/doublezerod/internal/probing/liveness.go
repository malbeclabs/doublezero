package probing

// Internal liveness state machine
type livenessStatus uint8

const (
	livenessStatusUnknown livenessStatus = iota
	livenessStatusUp
	livenessStatusDown
)

// Per-route liveness counters + state (hysteresis)
type livenessState struct {
	consecOK, consecFail uint
	status               livenessStatus
}

func newLivenessStateDown() livenessState {
	return livenessState{
		consecOK:   0,
		consecFail: 0,
		status:     livenessStatusDown,
	}
}

// Pure policy (thresholds -> next state + transition)
type livenessTransition int

const (
	livenessTransitionNoChange livenessTransition = iota
	livenessTransitionToUp
	livenessTransitionToDown
)

type livenessPolicy struct{ UpThreshold, DownThreshold uint }

func (p livenessPolicy) Next(s livenessState, ok bool) (livenessState, livenessTransition) {
	if ok {
		s.consecOK++
		s.consecFail = 0
	} else {
		s.consecFail++
		s.consecOK = 0
	}
	want := s.status
	if s.consecOK >= p.UpThreshold {
		want = livenessStatusUp
	}
	if s.consecFail >= p.DownThreshold {
		want = livenessStatusDown
	}

	if want == s.status {
		return s, livenessTransitionNoChange
	}
	s.status = want
	if want == livenessStatusUp {
		return s, livenessTransitionToUp
	}
	return s, livenessTransitionToDown
}
