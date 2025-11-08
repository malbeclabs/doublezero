package liveness

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

const (
	defaultMinTxFloor = 50 * time.Millisecond
	defaultMaxTxCeil  = 1 * time.Second
)

type Peer struct {
	Interface string
	LocalIP   string
	RemoteIP  string
}

func (p *Peer) String() string {
	return fmt.Sprintf("interface: %s, localIP: %s, remoteIP: %s", p.Interface, p.LocalIP, p.RemoteIP)
}

type RouteKey struct {
	Interface string
	SrcIP     string
	Table     int
	DstPrefix string
	NextHop   string
}

type ManagerConfig struct {
	Logger    *slog.Logger
	Netlinker RouteReaderWriter

	BindIP string
	Port   int

	TxMin      time.Duration
	RxMin      time.Duration
	DetectMult uint8

	MinTxFloor time.Duration
	MaxTxCeil  time.Duration
	BackoffMax time.Duration
}

func (c *ManagerConfig) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Netlinker == nil {
		return errors.New("netlinker is required")
	}
	if c.BindIP == "" {
		return errors.New("bind IP is required")
	}
	if c.Port < 0 {
		return errors.New("port must be greater than or equal to 0")
	}
	if c.TxMin <= 0 {
		return errors.New("txMin must be greater than 0")
	}
	if c.RxMin <= 0 {
		return errors.New("rxMin must be greater than 0")
	}
	if c.DetectMult <= 0 {
		return errors.New("detectMult must be greater than 0")
	}
	if c.MinTxFloor == 0 {
		c.MinTxFloor = defaultMinTxFloor
	}
	if c.MinTxFloor < 0 {
		return errors.New("minTxFloor must be greater than 0")
	}
	if c.MaxTxCeil == 0 {
		c.MaxTxCeil = defaultMaxTxCeil
	}
	if c.MaxTxCeil < 0 {
		return errors.New("maxTxCeil must be greater than 0")
	}
	if c.MaxTxCeil < c.MinTxFloor {
		return errors.New("maxTxCeil must be greater than minTxFloor")
	}
	if c.BackoffMax == 0 {
		c.BackoffMax = c.MaxTxCeil
	}
	if c.BackoffMax < 0 {
		return errors.New("backoffMax must be greater than 0")
	}
	if c.BackoffMax < c.MinTxFloor {
		return errors.New("backoffMax must be greater than or equal to minTxFloor")
	}
	return nil
}

type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	log  *slog.Logger
	cfg  *ManagerConfig
	conn *UDPConn

	sched *Scheduler
	recv  *Receiver

	mu        sync.Mutex
	sessions  map[Peer]*Session           // tracked liveness sessions
	desired   map[RouteKey]*routing.Route // routes we want to install
	installed map[RouteKey]bool           // routes actually in kernel

	// rate-limited warnings for unknown-peer packets
	unkWarnEvery time.Duration
	unkWarnLast  time.Time
	unkWarnMu    sync.Mutex
}

func NewManager(ctx context.Context, cfg *ManagerConfig) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("error validating manager config: %v", err)
	}

	conn, err := ListenUDP(cfg.BindIP, cfg.Port)
	if err != nil {
		return nil, fmt.Errorf("error creating UDP connection: %v", err)
	}

	log := cfg.Logger
	log.Info("liveness: manager starting", "localAddr", conn.LocalAddr().String(), "txMin", cfg.TxMin, "rxMin", cfg.RxMin, "detectMult", cfg.DetectMult)

	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{
		ctx:    ctx,
		cancel: cancel,

		log:  log,
		cfg:  cfg,
		conn: conn,

		sessions:  make(map[Peer]*Session),
		desired:   make(map[RouteKey]*routing.Route),
		installed: make(map[RouteKey]bool),

		unkWarnEvery: 5 * time.Second,
	}

	m.recv = NewReceiver(m.log, m.conn, m.HandleRx)
	m.sched = NewScheduler(m.log, m.conn, m.onSessionDown)

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		err := m.recv.Run(m.ctx)
		if err != nil {
			// TODO(snormore): What should we do when this returns an error? Reconnect/retry or propagate up and exit the daemon?
			m.log.Error("liveness: error running receiver", "error", err)
		}
	}()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.sched.Run(m.ctx)
	}()

	return m, nil
}

