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
	"github.com/prometheus/client_golang/prometheus"
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
	Logger          *slog.Logger
	Netlinker       RouteReaderWriter
	UDP             UDPService
	MetricsRegistry *prometheus.Registry

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
	EnablePeerMetrics bool

	// Maximum number of events to keep in the scheduler queue.
	// This is an upper bound for safety to prevent unbounded
	// memory usage in the event of regressions.
	// suggested: 4 * expected number of sessions
	// default: 10,240
	MaxEvents int

	// When true, a peer that advertises passive mode via control packet
	// (PeerAdvertisedPassive == true) is treated as dataplane-passive:
	// liveness still tracks state and metrics, but will not install or
	// uninstall its routes in the kernel for that session.
	HonorPeerAdvertisedPassive bool

	// Client version to advertise to peers in control packets.
	ClientVersion string
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
	if c.ClientVersion == "" {
		return errors.New("clientVersion is required")
	}
	return nil
}

type Route struct {
	routing.Route

	// If true, the route will not be uninstalled from the kernel on route withdrawal
	NoUninstall bool
}

type Manager interface {
	RegisterRoute(r *Route, iface string, port int) error
	WithdrawRoute(r *Route, iface string) error
	LocalAddr() *net.UDPAddr
	GetSessions() []SessionSnapshot
	GetSession(peer Peer) (*Session, bool)
	IsInstalled(rk RouteKey) bool
	AdminDownRoute(r *Route, iface string)
	Close() error
	Err() <-chan error
}

// Manager orchestrates liveness sessions per peer, integrates with routing,
// and runs the receiver/scheduler goroutines. It is safe for concurrent use.
type manager struct {
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	errCh  chan error

	log     *slog.Logger
	cfg     *ManagerConfig
	udp     UDPService // shared UDP transport
	metrics *Metrics

	sched *Scheduler // time-wheel/event-loop for TX/detect
	recv  *Receiver  // UDP packet reader → HandleRx

	mu       sync.Mutex
	sessions map[Peer]*Session   // active sessions keyed by Peer
	desired  map[RouteKey]*Route // routes we want installed

	// installed tracks routes that this Manager believes it has installed
	// in the kernel via Netlinker.RouteAdd, regardless of PassiveMode.
	// Dataplane policy (PassiveMode and HonorPeerAdvertisedPassive) only
	// affects whether we uninstall them on session down.
	installed map[RouteKey]bool

	// Rate-limited warnings for packets from unknown peers (not in sessions).
	unknownPeerErrWarnEvery time.Duration
	unknownPeerErrWarnLast  time.Time
	unknownPeerErrWarnMu    sync.Mutex
}

