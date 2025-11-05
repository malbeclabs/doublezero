package liveness

import (
	"context"
	"net"
	"time"
)

type Receiver struct {
	m     *Manager
	sched *Scheduler
}

func NewReceiver(m *Manager, sched *Scheduler) *Receiver {
	return &Receiver{m: m, sched: sched}
}

func (r *Receiver) Run(ctx context.Context) {
	r.m.log.Info("liveness.recv: rx loop started")
	buf := make([]byte, 1500)
	for {
		r.m.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, pktSrc, pktDst, pktIfname, err := readFromUDP(r.m.conn, buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				select {
				case <-ctx.Done():
					r.m.log.Info("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
					return
				default:
					continue
				}
			}
			select {
			case <-ctx.Done():
				r.m.log.Info("liveness.recv: rx loop stopped by context done", "reason", ctx.Err())
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
			r.m.log.Info("liveness.recv: control packet for unknown peer", "peer", peer.String())
			r.m.mu.Unlock()
			continue
		}

		// Only react if the session's state actually changed.
		changed := s.onRx(now, ctrl)

		if changed {
			switch s.state {
			case Up:
				// transitioned to Up
				r.m.log.Info("liveness.recv: session up", "peer", peer.String(), "route", s.route.String())
				go r.m.onSessionUp(s)
				r.sched.scheduleDetect(now, s) // keep detect armed while Up
			case Init:
				// transitioned to Init – arm detect; next >=Init promotes to Up
				r.sched.scheduleDetect(now, s)
			case Down:
				// transitioned to Down – do NOT schedule detect again
				// (onRx already cleared detectDeadline when mirroring Down)
				r.m.log.Info("liveness.recv: session down (rx)", "peer", peer.String(), "route", s.route.String())
				go r.m.onSessionDown(s)
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
