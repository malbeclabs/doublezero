package probing

type LivenessStatus uint8

const (
	LivenessStatusUnknown LivenessStatus = iota
	LivenessStatusUp
	LivenessStatusDown
)

type LivenessTransition int

const (
	LivenessTransitionNoChange LivenessTransition = iota
	LivenessTransitionToUp
	LivenessTransitionToDown
)

type LivenessTracker interface {
	OnProbe(ok bool) LivenessTransition
	Status() LivenessStatus
	ConsecutiveOK() uint
	ConsecutiveFail() uint
}

type LivenessPolicy interface {
	NewTracker() LivenessTracker
}

type livenessState struct {
	consecOK, consecFail uint
	status               LivenessStatus
}
type hysteresisTracker struct {
	s    livenessState
	up   uint
	down uint
}

func (t *hysteresisTracker) OnProbe(ok bool) LivenessTransition {
	if ok {
		t.s.consecOK++
		t.s.consecFail = 0
	} else {
		t.s.consecFail++
		t.s.consecOK = 0
	}
	want := t.s.status
	if t.s.consecOK >= t.up {
		want = LivenessStatusUp
	}
	if t.s.consecFail >= t.down {
		want = LivenessStatusDown
	}
	if want == t.s.status {
		return LivenessTransitionNoChange
	}
	t.s.status = want
	if want == LivenessStatusUp {
		return LivenessTransitionToUp
	}
	return LivenessTransitionToDown
}

func (t *hysteresisTracker) Status() LivenessStatus {
	return t.s.status
}

func (t *hysteresisTracker) ConsecutiveOK() uint {
	return t.s.consecOK
}

func (t *hysteresisTracker) ConsecutiveFail() uint {
	return t.s.consecFail
}

func NewHysteresisLivenessPolicy(up, down uint) LivenessPolicy {
	return &HysteresisPolicy{
		UpThreshold:   up,
		DownThreshold: down,
	}
}

type HysteresisPolicy struct {
	UpThreshold   uint
	DownThreshold uint
}

func (p *HysteresisPolicy) NewTracker() LivenessTracker {
	return &hysteresisTracker{
		s:    livenessState{status: LivenessStatusDown},
		up:   p.UpThreshold,
		down: p.DownThreshold,
	}
}
