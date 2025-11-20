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

	defaultMaxEvents = 10240
)

// Peer identifies a remote endpoint and the local interface context used to reach it.
// LocalIP is the IP on which we send/receive; PeerIP is the peer’s address.
type Peer struct {
	Interface string
	LocalIP   string
	PeerIP    string
}

func (p *Peer) String() string {
	return fmt.Sprintf("interface: %s, localIP: %s, peerIP: %s", p.Interface, p.LocalIP, p.PeerIP)
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
	UDP       *UDPService

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

	// Enable per peer metrics for route liveness (high cardinality).
	PeerMetrics bool

	// Maximum number of events to keep in the scheduler queue.
	// This is an upper bound for safety to prevent unbounded
	// memory usage in the event of regressions.
	// suggested: 4 * expected number of sessions
	// default: 10,240
	MaxEvents int
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
	if c.MaxEvents == 0 {
		c.MaxEvents = defaultMaxEvents
	}
	if c.MaxEvents < 0 {
		return errors.New("maxEvents must be greater than 0")
	}
	return nil
}

// Manager orchestrates liveness sessions per peer, integrates with routing,
// and runs the receiver/scheduler goroutines. It is safe for concurrent use.
type Manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	errCh  chan error

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

	udp := cfg.UDP
	if udp == nil {
		var err error
		udp, err = ListenUDP(cfg.BindIP, cfg.Port)
		if err != nil {
			return nil, fmt.Errorf("error creating UDP connection: %w", err)
		}
	}

	log := cfg.Logger
	log.Info("liveness: manager starting", "localAddr", udp.LocalAddr().String(), "txMin", cfg.TxMin.String(), "rxMin", cfg.RxMin.String(), "detectMult", cfg.DetectMult, "passiveMode", cfg.PassiveMode, "peerMetrics", cfg.PeerMetrics)

	ctx, cancel := context.WithCancel(ctx)
	m := &Manager{
		ctx:    ctx,
		cancel: cancel,
		errCh:  make(chan error, 10),

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
	m.sched = NewScheduler(m.log, m.udp, m.onSessionDown, m.cfg.MaxEvents, m.cfg.PeerMetrics)

	// Receiver goroutine: parses control packets and dispatches to HandleRx.
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		err := m.recv.Run(m.ctx)
		if err != nil {
			m.log.Error("liveness: error running receiver", "error", err)
			m.errCh <- err
			cancel()
		}
	}()

	// Scheduler goroutine: handles periodic TX and detect expirations.
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		err := m.sched.Run(m.ctx)
		if err != nil {
			m.log.Error("liveness: error running scheduler", "error", err)
			m.errCh <- err
			cancel()
		}
	}()

	return m, nil
}

// Err returns a channel that will receive any errors from the manager.
func (m *Manager) Err() <-chan error {
	return m.errCh
}

// RegisterRoute declares interest in monitoring reachability for route r via iface.
// It optionally installs the route immediately in PassiveMode, then creates or
// reuses a liveness Session and schedules immediate TX to begin handshake.
func (m *Manager) RegisterRoute(r *routing.Route, iface string) error {
	// Check that the route src and dst are valid IPv4 addresses.
	if r.Src == nil || r.Dst.IP == nil {
		return fmt.Errorf("error registering route: nil source or destination IP")
	}
	if r.Src.To4() == nil || r.Dst.IP.To4() == nil {
		return fmt.Errorf("error registering route: non-IPv4 source (%s) or destination IP (%s)", r.Src.String(), r.Dst.IP.String())
	}
	srcIP := r.Src.To4().String()
	dstIP := r.Dst.IP.To4().String()

	if m.cfg.PassiveMode {
		// In passive-mode we still update the kernel immediately (caller’s policy),
		// while also running liveness for observability.
		if err := m.cfg.Netlinker.RouteAdd(r); err != nil {
			metricRouteInstallFailures.WithLabelValues(iface, srcIP).Inc()
			return fmt.Errorf("error registering route: %v", err)
		}
		emitRouteInstallMetrics(iface, srcIP)
	}

	peerAddr, err := net.ResolveUDPAddr("udp", peerAddrFor(r, m.cfg.Port))
	if err != nil {
		return fmt.Errorf("error resolving peer address: %v", err)
	}
	if peerAddr == nil {
		return fmt.Errorf("error resolving peer address: nil address")
	}

	k := routeKeyFor(iface, r)
	m.mu.Lock()
	m.desired[k] = r
	m.mu.Unlock()

	peer := Peer{Interface: iface, LocalIP: srcIP, PeerIP: dstIP}
	m.log.Info("liveness: registering route", "route", r.String(), "peerAddr", peerAddr.String())

	m.mu.Lock()
	if _, ok := m.sessions[peer]; ok {
		m.mu.Unlock()
		return nil // session already exists
	}
	// Create a fresh session in Down with a random non-zero discriminator.
	sess := &Session{
		route:         r,
		localDiscr:    rand32(),
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
		lastUpdated:   time.Now(),
	}
	m.sessions[peer] = sess
	emitSessionStateMetrics(sess, nil, "register_route", m.cfg.PeerMetrics)
	if m.cfg.PeerMetrics {
		// Set initial detect time based on current view (localRxMin until peer timers arrive)
		sess.mu.Lock()
		dt := sess.detectTime()
		sess.mu.Unlock()
		emitPeerDetectTimeGauge(sess, dt)
	}
	// Kick off the first TX immediately; detect is armed after we see valid RX.
	m.sched.scheduleTx(time.Now(), sess)
	m.mu.Unlock()

	return nil
}