// NewManager constructs a Manager, opens the UDP socket, and launches the
// receiver and scheduler loops. The context governs their lifetime.
func NewManager(ctx context.Context, cfg *ManagerConfig) (*manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("error validating manager config: %v", err)
	}

	clientVersion, err := ParseClientVersion(cfg.ClientVersion)
	if err != nil {
		return nil, fmt.Errorf("error parsing client version: %v", err)
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
	log.Info("liveness: manager starting",
		"localAddr", udp.LocalAddr().String(),
		"txMin", cfg.TxMin.String(),
		"rxMin", cfg.RxMin.String(),
		"detectMult", cfg.DetectMult,
		"passiveMode", cfg.PassiveMode,
		"peerMetrics", cfg.EnablePeerMetrics,
		"honorPeerAdvertisedPassive", cfg.HonorPeerAdvertisedPassive,
	)

	ctx, cancel := context.WithCancel(ctx)
	m := &manager{
		ctx:    ctx,
		cancel: cancel,
		errCh:  make(chan error, 10),

		log: log,
		cfg: cfg,
		udp: udp,

		sessions:  make(map[Peer]*Session),
		desired:   make(map[RouteKey]*Route),
		installed: make(map[RouteKey]bool),

		unknownPeerErrWarnEvery: 5 * time.Second,
	}

	m.metrics = newMetrics()
	if cfg.MetricsRegistry == nil {
		m.metrics.Register(prometheus.DefaultRegisterer)
	} else {
		m.metrics.Register(cfg.MetricsRegistry)
	}

	// Wire up IO loops.
	m.recv = NewReceiver(m.log, m.udp, m.HandleRx, m.metrics)
	m.sched = NewScheduler(m.log, m.udp, m.onSessionDown, m.cfg.MaxEvents, m.cfg.EnablePeerMetrics, m.metrics, m.cfg.PassiveMode, clientVersion)

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
func (m *manager) Err() <-chan error {
	return m.errCh
}

// RegisterRoute declares interest in monitoring reachability for route r via iface.
// It optionally installs the route immediately in PassiveMode, then creates or
// reuses a liveness Session and schedules immediate TX to begin handshake.
func (m *manager) RegisterRoute(r *Route, iface string, port int) error {
	var srcIP, dstIP string
	if r.Src != nil && r.Src.To4() != nil {
		srcIP = r.Src.To4().String()
	}
	if r.Dst.IP != nil && r.Dst.IP.To4() != nil {
		dstIP = r.Dst.IP.To4().String()
	}

	// Check that the route src and dst are valid IPv4 addresses.
	if srcIP == "" || dstIP == "" {
		return fmt.Errorf("error registering route: non-IPv4 source (%s) or destination IP (%s)", srcIP, dstIP)
	}

	rk := routeKeyFor(iface, r)
	if m.cfg.PassiveMode {
		// In PassiveMode we update the kernel immediately, while also running liveness
		// for observability. We still track installed[] so onSessionDown can log
		// accurately while leaving the route in place.
		if err := m.cfg.Netlinker.RouteAdd(&r.Route); err != nil {
			m.metrics.RouteInstallFailures.WithLabelValues(iface, srcIP).Inc()
			return fmt.Errorf("error registering route: %v", err)
		}
		m.metrics.routeInstall(iface, srcIP)

		m.mu.Lock()
		m.installed[rk] = true
		m.mu.Unlock()
	}

	peerAddr, err := net.ResolveUDPAddr("udp", peerAddrFor(r, port))
	if err != nil {
		return fmt.Errorf("error resolving peer address: %v", err)
	}
	if peerAddr == nil {
		return fmt.Errorf("error resolving peer address: nil address")
	}

	m.mu.Lock()
	m.desired[rk] = r
	m.mu.Unlock()

	peer := Peer{Interface: iface, LocalIP: srcIP, PeerIP: dstIP}
	m.log.Debug("liveness: registering route", "route", r.String(), "peerAddr", peerAddr.String())

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
	m.metrics.sessionStateTransition(peer, nil, StateDown, "register_route", m.cfg.EnablePeerMetrics)
	if m.cfg.EnablePeerMetrics {
		// Set initial detect time based on current view (localRxMin until peer timers arrive)
		sess.mu.Lock()
		dt := sess.detectTime()
		sess.mu.Unlock()
		m.metrics.peerDetectTime(peer, dt)
	}
	// Kick off the first TX immediately; detect is armed after we see valid RX.
	m.sched.scheduleTx(time.Now(), sess)
	m.mu.Unlock()

	return nil
}

// WithdrawRoute removes interest in r via iface. It tears down the session,
// marks it not managed (alive=false), and withdraws the route if needed.
func (m *manager) WithdrawRoute(r *Route, iface string) error {
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

	if m.cfg.PassiveMode && !r.NoUninstall {
		// Passive-mode: caller wants immediate kernel update independent of liveness.
		if err := m.cfg.Netlinker.RouteDelete(&r.Route); err != nil {
			m.metrics.RouteUninstallFailures.WithLabelValues(iface, srcIP).Inc()
			return fmt.Errorf("error withdrawing route: %v", err)
		}
		m.metrics.routeWithdraw(iface, srcIP)
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
		state := sess.state
		sess.alive = false
		sess.mu.Unlock()
		m.metrics.cleanupWithdrawRoute(peer, state, m.cfg.EnablePeerMetrics)
	}
	delete(m.sessions, peer)
	m.mu.Unlock()

	// If we previously installed the route (and not in PassiveMode), remove it now.
	if wasInstalled && !m.cfg.PassiveMode && !r.NoUninstall {
		err := m.cfg.Netlinker.RouteDelete(&r.Route)
		if err != nil {
			m.metrics.RouteUninstallFailures.WithLabelValues(iface, srcIP).Inc()
			return err
		}
		m.metrics.routeWithdraw(iface, srcIP)
	}
	return nil
}

// LocalAddr exposes the bound UDP address if available (or nil if closed/unset).
func (m *manager) LocalAddr() *net.UDPAddr {
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
func (m *manager) GetSessions() []SessionSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	snapshots := make([]SessionSnapshot, 0, len(m.sessions))
	for _, sess := range m.sessions {
		snap := sess.Snapshot()

		rk := routeKeyFor(snap.Peer.Interface, &snap.Route)
		_, desired := m.desired[rk]
		installed := m.installed[rk]

		switch {
		case !desired:
			snap.ExpectedKernelState = KernelStateUnknown
		case installed:
			snap.ExpectedKernelState = KernelStatePresent
		default:
			snap.ExpectedKernelState = KernelStateAbsent
		}

		snapshots = append(snapshots, snap)
	}
	return snapshots
}

// GetSession returns the session for the given peer.
func (m *manager) GetSession(peer Peer) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sess, ok := m.sessions[peer]
	return sess, ok
}

