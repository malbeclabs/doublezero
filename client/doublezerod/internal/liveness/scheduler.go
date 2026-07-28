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

type SessionDownFunc func(s *Session)

// Scheduler drives session state progression and control message exchange.
// It runs a single event loop that processes transmit (TX) and detect events across sessions.
// New sessions schedule TX immediately; detect is armed/re-armed after valid RX during Init/Up.
type Scheduler struct {
	log           *slog.Logger    // structured logger for observability
	udp           UDPService      // shared UDP transport for all sessions
	onSessionDown SessionDownFunc // callback invoked when a session transitions to Down
	eq            *EventQueue     // global time-ordered event queue
	maxEvents     int             // 0 = unlimited

	// wake interrupts Run's sleep when an event is pushed that is due sooner than
	// the deadline Run is currently sleeping on. Buffered so a push that races the
	// sleep is not lost, and coalescing: Run re-polls the queue after any wake.
	wake chan struct{}

	writeErrWarnEvery time.Duration // min interval between repeated write warnings
	writeErrWarnLast  time.Time     // last time a warning was logged
	writeErrWarnMu    sync.Mutex    // guards writeErrWarnLast

	enablePeerMetrics bool
	metrics           *Metrics

	passiveMode   bool
	clientVersion ClientVersion
}

