package netlink

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	nl "github.com/vishvananda/netlink"
)

type Netlink struct{}

func (n Netlink) TunnelAdd(t *Tunnel) error {
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
func (n Netlink) TunnelDelete(t *Tunnel) error {
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
func (n Netlink) TunnelGet(t *Tunnel) error { return nil }

func (n Netlink) TunnelAddrAdd(t *Tunnel, prefix string) error {
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

func (n Netlink) TunnelUp(t *Tunnel) error {
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
func (n Netlink) RouteAdd(r *Route) error {
	return nl.RouteReplace(&nl.Route{
		Table: r.Table,
		Src:   r.Src,
		Dst:   r.Dst,
		Gw:    r.NextHop,
	})
}

// RouteDelete deletes a route from the kernel routing table via netlink.
func (n Netlink) RouteDelete(r *Route) error {
	return nl.RouteDel(&nl.Route{
		Dst:   r.Dst,
		Gw:    r.NextHop,
		Table: r.Table,
		Src:   r.Src,
	})
}

// RouteGet retrieves a route from the kernel routing table via netlink.
func (n Netlink) RouteGet(ip net.IP) ([]*Route, error) {
	nlr, err := nl.RouteGet(ip)
	if err != nil {
		return nil, err
	}
	routes := []*Route{}
	for _, r := range nlr {
		routes = append(routes, &Route{
			Table:   r.Table,
			Src:     r.Src,
			Dst:     r.Dst,
			NextHop: r.Gw,
		})
	}
	return routes, nil
}

func (n Netlink) RuleAdd(r *IPRule) error {
	rule := nl.NewRule()
	rule.Priority = r.Priority
	rule.Table = r.Table
	rule.Src = r.SrcNet
	rule.Dst = r.DstNet
	err := nl.RuleAdd(rule)
	if err != nil && errors.Is(err, syscall.EEXIST) {
		return ErrRuleExists
	}
	return err
}

func (n Netlink) RuleDel(r *IPRule) error {
	rule := nl.NewRule()
	rule.Priority = r.Priority
	rule.Table = r.Table
	rule.Src = r.SrcNet
	rule.Dst = r.DstNet
	return nl.RuleDel(rule)
}

func (n Netlink) RuleGet(r *IPRule) error { return nil }

func (n Netlink) Close(t *Tunnel, r []*IPRule, rt []*Route) {}
