package netlink

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	nl "github.com/vishvananda/netlink"
)

type Netlink struct{}

func (n Netlink) TunnelAdd(t *routing.Tunnel) error {
	gre := &nl.Gretun{
		LinkAttrs: nl.LinkAttrs{
			Name:      t.Name,
			EncapType: string(t.EncapType),
		},
		Local:  t.LocalUnderlay,
		Remote: t.RemoteUnderlay,
		Ttl:    64,
	}
	err := nl.LinkAdd(gre)
	if err != nil && errors.Is(err, syscall.EEXIST) {
		return ErrTunnelExists
	}
	return err
}
func (n Netlink) TunnelDelete(t *routing.Tunnel) error {
	gre := &nl.Gretun{
		LinkAttrs: nl.LinkAttrs{
			Name:      t.Name,
			EncapType: string(t.EncapType),
		},
		Local:  t.LocalUnderlay,
		Remote: t.RemoteUnderlay,
	}
	return nl.LinkDel(gre)
}
func (n Netlink) TunnelGet(t *routing.Tunnel) error { return nil }

func (n Netlink) TunnelAddrAdd(t *routing.Tunnel, prefix string) error {
	gre := &nl.Gretun{
		LinkAttrs: nl.LinkAttrs{
			Name:      t.Name,
			EncapType: string(t.EncapType),
		},
		Local:  t.LocalUnderlay,
		Remote: t.RemoteUnderlay,
	}
	addr, err := nl.ParseAddr(prefix)
	if err != nil {
		return fmt.Errorf("tunnel: error parsing addr: %v", err)
	}
	err = nl.AddrAdd(gre, addr)
	if err != nil && errors.Is(err, syscall.EEXIST) {
		return ErrAddressExists
	}
	return err
}

func (n Netlink) TunnelUp(t *routing.Tunnel) error {
	gre := &nl.Gretun{
		LinkAttrs: nl.LinkAttrs{
			Name:      t.Name,
			EncapType: string(t.EncapType),
		},
		Local:  t.LocalUnderlay,
		Remote: t.RemoteUnderlay,
	}
	return nl.LinkSetUp(gre)
}

// RouteAdd adds a route to the kernel routing table via netlink.
func (n Netlink) RouteAdd(r *routing.Route) error {
	return nl.RouteReplace(&nl.Route{
		Table:    r.Table,
		Src:      r.Src,
		Dst:      r.Dst,
		Gw:       r.NextHop,
		Protocol: nl.RouteProtocol(r.Protocol),
	})
}

// RouteDelete deletes a route from the kernel routing table via netlink.
func (n Netlink) RouteDelete(r *routing.Route) error {
	return nl.RouteDel(&nl.Route{
		Dst:   r.Dst,
		Gw:    r.NextHop,
		Table: r.Table,
		Src:   r.Src,
	})
}

// RouteGet retrieves a route from the kernel routing table via netlink.
func (n Netlink) RouteGet(ip net.IP) ([]*routing.Route, error) {
	nlr, err := nl.RouteGet(ip)
	if err != nil {
		return nil, err
	}
	routes := []*routing.Route{}
	for _, r := range nlr {
		routes = append(routes, &routing.Route{
			Table:    r.Table,
			Src:      r.Src,
			Dst:      r.Dst,
			NextHop:  r.Gw,
			Protocol: int(r.Protocol),
		})
	}
	return routes, nil
}

func (n Netlink) RouteByProtocol(protocol int) ([]*routing.Route, error) {
	routeFilter := &nl.Route{
		Protocol: nl.RouteProtocol(protocol),
	}

	nlr, err := nl.RouteListFiltered(nl.FAMILY_V4, routeFilter, nl.RT_FILTER_PROTOCOL)
	if err != nil {
		return []*routing.Route{}, err
	}

	routes := []*routing.Route{}
	for _, r := range nlr {
		routes = append(routes, &routing.Route{
			Table:    r.Table,
			Src:      r.Src,
			Dst:      r.Dst,
			NextHop:  r.Gw,
			Protocol: int(r.Protocol),
		})
	}
	return routes, nil
}

func (n Netlink) RuleAdd(r *routing.IPRule) error {
	rule := nl.NewRule()
	rule.Priority = r.Priority
	rule.Table = r.Table
	rule.Src = r.SrcNet
	rule.Dst = r.DstNet
	// we mark these rules as kernel protocol to prevent systemd from purging on networkd restarts
	// see https://github.com/malbeclabs/doublezero/issues/159
	rule.Protocol = syscall.RTPROT_KERNEL
	err := nl.RuleAdd(rule)
	if err != nil && errors.Is(err, syscall.EEXIST) {
		return ErrRuleExists
	}
	return err
}

func (n Netlink) RuleDel(r *routing.IPRule) error {
	rule := nl.NewRule()
	rule.Priority = r.Priority
	rule.Table = r.Table
	rule.Src = r.SrcNet
	rule.Dst = r.DstNet
	// we mark these rules as kernel protocol to prevent systemd from purging on networkd restarts
	// see https://github.com/malbeclabs/doublezero/issues/159
	rule.Protocol = syscall.RTPROT_KERNEL
	return nl.RuleDel(rule)
}

func (n Netlink) RuleGet(r *routing.IPRule) error { return nil }

func (n Netlink) Close(t *routing.Tunnel, r []*routing.IPRule, rt []*routing.Route) {}
