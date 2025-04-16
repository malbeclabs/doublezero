package bgp

import (
	"log/slog"
	"net"
	"net/netip"

	"github.com/jwhited/corebgp"
	gobgp "github.com/osrg/gobgp/pkg/packet/bgp"
)

type Plugin struct {
	AdvertisedNLRI []NLRI
	WriteChan      chan NLRI
	RemoveChan     chan NLRI
}

func NewBgpPlugin(writeChan, removeChan chan NLRI, advertised []NLRI) *Plugin {
	return &Plugin{WriteChan: writeChan, RemoveChan: removeChan, AdvertisedNLRI: advertised}
}

func (p *Plugin) GetCapabilities(c corebgp.PeerConfig) []corebgp.Capability {
	caps := make([]corebgp.Capability, 0)
	caps = append(caps, corebgp.NewMPExtensionsCapability(corebgp.AFI_IPV4, corebgp.SAFI_UNICAST))
	return caps
}

func (p *Plugin) OnOpenMessage(peer corebgp.PeerConfig, routerID netip.Addr, capabilities []corebgp.Capability) *corebgp.Notification {
	return nil
}

func (p *Plugin) OnEstablished(peer corebgp.PeerConfig, writer corebgp.UpdateMessageWriter) corebgp.UpdateMessageHandler {
	slog.Info("bgp: peer established")
	for _, nlri := range p.AdvertisedNLRI {
		update, err := p.buildUpdate(nlri)
		if err != nil {
			slog.Error("bgp: error building update message", "error", err)
		}
		// TODO: check if the generated update is malformed
		if err := writer.WriteUpdate(update); err != nil {
			slog.Error("bgp: error writing update to peer", "remote address", peer.RemoteAddress, "error", err)
		}
	}
	return p.handleUpdate
}

func (p *Plugin) OnClose(peer corebgp.PeerConfig) {
	slog.Info("bgp: peer closed")
}

func (p *Plugin) handleUpdate(peer corebgp.PeerConfig, u []byte) *corebgp.Notification {
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
		p.RemoveChan <- NLRI{Prefix: route.Prefix.String(), PrefixLength: route.Length, NextHop: peer.RemoteAddress.String()}
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
		slog.Info("bgp: got nlri prefix", "prefix", prefix.String())
		p.WriteChan <- NLRI{Prefix: prefix.Prefix.String(), PrefixLength: prefix.Length, NextHop: nexthop.String()}
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
