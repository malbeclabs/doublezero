package services

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"syscall"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type IBRLService struct {
	bgp            bgpReaderWriter
	nl             routing.Netlinker
	db             dbReaderWriter
	Tunnel         *routing.Tunnel
	DoubleZeroAddr net.IP
}

func (s *IBRLService) UserType() api.UserType   { return api.UserTypeIBRL }
func (s *IBRLService) ServiceType() ServiceType { return ServiceTypeUnicast }

func NewIBRLService(bgp bgpReaderWriter, nl routing.Netlinker, db dbReaderWriter) *IBRLService {
	return &IBRLService{
		bgp: bgp,
		nl:  nl,
		db:  db,
	}
}

// Setup creates an IBRL tunnel with or without an allocated IP address.
func (s *IBRLService) Setup(p *api.ProvisionRequest) error {
	tun, err := routing.NewTunnel(p.TunnelSrc, p.TunnelDst, p.TunnelNet.String())
	if err != nil {
		return fmt.Errorf("error generating new tunnel: %v", err)
	}

	flush := true
	switch p.UserType {
	case api.UserTypeIBRL:
		err = createBaseTunnel(s.nl, tun)
	case api.UserTypeIBRLWithAllocatedIP:
		err = createTunnelWithIP(s.nl, tun, p.DoubleZeroIP)
		flush = false
	default:
		return fmt.Errorf("unsupported tunnel type: %v\n", p)
	}
	if err != nil {
		return fmt.Errorf("error creating tunnel interface: %v", err)
	}

	s.Tunnel = tun
	s.DoubleZeroAddr = p.DoubleZeroIP

	peer := &bgp.PeerConfig{
		RemoteAddress: s.Tunnel.RemoteOverlay,
		LocalAddress:  s.Tunnel.LocalOverlay,
		LocalAs:       p.BgpLocalAsn,
		RemoteAs:      p.BgpRemoteAsn,
		RouteSrc:      p.DoubleZeroIP,
		RouteTable:    syscall.RT_TABLE_MAIN,
		FlushRoutes:   flush,
	}
	nlri, err := bgp.NewNLRI([]uint32{peer.LocalAs}, s.Tunnel.LocalOverlay.String(), p.DoubleZeroIP.String(), 32)
	if err != nil {
		return fmt.Errorf("error generating bgp nlri: %v", err)
	}
	err = s.bgp.AddPeer(peer, []bgp.NLRI{nlri})
	if err != nil {
		if errors.Is(err, bgp.ErrBgpPeerExists) {
			slog.Error("bgp not added", "peer local address", peer.RemoteAddress, "error", err)
		} else {
			return fmt.Errorf("error adding peer: %v", err)
		}
	}

	return nil

}

func (s *IBRLService) Teardown() error {
	var errRemoveTunnel, errRemovePeer error
	if s.Tunnel == nil {
		return nil
	}

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

func (s *IBRLService) Status() (*api.StatusResponse, error) {
	state := s.db.GetState(s.UserType())
	if state == nil {
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
	}, nil
}

type IBRLServiceWithAllocatedAddress struct {
	IBRLService
}

func NewIBRLServiceWithAllocatedAddress(bgp bgpReaderWriter, nl routing.Netlinker, db dbReaderWriter) *IBRLServiceWithAllocatedAddress {
	return &IBRLServiceWithAllocatedAddress{
		IBRLService: IBRLService{
			bgp: bgp,
			nl:  nl,
			db:  db,
		},
	}
}

func (s *IBRLServiceWithAllocatedAddress) UserType() api.UserType {
	return api.UserTypeIBRLWithAllocatedIP
}
func (s *IBRLServiceWithAllocatedAddress) ServiceType() ServiceType { return ServiceTypeUnicast }