// WithdrawRoute removes interest in r via iface. It tears down the session,
// marks it not managed (alive=false), and withdraws the route if needed.
func (m *Manager) WithdrawRoute(r *routing.Route, iface string) error {
	// Check that the route src and dst are valid IPv4 addresses.
	if r.Src == nil || r.Dst.IP == nil {
		return fmt.Errorf("error withdrawing route: nil source or destination IP")
	}
	if r.Src.To4() == nil || r.Dst.IP.To4() == nil {
		return fmt.Errorf("error withdrawing route: non-IPv4 source (%s) or destination IP (%s)", r.Src.String(), r.Dst.IP.String())
	}
	srcIP := r.Src.To4().String()
	dstIP := r.Dst.IP.To4().String()

	m.log.Info("liveness: withdrawing route", "route", r.String(), "iface", iface)

	if m.cfg.PassiveMode {
		// Passive-mode: caller wants immediate kernel update independent of liveness.
		if err := m.cfg.Netlinker.RouteDelete(r); err != nil {
			metricRouteUninstallFailures.WithLabelValues(iface, srcIP).Inc()
			return fmt.Errorf("error withdrawing route: %v", err)
		}
		emitRouteWithdrawMetrics(iface, srcIP)
	}

	rk := routeKeyFor(iface, r)
	m.mu.Lock()
	delete(m.desired, rk)
	wasInstalled := m.installed[rk]
	delete(m.installed, rk)
	m.mu.Unlock()

	peer := Peer{Interface: iface, LocalIP: srcIP, PeerIP: dstIP}

	// Mark session no longer managed and drop it from tracking.
	m.mu.Lock()
	if sess := m.sessions[peer]; sess != nil {
		sess.mu.Lock()
		sess.alive = false
		sess.mu.Unlock()
		metricsCleanupOnWithdrawRoute(sess, m.cfg.PeerMetrics)
	}
	delete(m.sessions, peer)
	m.mu.Unlock()

	// If we previously installed the route (and not in PassiveMode), remove it now.
	if wasInstalled && !m.cfg.PassiveMode {
		err := m.cfg.Netlinker.RouteDelete(r)
		if err != nil {
			metricRouteUninstallFailures.WithLabelValues(iface, srcIP).Inc()
			return err
		}
		emitRouteWithdrawMetrics(iface, srcIP)
	}
	return nil
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

// GetSessions returns the snapshots of all sessions in the manager.
func (m *Manager) GetSessions() []SessionSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshots := make([]SessionSnapshot, 0, len(m.sessions))
	for _, sess := range m.sessions {
		snapshots = append(snapshots, sess.Snapshot())
	}
	return snapshots
}

// GetSession returns the session for the given peer.
func (m *Manager) GetSession(peer Peer) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[peer]
	return sess, ok
}

// HasSession returns true if the manager has a session for the given peer.
func (m *Manager) HasSession(peer Peer) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[peer]
	return ok
}

// GetInstalled returns the installed route for the given peer.
func (m *Manager) IsInstalled(rk RouteKey) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.installed[rk]
}

// GetSessionsLen returns the number of sessions in the manager.
func (m *Manager) GetSessionsLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// GetInstalledRoutesLen returns the number of routes in the manager.
func (m *Manager) GetInstalledLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.installed)
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

