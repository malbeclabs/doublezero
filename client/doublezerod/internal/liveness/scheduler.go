package liveness

import (
	"container/heap"
	"context"
	"log/slog"
	"net"
	"sync"
	"time"
)

// evType distinguishes between scheduled transmit (TX) and detect-timeout (Detect) events.
type eventType uint8

const (
	eventTypeTX     eventType = 1 // transmit control packet
	eventTypeDetect eventType = 2 // detect timeout check
)

// event represents a single scheduled action tied to a session.
// Each event is timestamped and sequence-numbered to ensure stable ordering in the heap.
type event struct {
	when      time.Time // time when the event should fire
	eventType eventType // type of event (TX or Detect)
	session   *Session  // owning session
	seq       uint64    // sequence number for deterministic ordering
}

// EventQueue is a thread-safe priority queue of scheduled events.
// It supports pushing events and popping those whose time has come (or is nearest).
type EventQueue struct {
	mu  sync.Mutex
	pq  eventHeap // min-heap of events ordered by time then seq
	seq uint64    // global sequence counter for tie-breaking
}

// NewEventQueue constructs an initialized empty heap-based event queue.
func NewEventQueue() *EventQueue {
	h := eventHeap{}
	heap.Init(&h)
	return &EventQueue{pq: h}
}

// Push inserts a new event into the queue and assigns it a sequence number.
// Later events with identical timestamps are ordered by insertion.
func (q *EventQueue) Push(e *event) {
	q.mu.Lock()
	q.seq++
	e.seq = q.seq
	heap.Push(&q.pq, e)
	q.mu.Unlock()
}

// Pop removes and returns the next (earliest) event from the queue, or nil if empty.
func (q *EventQueue) Pop() *event {
	q.mu.Lock()
	if q.pq.Len() == 0 {
		q.mu.Unlock()
		return nil
	}
	ev := heap.Pop(&q.pq).(*event)
	q.mu.Unlock()
	return ev
}

// PopIfDue returns the next event if its scheduled time is due (<= now).
// Otherwise, it returns nil and the duration until the next event’s time,
// allowing the caller to sleep until that deadline.
func (q *EventQueue) PopIfDue(now time.Time) (*event, time.Duration) {
	q.mu.Lock()
	if q.pq.Len() == 0 {
		q.mu.Unlock()
		return nil, 10 * time.Millisecond
	}
	ev := q.pq[0]
	if d := ev.when.Sub(now); d > 0 {
		q.mu.Unlock()
		return nil, d
	}
	ev = heap.Pop(&q.pq).(*event)
	q.mu.Unlock()
	return ev, 0
}

// CountFor returns the number of events in the queue for a given interface and local IP.
func (q *EventQueue) CountFor(iface, localIP string) int {
	q.mu.Lock()
	defer q.mu.Unlock()
	c := 0
	for _, ev := range q.pq {
		if ev != nil && ev.session != nil && ev.session.peer != nil {
			p := ev.session.peer
			if p.Interface == iface && p.LocalIP == localIP {
				c++
			}
		}
	}
	return c
}

// Len returns the total number of events in the queue.
func (q *EventQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.pq.Len()
}

// eventHeap implements heap.Interface for event scheduling by time then seq.
type eventHeap []*event

func (h eventHeap) Len() int { return len(h) }

func (h eventHeap) Less(i, j int) bool {
	if h[i].when.Equal(h[j].when) {
		return h[i].seq < h[j].seq
	}
	return h[i].when.Before(h[j].when)
}

func (h eventHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }

