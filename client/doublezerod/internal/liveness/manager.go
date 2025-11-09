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
	// Default floors/ceilings for TX interval clamping; chosen to avoid
	// overly chatty probes and to keep failure detection reasonably fast.
	defaultMinTxFloor = 50 * time.Millisecond
	defaultMaxTxCeil  = 1 * time.Second
)

// Peer identifies a remote endpoint and the local interface context used to reach it.
// LocalIP is the IP on which we send/receive; RemoteIP is the peer’s address.
type Peer struct {
	Interface string
	LocalIP   string
	RemoteIP  string
}

func (p *Peer) String() string {
	return fmt.Sprintf("interface: %s, localIP: %s, remoteIP: %s", p.Interface, p.LocalIP, p.RemoteIP)
}

// RouteKey uniquely identifies a desired/installed route in the kernel.
// This is used as a stable key in Manager maps across lifecycle events.
type RouteKey struct {
	Interface string
	SrcIP     string
	Table     int
	DstPrefix string
	NextHop   string
}

// ManagerConfig controls Manager behavior, routing integration, and liveness timings.
type ManagerConfig struct {
	Logger    *slog.Logger
	Netlinker RouteReaderWriter

	BindIP string // local bind address for the UDP socket (IPv4)
	Port   int    // UDP port to listen/transmit on

	// PassiveMode: if true, Manager does NOT manage kernel routes automatically.
	// Instead it defers to Netlinker calls made by the caller. This enables
	// incremental rollout (observe liveness without changing dataplane).
	PassiveMode bool

	// Local desired probe intervals and detection multiplier for new sessions.
	TxMin      time.Duration
	RxMin      time.Duration
	DetectMult uint8

	// Global bounds for interval clamping and exponential backoff.
	MinTxFloor time.Duration
	MaxTxCeil  time.Duration
	BackoffMax time.Duration
}

// Validate fills defaults and enforces constraints for ManagerConfig.
// Returns a descriptive error when required fields are missing/invalid.
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

// Manager orchestrates liveness sessions per peer, integrates with routing,
// and runs the receiver/scheduler goroutines. It is safe for concurrent use.
type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	log *slog.Logger
	cfg *ManagerConfig
	udp *UDPService // shared UDP transport

	sched *Scheduler // time-wheel/event-loop for TX/detect
	recv  *Receiver  // UDP packet reader → HandleRx

	mu        sync.Mutex
	sessions  map[Peer]*Session           // active sessions keyed by Peer
	desired   map[RouteKey]*routing.Route // routes we want installed
	installed map[RouteKey]bool           // whether route is in kernel

	// Rate-limited warnings for packets from unknown peers (not in sessions).
	unkownPeerErrWarnEvery time.Duration
	unkownPeerErrWarnLast  time.Time
	unkownPeerErrWarnMu    sync.Mutex
}

// NewManager constructs a Manager, opens the UDP socket, and launches the
// receiver and scheduler loops. The context governs their lifetime.
func NewManager(ctx context.Context, cfg *ManagerConfig) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("error validating manager config: %v", err)
	}

	udp, err := ListenUDP(cfg.BindIP, cfg.Port)
	if err != nil {
		return nil, fmt.Errorf("error creating UDP connection: %v", err)
	}

	log := cfg.Logger
	log.Info("liveness: manager starting", "localAddr", udp.LocalAddr().String(), "txMin", cfg.TxMin, "rxMin", cfg.RxMin, "detectMult", cfg.DetectMult)

	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{
		ctx:    ctx,
		cancel: cancel,

		log: log,
		cfg: cfg,
		udp: udp,

		sessions:  make(map[Peer]*Session),
		desired:   make(map[RouteKey]*routing.Route),
		installed: make(map[RouteKey]bool),

		unkownPeerErrWarnEvery: 5 * time.Second,
	}

	// Wire up IO loops.
	m.recv = NewReceiver(m.log, m.udp, m.HandleRx)
	m.sched = NewScheduler(m.log, m.udp, m.onSessionDown)

	// Receiver goroutine: parses control packets and dispatches to HandleRx.
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		err := m.recv.Run(m.ctx)
		if err != nil {
			// TODO(snormore): What should we do when this returns an error? Reconnect/retry or
			// propagate up and exit the daemon?
			m.log.Error("liveness: error running receiver", "error", err)
		}
	}()

	// Scheduler goroutine: handles periodic TX and detect expirations.
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.sched.Run(m.ctx)
	}()

	return m, nil
}

