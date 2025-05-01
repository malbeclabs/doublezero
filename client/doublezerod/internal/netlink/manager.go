package netlink

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"syscall"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
)

type Netlinker interface {
	TunnelAdd(*Tunnel) error
	TunnelDelete(*Tunnel) error
	TunnelAddrAdd(*Tunnel, string) error
	TunnelUp(*Tunnel) error
	RouteAdd(*Route) error
	RouteDelete(*Route) error
	RouteGet(net.IP) ([]*Route, error)
	RuleAdd(*IPRule) error
	RuleDel(*IPRule) error
	RouteByDoubleZeroProtocol(int) ([]*Route, error)
}

// I can't think of a better name for this
type BgpReaderWriter interface {
	Serve([]net.Listener) error
	AddPeer(*bgp.PeerConfig, []bgp.NLRI) error
	DeletePeer(net.IP) error
	AddRoute() <-chan bgp.NLRI
	WithdrawRoute() <-chan bgp.NLRI
	FlushRoutes() <-chan struct{}
	GetPeerStatus(net.IP) bgp.Session
}

type DbReaderWriter interface {
	GetState() *ProvisionRequest
	DeleteState() error
	SaveState(p *ProvisionRequest) error
}

type NetlinkManager struct {
	netlink         Netlinker
	Routes          []*Route
	Rules           []*IPRule
	UnicastTunnel   *Tunnel
	MulticastTunnel *Tunnel
	DoubleZeroAddr  net.IP
	bgp             BgpReaderWriter
	db              DbReaderWriter
}

func NewNetlinkManager(netlink Netlinker, bgp BgpReaderWriter, db DbReaderWriter) *NetlinkManager {
	manager := &NetlinkManager{netlink: netlink, bgp: bgp, db: db}
	return manager
}

// provisionIBRL handles the provisioning of a user IBRL connection. This supports
// both IP reuse and DoubleZero allocated IP use cases.
func (n *NetlinkManager) provisionIBRL(p ProvisionRequest) error {
	tun, err := NewTunnel(p.TunnelSrc, p.TunnelDst, p.TunnelNet.String())
	if err != nil {
		return fmt.Errorf("error generating new tunnel: %v", err)
	}

	switch p.UserType {
	// IBRL mode re-uses the user's public address so we don't need to bind
	// the doublezero IP to the tunnel interface.
	case UserTypeIBRL:
		err = n.CreateTunnel(tun)
	// If we allocate the user an IP in IBRL mode, we need to bind it to the
	// tunnel interface.
	case UserTypeIBRLWithAllocatedIP:
		err = n.CreateTunnelWithIP(tun, p.DoubleZeroIP)
	default:
		return fmt.Errorf("unsupported tunnel type: %v\n", p)
	}

	if err != nil {
		return fmt.Errorf("error creating tunnel interface: %v", err)
	}

	n.UnicastTunnel = tun
	n.DoubleZeroAddr = p.DoubleZeroIP

	peer := &bgp.PeerConfig{
		RemoteAddress: n.UnicastTunnel.RemoteOverlay,
		LocalAddress:  n.UnicastTunnel.LocalOverlay,
		LocalAs:       p.BgpLocalAsn,
		RemoteAs:      p.BgpRemoteAsn,
		RouteTable:    syscall.RT_TABLE_MAIN,
	}
	nlri, err := bgp.NewNLRI([]uint32{peer.LocalAs}, n.UnicastTunnel.LocalOverlay.String(), p.DoubleZeroIP.String(), 32)
	if err != nil {
		return fmt.Errorf("error generating bgp nlri: %v", err)
	}
	err = n.bgp.AddPeer(peer, []bgp.NLRI{nlri})
	if err != nil {
		if errors.Is(err, bgp.ErrBgpPeerExists) {
			slog.Error("bgp not added", "peer local address", peer.RemoteAddress, "error", err)
		} else {
			return fmt.Errorf("error adding peer: %v", err)
		}
	}
	return nil
}

