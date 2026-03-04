package liveness

import (
	"flag"
	"math"
	"math/rand"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

var (
	clientsFlag    = flag.Int("clients", 10, "number of clients")
	txMinFlag      = flag.Duration("tx-min", 300*time.Millisecond, "tx min")
	rxMinFlag      = flag.Duration("rx-min", 300*time.Millisecond, "rx min")
	detectMultFlag = flag.Uint("detect-mult", 3, "detect mult")
	backoffMaxFlag = flag.Duration("backoff-max", 1*time.Second, "backoff max")

	stableDurationFlag  = flag.Duration("stable-duration", 2*time.Second, "stable duration")
	bgpFlapCyclesFlag   = flag.Uint("bgp-flap-cycles", 3, "number of BGP flap cycles")
	softLossPctFlag     = flag.Float64("soft-loss-pct", 0.01, "soft loss percentage")
	softLossCyclesFlag  = flag.Int("soft-loss-cycles", 3, "number of soft-loss windows to run")
	highSoftLossPctFlag = flag.Float64("soft-loss-high-pct", 0.8, "high soft loss percentage that should eventually cause timeout")

	// Fractions of clients to involve in each scenario (0 = default behavior).
	flapPairRatioFlag         = flag.Float64("flap-pair-ratio", 0, "fraction of clients to include in flap pairs (0 = single pair)")
	softLossPairRatioFlag     = flag.Float64("soft-loss-pair-ratio", 0, "fraction of clients to include in soft-loss pairs (0 = single pair)")
	highSoftLossPairRatioFlag = flag.Float64("soft-loss-high-pair-ratio", 0, "fraction of clients to include in high soft-loss pairs (0 = single pair)")
	hardLossRatioFlag         = flag.Float64("hard-loss-ratio", 0.5, "fraction of clients to hard-fail (0–1)")

	// Default to quiet logging.
	notQuietFlag = flag.Bool("not-quiet", false, "do not quiet logging")
)

func TestClient_Liveness_Faults_FlapsOnConnectivityLoss(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("Skipping flapping test in short mode")
	}

	clients, sw, cfg := setupLivenessClients(t, 20000)
	clientCount := cfg.clientCount
	bgpFlapCycles := *bgpFlapCyclesFlag
	flapRatio := *flapPairRatioFlag

	if clientCount < 2 {
		t.Skip("need at least 2 clients for flapping test")
	}

	nextHop := net.IPv4(10, 5, 0, 1)
	registerFullMeshRoutes(t, clients, nextHop)
	requireFullMeshUp(t, clients, clientCount-1, 30*time.Second)

	pairs := selectPairs(clients, flapRatio)
	if len(pairs) == 0 {
		t.Skip("no pairs selected for flapping")
	}

	type pairState struct {
		cA, cB   *testClient
		ipA, ipB net.IP
		peerAB   Peer
		peerBA   Peer
	}
	ps := make([]pairState, len(pairs))
	for i, p := range pairs {
		ipA := p[0].mgr.LocalAddr().IP
		ipB := p[1].mgr.LocalAddr().IP
		ps[i] = pairState{
			cA:  p[0],
			cB:  p[1],
			ipA: ipA,
			ipB: ipB,
			peerAB: Peer{
				Interface: p[0].iface,
				LocalIP:   ipA.To4().String(),
				PeerIP:    ipB.To4().String(),
			},
			peerBA: Peer{
				Interface: p[1].iface,
				LocalIP:   ipB.To4().String(),
				PeerIP:    ipA.To4().String(),
			},
		}
	}

	detectTime := cfg.rxMin * time.Duration(cfg.detectMult)
	waitDown := 5 * detectTime

	waitBothDown := func(p pairState) bool {
		sessAB, okAB := p.cA.mgr.GetSession(p.peerAB)
		sessBA, okBA := p.cB.mgr.GetSession(p.peerBA)
		if !okAB || !okBA {
			return false
		}
		snapAB := sessAB.Snapshot()
		snapBA := sessBA.Snapshot()
		return snapAB.State == StateDown && snapAB.LastDownReason == DownReasonTimeout &&
			snapBA.State == StateDown && snapBA.LastDownReason == DownReasonTimeout
	}

	waitBothUp := func(p pairState) bool {
		sessAB, okAB := p.cA.mgr.GetSession(p.peerAB)
		sessBA, okBA := p.cB.mgr.GetSession(p.peerBA)
		if !okAB || !okBA {
			return false
		}
		snapAB := sessAB.Snapshot()
		snapBA := sessBA.Snapshot()
		return snapAB.State == StateUp && snapBA.State == StateUp
	}

	for _, p := range ps {
		require.Eventually(t, func() bool { return waitBothUp(p) }, 10*time.Second, 200*time.Millisecond)
	}

	before := make(map[*testClient]float64)
	for _, p := range ps {
		if _, ok := before[p.cA]; !ok {
			before[p.cA] = sessionUpToDownTransitions(t, p.cA)
		}
		if _, ok := before[p.cB]; !ok {
			before[p.cB] = sessionUpToDownTransitions(t, p.cB)
		}
	}

	for range bgpFlapCycles {
		for _, p := range ps {
			sw.SetHardLoss(p.ipA, p.ipB)
		}

		for _, p := range ps {
			require.Eventually(t, func() bool { return waitBothDown(p) }, waitDown, 200*time.Millisecond)
		}

		for _, p := range ps {
			sw.SetNoFault(p.ipA, p.ipB)
		}

		for _, p := range ps {
			require.Eventually(t, func() bool { return waitBothUp(p) }, 30*time.Second, 500*time.Millisecond)
		}
	}

	for c, v := range before {
		after := sessionUpToDownTransitions(t, c)
		require.Greaterf(t, after, v, "expected Up→Down transitions metric to increase for client %d", c.id)
	}
}

