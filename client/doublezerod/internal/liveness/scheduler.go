package liveness

import (
	"container/heap"
	"context"
	"math/rand"
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
	m   *Manager
	pq  eventHeap
	seq uint64
	mu  sync.Mutex
}

func NewScheduler(m *Manager) *Scheduler {
	s := &Scheduler{m: m}
	heap.Init(&s.pq)
	return s
}

func (s *Scheduler) Run(ctx context.Context) {
	s.m.log.Info("liveness.scheduler: tx loop started")
	t := time.NewTimer(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			s.m.log.Info("liveness.scheduler: stopped by context done", "reason", ctx.Err())
			return
		default:
		}
		s.m.mu.Lock()
		ev := s.pop()
		s.m.mu.Unlock()
		if ev == nil {
			t.Reset(10 * time.Millisecond)
			select {
			case <-ctx.Done():
				s.m.log.Info("liveness.scheduler: stopped by context done", "reason", ctx.Err())
				return
			case <-t.C:
				continue
			}
		}
		now := time.Now()
		if d := ev.when.Sub(now); d > 0 {
			t.Reset(d)
			select {
			case <-ctx.Done():
				s.m.log.Info("liveness.scheduler: stopped by context done", "reason", ctx.Err())
				return
			case <-t.C:
			}
		}
		switch ev.typ {
		case evTX:
			s.doTX(ev.s)
			s.scheduleTx(time.Now(), ev.s)
		case evDetect:
			if s.tryExpire(ev.s) {
				s.m.log.Info("liveness.scheduler: session down", "peer", ev.s.peer.String(), "route", ev.s.route.String())
				go s.m.onDown(ev.s)
				break
			}
			ev.s.mu.Lock()
			st := ev.s.state
			ev.s.mu.Unlock()
			if st == Up || st == Init {
				s.scheduleDetect(time.Now(), ev.s)
			}
		}
	}
}

func (s *Scheduler) push(e *event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	e.seq = s.seq
	heap.Push(&s.pq, e)
}

func (s *Scheduler) pop() *event {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pq.Len() == 0 {
		return nil
	}
	return heap.Pop(&s.pq).(*event)
}

func (s *Scheduler) scheduleTx(now time.Time, sess *Session) {
	sess.mu.Lock()
	iv := sess.txInterval()
	j := iv / 10
	jit := time.Duration(rand.Intn(int(2*j+1))) - j
	next := now.Add(iv + jit)
	sess.nextTx = next
	sess.mu.Unlock()
	s.push(&event{when: next, typ: evTX, s: sess})
}

func (s *Scheduler) scheduleDetect(now time.Time, sess *Session) {
	sess.mu.Lock()
	if sess.detectDeadline.IsZero() {
		sess.mu.Unlock()
		return
	}
	ddl := sess.detectDeadline
	if !ddl.After(now) {
		ddl = now.Add(sess.detectTime())
		sess.detectDeadline = ddl
	}
	sess.mu.Unlock()
	s.push(&event{when: ddl, typ: evDetect, s: sess})
}

func (s *Scheduler) doTX(sess *Session) {
	sess.mu.Lock()
	pkt := (&Ctrl{
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
	_, err := writeUDP(s.m.conn, pkt, sess.peerAddr, sess.peer.iface, net.ParseIP(sess.route.Src.String()))
	if err != nil {
		// TODO(snormore): Should we just ignore this and/or debug log instead?
		s.m.log.Warn("liveness.scheduler: error writing UDP packet", "error", err)
	}
}

func (s *Scheduler) tryExpire(sess *Session) bool {
	now := time.Now()
	sess.mu.Lock()
	expired := (sess.state == Up || sess.state == Init) &&
		!sess.detectDeadline.IsZero() &&
		!now.Before(sess.detectDeadline)
	if expired {
		sess.state = Down
		sess.detectDeadline = time.Time{}
	}
	sess.mu.Unlock()
	if expired {
		s.push(&event{when: time.Now(), typ: evTX, s: sess})
	}
	return expired
}
