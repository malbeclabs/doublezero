package liveness

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type logRecorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (r *logRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (r *logRecorder) WithAttrs([]slog.Attr) slog.Handler       { return r }
func (r *logRecorder) WithGroup(string) slog.Handler            { return r }
func (r *logRecorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec.Clone())
	return nil
}

func (r *logRecorder) find(level slog.Level, msg string) []slog.Record {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []slog.Record
	for _, rec := range r.records {
		if rec.Level == level && rec.Message == msg {
			out = append(out, rec)
		}
	}
	return out
}

func recordAttr(rec slog.Record, key string) string {
	var v string
	rec.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			v = a.Value.String()
			return false
		}
		return true
	})
	return v
}

// newRouteActionTestManager skips NewManager so no ticker races the test's checks.
func newRouteActionTestManager(passive bool) (*manager, *logRecorder) {
	rec := &logRecorder{}
	return &manager{
		cfg:       &ManagerConfig{PassiveMode: passive},
		log:       slog.New(rec),
		sessions:  make(map[Peer]*Session),
		desired:   make(map[RouteKey]*Route),
		installed: make(map[RouteKey]bool),
		action:    RouteActionNone,
	}, rec
}

// checkAt runs one check at now, as the ticker does, and returns what a status read then reports.
func checkAt(m *manager, now time.Time) RouteAction {
	m.checkRouteAction(now)
	return m.RouteAction()
}

func addRouteActionTestSession(m *manager, peerIP string, state State, lastRx time.Time) *Session {
	r := newTestRoute(func(r *Route) {
		r.Dst = &net.IPNet{IP: net.ParseIP(peerIP), Mask: net.CIDRMask(32, 32)}
	})
	peer := Peer{Interface: "doublezero0", LocalIP: r.Src.String(), PeerIP: peerIP}
	sess := &Session{route: r, peer: &peer, state: state, lastRx: lastRx}
	m.desired[routeKeyFor(peer.Interface, r)] = r
	m.sessions[peer] = sess
	return sess
}

func TestClient_Liveness_RouteAction_Rule(t *testing.T) {
	t.Parallel()

	// lastRx is when the session last heard its peer, relative to the period's start:
	// zero for never, negative for before the period began.
	type sess struct {
		state  State
		lastRx time.Duration
	}
	for _, tc := range []struct {
		name     string
		passive  bool
		sessions []sess
		during   RouteAction // within routeActionGrace
		after    RouteAction // once it has passed
	}{
		{name: "no desired routes", during: RouteActionNone, after: RouteActionNone},
		{name: "all sessions down", sessions: []sess{{StateDown, 0}, {StateDown, 0}}, during: RouteActionStarting, after: RouteActionInbound},
		{name: "one session up", sessions: []sess{{StateDown, 0}, {StateUp, -time.Second}, {StateInit, -time.Second}}, during: RouteActionNone, after: RouteActionNone},
		{name: "init mixed with peers that never answer", sessions: []sess{{StateInit, routeActionGrace - time.Second}, {StateDown, 0}, {StateDown, 0}}, during: RouteActionStarting, after: RouteActionOutbound},
		{name: "probe since the period began, session down again", sessions: []sess{{StateDown, time.Minute}, {StateDown, 0}}, during: RouteActionStarting, after: RouteActionOutbound},
		{name: "probe older than the window", sessions: []sess{{StateDown, -time.Second}, {StateDown, 0}}, during: RouteActionStarting, after: RouteActionInbound},
		{name: "passive mode", passive: true, sessions: []sess{{StateDown, 0}}, during: RouteActionNone, after: RouteActionNone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, _ := newRouteActionTestManager(tc.passive)
			t0 := time.Now()
			for i, s := range tc.sessions {
				var lastRx time.Time
				if s.lastRx != 0 {
					lastRx = t0.Add(s.lastRx)
				}
				addRouteActionTestSession(m, net.IPv4(10, 4, 0, byte(11+i)).String(), s.state, lastRx)
			}

			require.Equal(t, tc.during, checkAt(m, t0))
			require.Equal(t, tc.during, checkAt(m, t0.Add(routeActionGrace-time.Second)), "within the grace period")
			require.Equal(t, tc.after, checkAt(m, t0.Add(routeActionGrace)))
		})
	}
}

// A route that stays installed means no action; a key onSessionDown cleared to false does not.
func TestClient_Liveness_RouteAction_InstalledRouteMeansNone(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		installed bool
		want      RouteAction
	}{
		{installed: true, want: RouteActionNone},
		{installed: false, want: RouteActionInbound},
	} {
		m, _ := newRouteActionTestManager(false)
		sess := addRouteActionTestSession(m, "10.4.0.11", StateDown, time.Time{})
		addRouteActionTestSession(m, "10.4.0.12", StateDown, time.Time{})
		m.installed[routeKeyFor(sess.peer.Interface, sess.route)] = tc.installed

		t0 := time.Now()
		m.checkRouteAction(t0)
		require.Equal(t, tc.want, checkAt(m, t0.Add(routeActionGrace)), "installed[rk] = %v", tc.installed)
	}
}