// HasSession returns true if the manager has a session for the given peer.
func (m *manager) HasSession(peer Peer) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.sessions[peer]
	return ok
}

// IsInstalled reports whether this Manager believes it installed the route in
// the kernel for this key. This is only meaningful when the Manager is actively
// managing dataplane (PassiveMode == false and the session is not treated as
// peer-passive). In PassiveMode or peer-passive cases this may return false
// even if the route exists in the kernel.
func (m *manager) IsInstalled(rk RouteKey) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.installed[rk]
}

// GetSessionsLen returns the number of sessions in the manager.
func (m *manager) GetSessionsLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// GetInstalledRoutesLen returns the number of routes in the manager.
func (m *manager) GetInstalledLen() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.installed)
}

// Close stops goroutines, waits for exit, and closes the UDP socket.
// Returns the last close error, if any.
func (m *manager) Close() error {
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
func (m *manager) HandleRx(ctrl *ControlPacket, peer Peer) {
	now := time.Now()

	m.mu.Lock()
	sess := m.sessions[peer]
	if sess == nil {
		m.metrics.UnknownPeerPackets.WithLabelValues(peer.Interface, peer.LocalIP).Inc()

		// Throttle warnings for packets from unknown peers to avoid log spam.
		m.unknownPeerErrWarnMu.Lock()
		if m.unknownPeerErrWarnLast.IsZero() || time.Since(m.unknownPeerErrWarnLast) >= m.unknownPeerErrWarnEvery {
			m.unknownPeerErrWarnLast = time.Now()
			m.log.Info("liveness: received control packet for unknown peer", "peer", peer.String(), "peerDiscr", ctrl.PeerDiscr, "localDiscr", ctrl.LocalDiscr, "state", ctrl.State)

		}
		m.unknownPeerErrWarnMu.Unlock()

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

		m.metrics.sessionStateTransition(peer, &prevSnap.State, newSnap.State, "handle_rx", m.cfg.EnablePeerMetrics)

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
		// No state change: just keep detect ticking for active states.
		switch sess.GetState() {
		case StateUp, StateInit:
			m.sched.scheduleDetect(now, sess)
		default:
			// Down/AdminDown: do nothing.
		}
	}

	if m.cfg.EnablePeerMetrics {
		// detect time = detectMult × rxInterval() with current clamped timers
		sess.mu.Lock()
		dt := sess.detectTime()
		sess.mu.Unlock()
		m.metrics.peerDetectTime(peer, dt)
	}

	m.mu.Unlock()
}

// AdminDownRoute transitions a session to AdminDown and withdraws the route.
func (m *manager) AdminDownRoute(r *Route, iface string) {
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
		// Withdraw route as an admin-driven down (respecting PassiveMode /
		// effective-passive for whether we touch the kernel).
		m.onSessionDown(sess)

		// Regardless of dataplane policy, an admin-down should clear our
		// internal "installed" bookkeeping so IsInstalled() returns false.
		rk := routeKeyFor(iface, r)
		m.mu.Lock()
		m.installed[rk] = false
		m.mu.Unlock()

		// Ensure we send at least one AdminDown packet promptly.
		m.sched.scheduleTx(now, sess)

		// Update metrics for the state transition.
		m.metrics.sessionStateTransition(peer, &prev, StateAdminDown, "admin_down_route", m.cfg.EnablePeerMetrics)
	}
}