// HandleRx dispatches an inbound control packet to the correct Session and
// applies manager-level effects triggered by any state transition.
//
// Responsibilities:
//   - Look up the Session for the peer (ignoring packets from unknown peers).
//   - Call sess.HandleRx to apply the RX-driven FSM update.
//   - If the session’s state changed:
//     |- emit state metrics
//     |- call onSessionUp/onSessionDown as appropriate
//     |- (re-)schedule detect for Up/Init; do not schedule detect for Down/AdminDown
//   - If the state did not change:
//     |- keep detect armed for Init/Up
//     |- no action for Down/AdminDown
//
// All transition rules live in Session.HandleRx. Manager.HandleRx only
// performs the side-effects and scheduling associated with the resulting state.
func (m *Manager) HandleRx(ctrl *ControlPacket, peer Peer) {
	now := time.Now()

	m.mu.Lock()
	sess := m.sessions[peer]
	if sess == nil {
		metricUnknownPeerPackets.WithLabelValues(peer.Interface, peer.LocalIP).Inc()

		// Throttle warnings for packets from unknown peers to avoid log spam.
		m.unkownPeerErrWarnMu.Lock()
		if m.unkownPeerErrWarnLast.IsZero() || time.Since(m.unkownPeerErrWarnLast) >= m.unkownPeerErrWarnEvery {
			m.unkownPeerErrWarnLast = time.Now()
			m.log.Warn("liveness: received control packet for unknown peer", "peer", peer.String(), "peerDiscr", ctrl.PeerDiscr, "localDiscr", ctrl.LocalDiscr, "state", ctrl.State)

		}
		m.unkownPeerErrWarnMu.Unlock()

		m.mu.Unlock()
		return
	}

	prevSnap := sess.Snapshot()

	m.log.Debug("liveness: HandleRx begin",
		"peer", peer.String(),
		"prevState", prevSnap.State.String(),
		"ctrlState", ctrl.State.String(),
		"sessLocalDiscr", prevSnap.LocalDiscr,
		"sessPeerDiscr", prevSnap.PeerDiscr,
		"ctrlLocalDiscr", ctrl.LocalDiscr,
		"ctrlPeerDiscr", ctrl.PeerDiscr,
		"convUpStart", prevSnap.ConvUpStart.String(),
		"convDownStart", prevSnap.ConvDownStart.String(),
		"upSince", prevSnap.UpSince.UTC().String(),
		"downSince", prevSnap.DownSince.UTC().String(),
		"lastDownReason", prevSnap.LastDownReason.String(),
	)

	changed := sess.HandleRx(now, ctrl)

	// Apply RX to the session FSM; only act when state actually changes.
	if changed {
		newSnap := sess.Snapshot()
		m.log.Debug("liveness: HandleRx state changed",
			"peer", peer.String(),
			"from", prevSnap.State.String(),
			"to", newSnap.State.String(),
			"convUpStart", newSnap.ConvUpStart.String(),
			"convDownStart", newSnap.ConvDownStart.String(),
			"upSince", newSnap.UpSince.UTC().String(),
			"downSince", newSnap.DownSince.UTC().String(),
			"lastDownReason", newSnap.LastDownReason.String(),
		)

		emitSessionStateMetrics(sess, &prevSnap.State, "handle_rx", m.cfg.PeerMetrics)

		switch sess.GetState() {
		case StateUp:
			go m.onSessionUp(sess)
			m.sched.scheduleDetect(now, sess) // keep detect armed while Up
		case StateInit:
			m.sched.scheduleDetect(now, sess) // arm detect; next >=Init promotes to Up
		case StateDown:
			// Transitioned to Down; withdraw and do NOT re-arm detect.
			go m.onSessionDown(sess)
		case StateAdminDown:
			// Transitioned to Down; withdraw and do NOT re-arm detect.
			go m.onSessionDown(sess)
		}
	} else {
		m.log.Debug("liveness: HandleRx no state change",
			"peer", peer.String(),
			"state", sess.state.String(),
			"convUpStart", prevSnap.ConvUpStart.String(),
			"convDownStart", prevSnap.ConvDownStart.String(),
			"upSince", prevSnap.UpSince.UTC().String(),
			"downSince", prevSnap.DownSince.UTC().String(),
			"lastDownReason", prevSnap.LastDownReason.String(),
		)

		// No state change: just keep detect ticking for active states.
		switch sess.GetState() {
		case StateUp, StateInit:
			m.sched.scheduleDetect(now, sess)
		default:
			// Down/AdminDown: do nothing.
		}
	}

	if m.cfg.PeerMetrics {
		// detect time = detectMult × rxInterval() with current clamped timers
		sess.mu.Lock()
		dt := sess.detectTime()
		sess.mu.Unlock()
		emitPeerDetectTimeGauge(sess, dt)
	}

	m.mu.Unlock()
}

