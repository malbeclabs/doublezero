package liveness

import (
	"errors"
	"math/rand"
	"net"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
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

type Scheduler struct {
	m   *Manager
	pq  priorityQ
	seq uint64
}

func NewScheduler(m *Manager) *Scheduler {
	return &Scheduler{m: m}
}

func (s *Scheduler) Start() {
	go s.run()
}

func (s *Scheduler) run() {
	s.m.log.Info("liveness.scheduler: started")
	t := time.NewTimer(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-s.m.ctx.Done():
			s.m.log.Info("liveness.scheduler: stopped")
			return
		default:
		}
		s.m.mu.Lock()
		ev := s.pop()
		s.m.mu.Unlock()
		if ev == nil {
			t.Reset(10 * time.Millisecond)
			select {
			case <-s.m.ctx.Done():
				s.m.log.Info("liveness.scheduler: stopped")
				return
			case <-t.C:
				continue
			}
		}
		now := time.Now()
		if d := ev.when.Sub(now); d > 0 {
			t.Reset(d)
			select {
			case <-s.m.ctx.Done():
				s.m.log.Info("liveness.scheduler: stopped")
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
				s.m.log.Info("liveness.scheduler: session down", "peer", ev.s.peer, "route", ev.s.route.String())
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

func (s *Scheduler) push(e *event) { s.seq++; e.seq = s.seq; s.heapPush(e) }

func (s *Scheduler) pop() *event {
	if len(s.pq.a) == 0 {
		return nil
	}
	e := s.pq.a[0]
	n := len(s.pq.a) - 1
	if n > 0 {
		s.pq.a[0] = s.pq.a[n]
	}
	s.pq.a = s.pq.a[:n]
	for i := 0; ; {
		l := 2*i + 1
		r := l + 1
		m := i
		if l < n && s.pq.Less(l, m) {
			m = l
		}
		if r < n && s.pq.Less(r, m) {
			m = r
		}
		if m == i {
			break
		}
		s.pq.Swap(i, m)
		i = m
	}
	return e
}

func (s *Scheduler) heapPush(ev *event) {
	q := &s.pq
	q.a = append(q.a, ev)
	for i := len(q.a) - 1; i > 0; {
		p := (i - 1) / 2
		if !q.Less(i, p) {
			break
		}
		q.Swap(i, p)
		i = p
	}
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

func (s *Session) txInterval() time.Duration {
	iv := s.localTxMin
	if s.remoteRxMin > iv {
		iv = s.remoteRxMin
	}
	if iv < s.mgr.minTxFloor {
		iv = s.mgr.minTxFloor
	}
	if iv > s.mgr.maxTxCeil {
		iv = s.mgr.maxTxCeil
	}
	return iv
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
	// s.m.log.Info("liveness.scheduler: writing UDP packet", "peerAddr", sess.peerAddr, "iface", sess.peer.iface, "src", sess.route.Src.String())
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
		// proactively notify peer
		s.push(&event{when: time.Now(), typ: evTX, s: sess})
	}
	return expired
}

func writeUDP(conn net.PacketConn, pkt []byte, dst *net.UDPAddr, iface string, src net.IP) (int, error) {
	if dst == nil || dst.IP == nil {
		return 0, errors.New("nil dst")
	}
	var ifidx int
	if iface != "" {
		ifi, err := net.InterfaceByName(iface)
		if err != nil {
			return 0, err
		}
		ifidx = ifi.Index
	}

	if ip4 := dst.IP.To4(); ip4 != nil {
		pc := ipv4.NewPacketConn(conn)
		if err := pc.SetControlMessage(ipv4.FlagInterface|ipv4.FlagSrc, true); err != nil {
			return 0, err
		}
		var cm ipv4.ControlMessage
		if ifidx != 0 {
			cm.IfIndex = ifidx
		}
		if src != nil {
			cm.Src = src
		}
		return pc.WriteTo(pkt, &cm, &net.UDPAddr{IP: ip4, Port: dst.Port, Zone: dst.Zone})
	}

	pc := ipv6.NewPacketConn(conn)
	if err := pc.SetControlMessage(ipv6.FlagInterface|ipv6.FlagSrc, true); err != nil {
		return 0, err
	}
	var cm ipv6.ControlMessage
	if ifidx != 0 {
		cm.IfIndex = ifidx
	}
	if src != nil {
		cm.Src = src
	}
	return pc.WriteTo(pkt, &cm, dst)
}
