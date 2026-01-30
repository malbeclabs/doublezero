package bgp

import (
	"context"
	"log/slog"
	"net"
	"net/netip"
	"sync/atomic"
	"time"

	"github.com/jwhited/corebgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	gobgp "github.com/osrg/gobgp/pkg/packet/bgp"
	"golang.org/x/sys/unix"
)

type Plugin struct {
	AdvertisedNLRI    []NLRI
	PeerStatusChan    chan SessionEvent
	RouteSrc          net.IP
	RouteTable        int // kernel routing table to target for writing/removing
	NoInstall         bool
	RouteReaderWriter RouteReaderWriter

	// These fields are used to track the initial establishment of the BGP session.
	startedAt              time.Time
	initialallyEstablished atomic.Bool
	currentlyEstablished   atomic.Bool // for timeout, reset on close

	// peerAddr is stored so the timeout goroutine can emit events
	peerAddr net.IP

	cancelTimeout context.CancelFunc

	tcpConnected atomic.Bool // set when GetCapabilities is called
}

const (
	BGPSessionTimeout = 30 * time.Second
)

func NewBgpPlugin(
	advertised []NLRI,
	routeSrc net.IP,
	routeTable int,
	peerStatus chan SessionEvent,
	noInstall bool,
	routeReaderWriter RouteReaderWriter) *Plugin {
	return &Plugin{
		AdvertisedNLRI:    advertised,
		RouteSrc:          routeSrc,
		RouteTable:        routeTable,
		PeerStatusChan:    peerStatus,
		NoInstall:         noInstall,
		RouteReaderWriter: routeReaderWriter,
		startedAt:         time.Now(),
	}
}

func (p *Plugin) GetCapabilities(peer corebgp.PeerConfig) []corebgp.Capability {
	p.tcpConnected.Store(true)
	caps := make([]corebgp.Capability, 0)
	caps = append(caps, corebgp.NewMPExtensionsCapability(corebgp.AFI_IPV4, corebgp.SAFI_UNICAST))
	p.peerAddr = net.ParseIP(peer.RemoteAddress.String())
	p.PeerStatusChan <- SessionEvent{
		PeerAddr: net.ParseIP(peer.RemoteAddress.String()),
		Session:  Session{SessionStatus: SessionStatusPending, LastSessionUpdate: time.Now().Unix()},
	}
	return caps
}

func (p *Plugin) startSessionTimeout() {
	if p.cancelTimeout != nil {
		p.cancelTimeout()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.cancelTimeout = cancel
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(BGPSessionTimeout):
			p.emitTimeoutStatus()
		}
	}()
}

// emitTimeoutStatus checks the current session state and emits the appropriate
// timeout status (Failed or Unreachable)
func (p *Plugin) emitTimeoutStatus() bool {
	if p.currentlyEstablished.Load() {
		return false
	}

	var status SessionStatus
	if !p.tcpConnected.Load() {
		status = SessionStatusUnreachable
		slog.Warn("bgp: network unreachable - TCP connection never established", "peer", p.peerAddr)
	} else {
		status = SessionStatusFailed
		slog.Warn("bgp: session failed - BGP handshake incomplete", "peer", p.peerAddr)
	}

	p.PeerStatusChan <- SessionEvent{
		PeerAddr: p.peerAddr,
		Session:  Session{SessionStatus: status, LastSessionUpdate: time.Now().Unix()},
	}
	MetricSessionStatus.Set(0)
	return true
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
	if p.cancelTimeout != nil {
		p.cancelTimeout()
	}

	p.currentlyEstablished.Store(true)

	if p.initialallyEstablished.CompareAndSwap(false, true) {
		// If this is the first time we've established the session, record the duration.
		// If the session is closed and then re-established within the lifetime of the same BGP plugin,
		// we don't want to record the duration again since we have no starting time to compare to for
		// those instances.
		duration := time.Since(p.startedAt)
		MetricSessionEstablishedDuration.WithLabelValues(peer.RemoteAddress.String()).Observe(duration.Seconds())
		slog.Info("bgp: peer established", "duration", duration.String(), "peer", peer.RemoteAddress)
	} else {
		slog.Info("bgp: peer re-established", "peer", peer.RemoteAddress)
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
	if p.cancelTimeout != nil {
		p.cancelTimeout()
	}

	p.currentlyEstablished.Store(false)

	slog.Info("bgp: peer closed", "peer", peer.RemoteAddress)
	p.PeerStatusChan <- SessionEvent{
		PeerAddr: net.ParseIP(peer.RemoteAddress.String()),
		Session:  Session{SessionStatus: SessionStatusDown, LastSessionUpdate: time.Now().Unix()},
	}
	slog.Info("bgp: sending peer flush message", "peer", peer.RemoteAddress)

	protocol := unix.RTPROT_BGP // 186
	routes, err := p.RouteReaderWriter.RouteByProtocol(protocol)
	if err != nil {
		slog.Error("routes: error getting routes by protocol on peer close", "protocol", protocol, "error", err)
	}
	for _, route := range routes {
		if err := p.RouteReaderWriter.RouteDelete(route); err != nil {
			slog.Error("routes: error deleting route on peer close", "route", route.String(), "error", err)
			continue
		}
	}

	MetricSessionStatus.Set(0)
	p.startSessionTimeout() // start a new timeout for the next session
}

func (p *Plugin) handleUpdate(peer corebgp.PeerConfig, u []byte) *corebgp.Notification {
	startTime := time.Now()
	defer func() {
		MetricHandleUpdateDuration.Observe(time.Since(startTime).Seconds())
	}()

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
	slog.Info("bgp: processing update", "peer", peer.RemoteAddress, "withdrawals", len(update.WithdrawnRoutes), "nlri", len(update.NLRI))
	for _, route := range update.WithdrawnRoutes {
		slog.Info("bgp: got withdraw for prefix", "route", route.String(), "next_hop", peer.RemoteAddress.String())
		// Nexthop is not included on a withdraw so we need to use the peer address upstream when writing to netlink.
		// If we don't include a nexthop/gw to netlink, and there are multiple routes, the kernel will remove
		// the first it finds.

		route := &routing.Route{Src: p.RouteSrc, Dst: &net.IPNet{IP: route.Prefix, Mask: net.CIDRMask(int(route.Length), 32)}, Table: p.RouteTable, NextHop: peer.RemoteAddress.AsSlice()}
		slog.Info("routes: removing route from table", "table", p.RouteTable, "dz route", route.String())
		err := p.RouteReaderWriter.RouteDelete(route)
		if err != nil {
			slog.Error("routes: error removing route from table", "table", p.RouteTable, "error", err, "route", route.String())
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
		if err := p.RouteReaderWriter.RouteAdd(route); err != nil {
			slog.Error("routes: error writing route", "table", p.RouteTable, "error", err, "route", route.String())
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