// provisionEdgeFiltering handles the provisioning of a user edge filtering connection.
func (n *NetlinkManager) provisionEdgeFiltering(p ProvisionRequest) (err error) {
	// TODO: have NewTunnel take a net.IPNet
	tun, err := NewTunnel(p.TunnelSrc, p.TunnelDst, p.TunnelNet.String())
	if err != nil {
		return fmt.Errorf("error generating new tunnel: %v", err)
	}

	err = n.CreateTunnelWithIP(tun, p.DoubleZeroIP)
	if err != nil {
		return fmt.Errorf("error creating tunnel interface: %v", err)
	}
	n.UnicastTunnel = tun
	n.DoubleZeroAddr = p.DoubleZeroIP

	// Apply IP Rules
	slog.Info("rules: creating ip rules")
	if err = n.CreateIPRules(p.DoubleZeroPrefixes); err != nil {
		return fmt.Errorf("error creating IP rules: %v", err)
	}

	slog.Info("routes: adding ip routes")
	if err = n.CreateDefaultRoutingTable(); err != nil {
		return fmt.Errorf("error creating routing tables: %v", err)
	}

	peer := &bgp.PeerConfig{
		RemoteAddress: n.UnicastTunnel.RemoteOverlay,
		LocalAddress:  n.UnicastTunnel.LocalOverlay,
		LocalAs:       p.BgpLocalAsn,
		RemoteAs:      p.BgpRemoteAsn,
		RouteTable:    RouteTableSpecific,
	}
	nlri, err := bgp.NewNLRI([]uint32{peer.LocalAs}, n.UnicastTunnel.LocalOverlay.String(), p.DoubleZeroIP.String(), 32)
	if err != nil {
		return fmt.Errorf("error generating bgp nlri: %v", err)
	}
	err = n.bgp.AddPeer(peer, []bgp.NLRI{nlri})
	if err != nil {
		if errors.Is(err, bgp.ErrBgpPeerExists) {
			slog.Error("bgp not added", "peer local address", peer.RemoteAddress, "error", err)
		} else {
			return fmt.Errorf("error adding peer: %v", err)
		}
	}

	return nil
}

// provisionMulticast handles the provisioning of a user multicast connection.
func (n *NetlinkManager) provisionMulticast(_ ProvisionRequest) error {
	return fmt.Errorf("multicast mode is unimplemented")
}

// Provision is the entry point for all user tunnel provisioning. This currently
// contains logic for IBRL, edge filtering and multicast use cases. After the user
// tunnel is provisioned, the original request is saved to disk so we're able to
// handle service restarts.
func (n *NetlinkManager) Provision(p ProvisionRequest) (err error) {
	if p.TunnelSrc == nil {
		if p.TunnelSrc, err = n.DiscoverTunnelSource(p.TunnelDst); err != nil {
			return fmt.Errorf("tunnel: error while finding tunnel source: %v", err)
		}
	}
	if p.TunnelSrc == nil {
		return fmt.Errorf("tunnel: no tunnel src addr specified or could be discovered")
	}

	if p.DoubleZeroIP == nil {
		return fmt.Errorf("tunnel: no doublezero address specified")
	}

	switch p.UserType {
	case UserTypeIBRL, UserTypeIBRLWithAllocatedIP:
		err = n.provisionIBRL(p)
	case UserTypeEdgeFiltering:
		err = n.provisionEdgeFiltering(p)
	case UserTypeMulticast:
		err = n.provisionMulticast(p)
	default:
		return fmt.Errorf("unsupported user type: %s", p.UserType)
	}
	if err != nil {
		return err
	}

	// finally store latest provisioned state
	// TODO: this needs to be updated to support multiple provisioning requests when multicast is added
	if err = n.db.SaveState(&p); err != nil {
		return fmt.Errorf("error saving state: %v", err)
	}

	return nil
}