func TestClient_Liveness_Faults_SoftLoss_PartialPacketLoss(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("Skipping soft-loss test in short mode")
	}

	clients, sw, cfg := setupLivenessClients(t, 21000)
	clientCount := cfg.clientCount
	softLossPct := *softLossPctFlag
	softLossCycles := *softLossCyclesFlag
	stableDuration := *stableDurationFlag
	softRatio := *softLossPairRatioFlag

	if clientCount < 2 {
		t.Skip("need at least 2 clients for soft-loss test")
	}

	nextHop := net.IPv4(10, 5, 0, 1)
	registerFullMeshRoutes(t, clients, nextHop)
	requireFullMeshUp(t, clients, clientCount-1, 30*time.Second)

	pairs := selectPairs(clients, softRatio)
	if len(pairs) == 0 {
		t.Skip("no pairs selected for soft-loss test")
	}

	type pairState struct {
		cA, cB   *testClient
		ipA, ipB net.IP
		peerAB   Peer
		peerBA   Peer
	}
	ps := make([]pairState, len(pairs))
	for i, p := range pairs {
		ipA := p[0].mgr.LocalAddr().IP
		ipB := p[1].mgr.LocalAddr().IP
		ps[i] = pairState{
			cA:  p[0],
			cB:  p[1],
			ipA: ipA,
			ipB: ipB,
			peerAB: Peer{
				Interface: p[0].iface,
				LocalIP:   ipA.To4().String(),
				PeerIP:    ipB.To4().String(),
			},
			peerBA: Peer{
				Interface: p[1].iface,
				LocalIP:   ipB.To4().String(),
				PeerIP:    ipA.To4().String(),
			},
		}
	}

	waitBothUp := func(p pairState) bool {
		sessAB, okAB := p.cA.mgr.GetSession(p.peerAB)
		sessBA, okBA := p.cB.mgr.GetSession(p.peerBA)
		if !okAB || !okBA {
			return false
		}
		snapAB := sessAB.Snapshot()
		snapBA := sessBA.Snapshot()
		return snapAB.State == StateUp && snapBA.State == StateUp
	}

	for _, p := range ps {
		require.Eventually(t, func() bool { return waitBothUp(p) }, 10*time.Second, 200*time.Millisecond)
	}

	for _, p := range ps {
		sw.SetSoftLoss(p.ipA, p.ipB, softLossPct)
	}

	before := make(map[*testClient]float64)
	for _, p := range ps {
		if _, ok := before[p.cA]; !ok {
			before[p.cA] = sessionUpToDownTransitions(t, p.cA)
		}
		if _, ok := before[p.cB]; !ok {
			before[p.cB] = sessionUpToDownTransitions(t, p.cB)
		}
	}

	// Let the sessions run under soft loss for the full duration. We rely on
	// the transition-counter metric below to detect any flaps rather than
	// polling the instantaneous state, which is racy under CI CPU pressure.
	for range softLossCycles {
		time.Sleep(stableDuration)
	}

	// Sessions should still be Up after the soft-loss period.
	for _, p := range ps {
		require.Eventually(t, func() bool { return waitBothUp(p) }, 10*time.Second, 200*time.Millisecond,
			"sessions between %v and %v should be Up after soft loss period", p.ipA, p.ipB)
	}

	for c, v := range before {
		after := sessionUpToDownTransitions(t, c)
		require.Equalf(t, v, after, "unexpected Up→Down transitions under mild soft loss for client %d", c.id)
	}
}