// RegisterRoute declares interest in monitoring reachability for route r via iface.
// It optionally installs the route immediately in PassiveMode, then creates or
// reuses a liveness Session and schedules immediate TX to begin handshake.
func (m *Manager) RegisterRoute(r *routing.Route, iface string) error {
	if m.cfg.PassiveMode {
		// In passive-mode we still update the kernel immediately (caller’s policy),
		// while also running liveness for observability.
		if err := m.cfg.Netlinker.RouteAdd(r); err != nil {
			return fmt.Errorf("error registering route: %v", err)
		}
	}

	// Skip routes with nil source or destination IP.
	if r.Src == nil || r.Dst.IP == nil {
		return fmt.Errorf("error registering route: nil source or destination IP")
	}

	// Skip routes that are not IPv4.
	if r.Src.To4() == nil || r.Dst.IP.To4() == nil {
		return fmt.Errorf("error registering route: non-IPv4 source (%s) or destination IP (%s)", r.Src.String(), r.Dst.IP.String())
	}

	peerAddr, err := net.ResolveUDPAddr("udp", peerAddrFor(r, m.cfg.Port))
	if err != nil {
		return fmt.Errorf("error resolving peer address: %v", err)
	}

	k := routeKeyFor(iface, r)
	m.mu.Lock()
	m.desired[k] = r
	m.mu.Unlock()

	peer := Peer{Interface: iface, LocalIP: r.Src.To4().String(), RemoteIP: r.Dst.IP.To4().String()}
	m.log.Info("liveness: registering route", "route", r.String(), "peerAddr", peerAddr)

	m.mu.Lock()
	if _, ok := m.sessions[peer]; ok {
		m.mu.Unlock()
		return nil // session already exists
	}
	// Create a fresh session in Down with a random non-zero discriminator.
	s := &Session{
		route:         r,
		myDisc:        rand32(),
		state:         StateDown,        // Initial Phase: start Down until handshake
		detectMult:    m.cfg.DetectMult, // governs detect timeout = mult × rxInterval
		localTxMin:    m.cfg.TxMin,
		localRxMin:    m.cfg.RxMin,
		peer:          &peer,
		peerAddr:      peerAddr,
		alive:         true,             // session is under management (TX/detect active)
		minTxFloor:    m.cfg.MinTxFloor, // clamp lower bound
		maxTxCeil:     m.cfg.MaxTxCeil,  // clamp upper bound
		backoffMax:    m.cfg.BackoffMax, // cap for exponential backoff while Down
		backoffFactor: 1,
	}
	m.sessions[peer] = s
	// Kick off the first TX immediately; detect is armed after we see valid RX.
	m.sched.scheduleTx(time.Now(), s)
	m.mu.Unlock()

	return nil
}

// WithdrawRoute removes interest in r via iface. It tears down the session,
// marks it not managed (alive=false), and withdraws the route if needed.
func (m *Manager) WithdrawRoute(r *routing.Route, iface string) error {
	m.log.Info("liveness: withdrawing route", "route", r.String(), "iface", iface)

	if m.cfg.PassiveMode {
		// Passive-mode: caller wants immediate kernel update independent of liveness.
		if err := m.cfg.Netlinker.RouteDelete(r); err != nil {
			return fmt.Errorf("error withdrawing route: %v", err)
		}
	}

	// Skip routes with nil source or destination IP.
	if r.Src == nil || r.Dst.IP == nil {
		return fmt.Errorf("error withdrawing route: nil source or destination IP")
	}

	// Skip routes that are not IPv4.
	if r.Src.To4() == nil || r.Dst.IP.To4() == nil {
		return fmt.Errorf("error withdrawing route: non-IPv4 source (%s) or destination IP (%s)", r.Src.String(), r.Dst.IP.String())
	}

	k := routeKeyFor(iface, r)
	m.mu.Lock()
	delete(m.desired, k)
	wasInstalled := m.installed[k]
	delete(m.installed, k)
	m.mu.Unlock()

	peer := Peer{Interface: iface, LocalIP: r.Src.To4().String(), RemoteIP: r.Dst.IP.To4().String()}

	// Mark session no longer managed and drop it from tracking.
	m.mu.Lock()
	if s := m.sessions[peer]; s != nil {
		s.mu.Lock()
		s.alive = false
		s.mu.Unlock()
	}
	delete(m.sessions, peer)
	m.mu.Unlock()

	// If we previously installed the route (and not in PassiveMode), remove it now.
	if wasInstalled && !m.cfg.PassiveMode {
		return m.cfg.Netlinker.RouteDelete(r)
	}
	return nil
}

