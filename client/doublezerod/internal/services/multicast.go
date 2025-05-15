package services

import (
	"net"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

// type bgpReaderWriter interface {
// 	AddPeer(*bgp.PeerConfig, []bgp.NLRI) error
// 	DeletePeer(net.IP) error
// 	GetPeerStatus(net.IP) bgp.Session
// }

// type dbReaderWriter interface {
// 	GetState() []*api.ProvisionRequest
// 	DeleteState() error
// 	SaveState(p *api.ProvisionRequest) error
// }

type MulticastService struct {
	bgp            bgpReaderWriter
	nl             routing.Netlinker
	db             dbReaderWriter
	UnicastTunnel  *routing.Tunnel
	DoubleZeroAddr net.IP
}

func NewMulticastService(bgp bgpReaderWriter, nl routing.Netlinker, db dbReaderWriter) *MulticastService {
	return &MulticastService{
		bgp: bgp,
		nl:  nl,
		db:  db,
	}
}

func (s *MulticastService) Setup(p *api.ProvisionRequest) error {
	return nil
}

func (s *MulticastService) Teardown() error { return nil }

func (s *MulticastService) Status() (*api.StatusResponse, error) {
	return nil, nil
}

func (s *MulticastService) ServiceType() ServiceType { return ServiceTypeMulticast }
