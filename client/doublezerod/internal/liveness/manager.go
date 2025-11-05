package liveness

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type Peer struct {
	iface   string
	theirIP string
	ourIP   string
}

func NewPeer(iface string, theirIP net.IP, ourIP net.IP) Peer {
	if theirIP == nil {
		theirIP = net.IPv4zero
	}
	if ourIP == nil {
		ourIP = net.IPv4zero
	}
	return Peer{iface: iface, theirIP: theirIP.String(), ourIP: ourIP.String()}
}

func (p *Peer) String() string {
	return fmt.Sprintf("iface: %s, theirIP: %s, ourIP: %s", p.iface, p.theirIP, p.ourIP)
}

type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	log  *slog.Logger
	nlr  RouteReaderWriter
	conn *net.UDPConn
	port int

	minTxFloor time.Duration
	maxTxCeil  time.Duration

	// TODO(snormore): Do we need a sepaarate lock for sessions vs desired/installed routes?
	// mu sync.Mutex
	sessions map[Peer]*Session

	// composed workers
	sched *Scheduler
	recv  *Receiver

	mu        sync.Mutex
	desired   map[RouteKey]*routing.Route // routes we want
	installed map[RouteKey]bool           // routes actually in kernel
}

func NewManager(ctx context.Context, log *slog.Logger, nlr RouteReaderWriter, bindIP string, port int) (*Manager, error) {
	laddr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", bindIP, port))
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return nil, err
	}

	log.Info("liveness: manager listening on", "address", conn.LocalAddr().String())

	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{
		ctx:    ctx,
		cancel: cancel,
		log:    log,
		nlr:    nlr,
		conn:   conn,
		port:   port,

		// TODO(snormore): Make these configurable
		minTxFloor: 50 * time.Millisecond,
		maxTxCeil:  1 * time.Second,

		sessions:  make(map[Peer]*Session),
		desired:   make(map[RouteKey]*routing.Route),
		installed: make(map[RouteKey]bool),
	}

	// Wire scheduler and receiver
	m.sched = NewScheduler(m.log, m.conn, m.onSessionDown)
	m.recv = NewReceiver(m.log, m.conn, m.HandleRx)

	// Start workers
	m.wg.Add(2)
	go func() {
		defer m.wg.Done()
		m.sched.Run(m.ctx)
	}()
	go func() {
		defer m.wg.Done()
		m.recv.Run(m.ctx)
	}()

	return m, nil
}

func (m *Manager) Close() error {
	m.cancel()
	err := m.conn.Close()
	if err != nil {
		m.log.Warn("liveness: error closing connection", "error", err)
	}
	m.wg.Wait()
	return err
}

func (m *Manager) RegisterRoute(r *routing.Route, peerAddr *net.UDPAddr, iface string, txMin, rxMin time.Duration, detectMult uint8) error {
	k := routeKeyFor(iface, r)
	m.mu.Lock()
	m.desired[k] = r
	m.mu.Unlock()

	peer := NewPeer(iface, r.Dst.IP, r.Src)

	m.log.Info("liveness: registering route", "route", r.String(), "peerAddr", peerAddr, "txMin", txMin, "rxMin", rxMin, "detectMult", detectMult)

	m.mu.Lock()
	if _, ok := m.sessions[peer]; ok {
		m.mu.Unlock()
		return nil
	}
	s := &Session{
		route: r,
		// Initial Phase: State = Down, random discriminator
		myDisc:     rand32(),
		state:      Down,
		detectMult: detectMult,
		localTxMin: txMin,
		localRxMin: rxMin,
		peer:       &peer,
		peerAddr:   peerAddr,
		mgr:        m,
		alive:      true,
	}
	m.sessions[peer] = s
	// schedule TX immediately; DO NOT schedule detect yet (no continuity to monitor)
	m.sched.scheduleTx(time.Now(), s)
	m.mu.Unlock()

	return nil
}