func TestClient_Liveness_Faults_HardLoss_PermanentOutage(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("Skipping hard-loss test in short mode")
	}

	clients, sw, cfg := setupLivenessClients(t, 22000)
	clientCount := cfg.clientCount
	stableDuration := *stableDurationFlag

	if clientCount < 2 {
		t.Skip("need at least 2 clients for hard-loss test")
	}

	nextHop := net.IPv4(10, 5, 0, 1)
	registerFullMeshRoutes(t, clients, nextHop)
	requireFullMeshUp(t, clients, clientCount-1, 30*time.Second)

	before := make(map[*testClient]float64, clientCount)
	for _, c := range clients {
		before[c] = sessionUpToDownTransitions(t, c)
	}

	ratio := *hardLossRatioFlag
	if ratio <= 0 {
		ratio = 0.5
	}
	if ratio > 1 {
		ratio = 1
	}
	failCount := int(math.Round(ratio * float64(clientCount)))
	if failCount < 1 {
		failCount = 1
	}
	if failCount > clientCount {
		failCount = clientCount
	}
	failed := clients[:failCount]

	failedIPs := make(map[string]struct{}, failCount)
	for _, f := range failed {
		ip := f.mgr.LocalAddr().IP.To4().String()
		failedIPs[ip] = struct{}{}
	}

	for _, f := range failed {
		ipF := f.mgr.LocalAddr().IP
		for _, c := range clients {
			if f == c {
				continue
			}
			ipC := c.mgr.LocalAddr().IP
			sw.SetHardLoss(ipF, ipC)
		}
	}

	detectTime := cfg.rxMin * time.Duration(cfg.detectMult)
	waitDown := 5 * detectTime

	checkStates := func() bool {
		for _, c := range clients {
			sessions := c.mgr.GetSessions()
			if len(sessions) != clientCount-1 {
				return false
			}
			for _, s := range sessions {
				_, localFailed := failedIPs[s.Peer.LocalIP]
				_, remoteFailed := failedIPs[s.Peer.PeerIP]
				if localFailed || remoteFailed {
					if s.State != StateDown || s.LastDownReason != DownReasonTimeout {
						return false
					}
				} else {
					if s.State != StateUp {
						return false
					}
				}
			}
		}
		return true
	}

	require.Eventually(t, checkStates, waitDown, 200*time.Millisecond)

	for c, v := range before {
		after := sessionUpToDownTransitions(t, c)
		require.Greaterf(t, after, v, "expected Up→Down transitions metric to increase for client %d under hard loss", c.id)
	}

	stableDeadline := time.Now().Add(stableDuration)
	for time.Now().Before(stableDeadline) {
		require.True(t, checkStates(), "session states should remain stable under hard loss")
		time.Sleep(detectTime / 2)
	}
}