// AdminDownRoute transitions a session to AdminDown and withdraws the route.
func (m *Manager) AdminDownRoute(r *routing.Route, iface string) {
	peer := Peer{
		Interface: iface,
		LocalIP:   r.Src.To4().String(),
		PeerIP:    r.Dst.IP.To4().String(),
	}

	m.mu.Lock()
	sess := m.sessions[peer]
	m.mu.Unlock()
	if sess == nil {
		return
	}

	now := time.Now()
	sess.mu.Lock()
	prev := sess.state
	if prev != StateAdminDown {
		if (prev == StateUp || prev == StateInit) && sess.convDownStart.IsZero() {
			sess.convDownStart = now
		}
		sess.state = StateAdminDown
		sess.downSince = now
		sess.lastDownReason = DownReasonLocalAdmin
		sess.upSince = time.Time{}
		sess.detectDeadline = time.Time{}
		sess.nextDetectScheduled = time.Time{}
		sess.lastUpdated = now
	}
	sess.mu.Unlock()

	if prev != StateAdminDown {
		// Withdraw route as an admin-driven down.
		go m.onSessionDown(sess)
		// Ensure we send at least one AdminDown packet promptly.
		m.sched.scheduleTx(now, sess)
	}
}

// onSessionUp installs the route if it is desired and not already installed.
// In PassiveMode, install was already done at registration time.
func (m *Manager) onSessionUp(sess *Session) {
	rk := routeKeyFor(sess.peer.Interface, sess.route)
	m.mu.Lock()
	route := m.desired[rk]
	if route == nil || m.installed[rk] {
		m.mu.Unlock()
		return
	}
	m.installed[rk] = true
	m.mu.Unlock()
	if !m.cfg.PassiveMode {
		err := m.cfg.Netlinker.RouteAdd(route)
		if err != nil {
			m.log.Error("liveness: error adding route on session up", "error", err, "route", route.String())
			metricRouteInstallFailures.WithLabelValues(sess.peer.Interface, sess.peer.LocalIP).Inc()
		} else {
			emitRouteInstallMetrics(sess.peer.Interface, sess.peer.LocalIP)
		}
	}

	// Convergence-to-up duration: from first valid RX while Down to post-install.
	now := time.Now()
	var start time.Time
	sess.mu.Lock()
	start = sess.convUpStart
	sess.convUpStart = time.Time{}
	peer := *sess.peer
	upSince := sess.upSince
	sess.mu.Unlock()
	var convergence time.Duration
	if !start.IsZero() && now.After(start) {
		convergence = now.Sub(start)
		emitConvergenceToUpMetrics(sess, convergence)
	}

	m.log.Info("liveness: session up", "peer", peer.String(), "route", route.String(), "convergence", convergence.String(), "upSince", upSince.UTC().String())
}

// onSessionDown withdraws the route if currently installed (unless PassiveMode).
func (m *Manager) onSessionDown(sess *Session) {
	rk := routeKeyFor(sess.peer.Interface, sess.route)

	m.mu.Lock()
	route := m.desired[rk]
	wasInstalled := m.installed[rk]
	m.installed[rk] = false
	m.mu.Unlock()

	// If we never installed it in this epoch or it's no longer desired, then log debug and return.
	if !wasInstalled || route == nil {
		m.log.Debug("liveness: session down (no-op: not installed or not desired)",
			"peer", sess.peer.String(),
			"routePresent", route != nil,
			"downSince", sess.downSince.UTC().String(),
			"downReason", sess.lastDownReason.String())
		return
	}

	// If we're not in PassiveMode, delete the route.
	if !m.cfg.PassiveMode {
		err := m.cfg.Netlinker.RouteDelete(route)
		if err != nil {
			m.log.Error("liveness: error deleting route on session down", "error", err, "route", route.String())
			metricRouteUninstallFailures.WithLabelValues(sess.peer.Interface, sess.peer.LocalIP).Inc()
		} else {
			emitRouteWithdrawMetrics(sess.peer.Interface, sess.peer.LocalIP)
		}
	}

	// Convergence-to-down duration: from first failed/missing while Up to post-delete.
	now := time.Now()
	var start time.Time
	sess.mu.Lock()
	start = sess.convDownStart
	sess.convDownStart = time.Time{}
	sess.mu.Unlock()
	var convergence time.Duration
	if !start.IsZero() && now.After(start) {
		convergence = now.Sub(start)
		emitConvergenceToDownMetrics(sess, convergence)
	}
	snap := sess.Snapshot()

	m.log.Info("liveness: session down",
		"peer", snap.Peer.String(),
		"route", snap.Route.String(),
		"convergence", convergence.String(),
		"downSince", snap.DownSince.UTC().String(),
		"downReason", snap.LastDownReason.String())
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
