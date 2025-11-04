package liveness

import (
	"net"
	"time"

	"golang.org/x/net/ipv4"
)

type Receiver struct {
	m     *Manager
	sched *Scheduler
}

func NewReceiver(m *Manager, sched *Scheduler) *Receiver {
	return &Receiver{m: m, sched: sched}
}

func (r *Receiver) Start() {
	go r.run()
}

func (r *Receiver) run() {
	r.m.log.Info("liveness.recv: rx loop started")
	buf := make([]byte, 1500)
	for {
		r.m.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, pktSrc, pktDst, pktIfname, err := readFromUDP(r.m.conn, buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-r.m.ctx.Done():
					r.m.log.Info("liveness.recv: rx loop stopped by context done", "reason", r.m.ctx.Err())
					return
				default:
					continue
				}
			}
			select {
			case <-r.m.ctx.Done():
				r.m.log.Info("liveness.recv: rx loop stopped by context done", "reason", r.m.ctx.Err())
				return
			default:
				continue
			}
		}

		ctrl, err := ParseCtrl(buf[:n])
		if err != nil {
			r.m.log.Error("liveness.recv: error parsing control packet", "error", err)
			continue
		}
		now := time.Now()

		peer := NewPeer(pktIfname, pktSrc.IP, pktDst)

		r.m.mu.Lock()
		s := r.m.sessions[peer]
		if s == nil {
			r.m.log.Info("liveness.recv: control packet for unknown peer", "peer", peer)
			r.m.mu.Unlock()
			continue
		}

		// Only react if the session's state actually changed.
		changed := s.onRx(now, ctrl)

		if changed {
			switch s.state {
			case Up:
				// transitioned to Up
				r.m.log.Info("liveness.recv: session up", "peer", peer, "route", s.route.String())
				go r.m.onUp(s)
				r.sched.scheduleDetect(now, s) // keep detect armed while Up
			case Init:
				// transitioned to Init – arm detect; next >=Init promotes to Up
				r.sched.scheduleDetect(now, s)
			case Down:
				// transitioned to Down – do NOT schedule detect again
				// (onRx already cleared detectDeadline when mirroring Down)
				r.m.log.Info("liveness.recv: session down (rx)", "peer", peer, "route", s.route.String())
				go r.m.onDown(s)
			}
		} else {
			// No state change; only keep detect ticking for Init/Up.
			switch s.state {
			case Up, Init:
				r.sched.scheduleDetect(now, s)
			default:
				// already Down/AdminDown: do nothing; avoid repeated “down” logs
			}
		}
		r.m.mu.Unlock()
	}
}

func (s *Session) rxRef() time.Duration {
	ref := s.remoteTxMin
	if s.localRxMin > ref {
		ref = s.localRxMin
	}
	if ref == 0 {
		ref = s.localRxMin
	}
	if ref < s.mgr.minTxFloor {
		ref = s.mgr.minTxFloor
	}
	return ref
}

func (s *Session) detectTime() time.Duration {
	return time.Duration(int64(s.detectMult) * int64(s.rxRef()))
}

func (s *Session) onRx(now time.Time, ctrl *Ctrl) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ignore if peer explicitly targets a different session.
	if ctrl.YourDiscr != 0 && ctrl.YourDiscr != s.myDisc {
		return false
	}

	prev := s.state

	// Learn/refresh peer discriminator.
	if s.yourDisc == 0 && ctrl.MyDiscr != 0 {
		s.yourDisc = ctrl.MyDiscr
	}

	// Peer timers + (re)arm detect on any valid RX.
	s.remoteTxMin = time.Duration(ctrl.DesiredMinTxUs) * time.Microsecond
	s.remoteRxMin = time.Duration(ctrl.RequiredMinRxUs) * time.Microsecond
	s.lastRx = now
	s.detectDeadline = now.Add(s.detectTime())

	switch prev {
	case Down:
		// Bring-up: as soon as we can identify the peer, move to Init.
		// If the peer already reports >= Init, go straight to Up.
		if s.yourDisc != 0 {
			if ctrl.State >= Init {
				// If peer is reporting Init or Up, promote our session to Up
				// Confirmation Phase: State = Up
				s.state = Up
			} else {
				// If peer is reporting Down, promote our session to Init
				// Learning Phase: State = Init
				s.state = Init
			}
		}

	case Init:
		// Do NOT mirror Down while initializing; let detect expiry handle failure.
		// Promote to Up once the peer reports >= Init.
		if s.yourDisc != 0 && ctrl.State >= Init {
			// If peer is reporting Init or Up, promote our session to Up
			// Confirmation Phase: State = Up
			s.state = Up
		}

	case Up:
		// Established and peer declares Down -> mirror once and stop detect.
		if ctrl.State == Down {
			// If peer is reporting Down, degrade our session to Down
			// De-activation Phase: State = Down
			s.state = Down
			s.detectDeadline = time.Time{} // stop detect while Down
		}
	}

	return s.state != prev
}

func readFromUDP(conn net.PacketConn, buf []byte) (n int, src *net.UDPAddr, dst net.IP, ifname string, err error) {
	p := ipv4.NewPacketConn(conn)
	if err = p.SetControlMessage(ipv4.FlagInterface|ipv4.FlagDst, true); err != nil {
		return
	}
	var cm *ipv4.ControlMessage
	var raddr net.Addr
	n, cm, raddr, err = p.ReadFrom(buf)
	if err != nil {
		return
	}
	if ua, ok := raddr.(*net.UDPAddr); ok {
		src = ua
	}
	if cm != nil && cm.Dst != nil {
		dst = cm.Dst
	}
	if cm != nil && cm.IfIndex != 0 {
		ifi, _ := net.InterfaceByIndex(cm.IfIndex)
		if ifi != nil {
			ifname = ifi.Name
		}
	}
	return
}
