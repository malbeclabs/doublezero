package services

import (
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net"
	"syscall"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

type MulticastService struct {
	bgp                BGPReaderWriter
	nl                 routing.Netlinker
	db                 DBReaderWriter
	pim                PIMWriter
	Tunnel             *routing.Tunnel
	DoubleZeroAddr     net.IP
	MulticastPubGroups []net.IP
	MulticastSubGroups []net.IP
}

func (s *MulticastService) UserType() api.UserType   { return api.UserTypeMulticast }
func (s *MulticastService) ServiceType() ServiceType { return ServiceTypeMulticast }

func NewMulticastService(bgp BGPReaderWriter, nl routing.Netlinker, db DBReaderWriter, pim PIMWriter) *MulticastService {
	return &MulticastService{
		bgp: bgp,
		nl:  nl,
		db:  db,
		pim: pim,
	}
}

func (s *MulticastService) isSubscriber() bool {
	return len(s.MulticastSubGroups) > 0
}

func (s *MulticastService) Setup(p *api.ProvisionRequest) error {
	if len(p.MulticastPubGroups) == 0 && len(p.MulticastSubGroups) == 0 {
		return fmt.Errorf("no multicast publisher or subscriber groups specified")
	}

	tun, err := routing.NewTunnel("doublezero1", p.TunnelSrc, p.TunnelDst, p.TunnelNet.String())
	if err != nil {
		return fmt.Errorf("error generating new tunnel: %v", err)
	}
	s.Tunnel = tun

	isPublisher := len(p.MulticastPubGroups) > 0
	isSubscriber := len(p.MulticastSubGroups) > 0

	if isPublisher && isSubscriber {
		return fmt.Errorf("cannot be both publisher and subscriber")
	}

	nlri := []bgp.NLRI{}
	if isPublisher {
		if err = createTunnelWithIP(s.nl, tun, p.DoubleZeroIP); err != nil {
			return fmt.Errorf("error creating tunnel interface: %v", err)
		}
		s.DoubleZeroAddr = p.DoubleZeroIP
		// advertise DZ IP over session
		rt, err := bgp.NewNLRI([]uint32{p.BgpLocalAsn}, s.Tunnel.LocalOverlay.String(), s.DoubleZeroAddr.String(), 32)
		if err != nil {
			return fmt.Errorf("error generating bgp nlri for publisher: %v", err)
		}
		nlri = append(nlri, rt)

		// add static multicast route for publishing group pointing to the tunnel
		for _, group := range p.MulticastPubGroups {
			_, groupNet, err := net.ParseCIDR(fmt.Sprintf("%s/32", group))
			if err != nil {
				return fmt.Errorf("error parsing multicast group address: %v", err)
			}
			mroute := &routing.Route{Dst: groupNet, NextHop: s.Tunnel.RemoteOverlay, Table: syscall.RT_TABLE_MAIN, Src: s.DoubleZeroAddr, Protocol: unix.RTPROT_STATIC}
			if err := s.nl.RouteAdd(mroute); err != nil {
				return fmt.Errorf("error adding multicast route: %v", err)
			}
		}
	}

	if isSubscriber {
		if err = createBaseTunnel(s.nl, tun); err != nil {
			return fmt.Errorf("error creating tunnel interface: %v", err)
		}
		s.MulticastSubGroups = p.MulticastSubGroups
		for _, group := range s.MulticastSubGroups {
			_, groupNet, err := net.ParseCIDR(fmt.Sprintf("%s/32", group))
			if err != nil {
				return fmt.Errorf("error parsing multicast group address: %v", err)
			}
			if err = s.nl.TunnelAddrAdd(s.Tunnel, groupNet.String(), true); err != nil {
				return fmt.Errorf("error adding multicast group address to tunnel: %v", err)
			}
		}

		c, err := net.ListenPacket("ip4:103", "0.0.0.0")
		if err != nil {
			return fmt.Errorf("failed to listen: %v", err)
		}
		r, err := ipv4.NewRawConn(c)
		if err != nil {
			return fmt.Errorf("failed to create raw conn: %v", err)
		}

		if err := s.pim.Start(r, s.Tunnel.Name, s.Tunnel.RemoteOverlay, s.MulticastSubGroups); err != nil {
			return fmt.Errorf("error starting pim FSM: %v", err)
		}
	}

	s.Tunnel = tun

	peer := &bgp.PeerConfig{
		RemoteAddress: s.Tunnel.RemoteOverlay,
		LocalAddress:  s.Tunnel.LocalOverlay,
		LocalAs:       p.BgpLocalAsn,
		RemoteAs:      p.BgpRemoteAsn,
		NoInstall:     true,
	}
	err = s.bgp.AddPeer(peer, nlri)
	if err != nil {
		if errors.Is(err, bgp.ErrBgpPeerExists) {
			slog.Error("bgp not added", "peer local address", peer.RemoteAddress, "error", err)
		} else {
			return fmt.Errorf("error adding peer: %v", err)
		}
	}
	return nil
}

func (s *MulticastService) Teardown() error {
	var errRemoveTunnel, errRemovePeer error
	if s.Tunnel == nil {
		return nil
	}

	if s.isSubscriber() {
		if err := s.pim.Close(); err != nil {
			slog.Error("error stopping pim FSM", "error", err)
		}
	}

	// both delete multicast tunnel
	err := s.bgp.DeletePeer(s.Tunnel.RemoteOverlay)
	if errors.Is(err, bgp.ErrBgpPeerNotExists) {
		slog.Error("bgp: peer does not exist", "peer tunnel", s.Tunnel.RemoteOverlay)
	} else if err != nil {
		errRemovePeer = fmt.Errorf("bgp: error while deleting peer: %v", err)
	}
	slog.Info("teardown: removing tunnel interface")
	if err := s.nl.TunnelDelete(s.Tunnel); err != nil {
		errRemoveTunnel = fmt.Errorf("error removing tunnel interface: %v", err)
	}
	return errors.Join(errRemoveTunnel, errRemovePeer)
}

func (s *MulticastService) Status() (*api.StatusResponse, error) {
	state := s.db.GetState(s.UserType())
	if state == nil {
		log.Printf("netlink: no state found for %v", s.UserType())
		return nil, nil
	}

	if s.Tunnel == nil {
		return nil, fmt.Errorf("netlink: saved state is not programmed into client")
	}
	peerStatus := s.bgp.GetPeerStatus(s.Tunnel.RemoteOverlay)

	return &api.StatusResponse{
		TunnelName:       s.Tunnel.Name,
		TunnelSrc:        s.Tunnel.LocalUnderlay,
		TunnelDst:        s.Tunnel.RemoteUnderlay,
		DoubleZeroIP:     s.DoubleZeroAddr,
		DoubleZeroStatus: peerStatus,
		UserType:         s.UserType(),
	}, nil
}
