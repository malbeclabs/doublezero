//go:build linux

package bgp

import (
	"log/slog"
	"net"
	"net/netip"
	"time"

	"github.com/jwhited/corebgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	gobgp "github.com/osrg/gobgp/pkg/packet/bgp"
	"golang.org/x/sys/unix"
)

type RouteManager interface {
	RouteAdd(route *routing.Route) error
	RouteDelete(route *routing.Route) error
	RouteByProtocol(protocol int) ([]*routing.Route, error)
	PeerOnEstablished() error
	PeerOnClose() error
}

type Plugin struct {
	AdvertisedNLRI []NLRI
	PeerStatusChan chan SessionEvent
	RouteSrc       net.IP
	RouteTable     int // kernel routing table to target for writing/removing
	FlushRoutes    bool
	NoInstall      bool
	RouteManager   RouteManager
}

func NewBgpPlugin(
	advertised []NLRI,
	routeSrc net.IP,
	routeTable int,
	peerStatus chan SessionEvent,
	flushRoutes bool,
	noInstall bool,
	routeManager RouteManager) *Plugin {
	return &Plugin{
		AdvertisedNLRI: advertised,
		RouteSrc:       routeSrc,
		RouteTable:     routeTable,
		PeerStatusChan: peerStatus,
		FlushRoutes:    flushRoutes,
		NoInstall:      noInstall,
		RouteManager:   routeManager,
	}
}

func (p *Plugin) GetCapabilities(peer corebgp.PeerConfig) []corebgp.Capability {
	caps := make([]corebgp.Capability, 0)
	caps = append(caps, corebgp.NewMPExtensionsCapability(corebgp.AFI_IPV4, corebgp.SAFI_UNICAST))
	p.PeerStatusChan <- SessionEvent{
		PeerAddr: net.ParseIP(peer.RemoteAddress.String()),
		Session:  Session{SessionStatus: SessionStatusPending, LastSessionUpdate: time.Now().Unix()},
	}
	return caps
}

func (p *Plugin) OnOpenMessage(peer corebgp.PeerConfig, routerID netip.Addr, capabilities []corebgp.Capability) *corebgp.Notification {
	slog.Info("bgp: peer initializing", "peer", peer.RemoteAddress)
	p.PeerStatusChan <- SessionEvent{
		PeerAddr: net.ParseIP(peer.RemoteAddress.String()),
		Session:  Session{SessionStatus: SessionStatusInitializing, LastSessionUpdate: time.Now().Unix()},
	}
	MetricSessionStatus.Set(0)
	return nil
}

func (p *Plugin) OnEstablished(peer corebgp.PeerConfig, writer corebgp.UpdateMessageWriter) corebgp.UpdateMessageHandler {
	slog.Info("bgp: peer established")

	if err := p.RouteManager.PeerOnEstablished(); err != nil {
		slog.Error("bgp: route manager on established error", "src_ip", p.RouteSrc, "error", err)
	}

	for _, nlri := range p.AdvertisedNLRI {
		update, err := p.buildUpdate(nlri)
		if err != nil {
			slog.Error("bgp: error building update message", "error", err)
		}
		// TODO: check if the generated update is malformed
		if err := writer.WriteUpdate(update); err != nil {
			slog.Error("bgp: error writing update to peer", "peer", peer.RemoteAddress, "error", err)
		}
	}
	p.PeerStatusChan <- SessionEvent{
		PeerAddr: net.ParseIP(peer.RemoteAddress.String()),
		Session:  Session{SessionStatus: SessionStatusUp, LastSessionUpdate: time.Now().Unix()},
	}
	MetricSessionStatus.Set(1)
	return p.handleUpdate
}