func (h *eventHeap) Push(x any) { *h = append(*h, x.(*event)) }
func (h *eventHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

// Scheduler drives session state progression and control message exchange.
// It runs a single event loop that processes transmit (TX) and detect events across sessions.
// New sessions schedule TX immediately; detect is armed/re-armed after valid RX during Init/Up.
type Scheduler struct {
	log           *slog.Logger     // structured logger for observability
	udp           *UDPService      // shared UDP transport for all sessions
	onSessionDown func(s *Session) // callback invoked when a session transitions to Down
	eq            *EventQueue      // global time-ordered event queue
	maxEvents     int              // 0 = unlimited

	writeErrWarnEvery time.Duration // min interval between repeated write warnings
	writeErrWarnLast  time.Time     // last time a warning was logged
	writeErrWarnMu    sync.Mutex    // guards writeErrWarnLast

	peerMetrics bool
}

// NewScheduler constructs a Scheduler bound to a UDP transport and logger.
// onSessionDown is called asynchronously whenever a session is detected as failed.
func NewScheduler(log *slog.Logger, udp *UDPService, onSessionDown func(s *Session), maxEvents int, peerMetrics bool) *Scheduler {
	eq := NewEventQueue()
	return &Scheduler{
		log:               log,
		udp:               udp,
		onSessionDown:     onSessionDown,
		eq:                eq,
		writeErrWarnEvery: 5 * time.Second,
		maxEvents:         maxEvents,
		peerMetrics:       peerMetrics,
	}
}

// Run executes the scheduler’s main loop until ctx is canceled.
// It continuously pops and processes due events, sleeping until the next one if necessary.
// Each TX event sends a control packet and re-schedules the next TX;
// each Detect event checks for timeout and invokes onSessionDown if expired.
func (s *Scheduler) Run(ctx context.Context) error {
	s.log.Debug("liveness.scheduler: tx loop started")

	t := time.NewTimer(time.Hour)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			s.log.Debug("liveness.scheduler: stopped by context done", "reason", ctx.Err())
			return nil
		default:
		}

		now := time.Now()
		ev, wait := s.eq.PopIfDue(now)
		if ev == nil {
			// No due events — sleep until next one or timeout.
			if wait <= 0 {
				wait = 10 * time.Millisecond
			}
			if !t.Stop() {
				select {
				case <-t.C:
				default:
				}
			}
			t.Reset(wait)
			select {
			case <-ctx.Done():
				s.log.Debug("liveness.scheduler: stopped by context done", "reason", ctx.Err())
				return nil
			case <-t.C:
				continue
			}
		}

		emitSchedulerServiceQueueLengthGauge(s.eq, ev.session)

		prevState := ev.session.GetState()

		switch ev.eventType {
		case eventTypeTX:
			ev.session.mu.Lock()
			if ev.when.Equal(ev.session.nextTxScheduled) {
				ev.session.nextTxScheduled = time.Time{}
			}
			ev.session.mu.Unlock()
			s.doTX(ev.session)
			s.scheduleTx(time.Now(), ev.session)
		case eventTypeDetect:
			// drop stale detect events
			ev.session.mu.Lock()
			if !ev.when.Equal(ev.session.detectDeadline) {
				if ev.when.Equal(ev.session.nextDetectScheduled) {
					ev.session.nextDetectScheduled = time.Time{}
				}
				ev.session.mu.Unlock()
				metricSchedulerEventsDropped.WithLabelValues("detect", "stale").Inc()
				metricSchedulerTotalQueueLen.Set(float64(s.eq.Len()))
				continue
			}
			if ev.when.Equal(ev.session.nextDetectScheduled) {
				ev.session.nextDetectScheduled = time.Time{}
			}
			ev.session.mu.Unlock()

			if s.tryExpire(ev.session) {
				emitSessionStateMetrics(ev.session, &prevState, "detect_timeout", s.peerMetrics)
				// Expiration triggers asynchronous session-down handling.
				go s.onSessionDown(ev.session)
				continue
			}
			// Still active; re-arm detect timer for next interval.
			st := ev.session.GetState()
			if st == StateUp || st == StateInit {
				s.scheduleDetect(time.Now(), ev.session)
			}
		}
	}
}

func (s *Scheduler) maybeDropOnOverflow(et eventType) bool {
	if s.maxEvents <= 0 {
		return false
	}
	if s.eq.Len() < s.maxEvents {
		return false
	}
	if et == eventTypeTX {
		// never drop TX
		return false
	}
	metricSchedulerEventsDropped.WithLabelValues("detect", "overflow").Inc()
	return true
}

