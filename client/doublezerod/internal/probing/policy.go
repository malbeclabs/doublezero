package probing

// Internal liveness state machine
type routeState uint8

const (
	stateUnknown routeState = iota
	stateUp
	stateDown
)

// Per-route liveness counters + state (hysteresis)
type LivenessState struct {
	consecOK, consecFail uint
	state                routeState
}

// Pure policy (thresholds -> next state + transition)
type Transition int

const (
	NoChange Transition = iota
	ToUp
	ToDown
)

type Policy struct{ UpThreshold, DownThreshold uint }

func (p Policy) Next(s LivenessState, ok bool) (LivenessState, Transition) {
	if ok {
		s.consecOK++
		s.consecFail = 0
	} else {
		s.consecFail++
		s.consecOK = 0
	}
	want := s.state
	if s.consecOK >= p.UpThreshold {
		want = stateUp
	}
	if s.consecFail >= p.DownThreshold {
		want = stateDown
	}

	if want == s.state {
		return s, NoChange
	}
	s.state = want
	if want == stateUp {
		return s, ToUp
	}
	return s, ToDown
}