func (n *NetlinkManager) Remove() error {
	// We've never been provisioned
	if n.db.GetState() == nil {
		return nil
	}

	// Remove rules, routes, and tunnel
	if err := n.Close(); err != nil {
		return err
	}
	// Delete state so we don't reprovision ourselves on restart
	if err := n.db.DeleteState(); err != nil {
		return fmt.Errorf("db: error deleting state file: %v", err)
	}
	return nil
}

// DiscoverTunnelSource attempts to discover the correct local address to use as the tunnel source
// based on the tunnel destination. It uses the kernel routing table to lookup the route the kernel
// would use to the tunnel destination and uses the address that would be chosen by kernel source address
// selection.
func (n *NetlinkManager) DiscoverTunnelSource(tunnelDst net.IP) (net.IP, error) {
	routes, err := n.netlink.RouteGet(tunnelDst)
	if err != nil {
		return nil, fmt.Errorf("error fetching route for dest %s: %v", tunnelDst.String(), err)
	}
	if len(routes) == 0 {
		return nil, fmt.Errorf("tunnel: no tunnel src address could be found based on default route")
	}

	// TODO: debug this log
	log.Printf("tunnel: %d routes to tunnel dest found\n", len(routes))
	for _, route := range routes {
		if route.Src != nil {
			// TODO: debug this log
			slog.Info("tunnel: using route to derive tunnel src address", "route", route)
			return route.Src, nil
		}
	}
	return nil, nil
}

// createBaseTunnel creates a tunnel interface, adds overlay addressing and brings up the interface.
func (n *NetlinkManager) createBaseTunnel(tun *Tunnel) error {
	if tun.LocalOverlay == nil {
		return fmt.Errorf("missing tunnel local overlay addressing")
	}

	err := n.netlink.TunnelAdd(tun)
	if err != nil {
		if errors.Is(err, ErrTunnelExists) {
			slog.Error("tunnel: tunnel already exists", "tunnel", tun.Name)
		} else {
			return fmt.Errorf("tunnel: could not add tunnel interface: %v", err)
		}
	}

	// TODO: debug this log
	slog.Info("tunnel: adding address to tunnel interface", "address", tun.LocalOverlay)
	err = n.netlink.TunnelAddrAdd(tun, tun.LocalOverlay.String()+"/31")
	if err != nil {
		if errors.Is(err, ErrAddressExists) {
			slog.Error("tunnel: address already present on tunnel")
		} else {
			return fmt.Errorf("error adding addressing to tunnel: %v", err)
		}
	}

	slog.Info("tunnel: bringing up tunnel interface")
	if err = n.netlink.TunnelUp(tun); err != nil {
		return fmt.Errorf("tunnel: error bring up tunnel interface: %v", err)
	}

	return nil
}

// CreateTunnel creates the tunnel interface, adds point to point addressing and brings the interface
// up.
func (n *NetlinkManager) CreateTunnel(tun *Tunnel) error {
	return n.createBaseTunnel(tun)
}

// CreateTunnelWithIP creates the tunnel interface, adds point-to-point addressing, binds the doublezero IP
// to the interface and brings the tunnel up.
func (n *NetlinkManager) CreateTunnelWithIP(tun *Tunnel, dzIp net.IP) (err error) {
	if err := n.createBaseTunnel(tun); err != nil {
		return fmt.Errorf("error creating base tunnel: %v", err)
	}

	slog.Info("tunnel: adding dz address to tunnel interface", "dz address", dzIp.String()+"/32")
	err = n.netlink.TunnelAddrAdd(tun, dzIp.String()+"/32")
	if err != nil {
		if errors.Is(err, ErrAddressExists) {
			slog.Error("tunnel: address already present on tunnel")
		} else {
			return fmt.Errorf("error adding addressing to tunnel: %v", err)
		}
	}

	return nil
}

func (n *NetlinkManager) RemoveTunnel() error {
	if n.UnicastTunnel == nil {
		return nil
	}
	if err := n.netlink.TunnelDelete(n.UnicastTunnel); err != nil {
		return fmt.Errorf("tunnel: error while deleting tunnel: %v", err)
	}
	n.UnicastTunnel = nil
	return nil
}

