package liveness

import (
	"container/heap"
	"context"
	"log/slog"
	"net"
	"sync"
	"time"
)

type evType uint8

const (
	evTX     evType = 1
	evDetect evType = 2
)

type event struct {
	when time.Time
	typ  evType
	s    *Session
	seq  uint64
}

type EventQueue struct {
	mu  sync.Mutex
	pq  eventHeap
	seq uint64
}

func NewEventQueue() *EventQueue {
	h := eventHeap{}
	heap.Init(&h)
	return &EventQueue{pq: h}
}

func (q *EventQueue) Push(e *event) {
	q.mu.Lock()
	q.seq++
	e.seq = q.seq
	heap.Push(&q.pq, e)
	q.mu.Unlock()
}

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

type eventHeap []*event

func (h eventHeap) Len() int {
	return len(h)
}

func (h eventHeap) Less(i, j int) bool {
	if h[i].when.Equal(h[j].when) {
		return h[i].seq < h[j].seq
	}
	return h[i].when.Before(h[j].when)
}
func (h eventHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *eventHeap) Push(x any) {
	*h = append(*h, x.(*event))
}

func (h *eventHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

type Scheduler struct {
	log           *slog.Logger
	conn          *UDPConn
	onSessionDown func(s *Session)
	eq            *EventQueue
}

func NewScheduler(log *slog.Logger, conn *UDPConn, onSessionDown func(s *Session)) *Scheduler {
	eq := NewEventQueue()
	return &Scheduler{
		log:           log,
		conn:          conn,
		onSessionDown: onSessionDown,
		eq:            eq,
	}
}

func (s *Scheduler) Run(ctx context.Context) {
	s.log.Debug("liveness.scheduler: tx loop started")

	t := time.NewTimer(time.Hour)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			s.log.Debug("liveness.scheduler: stopped by context done", "reason", ctx.Err())
			return
		default:
		}

		now := time.Now()
		ev, wait := s.eq.PopIfDue(now)
		if ev == nil {
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
				return
			case <-t.C:
				continue
			}
		}

		switch ev.typ {
		case evTX:
			s.doTX(ev.s)
			s.scheduleTx(time.Now(), ev.s)
		case evDetect:
			if s.tryExpire(ev.s) {
				go s.onSessionDown(ev.s)
				continue
			}
			ev.s.mu.Lock()
			st := ev.s.state
			ev.s.mu.Unlock()
			if st == StateUp || st == StateInit {
				s.scheduleDetect(time.Now(), ev.s)
			}
		}
	}
}

func (s *Scheduler) scheduleTx(now time.Time, sess *Session) {
	sess.mu.Lock()
	isAdminDown := !sess.alive || sess.state == StateAdminDown
	sess.mu.Unlock()
	if isAdminDown {
		return
	}
	// Adaptive backoff while Down is applied inside ComputeNextTx by multiplying
	// the base interval by an exponential backoffFactor and capping at backoffMax.
	// AdminDown still suppresses TX entirely.
	next := sess.ComputeNextTx(now, nil)
	s.eq.Push(&event{when: next, typ: evTX, s: sess})
}

func (s *Scheduler) scheduleDetect(now time.Time, sess *Session) {
	ddl, ok := sess.ArmDetect(now)
	if !ok {
		return
	}
	s.eq.Push(&event{when: ddl, typ: evDetect, s: sess})
}

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
		MyDiscr:         sess.myDisc,
		YourDiscr:       sess.yourDisc,
		DesiredMinTxUs:  uint32(sess.localTxMin / time.Microsecond),
		RequiredMinRxUs: uint32(sess.localRxMin / time.Microsecond),
	}).Marshal()
	sess.mu.Unlock()
	_, err := s.conn.WriteTo(pkt, sess.peerAddr, sess.peer.Interface, net.ParseIP(sess.route.Src.String()))
	if err != nil {
		s.log.Debug("liveness.scheduler: error writing UDP packet", "error", err)
	}
}

func (s *Scheduler) tryExpire(sess *Session) bool {
	now := time.Now()
	if sess.ExpireIfDue(now) {
		// kick an immediate TX to advertise Down once
		s.eq.Push(&event{when: now, typ: evTX, s: sess})
		return true
	}
	return false
}
