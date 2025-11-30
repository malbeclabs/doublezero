package solmon

import "time"

type ValidatorHealth struct {
	EWMAAvailability float64
	LastSuccess      time.Time
	LastFailure      time.Time
	ConsecutiveFail  uint32
}

func (h *ValidatorHealth) Update(now time.Time, ok bool, alpha float64) {
	if ok {
		h.LastSuccess = now
		h.ConsecutiveFail = 0
	} else {
		h.LastFailure = now
		h.ConsecutiveFail++
	}
	if h.EWMAAvailability == 0 && h.LastSuccess.IsZero() && h.LastFailure.IsZero() {
		if ok {
			h.EWMAAvailability = 1
		} else {
			h.EWMAAvailability = 0
		}
		return
	}
	sample := 0.0
	if ok {
		sample = 1.0
	}
	h.EWMAAvailability = alpha*sample + (1-alpha)*h.EWMAAvailability
}

type windowBucket struct {
	slot      int64
	successes uint32
	failures  uint32
	sumRTT    time.Duration
	countRTT  uint32
}

type rollingWindow struct {
	buckets []windowBucket
	res     time.Duration
}

func newRollingWindow(slots int, res time.Duration) *rollingWindow {
	bs := make([]windowBucket, slots)
	for i := range bs {
		bs[i].slot = -1
	}
	return &rollingWindow{buckets: bs, res: res}
}

func (w *rollingWindow) addSample(t time.Time, ok bool, rtt time.Duration) {
	if w == nil || len(w.buckets) == 0 || w.res <= 0 {
		return
	}
	slot := t.UnixNano() / int64(w.res)
	idx := int(slot % int64(len(w.buckets)))
	if idx < 0 {
		idx += len(w.buckets)
	}
	b := &w.buckets[idx]
	if b.slot != slot {
		*b = windowBucket{slot: slot}
	}
	if ok {
		b.successes++
		b.sumRTT += rtt
		b.countRTT++
	} else {
		b.failures++
	}
}

func (w *rollingWindow) Availability() float64 {
	if w == nil {
		return 0
	}
	var s, f uint64
	for i := range w.buckets {
		s += uint64(w.buckets[i].successes)
		f += uint64(w.buckets[i].failures)
	}
	if s+f == 0 {
		return 0
	}
	return float64(s) / float64(s+f)
}

func (w *rollingWindow) MeanRTT() time.Duration {
	if w == nil {
		return 0
	}
	var sum time.Duration
	var cnt uint64
	for i := range w.buckets {
		sum += w.buckets[i].sumRTT
		cnt += uint64(w.buckets[i].countRTT)
	}
	if cnt == 0 {
		return 0
	}
	return time.Duration(int64(sum) / int64(cnt))
}

func (w *rollingWindow) Counts() (successes uint64, failures uint64) {
	if w == nil {
		return 0, 0
	}
	for i := range w.buckets {
		successes += uint64(w.buckets[i].successes)
		failures += uint64(w.buckets[i].failures)
	}
	return successes, failures
}