// NewScheduler constructs a Scheduler bound to a UDP transport and logger.
// onSessionDown is called asynchronously whenever a session is detected as failed.
func NewScheduler(log *slog.Logger, udp UDPService, onSessionDown SessionDownFunc, maxEvents int, enablePeerMetrics bool, metrics *Metrics, passiveMode bool, clientVersion ClientVersion) *Scheduler {
	eq := NewEventQueue()
	return &Scheduler{
		log:               log,
		udp:               udp,
		onSessionDown:     onSessionDown,
		eq:                eq,
		writeErrWarnEvery: 5 * time.Second,
		maxEvents:         maxEvents,
		wake:              make(chan struct{}, 1),
		enablePeerMetrics: enablePeerMetrics,
		metrics:           metrics,
		passiveMode:       passiveMode,
		clientVersion:     clientVersion,
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

	var lastServiceQueueUpdate time.Time

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
			case <-s.wake:
				// An event was pushed that may be due before our sleep deadline.
				continue
			case <-t.C:
				continue
			}
		}

		ev.session.mu.Lock()
		peer := *ev.session.peer
		ev.session.mu.Unlock()

		// Periodically update per-service queue length metric (O(n) scan, throttled).
		if now.Sub(lastServiceQueueUpdate) >= 10*time.Second {
			lastServiceQueueUpdate = now
			s.metrics.schedulerServiceQueueLength(s.eq, peer)
		}

		prevState := ev.session.GetState()

		switch ev.eventType {
		case eventTypeTX:
			// Drop stale TX events, mirroring the detect branch below. An event is
			// stale when scheduleTxNow displaced it by taking the marker over; firing
			// it would emit a duplicate packet, and under a sustained state-change
			// stream the orphans would accumulate (maybeDropOnOverflow exempts TX from
			// maxEvents, so they cannot be shed under queue pressure either).
			//
			// Safe because every marker write is paired with a push and every clear is
			// followed by a re-arm, so a dropped event always has a live successor.
			ev.session.mu.Lock()
			if !ev.when.Equal(ev.session.nextTxScheduled) {
				ev.session.mu.Unlock()
				s.metrics.SchedulerEventsDropped.WithLabelValues(peer.Interface, peer.LocalIP, "stale").Inc()
				s.metrics.SchedulerTotalQueueLen.Set(float64(s.eq.Len()))
				continue
			}
			ev.session.nextTxScheduled = time.Time{}
			ev.session.mu.Unlock()
			s.doTX(ctx, ev.session)
			// Do not reschedule periodic TX while AdminDown; we only want the explicit one
			if ev.session.GetState() != StateAdminDown {
				s.scheduleTx(time.Now(), ev.session)
			}
		case eventTypeDetect:
			// drop stale detect events
			ev.session.mu.Lock()
			if !ev.when.Equal(ev.session.detectDeadline) {
				if ev.when.Equal(ev.session.nextDetectScheduled) {
					ev.session.nextDetectScheduled = time.Time{}
				}
				ev.session.mu.Unlock()
				s.metrics.SchedulerEventsDropped.WithLabelValues(peer.Interface, peer.LocalIP, "stale").Inc()
				s.metrics.SchedulerTotalQueueLen.Set(float64(s.eq.Len()))
				continue
			}
			if ev.when.Equal(ev.session.nextDetectScheduled) {
				ev.session.nextDetectScheduled = time.Time{}
			}
			ev.session.mu.Unlock()

			s.log.Debug("liveness.scheduler: detect event",
				"peer", peer.String(),
				"when", ev.when,
			)

			if s.tryExpire(ev.session) {
				s.metrics.sessionStateTransition(peer, &prevState, StateDown, "detect_timeout", s.enablePeerMetrics)
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

// push enqueues an event, updates the queue-length gauge, and nudges Run.
//
// The nudge matters because Run sleeps until the deadline of the queue head it last
// observed; an event pushed from outside the loop (route registration, or an RX-driven
// state change) is otherwise not noticed until that sleep ends, which while a peer is
// Down can be as long as backoffMax. The send is non-blocking: a pending token already
// forces the re-poll, and s.wake is nil in tests that construct a Scheduler without Run.
func (s *Scheduler) push(ev *event) {
	s.eq.Push(ev)
	s.metrics.SchedulerTotalQueueLen.Set(float64(s.eq.Len()))
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Scheduler) maybeDropOnOverflow(et eventType, peer Peer) bool {
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
	s.metrics.SchedulerEventsDropped.WithLabelValues(peer.Interface, peer.LocalIP, "overflow").Inc()
	return true
}

// scheduleTx schedules the next transmit event for the given session.
// Skips sessions that are not alive; backoff is handled by ComputeNextTx.
func (s *Scheduler) scheduleTx(now time.Time, sess *Session) {
	// If TX already scheduled, bail without recomputing.
	sess.mu.Lock()
	if !sess.alive || !sess.nextTxScheduled.IsZero() {
		sess.mu.Unlock()
		return
	}
	sess.mu.Unlock()

	// Compute next (locks internally, updates sess.nextTx)
	next := sess.ComputeNextTx(now, nil)

	// Publish the scheduled marker (re-check in case of race).
	sess.mu.Lock()
	if !sess.alive || !sess.nextTxScheduled.IsZero() {
		sess.mu.Unlock()
		return
	}
	sess.nextTxScheduled = next
	sess.mu.Unlock()

	s.push(&event{when: next, eventType: eventTypeTX, session: sess})
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
	peer := *sess.peer
	sess.mu.Unlock()

	if s.maybeDropOnOverflow(eventTypeDetect, peer) {
		// undo marker since we didn’t enqueue
		sess.mu.Lock()
		if sess.nextDetectScheduled.Equal(ddl) {
			sess.nextDetectScheduled = time.Time{}
		}
		sess.mu.Unlock()
		return
	}

	s.push(&event{when: ddl, eventType: eventTypeDetect, session: sess})
}

// doTX builds and transmits a ControlPacket representing the session’s current state.
// It reads protected fields under lock, serializes the packet, and sends via UDPService.
// Any transient send errors are logged at debug level.
func (s *Scheduler) doTX(ctx context.Context, sess *Session) {
	sess.mu.Lock()
	if !sess.alive {
		sess.mu.Unlock()
		return
	}
	// Record the transmit time for pacing before we drop the lock. Set on attempt
	// rather than on success so a failing socket cannot be spun on either.
	sess.lastTx = time.Now()
	state := sess.state
	localDiscr := sess.localDiscr
	peerDiscr := sess.peerDiscr
	pkt := &ControlPacket{
		Version:         1,
		State:           sess.state,
		DetectMult:      sess.detectMult,
		Length:          40,
		LocalDiscr:      sess.localDiscr,
		PeerDiscr:       sess.peerDiscr,
		DesiredMinTxUs:  uint32(sess.localTxMin / time.Microsecond),
		RequiredMinRxUs: uint32(sess.localRxMin / time.Microsecond),
	}
	if s.passiveMode {
		pkt.SetPassive()
	}
	pkt.ClientVersion = s.clientVersion
	bpkt := pkt.Marshal()
	peer := *sess.peer
	sess.mu.Unlock()
	src := net.IP(nil)
	if sess.route != nil {
		src = sess.route.Src
	}
	_, err := s.udp.WriteTo(bpkt, sess.peerAddr, sess.peer.Interface, src)
	if err != nil {
		select {
		case <-ctx.Done():
			return
		default:
		}

		s.metrics.WriteSocketErrors.WithLabelValues(peer.Interface, peer.LocalIP).Inc()

		// Log throttled warnings for transient errors (e.g., bad FD state).
		now := time.Now()
		s.writeErrWarnMu.Lock()
		if s.writeErrWarnLast.IsZero() || now.Sub(s.writeErrWarnLast) >= s.writeErrWarnEvery {
			s.writeErrWarnLast = now
			s.writeErrWarnMu.Unlock()
			s.log.Warn("liveness.scheduler: error writing UDP packet", "error", err, "peer", sess.peer.String())
		} else {
			s.writeErrWarnMu.Unlock()
		}
	} else {
		s.metrics.ControlPacketsTX.WithLabelValues(peer.Interface, peer.LocalIP).Inc()
		s.log.Debug("liveness.scheduler: sent control packet",
			"iface", peer.Interface,
			"localIP", peer.LocalIP,
			"peerIP", peer.PeerIP,
			"state", state.String(),
			"localDiscr", localDiscr,
			"peerDiscr", peerDiscr,
		)
	}
}

// scheduleTxNow enqueues a TX event due immediately, so a state change is
// advertised without waiting out the session's current transmit interval.
//
// It takes over sess.nextTxScheduled rather than adding alongside it. That marker
// is what Run clears before re-arming the periodic cadence, so leaving the old
// (possibly heavily backed-off) deadline in place would make Run's scheduleTx bail
// and starve the session: after a Down->Init->Up recovery we would advertise Up and
// then go silent for up to backoffMax, while the peer's detect timer is only
// detectMult x rxInterval — the peer times us out, withdraws the route, and the
// session flaps. Taking over the marker means popping this event re-arms the cadence
// at the new state's interval instead.
//
// The TX is paced by txInterval() measured from the last transmit, not sent
// unconditionally at now. Session.HandleRx treats a repeating peer-Down stream as a
// state change on every packet (Down->Init on one, Init->Down on the next, since the
// stale-Down suppression only covers prev==StateUp), so without a floor our transmit
// rate would track the peer's send rate 1:1 and bypass MinTxFloor/MaxTxCeil entirely.
// Clamping rather than dropping keeps the point of the fix: the state change still
// short-circuits a backed-off deadline, it just cannot outrun the configured floor.
//
// Claiming the marker also coalesces: if a TX is already pending no later than the
// time we would pick, that one carries the new state and there is nothing to queue.
//
// A displaced far-future event stays in the queue but is dropped as stale when it pops
// (Run compares its when against the marker), so a takeover costs no extra packet and
// leaves no backlog. Rewriting queued events instead would mean mutating the heap from
// a concurrent hot path.
func (s *Scheduler) scheduleTxNow(now time.Time, sess *Session) {
	sess.mu.Lock()
	if !sess.alive {
		sess.mu.Unlock()
		return
	}

	when := now
	if !sess.lastTx.IsZero() {
		if earliest := sess.lastTx.Add(sess.txInterval()); earliest.After(when) {
			when = earliest
		}
	}
	if !sess.nextTxScheduled.IsZero() && !sess.nextTxScheduled.After(when) {
		// A TX is already pending no later than this; it will carry the new state.
		sess.mu.Unlock()
		return
	}
	sess.nextTxScheduled = when
	sess.mu.Unlock()

	s.push(&event{when: when, eventType: eventTypeTX, session: sess})
}

// tryExpire checks whether the session’s detect deadline has passed.
// If so, it transitions the session to Down, triggers an immediate TX
// to advertise the Down state, and returns true to signal expiration.
func (s *Scheduler) tryExpire(sess *Session) bool {
	now := time.Now()
	if sess.ExpireIfDue(now) {
		sess.mu.Lock()
		peer := *sess.peer
		sess.mu.Unlock()
		s.log.Debug("liveness.scheduler: detect timeout -> Down",
			"peer", peer.String(),
		)
		s.scheduleTxNow(now, sess)
		return true
	}
	return false
}