// scheduleTx schedules the next transmit event for the given session.
// Skips sessions that are not alive or are AdminDown; backoff is handled by ComputeNextTx.
func (s *Scheduler) scheduleTx(now time.Time, sess *Session) {
	// If TX already scheduled, bail without recomputing.
	sess.mu.Lock()
	if !sess.alive || sess.state == StateAdminDown || !sess.nextTxScheduled.IsZero() {
		sess.mu.Unlock()
		return
	}
	sess.mu.Unlock()

	// Compute next (locks internally, updates sess.nextTx)
	next := sess.ComputeNextTx(now, nil)

	// Publish the scheduled marker (re-check in case of race).
	sess.mu.Lock()
	if !sess.alive || sess.state == StateAdminDown || !sess.nextTxScheduled.IsZero() {
		sess.mu.Unlock()
		return
	}
	sess.nextTxScheduled = next
	sess.mu.Unlock()

	s.eq.Push(&event{when: next, eventType: eventTypeTX, session: sess})
	emitSchedulerServiceQueueLengthGauge(s.eq, sess)
}

// scheduleDetect arms or re-arms a session’s detection timer and enqueues a detect event.
// If the session is not alive or lacks a valid deadline, nothing is scheduled.
func (s *Scheduler) scheduleDetect(now time.Time, sess *Session) {
	ddl, ok := sess.ArmDetect(now)
	if !ok {
		return
	}

	sess.mu.Lock()
	if sess.nextDetectScheduled.Equal(ddl) {
		sess.mu.Unlock()
		return // already scheduled for this exact deadline
	}
	sess.nextDetectScheduled = ddl
	sess.mu.Unlock()

	if s.maybeDropOnOverflow(eventTypeDetect) {
		// undo marker since we didn’t enqueue
		sess.mu.Lock()
		if sess.nextDetectScheduled.Equal(ddl) {
			sess.nextDetectScheduled = time.Time{}
		}
		sess.mu.Unlock()
		return
	}

	s.eq.Push(&event{when: ddl, eventType: eventTypeDetect, session: sess})
	metricSchedulerTotalQueueLen.Set(float64(s.eq.Len()))
	emitSchedulerServiceQueueLengthGauge(s.eq, sess)
}

// doTX builds and transmits a ControlPacket representing the session’s current state.
// It reads protected fields under lock, serializes the packet, and sends via UDPService.
// Any transient send errors are logged at debug level.
func (s *Scheduler) doTX(sess *Session) {
	sess.mu.Lock()
	if !sess.alive || sess.state == StateAdminDown {
		sess.mu.Unlock()
		return
	}
	pkt := (&ControlPacket{
		Version:         1,
		State:           sess.state,
		DetectMult:      sess.detectMult,
		Length:          40,
		LocalDiscrr:     sess.localDiscr,
		peerDiscrr:      sess.peerDiscr,
		DesiredMinTxUs:  uint32(sess.localTxMin / time.Microsecond),
		RequiredMinRxUs: uint32(sess.localRxMin / time.Microsecond),
	}).Marshal()
	sess.mu.Unlock()
	src := net.IP(nil)
	if sess.route != nil {
		src = sess.route.Src
	}
	_, err := s.udp.WriteTo(pkt, sess.peerAddr, sess.peer.Interface, src)
	if err != nil {
		metricWriteSocketErrors.WithLabelValues(sess.peer.Interface, sess.peer.LocalIP).Inc()

		// Log throttled warnings for transient errors (e.g., bad FD state).
		now := time.Now()
		s.writeErrWarnMu.Lock()
		if s.writeErrWarnLast.IsZero() || now.Sub(s.writeErrWarnLast) >= s.writeErrWarnEvery {
			s.writeErrWarnLast = now
			s.writeErrWarnMu.Unlock()
			s.log.Warn("liveness.scheduler: error writing UDP packet", "error", err)
		} else {
			s.writeErrWarnMu.Unlock()
		}
	} else {
		metricControlPacketsTX.WithLabelValues(sess.peer.Interface, sess.peer.LocalIP).Inc()
	}
}

// tryExpire checks whether the session’s detect deadline has passed.
// If so, it transitions the session to Down, triggers an immediate TX
// to advertise the Down state, and returns true to signal expiration.
func (s *Scheduler) tryExpire(sess *Session) bool {
	now := time.Now()
	if sess.ExpireIfDue(now) {
		s.eq.Push(&event{when: now, eventType: eventTypeTX, session: sess})
		return true
	}
	return false
}