func (n *NetlinkManager) WriteRoute(r *Route) error {
	return n.netlink.RouteAdd(r)
}

func (n *NetlinkManager) RemoveRoute(r *Route) error {
	return n.netlink.RouteDelete(r)
}

func (n *NetlinkManager) CreateIPRules(prefixes []*net.IPNet) error {
	rules := []*IPRule{}

	for _, prefix := range prefixes {
		// dz-specifics table
		rule, err := NewIPRule(100, dzTableSpecific, "0.0.0.0/0", prefix.String())
		if err != nil {
			return fmt.Errorf("rules: error creating IP rule: %v", err)
		}
		rules = append(rules, rule)
		// dz-default table - anything sourced from dz space can't go out the public interface
		rule, err = NewIPRule(101, dzTableDefault, prefix.String(), "0.0.0.0/0")
		if err != nil {
			return fmt.Errorf("rules: error creating IP rule: %v", err)
		}
		rules = append(rules, rule)
	}

	for _, rule := range rules {
		slog.Info("rules: applying the following rule", "rule", rule)
		err := n.netlink.RuleAdd(rule)
		if err != nil {
			if errors.Is(err, ErrRuleExists) {
				slog.Error("rules: rule already exists", "rule", rule)
				continue
			}
			return fmt.Errorf("error adding IP rules: %v", err)
		}
	}
	n.Rules = rules
	return nil
}

func (n *NetlinkManager) CreateDefaultRoutingTable() error {
	_, defaultRt, err := net.ParseCIDR("0.0.0.0/0")
	if err != nil {
		return fmt.Errorf("routes: unable to parse default route: %v", err)
	}
	routes := []*Route{
		{Dst: defaultRt, Src: n.DoubleZeroAddr, Table: RouteTableDefault, NextHop: n.UnicastTunnel.RemoteOverlay},
	}
	for _, route := range routes {
		if err := n.netlink.RouteAdd(route); err != nil {
			return fmt.Errorf("routes: error adding route: %s", route)
		}
	}
	n.Routes = routes
	return nil
}

func (n *NetlinkManager) FlushRules() error {
	var err error
	for _, rule := range n.Rules {
		if err = n.netlink.RuleDel(rule); err != nil {
			err = errors.Join(err, fmt.Errorf("error deleting rule %s: %v", rule, err))
		}
	}
	n.Rules = []*IPRule{}
	return err
}

func (n *NetlinkManager) FlushRoutes() error {
	var err error
	for _, route := range n.Routes {
		if err = n.netlink.RouteDelete(route); err != nil {
			err = errors.Join(err, fmt.Errorf("error deleting route %s: %v", route, err))
		}
	}
	n.Routes = []*Route{}
	return err
}

func (n *NetlinkManager) Close() error {
	var errFlushRules, errFlushRoutes, errRemoveTunnel, errRemovePeer error

	slog.Info("teardown: flushing rules")
	if err := n.FlushRules(); err != nil {
		errFlushRules = fmt.Errorf("error flushing rules: %v", err)
	}

	slog.Info("teardown: flushing routes")
	if err := n.FlushRoutes(); err != nil {
		errFlushRoutes = fmt.Errorf("error flushing routes: %v", err)
	}

	if n.UnicastTunnel == nil {
		return nil
	}

	err := n.bgp.DeletePeer(n.UnicastTunnel.RemoteOverlay)
	if errors.Is(err, bgp.ErrBgpPeerNotExists) {
		slog.Error("bgp: peer does not exist", "peer tunnel", n.UnicastTunnel.RemoteOverlay)
	} else if err != nil {
		errRemovePeer = fmt.Errorf("bgp: error while deleting peer: %v", err)
	}

	slog.Info("teardown: removing tunnel interface")
	if err := n.RemoveTunnel(); err != nil {
		errRemoveTunnel = fmt.Errorf("error removing tunnel interface: %v", err)
	}

	return errors.Join(errFlushRules, errFlushRoutes, errRemoveTunnel, errRemovePeer)
}