func TestClient_Liveness_Faults_SoftLoss_TimeoutAtHighLoss(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("Skipping high soft-loss timeout test in short mode")
	}

	clients, sw, cfg := setupLivenessClients(t, 23000)
	clientCount := cfg.clientCount
	if clientCount < 2 {
		t.Skip("need at least 2 clients for high soft-loss test")
	}

	highSoftLossPct := *highSoftLossPctFlag
	highSoftRatio := *highSoftLossPairRatioFlag
	stableDuration := *stableDurationFlag
	nextHop := net.IPv4(10, 5, 0, 1)

	registerFullMeshRoutes(t, clients, nextHop)
	requireFullMeshUp(t, clients, clientCount-1, 30*time.Second)

	pairs := selectPairs(clients, highSoftRatio)
	if len(pairs) == 0 {
		t.Skip("no pairs selected for high soft-loss test")
	}

	type pairState struct {
		cA, cB   *testClient
		ipA, ipB net.IP
		peerAB   Peer
		peerBA   Peer
	}
	ps := make([]pairState, len(pairs))
	badIPs := make(map[string]struct{})
	degradedClients := make(map[*testClient]struct{})

	for i, p := range pairs {
		ipA := p[0].mgr.LocalAddr().IP
		ipB := p[1].mgr.LocalAddr().IP
		ipA4 := ipA.To4().String()
		ipB4 := ipB.To4().String()

		ps[i] = pairState{
			cA:  p[0],
			cB:  p[1],
			ipA: ipA,
			ipB: ipB,
			peerAB: Peer{
				Interface: p[0].iface,
				LocalIP:   ipA4,
				PeerIP:    ipB4,
			},
			peerBA: Peer{
				Interface: p[1].iface,
				LocalIP:   ipB4,
				PeerIP:    ipA4,
			},
		}
		badIPs[ipA4] = struct{}{}
		badIPs[ipB4] = struct{}{}
		degradedClients[p[0]] = struct{}{}
		degradedClients[p[1]] = struct{}{}
	}

	waitBothUp := func(p pairState) bool {
		sessAB, okAB := p.cA.mgr.GetSession(p.peerAB)
		sessBA, okBA := p.cB.mgr.GetSession(p.peerBA)
		if !okAB || !okBA {
			return false
		}
		snapAB := sessAB.Snapshot()
		snapBA := sessBA.Snapshot()
		return snapAB.State == StateUp && snapBA.State == StateUp
	}
	waitBothDown := func(p pairState) bool {
		sessAB, okAB := p.cA.mgr.GetSession(p.peerAB)
		sessBA, okBA := p.cB.mgr.GetSession(p.peerBA)
		if !okAB || !okBA {
			return false
		}
		snapAB := sessAB.Snapshot()
		snapBA := sessBA.Snapshot()
		return snapAB.State == StateDown && snapAB.LastDownReason == DownReasonTimeout &&
			snapBA.State == StateDown && snapBA.LastDownReason == DownReasonTimeout
	}

	for _, p := range ps {
		require.Eventually(t, func() bool { return waitBothUp(p) }, 10*time.Second, 200*time.Millisecond)
	}

	before := make(map[*testClient]float64)
	for c := range degradedClients {
		before[c] = sessionUpToDownTransitions(t, c)
	}

	for _, p := range ps {
		sw.SetSoftLoss(p.ipA, p.ipB, highSoftLossPct)
	}

	detectTime := cfg.rxMin * time.Duration(cfg.detectMult)
	waitDown := 30 * detectTime

	for _, p := range ps {
		require.Eventually(t, func() bool { return waitBothDown(p) }, waitDown, 200*time.Millisecond)
	}

	for c, v := range before {
		after := sessionUpToDownTransitions(t, c)
		require.Greaterf(t, after, v, "expected Up→Down transitions to increase for degraded client %d under high soft loss", c.id)
	}

	checkHealthy := func() bool {
		for _, c := range clients {
			sessions := c.mgr.GetSessions()
			if len(sessions) != clientCount-1 {
				return false
			}
			for _, s := range sessions {
				_, localBad := badIPs[s.Peer.LocalIP]
				_, remoteBad := badIPs[s.Peer.PeerIP]

				if localBad && remoteBad {
					if s.State == StateDown && s.LastDownReason != DownReasonTimeout {
						return false
					}
					continue
				}
				if s.State != StateUp {
					return false
				}
			}
		}
		return true
	}

	stableDeadline := time.Now().Add(stableDuration)
	for time.Now().Before(stableDeadline) {
		require.True(t, checkHealthy(), "non-degraded sessions must remain Up under high soft loss")
		time.Sleep(detectTime / 2)
	}
}