func (p *Plugin) OnClose(peer corebgp.PeerConfig) {
	slog.Info("bgp: peer closed", "peer", peer.RemoteAddress)

	if err := p.RouteManager.PeerOnClose(); err != nil {
		slog.Error("bgp: route manager on close error", "src_ip", p.RouteSrc, "error", err)
	}

	p.PeerStatusChan <- SessionEvent{
		PeerAddr: net.ParseIP(peer.RemoteAddress.String()),
		Session:  Session{SessionStatus: SessionStatusDown, LastSessionUpdate: time.Now().Unix()},
	}
	slog.Info("bgp: sending peer flush message", "peer", peer.RemoteAddress)

	if p.FlushRoutes {
		protocol := unix.RTPROT_BGP // 186
		routes, err := p.RouteManager.RouteByProtocol(protocol)
		if err != nil {
			slog.Error("routes: error getting routes by protocol", "protocol", protocol)
		}
		for _, route := range routes {
			if err := p.RouteManager.RouteDelete(route); err != nil {
				slog.Error("Error deleting route", "route", route)
				continue
			}
		}
	}
	MetricSessionStatus.Set(0)
}

func (p *Plugin) handleUpdate(peer corebgp.PeerConfig, u []byte) *corebgp.Notification {
	if p.NoInstall {
		return nil
	}
	update := gobgp.BGPUpdate{}
	if err := update.DecodeFromBytes(u); err != nil {
		// TODO: send back notification message
		slog.Error("bgp: error decoding update message", "remote address", peer.RemoteAddress, "error", err)
		return nil
	}
	var nexthop net.IP
	for _, route := range update.WithdrawnRoutes {
		slog.Info("bgp: got withdraw for prefix", "route", route.String(), "next_hop", peer.RemoteAddress.String())
		// Nexthop is not included on a withdraw so we need to use the peer address upstream when writing to netlink.
		// If we don't include a nexthop/gw to netlink, and there are multiple routes, the kernel will remove
		// the first it finds.

		route := &routing.Route{Src: p.RouteSrc, Dst: &net.IPNet{IP: route.Prefix, Mask: net.CIDRMask(int(route.Length), 32)}, Table: p.RouteTable, NextHop: peer.RemoteAddress.AsSlice()}
		slog.Info("routes: removing route from table", "table", p.RouteTable, "dz route", route.String())
		err := p.RouteManager.RouteDelete(route)
		if err != nil {
			slog.Error("routes: error removing route from table", "table", p.RouteTable, "error", err)
		}
	}

	for _, attr := range update.PathAttributes {
		switch attr.GetType() {
		case gobgp.BGP_ATTR_TYPE_NEXT_HOP:

			nexthop = attr.(*gobgp.PathAttributeNextHop).Value
			if nexthop == nil {
				slog.Info("bgp: no nexthop found in update message")
			}
		}
	}

	for _, prefix := range update.NLRI {
		// If we get a prefix, we should write it to the kernel RIB
		slog.Info("bgp: got nlri prefix", "prefix", prefix.String(), "nexthop", nexthop.String())
		route := &routing.Route{
			Src:      p.RouteSrc,
			Dst:      &net.IPNet{IP: prefix.Prefix, Mask: net.CIDRMask(int(prefix.Length), 32)},
			Table:    p.RouteTable,
			NextHop:  nexthop,
			Protocol: unix.RTPROT_BGP}
		slog.Info("routes: writing route", "table", p.RouteTable, "dz route", route.String())
		if err := p.RouteManager.RouteAdd(route); err != nil {
			slog.Error("routes: error writing route", "table", p.RouteTable, "error", err)
		}
	}
	return nil
}

func (p *Plugin) buildUpdate(nlri NLRI) ([]byte, error) {
	origin := gobgp.NewPathAttributeOrigin(0)
	// med := gobgp.NewPathAttributeMultiExitDisc(0)
	nexthop := gobgp.NewPathAttributeNextHop(nlri.NextHop)
	param := gobgp.NewAs4PathParam(2, nlri.AsPath)
	aspath := gobgp.NewPathAttributeAsPath([]gobgp.AsPathParamInterface{param})
	update := gobgp.NewBGPUpdateMessage(
		[]*gobgp.IPAddrPrefix{},
		[]gobgp.PathAttributeInterface{origin, nexthop, aspath},
		[]*gobgp.IPAddrPrefix{gobgp.NewIPAddrPrefix(nlri.PrefixLength, nlri.Prefix)})
	return update.Body.Serialize()
}