func (n *NetlinkManager) Serve(ctx context.Context) error {
	errCh := make(chan error)
	slog.Info("bgp: starting bgp fsm")

	go func() {
		err := n.bgp.Serve([]net.Listener{})
		errCh <- err
	}()

	slog.Info("routes: starting netlink writer thread")
	go func() {
		for {
			select {
			case <-ctx.Done():
				slog.Info("exiting netlink writer thread")
				return
			// TODO: implement some batching logic for writes via netlink
			// TODO: pull table number from NLRI
			case p := <-n.bgp.AddRoute():
				_, dzNet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", p.Prefix, p.PrefixLength))
				if err != nil {
					slog.Error("routes: error parsing nlri from update", "error", err)
				}

				route := &Route{Src: n.DoubleZeroAddr, Dst: dzNet, Table: p.RouteTable, NextHop: net.ParseIP(p.NextHop), Protocol: 186}
				slog.Info("routes: writing route", "table", p.RouteTable, "dz route", route.String())
				if err := n.WriteRoute(route); err != nil {
					slog.Error("routes: error writing route", "table", p.RouteTable, "error", err)
				}
			// TODO: pull table number from NLRI
			case p := <-n.bgp.WithdrawRoute():
				_, dzNet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", p.Prefix, p.PrefixLength))
				if err != nil {
					slog.Error("routes: error parsing nlri from update", "error", err)
				}

				route := &Route{Src: n.DoubleZeroAddr, Dst: dzNet, Table: p.RouteTable, NextHop: net.ParseIP(p.NextHop)}
				slog.Info("routes: removing route from table", "table", p.RouteTable, "dz route", route.String())

				if err := n.RemoveRoute(route); err != nil {
					slog.Error("routes: error removing route", "route", route.Dst.String(), "table", RouteTableSpecific, "error", err)
				}
			case <-n.bgp.FlushRoutes():
				if n.db.GetState().UserType != UserTypeIBRL {
					continue
				}

				// protocol 186 is bgp
				protocol := 186
				routes, err := n.netlink.RouteByDoubleZeroProtocol(protocol)
				if err != nil {
					slog.Error("routes: error getting routes by protocol", "protocol", protocol)
				}
				for _, route := range routes {
					if err := n.netlink.RouteDelete(route); err != nil {
						slog.Error("Error deleting route", "route", route)
						continue
					}
				}

			}
		}
	}()

	// attempt to recover from last provisioned state
	if err := n.Recover(); err != nil {
		slog.Error("netlink: error recovering provisioned state", "error", err)
	}

	select {
	case <-ctx.Done():
		slog.Info("teardown: closing server")
		return nil
	case err := <-errCh:
		return fmt.Errorf("netlink: error from manager: %v", err)
	}
}

func (n *NetlinkManager) Recover() error {
	// check last provisioned state and attempt to recover
	state := n.db.GetState()
	if state == nil {
		return nil
	}
	slog.Info("netlink: restoring previous provisioned state")
	return n.Provision(*state)
}

func (n *NetlinkManager) Status() (*StatusResponse, error) {
	state := n.db.GetState()
	if state == nil {
		return nil, nil
	}

	if n.UnicastTunnel == nil {
		return nil, fmt.Errorf("netlink: saved state is not programmed into client")
	}

	peerStatus := n.bgp.GetPeerStatus(n.UnicastTunnel.RemoteOverlay)
	return &StatusResponse{
		TunnelName:       n.UnicastTunnel.Name,
		TunnelSrc:        n.UnicastTunnel.LocalUnderlay,
		TunnelDst:        n.UnicastTunnel.RemoteUnderlay,
		DoubleZeroIP:     n.DoubleZeroAddr,
		DoubleZeroStatus: peerStatus,
	}, nil
}