func TestClient_Liveness_Faults_MixedPassiveConfigs_RemoteAdminAndTimeoutKeepRouteInstalled(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("Skipping mixed passive fault test in short mode")
	}

	clients, sw, cfg := setupMixedPassiveLivenessClients(t, 24000)
	clientCount := cfg.clientCount
	if clientCount < 2 {
		t.Skip("need at least 2 clients for mixed passive test")
	}

	nextHop := net.IPv4(10, 5, 0, 1)
	registerFullMeshRoutes(t, clients, nextHop)
	requireFullMeshUp(t, clients, clientCount-1, 30*time.Second)

	// By construction:
	//   clients[0] -> HonorPeerAdvertisedPassive = true
	//   clients[1] -> PassiveMode = true
	honor := clients[0]
	passive := clients[1]

	ipHonor := honor.mgr.LocalAddr().IP
	ipPassive := passive.mgr.LocalAddr().IP

	peerHonorToPassive := Peer{
		Interface: honor.iface,
		LocalIP:   ipHonor.To4().String(),
		PeerIP:    ipPassive.To4().String(),
	}
	peerPassiveToHonor := Peer{
		Interface: passive.iface,
		LocalIP:   ipPassive.To4().String(),
		PeerIP:    ipHonor.To4().String(),
	}

	// Ensure the honor-side session is Up.
	require.Eventually(t, func() bool {
		sess, ok := honor.mgr.GetSession(peerHonorToPassive)
		if !ok || sess == nil {
			return false
		}
		snap := sess.Snapshot()
		return snap.State == StateUp
	}, 10*time.Second, 200*time.Millisecond, "honor client's session must reach Up")

	// Ensure honor side has learned that the peer advertises passive.
	require.Eventually(t, func() bool {
		sess, ok := honor.mgr.GetSession(peerHonorToPassive)
		if !ok || sess == nil {
			return false
		}
		snap := sess.Snapshot()
		return snap.PeerAdvertisedMode == PeerModePassive
	}, 5*time.Second, 200*time.Millisecond, "honor client must see peer as passive")

	// Capture route key and confirm installed before any faults.
	sessHonor, ok := honor.mgr.GetSession(peerHonorToPassive)
	require.True(t, ok)
	require.NotNil(t, sessHonor)
	rkHonor := routeKeyFor(peerHonorToPassive.Interface, sessHonor.route)
	require.True(t, honor.mgr.IsInstalled(rkHonor), "route should be installed before any faults")

	// ---- Phase 1: Hard loss -> detect timeout, but route kept installed ----

	sw.SetHardLoss(ipPassive, ipHonor)

	detectTime := cfg.rxMin * time.Duration(cfg.detectMult)
	waitDown := 10 * detectTime

	require.Eventually(t, func() bool {
		s, ok := honor.mgr.GetSession(peerHonorToPassive)
		if !ok || s == nil {
			return false
		}
		snap := s.Snapshot()
		return snap.State == StateDown && snap.LastDownReason == DownReasonTimeout
	}, waitDown, 200*time.Millisecond, "honor client should time out under hard loss")

	require.True(t, honor.mgr.IsInstalled(rkHonor),
		"route should remain installed on honor side after timeout when peer is effectively passive")

	// Clear hard loss so we can do a clean remote AdminDown next.
	sw.SetNoFault(ipPassive, ipHonor)

	// Let the sessions converge back to Up.
	require.Eventually(t, func() bool {
		sessH, okH := honor.mgr.GetSession(peerHonorToPassive)
		sessP, okP := passive.mgr.GetSession(peerPassiveToHonor)
		if !okH || !okP || sessH == nil || sessP == nil {
			return false
		}
		snapH := sessH.Snapshot()
		snapP := sessP.Snapshot()
		return snapH.State == StateUp && snapP.State == StateUp
	}, 30*time.Second, 500*time.Millisecond, "sessions should return to Up after clearing hard loss")

	require.True(t, honor.mgr.IsInstalled(rkHonor),
		"route should still be installed on honor side after recovery from hard loss")

	// ---- Phase 2: Remote AdminDown from the passive client -> no withdraw ----

	sessPassive, ok := passive.mgr.GetSession(peerPassiveToHonor)
	require.True(t, ok)
	require.NotNil(t, sessPassive)
	remoteSnap := sessPassive.Snapshot()
	remoteRoute := &remoteSnap.Route

	// AdminDownRoute on the passive side will send an AdminDown control packet with PASSIVE set.
	passive.mgr.AdminDownRoute(remoteRoute, passive.iface)

	// Honor side should see Down with reason remote_admin, but keep the route installed
	// because the peer is effectively passive and HonorPeerAdvertisedPassive is enabled.
	require.Eventually(t, func() bool {
		s, ok := honor.mgr.GetSession(peerHonorToPassive)
		if !ok || s == nil {
			return false
		}
		snap := s.Snapshot()
		return snap.State == StateDown && snap.LastDownReason == DownReasonRemoteAdmin
	}, 10*time.Second, 200*time.Millisecond, "honor client should see remote-admin Down")

	require.True(t, honor.mgr.IsInstalled(rkHonor),
		"route should remain installed on honor side after remote AdminDown from effectively passive peer")
}

type livenessTestConfig struct {
	clientCount int
	txMin       time.Duration
	rxMin       time.Duration
	detectMult  uint8
	backoffMax  time.Duration
}

// getLivenessTestConfig parses flags once and returns the shared config for all tests.
func getLivenessTestConfig(t *testing.T) livenessTestConfig {
	t.Helper()
	if !flag.Parsed() {
		flag.Parse()
	}
	return livenessTestConfig{
		clientCount: *clientsFlag,
		txMin:       *txMinFlag,
		rxMin:       *rxMinFlag,
		detectMult:  uint8(*detectMultFlag),
		backoffMax:  *backoffMaxFlag,
	}
}

type testClient struct {
	id      uint32
	mgr     Manager
	metrics *prometheus.Registry
	nlr     RouteReaderWriter
	iface   string
}

func (c *testClient) Close() error {
	return c.mgr.Close()
}