// onSessionUp installs the route if it is desired and not already installed.
// In PassiveMode, install was already done at registration time and we do
// not touch installed[] here.
//
// Peer-advertised passive does NOT prevent installation: it only affects
// the Down path (onSessionDown), where we may decide to leave the route
// in place and skip RouteDelete.
func (m *manager) onSessionUp(sess *Session) {
	snap := sess.Snapshot()
	peer := snap.Peer
	rk := routeKeyFor(peer.Interface, &snap.Route)

	m.mu.Lock()
	route := m.desired[rk]
	alreadyInstalled := m.installed[rk]

	var logSuffix string

	// If route is missing from desired, already installed, or we’re in
	// global PassiveMode, do not touch dataplane or installed[] here.
	if route == nil || alreadyInstalled || m.cfg.PassiveMode {
		m.mu.Unlock()

		switch {
		case m.cfg.PassiveMode:
			logSuffix = " (global passive; no-op)"
		case alreadyInstalled:
			logSuffix = " (already installed; no-op)"
		case route == nil:
			logSuffix = " (not desired; no-op)"
		}
	} else {
		m.installed[rk] = true
		m.mu.Unlock()

		if err := m.cfg.Netlinker.RouteAdd(&route.Route); err != nil {
			m.log.Error("liveness: error adding route on session up",
				"error", err, "route", route.String())
			m.metrics.RouteInstallFailures.WithLabelValues(peer.Interface, peer.LocalIP).Inc()
		} else {
			m.metrics.routeInstall(peer.Interface, peer.LocalIP)
		}
	}

	// Convergence-to-up metrics
	now := time.Now()
	var convergence time.Duration
	if !snap.ConvUpStart.IsZero() && now.After(snap.ConvUpStart) {
		convergence = now.Sub(snap.ConvUpStart)
		m.metrics.convergenceToUp(peer, convergence)
	}
	sess.mu.Lock()
	sess.convUpStart = time.Time{}
	sess.mu.Unlock()

	m.log.Info("liveness: session up"+logSuffix,
		"peer", peer.String(),
		"route", snap.Route.String(),
		"convergence", convergence.String(),
		"upSince", snap.UpSince.UTC().String(),
		"peerAdvertisedMode", snap.PeerAdvertisedMode.String(),
		"peerClientVersion", snap.PeerClientVersion.String(),
	)
}

