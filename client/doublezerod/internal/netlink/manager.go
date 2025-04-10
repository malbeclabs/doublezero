package netlink

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"

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
}

// I can't think of a better name for this
type BgpReaderWriter interface {
	Serve([]net.Listener) error
	AddPeer(*bgp.PeerConfig, []bgp.NLRI) error
	DeletePeer(net.IP) error
	AddRoute() <-chan bgp.NLRI
	WithdrawRoute() <-chan bgp.NLRI
}

type DbReaderWriter interface {
	GetState() *ProvisionRequest
	DeleteState() error
	SaveState(p *ProvisionRequest) error
}

type NetlinkManager struct {
	netlink        Netlinker
	Routes         []*Route
	Rules          []*IPRule
	Tunnel         *Tunnel
	DoubleZeroAddr net.IP
	bgp            BgpReaderWriter
	db             DbReaderWriter
}

func NewNetlinkManager(netlink Netlinker, bgp BgpReaderWriter, db DbReaderWriter) *NetlinkManager {
	manager := &NetlinkManager{netlink: netlink, bgp: bgp, db: db}
	return manager
}

func (n *NetlinkManager) Provision(p ProvisionRequest) error {
	var err error
	if p.TunnelSrc == nil {
		if p.TunnelSrc, err = n.DiscoverTunnelSource(p.TunnelDst); err != nil {
			return fmt.Errorf("tunnel: error while finding tunnel source: %v", err)
		}
	}
	if p.TunnelSrc == nil {
		return fmt.Errorf("tunnel: no tunnel src addr specified or could be discovered")
	}

	// TODO: have NewTunnel take a net.IPNet
	gre, err := NewTunnel(p.TunnelSrc, p.TunnelDst, p.TunnelNet.String())
	if err != nil {
		return fmt.Errorf("error generating new tunnel: %v", err)
	}

	// TODO: CreateTunnel should take a net.IP
	err = n.CreateTunnel(gre, p.DoubleZeroIP.String())
	if err != nil {
		return fmt.Errorf("error creating tunnel interface: %v", err)
	}

	// Apply IP Rules
	log.Println("rules: creating ip rules")
	if err = n.CreateIPRules(p.DoubleZeroPrefixes); err != nil {
		return fmt.Errorf("error creating IP rules: %v", err)
	}

	log.Println("routes: adding ip routes")
	if err = n.CreateRoutingTables(); err != nil {
		return fmt.Errorf("error creating routing tables: %v", err)
	}

	peer := &bgp.PeerConfig{
		RemoteAddress: n.Tunnel.RemoteOverlay,
		LocalAddress:  n.Tunnel.LocalOverlay,
		LocalAs:       p.BgpLocalAsn,
		RemoteAs:      p.BgpRemoteAsn,
	}
	nlri, err := bgp.NewNLRI([]uint32{peer.LocalAs}, n.Tunnel.LocalOverlay.String(), p.DoubleZeroIP.String(), 32)
	if err != nil {
		return fmt.Errorf("error generating bgp nlri: %v", err)
	}
	err = n.bgp.AddPeer(peer, []bgp.NLRI{nlri})
	if err != nil {
		if errors.Is(err, bgp.ErrBgpPeerExists) {
			log.Printf("bgp: %s not added: %v", peer.LocalAddress, err)
		} else {
			return fmt.Errorf("error adding peer: %v", err)
		}
	}

	// finally store latest provisioned state
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
	// Since we need to keep running, delete the bgp peer
	err := n.bgp.DeletePeer(n.Tunnel.RemoteOverlay)
	if errors.Is(err, bgp.ErrBgpPeerNotExists) {
		log.Printf("bgp: peer %s does not exist", n.Tunnel.RemoteOverlay)
	} else if err != nil {
		return fmt.Errorf("bgp: error while deleting peer: %v", err)
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
			log.Printf("tunnel: using route %s to derive tunnel src address\n", route)
			return route.Src, nil
		}
	}
	return nil, nil
}

// CreateTunnel creates the tunnel interface, adds the interface addressing and brings the interface up.
func (n *NetlinkManager) CreateTunnel(tun *Tunnel, dzIp string) error {
	if tun.LocalOverlay == nil {
		return fmt.Errorf("missing tunnel local overlay addressing")
	}
	if dzIp == "" || net.ParseIP(dzIp) == nil {
		return fmt.Errorf("invalid doublezero host")
	}

	err := n.netlink.TunnelAdd(tun)
	if err != nil {
		// if tunnel interface exists, it could be recovering from a crash so continue
		if errors.Is(err, ErrTunnelExists) {
			log.Printf("tunnel: tunnel %s already exists\n", tun.Name)
		} else {
			return fmt.Errorf("tunnel: could not add tunnel interface: %v", err)
		}
	}

	// TODO: debug this log
	log.Printf("tunnel: adding address %s to tunnel interface", tun.LocalOverlay)
	err = n.netlink.TunnelAddrAdd(tun, tun.LocalOverlay.String()+"/31")
	if err != nil {
		if errors.Is(err, ErrAddressExists) {
			log.Printf("tunnel: address already present on tunnel")
		} else {
			return fmt.Errorf("error adding addressing to tunnel: %v", err)
		}
	}

	// TODO: temp add of dz client address to tunnel interface; this should probably
	// be on a loopback.
	log.Printf("tunnel: adding dz address %s to tunnel interface", dzIp+"/32")
	err = n.netlink.TunnelAddrAdd(tun, dzIp+"/32")
	if err != nil {
		if errors.Is(err, ErrAddressExists) {
			log.Printf("tunnel: address already present on tunnel")
		} else {
			return fmt.Errorf("error adding addressing to tunnel: %v", err)
		}
	}

	log.Println("tunnel: bringing up tunnel interface")
	if err = n.netlink.TunnelUp(tun); err != nil {
		log.Fatalf("tunnel: error bring up tunnel interface: %v", err)
	}
	n.Tunnel = tun
	// TODO: probably shouldn't be here
	n.DoubleZeroAddr = net.ParseIP(dzIp)
	return nil
}

