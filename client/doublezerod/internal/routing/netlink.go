package routing

import (
	"errors"
	"fmt"
	"net"
	"syscall"

	nl "github.com/vishvananda/netlink"
	nlmsg "github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

const RPFilterLoose = 2

// ipv4DevconfRPFilter is IPV4_DEVCONF_RP_FILTER from linux/ip.h; x/sys/unix does not export it.
const ipv4DevconfRPFilter = 8

type Netlink struct{}

type Netlinker interface {
	TunnelAdd(*Tunnel) error
	TunnelDown(*Tunnel) error
	TunnelDelete(*Tunnel) error
	// TunnelAddrAdd adds an address to a tunnel interface with the given scope (syscall.RT_SCOPE_*).
	TunnelAddrAdd(*Tunnel, string, int) error
	TunnelUp(*Tunnel) error
	RouteAdd(*Route) error
	RouteDelete(*Route) error
	RouteGet(net.IP) ([]*Route, error)
	RuleAdd(*IPRule) error
	RuleDel(*IPRule) error
	RouteByProtocol(int) ([]*Route, error)
	LinkSetRPFilter(ifname string, mode int) error
}

func (n Netlink) TunnelAdd(t *Tunnel) error {
	gre := &nl.Gretun{
		LinkAttrs: nl.LinkAttrs{
			Name:      t.Name,
			MTU:       t.MTU,
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
func (n Netlink) TunnelDown(t *Tunnel) error {
	gre := &nl.Gretun{
		LinkAttrs: nl.LinkAttrs{
			Name:      t.Name,
			EncapType: string(t.EncapType),
		},
		Local:  t.LocalUnderlay,
		Remote: t.RemoteUnderlay,
	}
	return nl.LinkSetDown(gre)
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

func (n Netlink) TunnelAddrAdd(t *Tunnel, prefix string, scope int) error {
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
	addr.Scope = scope

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

// LinkSetRPFilter sets net.ipv4.conf.<ifname>.rp_filter over rtnetlink, which
// needs only CAP_NET_ADMIN; containers mount /proc/sys read-only.
func (n Netlink) LinkSetRPFilter(ifname string, mode int) error {
	link, err := nl.LinkByName(ifname)
	if err != nil {
		return err
	}
	req := nlmsg.NewNetlinkRequest(unix.RTM_SETLINK, unix.NLM_F_ACK)
	msg := nlmsg.NewIfInfomsg(unix.AF_UNSPEC)
	msg.Index = int32(link.Attrs().Index)
	req.AddData(msg)

	spec := nlmsg.NewRtAttr(unix.IFLA_AF_SPEC, nil)
	conf := spec.AddRtAttr(unix.AF_INET, nil).AddRtAttr(unix.IFLA_INET_CONF, nil)
	conf.AddRtAttr(ipv4DevconfRPFilter, nlmsg.Uint32Attr(uint32(mode)))
	req.AddData(spec)

	_, err = req.Execute(unix.NETLINK_ROUTE, 0)
	return err
}

// RouteAdd adds a route to the kernel routing table via netlink.
func (n Netlink) RouteAdd(r *Route) error {
	return nl.RouteReplace(&nl.Route{
		Table:    r.Table,
		Src:      r.Src,
		Dst:      r.Dst,
		Gw:       r.NextHop,
		Protocol: nl.RouteProtocol(r.Protocol),
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
			Table:    r.Table,
			Src:      r.Src,
			Dst:      r.Dst,
			NextHop:  r.Gw,
			Protocol: int(r.Protocol),
		})
	}
	return routes, nil
}

// RouteByProtocol returns IPv4 routes matching the given routing protocol.
//
// NOTE: only RT_FILTER_PROTOCOL is set, so vishvananda/netlink restricts the
// listing to the main routing table (RT_TABLE_MAIN); routes in other tables,
// such as the edge-filtering BGP routes in table 100, are skipped. Use
// RouteByProtocolAllTables to see those.
func (n Netlink) RouteByProtocol(protocol int) ([]*Route, error) {
	return routeByProtocol(protocol, nl.RT_FILTER_PROTOCOL)
}

// RouteByProtocolAllTables returns IPv4 routes matching the given routing
// protocol in every routing table. Setting RT_FILTER_TABLE with the table left
// unspecified lifts the main-table restriction without selecting a table.
func (n Netlink) RouteByProtocolAllTables(protocol int) ([]*Route, error) {
	return routeByProtocol(protocol, nl.RT_FILTER_PROTOCOL|nl.RT_FILTER_TABLE)
}

func routeByProtocol(protocol int, filterMask uint64) ([]*Route, error) {
	routeFilter := &nl.Route{
		Protocol: nl.RouteProtocol(protocol),
	}

	nlr, err := nl.RouteListFiltered(nl.FAMILY_V4, routeFilter, filterMask)
	if err != nil {
		return []*Route{}, err
	}

	routes := []*Route{}
	for _, r := range nlr {
		routes = append(routes, &Route{
			Table:    r.Table,
			Src:      r.Src,
			Dst:      r.Dst,
			NextHop:  r.Gw,
			Protocol: int(r.Protocol),
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
	// we mark these rules as kernel protocol to prevent systemd from purging on networkd restarts
	// see https://github.com/malbeclabs/doublezero/issues/159
	rule.Protocol = syscall.RTPROT_KERNEL
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
	// we mark these rules as kernel protocol to prevent systemd from purging on networkd restarts
	// see https://github.com/malbeclabs/doublezero/issues/159
	rule.Protocol = syscall.RTPROT_KERNEL
	return nl.RuleDel(rule)
}

func (n Netlink) RuleGet(r *IPRule) error { return nil }

func (n Netlink) Close(t *Tunnel, r []*IPRule, rt []*Route) {}
