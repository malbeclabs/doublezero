package services

import (
	"errors"
	"fmt"
	"log/slog"
	"net"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
)

type EdgeFilteringService struct {
	bgp            BGPReaderWriter
	nl             routing.Netlinker
	Tunnel         *routing.Tunnel
	DoubleZeroAddr net.IP
	Routes         []*routing.Route
	Rules          []*routing.IPRule
	provisionReq   *api.ProvisionRequest
}

func (s *EdgeFilteringService) UserType() api.UserType   { return api.UserTypeEdgeFiltering }
func (s *EdgeFilteringService) ServiceType() ServiceType { return ServiceTypeUnicast }

func NewEdgeFilteringService(bgp BGPReaderWriter, nl routing.Netlinker) *EdgeFilteringService {
	return &EdgeFilteringService{
		bgp: bgp,
		nl:  nl,
	}
}

func (s *EdgeFilteringService) Setup(p *api.ProvisionRequest) error {
	// TODO: have NewTunnel take a net.IPNet
	tun, err := routing.NewTunnel("doublezero0", p.TunnelSrc, p.TunnelDst, p.TunnelNet.String())
	if err != nil {
		return fmt.Errorf("error generating new tunnel: %v", err)
	}

	err = createTunnelWithIP(s.nl, tun, p.DoubleZeroIP)
	if err != nil {
		return fmt.Errorf("error creating tunnel interface: %v", err)
	}
	s.Tunnel = tun
	s.DoubleZeroAddr = p.DoubleZeroIP
	s.provisionReq = p

	// Apply IP Rules
	slog.Info("rules: creating ip rules")
	if err = s.createIPRules(p.DoubleZeroPrefixes); err != nil {
		return fmt.Errorf("error creating IP rules: %v", err)
	}

	slog.Info("routes: adding ip routes")
	if err = s.createDefaultRoutingTable(); err != nil {
		return fmt.Errorf("error creating routing tables: %v", err)
	}

	peer := &bgp.PeerConfig{
		RemoteAddress: s.Tunnel.RemoteOverlay,
		LocalAddress:  s.Tunnel.LocalOverlay,
		LocalAs:       p.BgpLocalAsn,
		RemoteAs:      p.BgpRemoteAsn,
		RouteTable:    routing.RouteTableSpecific, // TODO: this needs to go
		RouteSrc:      p.DoubleZeroIP,
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

func (s *EdgeFilteringService) Teardown() error {
	var errFlushRules, errFlushRoutes, errRemoveTunnel, errRemovePeer error

	slog.Info("teardown: flushing rules")
	if err := s.flushRules(); err != nil {
		errFlushRules = fmt.Errorf("error flushing rules: %v", err)
	}

	slog.Info("teardown: flushing routes")
	if err := s.flushStaticRoutes(); err != nil {
		errFlushRoutes = fmt.Errorf("error flushing routes: %v", err)
	}

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

	return errors.Join(errFlushRules, errFlushRoutes, errRemoveTunnel, errRemovePeer)
}

func (s *EdgeFilteringService) Status() (*api.StatusResponse, error) {
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

func (s *EdgeFilteringService) ProvisionRequest() *api.ProvisionRequest {
	return s.provisionReq
}

func (s *EdgeFilteringService) createIPRules(prefixes []*net.IPNet) error {
	rules := []*routing.IPRule{}
	for _, prefix := range prefixes {
		// dz-specifics table
		rule, err := routing.NewIPRule(100, routing.DzTableSpecific, "0.0.0.0/0", prefix.String())
		if err != nil {
			slog.Error("rules: error creating IP rule", "prefix", prefix, "error", err)
			return fmt.Errorf("rules: error creating IP rule: %v", err)
		}
		rules = append(rules, rule)
		// dz-default table - anything sourced from dz space can't go out the public interface
		rule, err = routing.NewIPRule(101, routing.DzTableDefault, prefix.String(), "0.0.0.0/0")
		if err != nil {
			return fmt.Errorf("rules: error creating IP rule: %v", err)
		}
		rules = append(rules, rule)
	}

	if err := routing.CreateIPRules(s.nl, rules); err != nil {
		return fmt.Errorf("rules: error creating IP rules: %v", err)
	}
	s.Rules = rules
	return nil
}

func (s *EdgeFilteringService) createDefaultRoutingTable() error {
	_, defaultRt, err := net.ParseCIDR("0.0.0.0/0")
	if err != nil {
		return fmt.Errorf("routes: unable to parse default route: %v", err)
	}
	routes := []*routing.Route{
		{Dst: defaultRt, Src: s.DoubleZeroAddr, Table: routing.RouteTableDefault, NextHop: s.Tunnel.RemoteOverlay},
	}
	for _, route := range routes {
		if err := s.nl.RouteAdd(route); err != nil {
			return fmt.Errorf("routes: error adding route: %s", route)
		}
	}
	s.Routes = routes
	return nil
}

func (s *EdgeFilteringService) flushRules() error {
	var err error
	for _, rule := range s.Rules {
		if err = s.nl.RuleDel(rule); err != nil {
			err = errors.Join(err, fmt.Errorf("error deleting rule %s: %v", rule, err))
		}

	}
	s.Rules = []*routing.IPRule{}
	return err
}

func (s *EdgeFilteringService) flushStaticRoutes() error {
	var err error
	for _, route := range s.Routes {
		if err = s.nl.RouteDelete(route); err != nil {
			err = errors.Join(err, fmt.Errorf("error deleting route %s: %v", route, err))
		}
	}
	s.Routes = []*routing.Route{}
	return err
}
