package liveness

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type RouteManager interface {
	RouteAdd(route *routing.Route) error
	RouteDelete(route *routing.Route) error
	RouteByProtocol(protocol int) ([]*routing.Route, error)
	PeerOnEstablished() error
	PeerOnClose() error
}

type Config struct {
	Logger  *slog.Logger
	Netlink routing.Netlinker

	Iface  string
	BindIP string
	Port   int

	TxMin      time.Duration
	RxMin      time.Duration
	DetectMult uint8
}

func (c *Config) Validate() error {
	if c.Logger == nil {
		return errors.New("logger is required")
	}
	if c.Netlink == nil {
		return errors.New("netlink is required")
	}
	if c.Iface == "" {
		return errors.New("iface is required")
	}
	if c.BindIP == "" {
		return errors.New("bindIP is required")
	}
	if c.Port <= 0 {
		return errors.New("port must be greater than 0")
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
	return nil
}

type RouteKey struct {
	Iface     string
	SrcIP     string
	Table     int
	DstPrefix string
	NextHop   string
}

type LivenessAwareRouteManager struct {
	ctx    context.Context
	cancel context.CancelFunc

	log *slog.Logger
	cfg *Config

	lm *Manager // the BFD-lite manager

	mu        sync.Mutex
	desired   map[RouteKey]*routing.Route // routes we want
	installed map[RouteKey]bool           // routes actually in kernel
}

func NewLivenessAwareRouteManager(ctx context.Context, cfg *Config) (*LivenessAwareRouteManager, error) {
	lm, err := NewManager(ctx, cfg.Logger, cfg.Iface, cfg.BindIP, cfg.Port)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	m := &LivenessAwareRouteManager{
		ctx:       ctx,
		cancel:    cancel,
		log:       cfg.Logger,
		cfg:       cfg,
		lm:        lm,
		desired:   make(map[RouteKey]*routing.Route),
		installed: make(map[RouteKey]bool),
	}
	lm.onUp = m.onSessionUp
	lm.onDown = m.onSessionDown
	return m, nil
}

func (m *LivenessAwareRouteManager) Close() error { m.cancel(); return m.lm.Close() }

func (m *LivenessAwareRouteManager) RouteAdd(r *routing.Route) error {
	if r.Dst.IP.String() == "8.8.8.8" {
		return m.cfg.Netlink.RouteAdd(r)
	}

	k := routeKeyFor(m.cfg.Iface, r)
	m.mu.Lock()
	m.desired[k] = r
	m.mu.Unlock()

	m.log.Info("liveness: adding route", "route", r.String(), "desired", m.desired, "installed", m.installed, "iface", m.cfg.Iface)

	// TODO(snormore): Note this is the the expected peer port, which we are overloading with our
	// own port. Maybe that's fine, maybe it should be configured separately.
	peerAddr, err := net.ResolveUDPAddr("udp", peerAddrFor(r, m.cfg.Port))
	if err != nil {
		return fmt.Errorf("error resolving peer address: %v", err)
	}

	_, err = m.lm.RegisterRoute(r, peerAddr, m.cfg.Iface, m.cfg.TxMin, m.cfg.RxMin, m.cfg.DetectMult)
	return err
}

func (m *LivenessAwareRouteManager) RouteDelete(r *routing.Route) error {
	if r.Dst.IP.String() == "8.8.8.8" {
		return m.cfg.Netlink.RouteDelete(r)
	}

	k := routeKeyFor(m.cfg.Iface, r)
	m.mu.Lock()
	delete(m.desired, k)
	wasInstalled := m.installed[k]
	delete(m.installed, k)
	m.mu.Unlock()

	m.log.Info("liveness: deleting route", "route", r.String(), "desired", m.desired, "installed", m.installed)

	m.lm.WithdrawRoute(r, m.cfg.Iface)
	if wasInstalled {
		return m.cfg.Netlink.RouteDelete(r)
	}
	return nil
}

func (m *LivenessAwareRouteManager) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*routing.Route, 0, len(m.installed))
	for k, ok := range m.installed {
		if ok {
			if r := m.desired[k]; r != nil {
				out = append(out, r)
			}
		}
	}
	return out, nil
}

func (m *LivenessAwareRouteManager) PeerOnEstablished() error {
	m.log.Info("liveness: bgp peer on established", "desired", len(m.desired), "installed", len(m.installed))
	m.lm.PollAll()
	return nil
}
func (m *LivenessAwareRouteManager) PeerOnClose() error {
	m.log.Info("liveness: bgp peer on close", "desired", len(m.desired), "installed", len(m.installed))
	m.mu.Lock()
	var toDel []*routing.Route
	for k, r := range m.desired {
		if m.installed[k] {
			toDel = append(toDel, r)
		}
	}
	m.mu.Unlock()
	for _, r := range toDel {
		_ = m.cfg.Netlink.RouteDelete(r)
	}
	m.lm.AdminDownAll()
	return nil
}

func (m *LivenessAwareRouteManager) onSessionUp(s *Session) {
	rk := routeKeyFor(s.peer.iface, s.route)
	m.mu.Lock()
	r := m.desired[rk]
	if r == nil || m.installed[rk] {
		m.mu.Unlock()
		return
	}
	m.installed[rk] = true
	m.mu.Unlock()
	_ = m.cfg.Netlink.RouteAdd(r)
}

func (m *LivenessAwareRouteManager) onSessionDown(s *Session) {
	rk := routeKeyFor(s.peer.iface, s.route)
	m.mu.Lock()
	r := m.desired[rk]
	was := m.installed[rk]
	m.installed[rk] = false
	m.mu.Unlock()
	if was && r != nil {
		_ = m.cfg.Netlink.RouteDelete(r)
	}
}

func routeKeyFor(iface string, r *routing.Route) RouteKey {
	return RouteKey{Iface: iface, SrcIP: r.Src.String(), Table: r.Table, DstPrefix: r.Dst.String(), NextHop: r.NextHop.String()}
}

func peerAddrFor(r *routing.Route, port int) string {
	return fmt.Sprintf("%s:%d", r.Dst.IP.String(), port)
}