// Shared setup for all three tests: builds N managers, wiring them to a common fake switch.
func setupLivenessClients(t *testing.T, basePort int) ([]*testClient, *fakeUDPSwitch, livenessTestConfig) {
	t.Helper()

	cfg := getLivenessTestConfig(t)
	log := newTestLoggerWith(t, !*notQuietFlag, *debugFlag)
	sw := newFakeUDPSwitch()

	clients := make([]*testClient, 0, cfg.clientCount)

	for i := range cfg.clientCount {
		cid := uint32(i + 1)

		nlr := &MockRouteReaderWriter{
			RouteAddFunc:        func(r *routing.Route) error { return nil },
			RouteDeleteFunc:     func(r *routing.Route) error { return nil },
			RouteByProtocolFunc: func(protocol int) ([]*routing.Route, error) { return nil, nil },
		}

		iface := "lo"
		ip := nthTestIPv4(i)
		udp := newFakeUDPConn(sw, ip.String(), basePort+i, iface)

		reg := prometheus.NewRegistry()

		lm, err := NewManager(t.Context(), &ManagerConfig{
			Logger:          log.With("clientID", cid),
			Netlinker:       nlr,
			MetricsRegistry: reg,
			UDP:             udp,
			BindIP:          ip.String(),
			Port:            udp.LocalAddr().(*net.UDPAddr).Port,
			TxMin:           cfg.txMin,
			RxMin:           cfg.rxMin,
			DetectMult:      cfg.detectMult,
			MinTxFloor:      cfg.txMin,
			MaxTxCeil:       cfg.txMin,
			BackoffMax:      cfg.backoffMax,
			ClientVersion:   "1.2.3-dev",
		}, nil)
		require.NoError(t, err)

		tc := &testClient{
			id:      cid,
			mgr:     lm,
			metrics: reg,
			nlr:     nlr,
			iface:   iface,
		}
		clients = append(clients, tc)

		t.Cleanup(func() {
			if err := lm.Close(); err != nil {
				log.Warn("error closing manager", "error", err)
			}
		})
	}

	return clients, sw, cfg
}

// setupMixedPassiveLivenessClients sets up a mixed passive/active liveness clients.
// The first client is active, the second is passive.
func setupMixedPassiveLivenessClients(t *testing.T, basePort int) ([]*testClient, *fakeUDPSwitch, livenessTestConfig) {
	t.Helper()

	cfg := getLivenessTestConfig(t)
	log := newTestLoggerWith(t, !*notQuietFlag, *debugFlag)
	sw := newFakeUDPSwitch()

	clients := make([]*testClient, 0, cfg.clientCount)

	for i := range cfg.clientCount {
		cid := uint32(i + 1)

		nlr := &MockRouteReaderWriter{
			RouteAddFunc:        func(r *routing.Route) error { return nil },
			RouteDeleteFunc:     func(r *routing.Route) error { return nil },
			RouteByProtocolFunc: func(protocol int) ([]*routing.Route, error) { return nil, nil },
		}

		iface := "lo"
		ip := nthTestIPv4(i)
		udp := newFakeUDPConn(sw, ip.String(), basePort+i, iface)

		reg := prometheus.NewRegistry()

		mcfg := &ManagerConfig{
			Logger:          log.With("clientID", cid),
			Netlinker:       nlr,
			MetricsRegistry: reg,
			UDP:             udp,
			BindIP:          ip.String(),
			Port:            udp.LocalAddr().(*net.UDPAddr).Port,
			TxMin:           cfg.txMin,
			RxMin:           cfg.rxMin,
			DetectMult:      cfg.detectMult,
			MinTxFloor:      cfg.txMin,
			MaxTxCeil:       cfg.txMin,
			BackoffMax:      cfg.backoffMax,
			ClientVersion:   "1.2.3-dev",
		}

		switch i {
		case 0:
			// Client 0: honors peer advertised passive, but is otherwise active.
			mcfg.HonorPeerAdvertisedPassive = true
		case 1:
			// Client 1: globally passive (always advertises PASSIVE bit).
			mcfg.PassiveMode = true
		}

		lm, err := NewManager(t.Context(), mcfg, nil)
		require.NoError(t, err)

		tc := &testClient{
			id:      cid,
			mgr:     lm,
			metrics: reg,
			nlr:     nlr,
			iface:   iface,
		}
		clients = append(clients, tc)

		t.Cleanup(func() {
			if err := lm.Close(); err != nil {
				log.Warn("error closing manager", "error", err)
			}
		})
	}

	return clients, sw, cfg
}

// Registers a full mesh of routes between all clients.
func registerFullMeshRoutes(t *testing.T, clients []*testClient, nextHop net.IP) {
	t.Helper()

	var wg sync.WaitGroup
	for _, c1 := range clients {
		for _, c2 := range clients {
			if c1 == c2 {
				continue
			}
			wg.Add(1)
			go func(c1 *testClient, c2 *testClient) {
				defer wg.Done()
				err := c1.mgr.RegisterRoute(&Route{Route: routing.Route{
					Table: 100,
					Src:   c1.mgr.LocalAddr().IP,
					Dst: &net.IPNet{
						IP:   c2.mgr.LocalAddr().IP.To4(),
						Mask: net.CIDRMask(32, 32),
					},
					NextHop:  nextHop,
					Protocol: unix.RTPROT_BGP,
				}}, c1.iface, c1.mgr.LocalAddr().Port)
				require.NoError(t, err)
			}(c1, c2)
		}
	}
	wg.Wait()
}