func (m *Manager) RegisterRoute(r *routing.Route, iface string) error {
	peerAddr, err := net.ResolveUDPAddr("udp", peerAddrFor(r, m.cfg.Port))
	if err != nil {
		return fmt.Errorf("error resolving peer address: %v", err)
	}

	k := routeKeyFor(iface, r)
	m.mu.Lock()
	m.desired[k] = r
	m.mu.Unlock()

	peer := Peer{Interface: iface, LocalIP: r.Src.String(), RemoteIP: r.Dst.IP.String()}

	m.log.Info("liveness: registering route", "route", r.String(), "peerAddr", peerAddr)

	m.mu.Lock()
	if _, ok := m.sessions[peer]; ok {
		m.mu.Unlock()
		return nil
	}
	s := &Session{
		route: r,
		// Initial Phase: State = Down, random discriminator
		myDisc:        rand32(),
		state:         StateDown,
		detectMult:    m.cfg.DetectMult,
		localTxMin:    m.cfg.TxMin,
		localRxMin:    m.cfg.RxMin,
		peer:          &peer,
		peerAddr:      peerAddr,
		alive:         true,
		minTxFloor:    m.cfg.MinTxFloor,
		maxTxCeil:     m.cfg.MaxTxCeil,
		backoffMax:    m.cfg.BackoffMax,
		backoffFactor: 1,
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

	peer := Peer{Interface: iface, LocalIP: r.Src.String(), RemoteIP: r.Dst.IP.String()}

	m.mu.Lock()
	if s := m.sessions[peer]; s != nil {
		s.mu.Lock()
		s.alive = false
		s.mu.Unlock()
	}
	delete(m.sessions, peer)
	m.mu.Unlock()

	if wasInstalled {
		return m.cfg.Netlinker.RouteDelete(r)
	}
	return nil
}

func (m *Manager) AdminDownAll() {
	m.log.Info("liveness: admin down all")

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, s := range m.sessions {
		s.mu.Lock()
		prev := s.state
		s.state = StateAdminDown
		s.detectDeadline = time.Time{} // stop detect while AdminDown
		s.mu.Unlock()
		if prev != StateAdminDown {
			// Withdraw once per session when entering AdminDown.
			go m.onSessionDown(s)
		}
	}
}

func (m *Manager) LocalAddr() *net.UDPAddr {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conn == nil {
		return nil
	}
	if addr, ok := m.conn.LocalAddr().(*net.UDPAddr); ok {
		return addr
	}
	return nil
}

func (m *Manager) Close() error {
	m.cancel()
	m.wg.Wait()

	var cerr error
	m.mu.Lock()
	if m.conn != nil {
		if err := m.conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			m.log.Warn("liveness: error closing connection", "error", err)
			cerr = err
		}
		m.conn = nil
	}
	m.mu.Unlock()

	return cerr
}

func (m *Manager) HandleRx(ctrl *ControlPacket, peer Peer) {
	now := time.Now()

	m.mu.Lock()
	s := m.sessions[peer]
	if s == nil {
		// Throttled warning for unknown-peer packets.
		m.unkWarnMu.Lock()
		if m.unkWarnLast.IsZero() || time.Since(m.unkWarnLast) >= m.unkWarnEvery {
			m.unkWarnLast = time.Now()
			m.log.Warn("liveness: received control packet for unknown peer", "peer", peer.String())
		}
		m.unkWarnMu.Unlock()

		m.mu.Unlock()
		return
	}

	// Only react if the session's state actually changed.
	changed := s.HandleRx(now, ctrl)

	if changed {
		switch s.state {
		case StateUp:
			go m.onSessionUp(s)
			m.sched.scheduleDetect(now, s) // keep detect armed while Up
		case StateInit:
			m.sched.scheduleDetect(now, s) // arm detect; next >=Init promotes to Up
		case StateDown:
			// transitioned to Down – do NOT schedule detect again
			go m.onSessionDown(s)
		}
	} else {
		// No state change; only keep detect ticking for Init/Up.
		switch s.state {
		case StateUp, StateInit:
			m.sched.scheduleDetect(now, s)
		default:
			// already Down/AdminDown: do nothing; avoid repeated “down” logs
		}
	}
	m.mu.Unlock()
}

func (m *Manager) onSessionUp(s *Session) {
	rk := routeKeyFor(s.peer.Interface, s.route)
	m.mu.Lock()
	r := m.desired[rk]
	if r == nil || m.installed[rk] {
		m.mu.Unlock()
		return
	}
	m.installed[rk] = true
	m.mu.Unlock()
	_ = m.cfg.Netlinker.RouteAdd(r)
	m.log.Info("liveness: session up", "peer", s.peer.String(), "route", s.route.String())
}

func (m *Manager) onSessionDown(s *Session) {
	rk := routeKeyFor(s.peer.Interface, s.route)
	m.mu.Lock()
	r := m.desired[rk]
	was := m.installed[rk]
	m.installed[rk] = false
	m.mu.Unlock()
	if was && r != nil {
		_ = m.cfg.Netlinker.RouteDelete(r)
		m.log.Info("liveness: session down", "peer", s.peer.String(), "route", s.route.String())
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
	return RouteKey{Interface: iface, SrcIP: r.Src.String(), Table: r.Table, DstPrefix: r.Dst.String(), NextHop: r.NextHop.String()}
}

func peerAddrFor(r *routing.Route, port int) string {
	return fmt.Sprintf("%s:%d", r.Dst.IP.String(), port)
}