// onSessionDown withdraws the route if currently installed (unless PassiveMode
// or the session is effectively passive due to peer advertising passive mode).
func (m *manager) onSessionDown(sess *Session) {
	snap := sess.Snapshot()
	peer := snap.Peer
	rk := routeKeyFor(peer.Interface, &snap.Route)

	effectivelyPassive := m.isPeerEffectivelyPassive(snap)

	m.mu.Lock()
	route := m.desired[rk]
	wasInstalled := m.installed[rk]

	if m.cfg.PassiveMode || effectivelyPassive {
		m.mu.Unlock()
	} else {
		if wasInstalled {
			m.installed[rk] = false
		}
		m.mu.Unlock()
	}

	// At this point we had a desired, installed route: record convergence-to-down
	// regardless of dataplane policy (PassiveMode / peer-passive).
	now := time.Now()
	var convergence time.Duration
	if !snap.ConvDownStart.IsZero() && now.After(snap.ConvDownStart) {
		convergence = now.Sub(snap.ConvDownStart)
		m.metrics.convergenceToDown(peer, convergence)
	}
	sess.mu.Lock()
	sess.convDownStart = time.Time{}
	sess.mu.Unlock()

	if !wasInstalled || route == nil {
		m.log.Debug("liveness: session down (no-op: not installed or not desired)",
			"peer", peer.String(),
			"routePresent", route != nil,
			"downSince", snap.DownSince.UTC().String(),
			"downReason", snap.LastDownReason.String(),
			"peerClientVersion", snap.PeerClientVersion.String(),
		)
		return
	}

	if m.cfg.PassiveMode {
		m.log.Info("liveness: session down (global passive; keeping route)",
			"peer", peer.String(),
			"route", snap.Route.String(),
			"downSince", snap.DownSince.UTC().String(),
			"downReason", snap.LastDownReason.String(),
			"peerClientVersion", snap.PeerClientVersion.String(),
		)
		return
	}

	if effectivelyPassive {
		m.log.Info("liveness: session down (peer passive; keeping route)",
			"peer", peer.String(),
			"route", snap.Route.String(),
			"downSince", snap.DownSince.UTC().String(),
			"downReason", snap.LastDownReason.String(),
			"peerClientVersion", snap.PeerClientVersion.String(),
		)
		return
	}

	if !route.NoUninstall {
		if err := m.cfg.Netlinker.RouteDelete(&route.Route); err != nil {
			m.log.Error("liveness: error deleting route on session down",
				"error", err, "route", route.String())
			m.metrics.RouteUninstallFailures.WithLabelValues(peer.Interface, peer.LocalIP).Inc()
		} else {
			m.metrics.routeWithdraw(peer.Interface, peer.LocalIP)
		}
	}

	m.log.Info("liveness: session down",
		"peer", peer.String(),
		"route", snap.Route.String(),
		"convergence", convergence.String(),
		"downSince", snap.DownSince.UTC().String(),
		"downReason", snap.LastDownReason.String(),
		"peerClientVersion", snap.PeerClientVersion.String(),
	)
}

// isPeerEffectivelyPassive returns true when this session should not have its
// dataplane (kernel route) managed due to peer-advertised passive mode.
//
// Global PassiveMode is handled separately: it disables dataplane changes for
// all sessions, but we still track installed[] only for non-passive sessions.
func (m *manager) isPeerEffectivelyPassive(snap SessionSnapshot) bool {
	return m.cfg.HonorPeerAdvertisedPassive && snap.PeerAdvertisedMode == PeerModePassive
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
func routeKeyFor(iface string, r *Route) RouteKey {
	var srcIP, dstIP string
	if r.Src != nil && r.Src.To4() != nil {
		srcIP = r.Src.To4().String()
	}
	if r.Dst.IP != nil && r.Dst.IP.To4() != nil {
		dstIP = r.Dst.IP.To4().String()
	}
	var nextHopIP string
	if r.NextHop != nil && r.NextHop.To4() != nil {
		nextHopIP = r.NextHop.To4().String()
	}
	return RouteKey{Interface: iface, SrcIP: srcIP, Table: r.Table, DstPrefix: dstIP, NextHop: nextHopIP}
}

// peerAddrFor returns "<dst-ip>:<port>" for UDP control messages to a peer.
func peerAddrFor(r *Route, port int) string {
	return fmt.Sprintf("%s:%d", r.Dst.IP.To4().String(), port)
}