// Wait until all sessions are Up in the full mesh.
func requireFullMeshUp(t *testing.T, clients []*testClient, expectedPerClient int, timeout time.Duration) {
	t.Helper()

	require.Eventually(t, func() bool {
		for _, c := range clients {
			sessions := c.mgr.GetSessions()
			if len(sessions) != expectedPerClient {
				return false
			}
			for _, sess := range sessions {
				if sess.State != StateUp {
					return false
				}
			}
		}
		return true
	}, timeout, 500*time.Millisecond)
}

func nthTestIPv4(n int) net.IP {
	if n < 0 {
		return nil
	}
	base := uint32(10)<<24 | 0x00000001 // 10.0.0.1
	addr := base + uint32(n)
	// stop at 10.255.255.255
	if addr > (uint32(10)<<24 | 0x00FFFFFF) {
		return nil
	}
	b := []byte{
		byte(addr >> 24),
		byte(addr >> 16),
		byte(addr >> 8),
		byte(addr),
	}
	return net.IPv4(b[0], b[1], b[2], b[3])
}

type fakeUDPPacket struct {
	data  []byte
	src   *net.UDPAddr
	iface string
}

type fakeUDPConn struct {
	sw     *fakeUDPSwitch
	local  *net.UDPAddr
	ifname string

	mu       sync.Mutex
	deadline time.Time
	closed   bool

	in chan fakeUDPPacket
}

func newFakeUDPConn(sw *fakeUDPSwitch, ip string, port int, ifname string) *fakeUDPConn {
	c := &fakeUDPConn{
		sw:     sw,
		local:  &net.UDPAddr{IP: net.ParseIP(ip), Port: port},
		ifname: ifname,
		in:     make(chan fakeUDPPacket, 8192),
	}
	sw.register(c)
	return c
}

func (c *fakeUDPConn) WriteTo(pkt []byte, dst *net.UDPAddr, iface string, src net.IP) (int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	c.mu.Unlock()

	cp := make([]byte, len(pkt))
	copy(cp, pkt)

	c.sw.deliver(dst, fakeUDPPacket{
		data:  cp,
		src:   c.local, // sender addr
		iface: iface,
	})

	return len(pkt), nil
}

func (c *fakeUDPConn) ReadFrom(buf []byte) (int, *net.UDPAddr, net.IP, string, error) {
	c.mu.Lock()
	deadline := c.deadline
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return 0, nil, nil, "", net.ErrClosed
	}

	var timeout <-chan time.Time
	if !deadline.IsZero() {
		timeout = time.After(time.Until(deadline))
	}

	select {
	case pkt := <-c.in:
		n := copy(buf, pkt.data)
		// remoteAddr = sender; localIP = *this* conn's IP; iface = this conn's ifname
		return n, pkt.src, c.local.IP, c.ifname, nil
	case <-timeout:
		return 0, nil, nil, "", os.ErrDeadlineExceeded
	}
}

func (c *fakeUDPConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadline = t
	return nil
}

func (c *fakeUDPConn) LocalAddr() net.Addr {
	return c.local
}

func (c *fakeUDPConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	close(c.in)
	return nil
}

type faultMode int

const (
	faultModeNone faultMode = iota
	faultModeDropAll
	faultModeSoftDrop
)

type linkKey struct {
	a, b string
}

func makeLinkKey(a, b net.IP) linkKey {
	as, bs := a.String(), b.String()
	if as < bs {
		return linkKey{a: as, b: bs}
	}
	return linkKey{a: bs, b: as}
}

type linkFaultProfile struct {
	mode    faultMode
	lossPct float64 // 0–1 for soft-drop
}

type fakeUDPSwitch struct {
	mu         sync.Mutex
	conns      map[string]*fakeUDPConn
	linkFaults map[linkKey]linkFaultProfile
	rng        *rand.Rand
}

// fakeUDPSwitch routes purely by IP (port is ignored) because each test client
// has a unique IP. This keeps the fake network simple while still matching the
// liveness manager's behavior, which sends to dst.IP.
func newFakeUDPSwitch() *fakeUDPSwitch {
	return &fakeUDPSwitch{
		conns:      make(map[string]*fakeUDPConn),
		linkFaults: make(map[linkKey]linkFaultProfile),
		rng:        rand.New(rand.NewSource(1)), // deterministic for tests
	}
}

func (s *fakeUDPSwitch) register(c *fakeUDPConn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[c.local.IP.String()] = c
}