// AdminDownAll forces all sessions to AdminDown (operator action).
// It halts detection per session and triggers a one-time withdraw.
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

// LocalAddr exposes the bound UDP address if available (or nil if closed/unset).
func (m *Manager) LocalAddr() *net.UDPAddr {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.udp == nil {
		return nil
	}
	if addr, ok := m.udp.LocalAddr().(*net.UDPAddr); ok {
		return addr
	}
	return nil
}

// Close stops goroutines, waits for exit, and closes the UDP socket.
// Returns the last close error, if any.
func (m *Manager) Close() error {
	m.cancel()
	m.wg.Wait()

	var cerr error
	m.mu.Lock()
	if m.udp != nil {
		if err := m.udp.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			m.log.Warn("liveness: error closing connection", "error", err)
			cerr = err
		}
		m.udp = nil
	}
	m.mu.Unlock()

	return cerr
}

// HandleRx is the receiver callback: it routes an inbound control packet to the
// correct Session, drives its state machine, and schedules detect as needed.
func (m *Manager) HandleRx(ctrl *ControlPacket, peer Peer) {
	now := time.Now()

	m.mu.Lock()
	s := m.sessions[peer]
	if s == nil {
		// Throttle warnings for packets from unknown peers to avoid log spam.
		m.unkownPeerErrWarnMu.Lock()
		if m.unkownPeerErrWarnLast.IsZero() || time.Since(m.unkownPeerErrWarnLast) >= m.unkownPeerErrWarnEvery {
			m.unkownPeerErrWarnLast = time.Now()
			m.log.Warn("liveness: received control packet for unknown peer", "peer", peer.String(), "yourDiscr", ctrl.YourDiscr, "myDiscr", ctrl.MyDiscr, "state", ctrl.State)

		}
		m.unkownPeerErrWarnMu.Unlock()

		m.mu.Unlock()
		return
	}

	// Apply RX to the session FSM; only act when state actually changes.
	changed := s.HandleRx(now, ctrl)

	if changed {
		switch s.state {
		case StateUp:
			go m.onSessionUp(s)
			m.sched.scheduleDetect(now, s) // keep detect armed while Up
		case StateInit:
			m.sched.scheduleDetect(now, s) // arm detect; next >=Init promotes to Up
		case StateDown:
			// Transitioned to Down; withdraw and do NOT re-arm detect.
			go m.onSessionDown(s)
		}
	} else {
		// No state change: just keep detect ticking for active states.
		switch s.state {
		case StateUp, StateInit:
			m.sched.scheduleDetect(now, s)
		default:
			// Down/AdminDown: do nothing; avoid noisy logs.
		}
	}
	m.mu.Unlock()
}

// onSessionUp installs the route if it is desired and not already installed.
// In PassiveMode, install was already done at registration time.
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
	if !m.cfg.PassiveMode {
		_ = m.cfg.Netlinker.RouteAdd(r)
	}
	m.log.Info("liveness: session up", "peer", s.peer.String(), "route", s.route.String())
}

// onSessionDown withdraws the route if currently installed (unless PassiveMode).
func (m *Manager) onSessionDown(s *Session) {
	rk := routeKeyFor(s.peer.Interface, s.route)
	m.mu.Lock()
	r := m.desired[rk]
	was := m.installed[rk]
	m.installed[rk] = false
	m.mu.Unlock()
	if was && r != nil {
		if !m.cfg.PassiveMode {
			_ = m.cfg.Netlinker.RouteDelete(r)
		}
		m.log.Info("liveness: session down", "peer", s.peer.String(), "route", s.route.String())
	}
}

// rand32 returns a non-zero random uint32 for use as a discriminator.
// (BFD treats 0 as invalid; ensure we never emit 0.)
func rand32() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	v := binary.BigEndian.Uint32(b[:])
	if v == 0 {
		v = 1
	}
	return v
}

// routeKeyFor builds a RouteKey for map indexing based on interface + route fields.
func routeKeyFor(iface string, r *routing.Route) RouteKey {
	return RouteKey{Interface: iface, SrcIP: r.Src.To4().String(), Table: r.Table, DstPrefix: r.Dst.IP.To4().String(), NextHop: r.NextHop.To4().String()}
}

// peerAddrFor returns "<dst-ip>:<port>" for UDP control messages to a peer.
func peerAddrFor(r *routing.Route, port int) string {
	return fmt.Sprintf("%s:%d", r.Dst.IP.To4().String(), port)
}