func (n *NetlinkManager) RemoveTunnel() error {
	if n.Tunnel == nil {
		return nil
	}
	if err := n.netlink.TunnelDelete(n.Tunnel); err != nil {
		return fmt.Errorf("tunnel: error while deleting tunnel: %v", err)
	}
	n.Tunnel = nil
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
		log.Printf("rules: applying the following rule: %s", rule)
		err := n.netlink.RuleAdd(rule)
		if err != nil {
			if errors.Is(err, ErrRuleExists) {
				log.Printf("rules: rule %s already exists", rule)
				continue
			}
			return fmt.Errorf("error adding IP rules: %v", err)
		}
	}
	n.Rules = rules
	return nil
}

func (n *NetlinkManager) CreateRoutingTables() error {
	_, defaultRt, err := net.ParseCIDR("0.0.0.0/0")
	if err != nil {
		return fmt.Errorf("routes: unable to parse default route: %v", err)
	}
	routes := []*Route{
		{Dst: defaultRt, Src: n.DoubleZeroAddr, Table: dzTableDefault, NextHop: n.Tunnel.RemoteOverlay},
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
	var errFlushRules, errFlushRoutes, errRemoveTunnel error

	log.Println("teardown: flushing rules")
	if err := n.FlushRules(); err != nil {
		errFlushRules = fmt.Errorf("error flushing rules: %v", err)
	}

	log.Println("teardown: flushing routes")
	if err := n.FlushRoutes(); err != nil {
		errFlushRoutes = fmt.Errorf("error flushing routes: %v", err)
	}

	log.Println("teardown: removing tunnel interface")
	if err := n.RemoveTunnel(); err != nil {
		errRemoveTunnel = fmt.Errorf("error removing tunnel interface: %v", err)
	}

	return errors.Join(errFlushRules, errFlushRoutes, errRemoveTunnel)
}

func (n *NetlinkManager) Serve(ctx context.Context) error {
	errCh := make(chan error)
	log.Println("bgp: starting bgp fsm")

	go func() {
		err := n.bgp.Serve([]net.Listener{})
		errCh <- err
	}()

	log.Println("routes: starting netlink writer thread")
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Println("exiting netlink writer thread")
				return
			// TODO: implement some batching logic for writes via netlink
			case p := <-n.bgp.AddRoute():
				_, dzNet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", p.Prefix, p.PrefixLength))
				if err != nil {
					log.Printf("routes: error parsing nlri from update: %v", err)
				}
				route := &Route{Src: n.DoubleZeroAddr, Dst: dzNet, Table: RouteTableSpecific, NextHop: net.ParseIP(p.NextHop)}
				log.Printf("routes: writing route to table %d (dz-specific): %s", RouteTableSpecific, route.String())
				if err := n.WriteRoute(route); err != nil {
					log.Printf("routes: error writing route to table %d (dz-specific): %v", RouteTableSpecific, err)
				}
			case p := <-n.bgp.WithdrawRoute():
				_, dzNet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", p.Prefix, p.PrefixLength))
				if err != nil {
					log.Printf("routes: error parsing nlri from update: %v", err)
				}
				route := &Route{Src: n.DoubleZeroAddr, Dst: dzNet, Table: RouteTableSpecific, NextHop: net.ParseIP(p.NextHop)}
				log.Printf("routes: removing route from table %d (dz-specific): %s", RouteTableSpecific, route.String())
				if err := n.RemoveRoute(route); err != nil {
					log.Printf("routes: error removing route %s from table %d (dz-specific): %v", route.Dst.String(), RouteTableSpecific, err)
				}
			}
		}
	}()

	// attempt to recover from last provisioned state
	if err := n.Recover(); err != nil {
		log.Printf("netlink: error recovering provisioned state: %v", err)
	}

	select {
	case <-ctx.Done():
		log.Println("teardown: closing server")
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
	log.Println("netlink: restoring previous provisioned state")
	return n.Provision(*state)
}

func (n *NetlinkManager) Status() (*StatusResponse, error) {
	state := n.db.GetState()
	if state == nil {
		return nil, nil
	}

	if n.Tunnel == nil {
		return nil, fmt.Errorf("netlink: saved state is not programmed into client")
	}

	return &StatusResponse{
		TunnelName:   n.Tunnel.Name,
		TunnelSrc:    n.Tunnel.LocalUnderlay,
		TunnelDst:    n.Tunnel.RemoteUnderlay,
		DoubleZeroIP: n.DoubleZeroAddr,
		Status:       "connected",
	}, nil
}
