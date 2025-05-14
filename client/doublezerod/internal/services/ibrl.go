package services

import (
	"errors"
	"fmt"
	"log/slog"
	"syscall"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/netlink"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type IBRLService struct {
	bgp netlink.BgpReaderWriter
	nl  netlink.Netlinker
	nlm netlink.NetlinkManager
	db  netlink.DbReaderWriter
}

func NewIBRLService(bgp netlink.BgpReaderWriter, nl netlink.Netlinker, nlm netlink.NetlinkManager, db netlink.DbReaderWriter) *IBRLService {
	return &IBRLService{
		bgp: bgp,
		nl:  nl,
		db:  db,
		nlm: nlm,
	}
}

func (s *IBRLService) CreateTunnel(tun *routing.Tunnel) error {
	if tun.LocalOverlay == nil {
		return fmt.Errorf("missing tunnel local overlay addressing")
	}

	err := s.nl.TunnelAdd(tun)
	if err != nil {
		if errors.Is(err, netlink.ErrTunnelExists) {
			slog.Error("tunnel: tunnel already exists", "tunnel", tun.Name)
		} else {
			return fmt.Errorf("tunnel: could not add tunnel interface: %v", err)
		}
	}

	// TODO: debug this log
	slog.Info("tunnel: adding address to tunnel interface", "address", tun.LocalOverlay)
	err = s.nl.TunnelAddrAdd(tun, tun.LocalOverlay.String()+"/31")
	if err != nil {
		if errors.Is(err, netlink.ErrAddressExists) {
			slog.Error("tunnel: address already present on tunnel")
		} else {
			return fmt.Errorf("error adding addressing to tunnel: %v", err)
		}
	}

	slog.Info("tunnel: bringing up tunnel interface")
	if err = s.nl.TunnelUp(tun); err != nil {
		return fmt.Errorf("tunnel: error bring up tunnel interface: %v", err)
	}

	return nil

}
func (s *IBRLService) Setup(p *netlink.ProvisionRequest) error {

	tun, err := routing.NewTunnel(p.TunnelSrc, p.TunnelDst, p.TunnelNet.String())
	if err != nil {
		return fmt.Errorf("error generating new tunnel: %v", err)
	}

	flush := true
	err = s.CreateTunnel(tun)

	if err != nil {
		return fmt.Errorf("error creating tunnel interface: %v", err)
	}

	// need netlinkmanager?
	s.nlm.UnicastTunnel = tun
	s.nlm.DoubleZeroAddr = p.DoubleZeroIP

	// TODO: add flush routes flag; depending on IBRL mode, we may or may not flush routes
	peer := &bgp.PeerConfig{
		RemoteAddress: s.nlm.UnicastTunnel.RemoteOverlay,
		LocalAddress:  s.nlm.UnicastTunnel.LocalOverlay,
		LocalAs:       p.BgpLocalAsn,
		RemoteAs:      p.BgpRemoteAsn,
		RouteSrc:      p.DoubleZeroIP,
		RouteTable:    syscall.RT_TABLE_MAIN,
		FlushRoutes:   flush,
	}
	nlri, err := bgp.NewNLRI([]uint32{peer.LocalAs}, s.nlm.UnicastTunnel.LocalOverlay.String(), p.DoubleZeroIP.String(), 32)
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

func (s *IBRLService) Teardown() error { return nil }

func (s *IBRLService) Status() (*netlink.StatusResponse, error) { return nil, nil }