// setFault is the unified injection API for link a<->b (unordered).
// mode controls the basic behavior; lossPct is only used for faultModeSoftDrop.
func (s *fakeUDPSwitch) setFault(a, b net.IP, mode faultMode, lossPct float64) {
	if a == nil || b == nil {
		return
	}
	lk := makeLinkKey(a, b)

	s.mu.Lock()
	defer s.mu.Unlock()

	switch mode {
	case faultModeNone:
		delete(s.linkFaults, lk)
		return
	case faultModeSoftDrop:
		if lossPct <= 0 {
			// effect is equivalent to clearing the fault
			delete(s.linkFaults, lk)
			return
		}
		if lossPct > 1 {
			lossPct = 1
		}
		s.linkFaults[lk] = linkFaultProfile{
			mode:    faultModeSoftDrop,
			lossPct: lossPct,
		}
	default: // faultModeDropAll or any future "hard" modes
		s.linkFaults[lk] = linkFaultProfile{
			mode:    mode,
			lossPct: 0,
		}
	}
}

// SetNoFault clears any injected behavior for link a<->b.
func (s *fakeUDPSwitch) SetNoFault(a, b net.IP) {
	s.setFault(a, b, faultModeNone, 0)
}

// SetHardLoss configures a permanent drop-all fault for link a<->b.
func (s *fakeUDPSwitch) SetHardLoss(a, b net.IP) {
	s.setFault(a, b, faultModeDropAll, 0)
}

// SetSoftLoss configures probabilistic packet loss for link a<->b.
// lossPct is 0–1 (e.g., 0.01 for 1% loss).
func (s *fakeUDPSwitch) SetSoftLoss(a, b net.IP, lossPct float64) {
	s.setFault(a, b, faultModeSoftDrop, lossPct)
}

func (s *fakeUDPSwitch) deliver(dst *net.UDPAddr, pkt fakeUDPPacket) {
	if dst == nil || dst.IP == nil {
		return
	}
	key := dst.IP.String()

	s.mu.Lock()
	if pkt.src != nil && pkt.src.IP != nil {
		lk := makeLinkKey(pkt.src.IP, dst.IP)
		if prof, ok := s.linkFaults[lk]; ok {
			switch prof.mode {
			case faultModeDropAll:
				s.mu.Unlock()
				return
			case faultModeSoftDrop:
				if prof.lossPct > 0 && s.rng.Float64() < prof.lossPct {
					s.mu.Unlock()
					return
				}
			}
		}
	}
	c := s.conns[key]
	s.mu.Unlock()

	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	select {
	case c.in <- pkt:
	default:
	}
}

// selectPairs deterministically picks disjoint pairs of clients from the front
// of the slice, involving roughly ratio*len(clients) clients.
// ratio:
//   - <=0 → exactly one pair (clients[0], clients[1]) if possible
//   - >=1 → all clients (rounded to an even count)
//
// For determinism and simplicity we don't randomize; we just walk the slice.
func selectPairs(clients []*testClient, ratio float64) [][2]*testClient {
	n := len(clients)
	if n < 2 {
		return nil
	}

	if ratio <= 0 {
		return [][2]*testClient{{clients[0], clients[1]}}
	}

	if ratio > 1 {
		ratio = 1
	}

	k := int(math.Round(ratio * float64(n)))
	if k < 2 {
		k = 2
	}
	if k > n {
		k = n
	}
	if k%2 == 1 {
		k--
	}
	if k < 2 {
		// If we rounded down to 1, fall back to a single pair.
		return [][2]*testClient{{clients[0], clients[1]}}
	}

	pairs := make([][2]*testClient, 0, k/2)
	for i := 0; i+1 < k; i += 2 {
		pairs = append(pairs, [2]*testClient{clients[i], clients[i+1]})
	}
	return pairs
}

func metricValueWithLabels(t *testing.T, reg *prometheus.Registry, name string, labelFilter map[string]string) float64 {
	t.Helper()

	mfs, err := reg.Gather()
	require.NoError(t, err)

	var total float64
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			match := true
			for _, lp := range m.GetLabel() {
				want, ok := labelFilter[lp.GetName()]
				if ok && lp.GetValue() != want {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			if c := m.GetCounter(); c != nil {
				total += c.GetValue()
			} else if g := m.GetGauge(); g != nil {
				total += g.GetValue()
			}
		}
	}
	return total
}

func sessionUpToDownTransitions(t *testing.T, c *testClient) float64 {
	ip := c.mgr.LocalAddr().IP.To4().String()
	return metricValueWithLabels(t, c.metrics, "doublezero_liveness_session_transitions_total", map[string]string{
		LabelIface:     c.iface,
		LabelLocalIP:   ip,
		LabelStateFrom: StateUp.String(),
		LabelStateTo:   StateDown.String(),
		// LabelReason intentionally omitted: we sum across all reasons.
	})
}