func (m *Manager) WithdrawRoute(r *routing.Route, iface string) error {
	m.log.Info("liveness: withdrawing route", "route", r.String(), "iface", iface)

	k := routeKeyFor(iface, r)
	m.mu.Lock()
	delete(m.desired, k)
	wasInstalled := m.installed[k]
	delete(m.installed, k)
	m.mu.Unlock()

	peer := NewPeer(iface, r.Dst.IP, r.Src)

	m.mu.Lock()
	if s := m.sessions[peer]; s != nil {
		s.mu.Lock()
		s.alive = false
		s.mu.Unlock()
	}
	delete(m.sessions, peer)
	m.mu.Unlock()

	if wasInstalled {
		return m.nlr.RouteDelete(r)
	}
	return nil
}

func (m *Manager) AdminDownAll() {
	m.log.Info("liveness: admin down all")

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		s.mu.Lock()
		s.state = AdminDown
		s.mu.Unlock()
	}
}

// func (m *Manager) PollAll() {
// 	m.log.Info("liveness: polling all")

// 	m.mu.Lock()
// 	defer m.mu.Unlock()
// 	now := time.Now()
// 	for _, s := range m.sessions {
// 		m.sched.scheduleTx(now, s)
// 	}
// }

func (m *Manager) LocalAddr() *net.UDPAddr {
	if m.conn == nil {
		return nil
	}
	if addr, ok := m.conn.LocalAddr().(*net.UDPAddr); ok {
		return addr
	}
	return nil
}

func (m *Manager) HandleRx(ctrl *ControlPacket, pktSrc *net.UDPAddr, pktDst net.IP, pktIfname string) {
	now := time.Now()

	peer := NewPeer(pktIfname, pktSrc.IP, pktDst)

	m.mu.Lock()
	s := m.sessions[peer]
	if s == nil {
		m.log.Info("liveness: received control packet for unknown peer", "peer", peer.String())
		m.mu.Unlock()
		return
	}

	// Only react if the session's state actually changed.
	changed := s.onRx(now, ctrl)

	if changed {
		switch s.state {
		case Up:
			// transitioned to Up
			m.log.Info("liveness: session up", "peer", peer.String(), "route", s.route.String())
			go m.onSessionUp(s)
			m.sched.scheduleDetect(now, s) // keep detect armed while Up
		case Init:
			// transitioned to Init – arm detect; next >=Init promotes to Up
			m.sched.scheduleDetect(now, s)
		case Down:
			// transitioned to Down – do NOT schedule detect again
			// (onRx already cleared detectDeadline when mirroring Down)
			m.log.Info("liveness: session down (rx)", "peer", peer.String(), "route", s.route.String())
			go m.onSessionDown(s)
		}
	} else {
		// No state change; only keep detect ticking for Init/Up.
		switch s.state {
		case Up, Init:
			m.sched.scheduleDetect(now, s)
		default:
			// already Down/AdminDown: do nothing; avoid repeated “down” logs
		}
	}
	m.mu.Unlock()
}

func (m *Manager) onSessionUp(s *Session) {
	rk := routeKeyFor(s.peer.iface, s.route)
	m.mu.Lock()
	r := m.desired[rk]
	if r == nil || m.installed[rk] {
		m.mu.Unlock()
		return
	}
	m.installed[rk] = true
	m.mu.Unlock()
	_ = m.nlr.RouteAdd(r)
}

func (m *Manager) onSessionDown(s *Session) {
	rk := routeKeyFor(s.peer.iface, s.route)
	m.mu.Lock()
	r := m.desired[rk]
	was := m.installed[rk]
	m.installed[rk] = false
	m.mu.Unlock()
	if was && r != nil {
		_ = m.nlr.RouteDelete(r)
	}
}

func rand32() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	v := binary.BigEndian.Uint32(b[:])
	if v == 0 {
		v = 1
	}
	return v
}

func routeKeyFor(iface string, r *routing.Route) RouteKey {
	return RouteKey{Iface: iface, SrcIP: r.Src.String(), Table: r.Table, DstPrefix: r.Dst.String(), NextHop: r.NextHop.String()}
}

func peerAddrFor(r *routing.Route, port int) string {
	return fmt.Sprintf("%s:%d", r.Dst.IP.String(), port)
}