func TestClient_Liveness_RouteAction_WarnRepeatsAndClears(t *testing.T) {
	t.Parallel()

	m, logs := newRouteActionTestManager(false)
	sess := addRouteActionTestSession(m, "10.4.0.11", StateDown, time.Time{})
	const (
		inbound  = "liveness: missing peer routes: this host receives no liveness probes from peers; check that inbound UDP 44880 is allowed on doublezero0"
		outbound = "liveness: missing peer routes: peers receive no liveness probes from this host; check that outbound UDP 44880 is allowed on doublezero0"
		cleared  = "liveness: missing peer routes cleared"
	)

	t0 := time.Now()
	m.checkRouteAction(t0)
	require.Empty(t, logs.find(slog.LevelWarn, inbound), "no WARN while starting")
	m.checkRouteAction(t0.Add(routeActionGrace))
	m.checkRouteAction(t0.Add(routeActionGrace + routeActionWarnEvery - time.Second))
	warns := logs.find(slog.LevelWarn, inbound)
	require.Len(t, warns, 1, "one WARN until the interval passes")
	require.Equal(t, "inbound", recordAttr(warns[0], "direction"))
	require.Equal(t, "1", recordAttr(warns[0], "peers"))
	require.Equal(t, DocsMissingPeerRoutes, recordAttr(warns[0], "docs"))

	m.checkRouteAction(t0.Add(routeActionGrace + routeActionWarnEvery))
	require.Len(t, logs.find(slog.LevelWarn, inbound), 2, "WARN repeats on the interval")

	probe := t0.Add(routeActionGrace + routeActionWarnEvery)
	sess.mu.Lock()
	sess.lastRx = probe
	sess.mu.Unlock()
	m.checkRouteAction(probe.Add(time.Second))
	require.Len(t, logs.find(slog.LevelWarn, outbound), 1, "a new direction is logged at once")
	m.checkRouteAction(probe.Add(routeActionGrace + time.Second))
	require.Len(t, logs.find(slog.LevelWarn, inbound), 3, "the probe ages out, so inbound returns")

	sess.mu.Lock()
	sess.state = StateUp
	sess.mu.Unlock()
	m.checkRouteAction(t0.Add(time.Hour))
	m.checkRouteAction(t0.Add(time.Hour + time.Minute))
	require.Len(t, logs.find(slog.LevelInfo, cleared), 1)
	require.Len(t, logs.find(slog.LevelWarn, inbound), 3)
	require.Len(t, logs.find(slog.LevelWarn, outbound), 1)
}

// A status read reports the last check and runs no rule of its own.
func TestClient_Liveness_RouteAction_ReadReportsLastCheck(t *testing.T) {
	t.Parallel()

	m, logs := newRouteActionTestManager(false)
	sess := addRouteActionTestSession(m, "10.4.0.11", StateDown, time.Time{})

	require.Equal(t, RouteActionNone, m.RouteAction())
	require.True(t, m.zeroUpSince.IsZero(), "a read must not start the grace period")

	t0 := time.Now()
	m.checkRouteAction(t0)
	m.checkRouteAction(t0.Add(routeActionGrace))
	sess.mu.Lock()
	sess.state = StateUp
	sess.mu.Unlock()
	require.Equal(t, RouteActionInbound, m.RouteAction(), "a read reports the last check until the next one")
	require.Empty(t, logs.find(slog.LevelInfo, "liveness: missing peer routes cleared"), "a read must not log")
}

// The grace period starts again on reconnect, even one faster than a check.
func TestClient_Liveness_RouteAction_WithdrawingLastRouteRestartsGrace(t *testing.T) {
	t.Parallel()

	m, err := newTestManager(t, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(nil)
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))
	t0 := time.Now()
	m.checkRouteAction(t0)
	require.NoError(t, m.WithdrawRoute(r, "lo"))
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))

	require.Equal(t, RouteActionStarting, checkAt(m, t0.Add(routeActionGrace)))
	require.Equal(t, RouteActionInbound, checkAt(m, t0.Add(2*routeActionGrace)))
}

// A peer that never hears us sends Down, which knocks our session from Init back to
// Down, so outbound must not depend on catching the session in Init.
func TestClient_Liveness_RouteAction_OutboundAfterDeafPeerResetsInit(t *testing.T) {
	t.Parallel()

	m, err := newTestManager(t, func(cfg *ManagerConfig) {
		cfg.TxMin, cfg.RxMin, cfg.MaxTxCeil = time.Second, time.Second, 3*time.Second
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	r := newTestRoute(nil)
	require.NoError(t, m.RegisterRoute(r, "lo", m.LocalAddr().Port))
	peer := Peer{Interface: "lo", LocalIP: r.Src.String(), PeerIP: r.Dst.IP.String()}
	sess, ok := m.GetSession(peer)
	require.True(t, ok)
	t0 := time.Now()
	m.checkRouteAction(t0.Add(-time.Second))

	deaf := &ControlPacket{State: StateDown, LocalDiscr: 99}
	m.HandleRx(deaf, peer)
	require.Equal(t, StateInit, sess.GetState())
	m.HandleRx(deaf, peer)
	require.Equal(t, StateDown, sess.GetState())
	require.Equal(t, DownReasonRemoteDown, sess.Snapshot().LastDownReason)

	require.Equal(t, RouteActionOutbound, checkAt(m, t0.Add(routeActionGrace-time.Second)))
}
