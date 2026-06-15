package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/multicast"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"golang.org/x/net/ipv4"
	"golang.org/x/sys/unix"
)

type MulticastService struct {
	bgp                BGPReaderWriter
	nl                 routing.Netlinker
	pim                PIMWriter
	heartbeat          HeartbeatWriter
	Tunnel             *routing.Tunnel
	DoubleZeroAddr     net.IP
	MulticastPubGroups []net.IP
	MulticastSubGroups []net.IP
	provisionReq       *api.ProvisionRequest
	heartbeatCancel    context.CancelFunc
	heartbeatWatcherWG sync.WaitGroup

	// mu guards the heartbeat lifecycle: heartbeatStarted records whether the
	// readiness watcher has started the sender, and heartbeatGroups is the group
	// set the sender should use (updated by UpdateGroups before the sender starts).
	mu               sync.Mutex
	heartbeatStarted bool
	heartbeatGroups  []net.IP
}

// publisherReadyPollInterval is how often the readiness watcher checks the BGP
// session state before starting the publisher heartbeat. It is a poll cadence,
// not a readiness timeout: the watcher rides the existing BGP session lifecycle
// (the session resolves to Up or a failure within bgp.BGPSessionTimeout) and is
// cancelled on Teardown.
const publisherReadyPollInterval = 250 * time.Millisecond

func (s *MulticastService) UserType() api.UserType   { return api.UserTypeMulticast }
func (s *MulticastService) ServiceType() ServiceType { return ServiceTypeMulticast }

func NewMulticastService(bgp BGPReaderWriter, nl routing.Netlinker, pim PIMWriter, heartbeat HeartbeatWriter) *MulticastService {
	return &MulticastService{
		bgp:       bgp,
		nl:        nl,
		pim:       pim,
		heartbeat: heartbeat,
	}
}

func (s *MulticastService) isPublisher() bool {
	return len(s.MulticastPubGroups) > 0
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

	nlri := []bgp.NLRI{}
	if isPublisher {
		if err = createTunnelWithIP(s.nl, tun, p.DoubleZeroIP); err != nil {
			return fmt.Errorf("error creating tunnel interface: %v", err)
		}
		s.DoubleZeroAddr = p.DoubleZeroIP
		s.MulticastPubGroups = p.MulticastPubGroups
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
		if !isPublisher {
			if err = createBaseTunnel(s.nl, tun); err != nil {
				return fmt.Errorf("error creating tunnel interface: %v", err)
			}
		}
		s.MulticastSubGroups = p.MulticastSubGroups
		for _, group := range s.MulticastSubGroups {
			// Skip groups already routed by the publisher block (which sets Src for correct source IP).
			if isPublisher && containsIP(p.MulticastPubGroups, group) {
				continue
			}
			_, groupNet, err := net.ParseCIDR(fmt.Sprintf("%s/32", group))
			if err != nil {
				return fmt.Errorf("error parsing multicast group address: %v", err)
			}

			mroute := &routing.Route{Dst: groupNet, NextHop: s.Tunnel.RemoteOverlay, Table: syscall.RT_TABLE_MAIN, Protocol: unix.RTPROT_STATIC}
			if err := s.nl.RouteAdd(mroute); err != nil {
				return fmt.Errorf("error adding multicast route: %v", err)
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
	s.provisionReq = p

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

	// Defer the publisher heartbeat (which registers the source at the DZD) until
	// the BGP session is Up, so the DZD never sees a source register before its
	// tunnel/PIM plumbing is ready — the overlap that produces a wedged (S,G).
	if isPublisher {
		s.mu.Lock()
		s.heartbeatGroups = p.MulticastPubGroups
		s.mu.Unlock()
		ctx, cancel := context.WithCancel(context.Background())
		s.heartbeatCancel = cancel
		s.heartbeatWatcherWG.Add(1)
		go func() {
			defer s.heartbeatWatcherWG.Done()
			s.startHeartbeatWhenReady(ctx, s.Tunnel.Name, p.DoubleZeroIP, s.Tunnel.RemoteOverlay)
		}()
	}

	return nil
}

// startHeartbeatWhenReady waits for the BGP session to reach Up, then starts the
// publisher heartbeat with the current group set. It rides the existing BGP
// session lifecycle: the session resolves to Up or a failure within
// bgp.BGPSessionTimeout, and a cycle that never reaches Up is retried when the
// manager reconcile re-provisions. It exits when the context is cancelled
// (Teardown), so no heartbeat start races with or follows a Close.
func (s *MulticastService) startHeartbeatWhenReady(ctx context.Context, iface string, srcIP net.IP, remoteOverlay net.IP) {
	ticker := time.NewTicker(publisherReadyPollInterval)
	defer ticker.Stop()
	for {
		if s.bgp.GetPeerStatus(remoteOverlay).SessionStatus == bgp.SessionStatusUp {
			// Hold the lock across Start so a concurrent UpdateGroups can't touch
			// the sender before it exists: UpdateGroups observes heartbeatStarted
			// and either updates in place or leaves the group set for us to start
			// with here.
			s.mu.Lock()
			if !s.heartbeatStarted {
				if err := s.heartbeat.Start(iface, srcIP, s.heartbeatGroups, multicast.DefaultHeartbeatTTL, multicast.DefaultHeartbeatInterval); err != nil {
					slog.Error("error starting heartbeat sender", "error", err)
				} else {
					s.heartbeatStarted = true
				}
			}
			s.mu.Unlock()
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *MulticastService) Teardown() error {
	var errRemoveTunnel, errRemovePeer error
	if s.Tunnel == nil {
		return nil
	}

	if s.isPublisher() {
		// Stop the readiness watcher and wait for it to exit before closing the
		// heartbeat, so a pending start can't race with or follow the close.
		if s.heartbeatCancel != nil {
			s.heartbeatCancel()
		}
		s.heartbeatWatcherWG.Wait()
		if err := s.heartbeat.Close(); err != nil {
			slog.Error("error stopping heartbeat sender", "error", err)
		}
	}

	if s.isSubscriber() {
		if err := s.pim.Close(); err != nil {
			slog.Error("error stopping pim FSM", "error", err)
		}
	}

	// the tunnel gets torn down before the prune message is received
	// so the subscriber continues to publish towards a no longer existent subscriber
	// there's no ack from the publisher that is got the prune so the 1 second delay gives
	// time to ensure the prune message is received before the tunnel is torn down
	// NOTE: even if this is missed, the publisher will stop sending messages because the
	// hello / joinprune messages are no longer being sent by the subscriber and the publisher
	// will automatically prune the subscriber after a configurable timeout
	time.Sleep(1 * time.Second)

	// both delete multicast tunnel
	err := s.bgp.DeletePeer(s.Tunnel.RemoteOverlay)
	if errors.Is(err, bgp.ErrBgpPeerNotExists) {
		slog.Error("bgp: peer does not exist", "peer tunnel", s.Tunnel.RemoteOverlay)
	} else if err != nil {
		errRemovePeer = fmt.Errorf("bgp: error while deleting peer: %v", err)
	}
	slog.Info("teardown: setting tunnel interface down")
	if err := s.nl.TunnelDown(s.Tunnel); err != nil {
		slog.Error("teardown: error setting tunnel interface down", "error", err)
	}

	slog.Info("teardown: removing tunnel interface")
	if err := s.nl.TunnelDelete(s.Tunnel); err != nil {
		errRemoveTunnel = fmt.Errorf("error removing tunnel interface: %v", err)
	}
	return errors.Join(errRemoveTunnel, errRemovePeer)
}

func (s *MulticastService) Status() (*api.StatusResponse, error) {
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

func (s *MulticastService) ProvisionRequest() *api.ProvisionRequest {
	return s.provisionReq
}

// UpdateGroups incrementally applies multicast group changes without tearing
// down the tunnel or BGP session. It returns an error if a publisher role
// transition is detected (gaining or losing publisher status requires
// adding/removing the DZ IP on the tunnel interface, so a full reprovision is
// needed).
func (s *MulticastService) UpdateGroups(newPR *api.ProvisionRequest) error {
	wasPublisher := s.isPublisher()
	isPublisher := len(newPR.MulticastPubGroups) > 0

	// Gaining or losing publisher role requires adding/removing the DZ IP on
	// the tunnel interface and changing BGP NLRI — can't be done incrementally.
	if wasPublisher != isPublisher {
		return fmt.Errorf("publisher role transition detected (was=%t, now=%t): full reprovision required", wasPublisher, isPublisher)
	}

	wasSubscriber := s.isSubscriber()
	isSubscriber := len(newPR.MulticastSubGroups) > 0

	pubAdded, pubRemoved := api.IPSetDiff(s.MulticastPubGroups, newPR.MulticastPubGroups)
	subAdded, subRemoved := api.IPSetDiff(s.MulticastSubGroups, newPR.MulticastSubGroups)

	// Update publisher routes
	for _, group := range pubRemoved {
		_, groupNet, err := net.ParseCIDR(fmt.Sprintf("%s/32", group))
		if err != nil {
			return fmt.Errorf("error parsing multicast group address: %v", err)
		}
		mroute := &routing.Route{Dst: groupNet, NextHop: s.Tunnel.RemoteOverlay, Table: syscall.RT_TABLE_MAIN, Src: s.DoubleZeroAddr, Protocol: unix.RTPROT_STATIC}
		if err := s.nl.RouteDelete(mroute); err != nil {
			return fmt.Errorf("error deleting publisher multicast route: %v", err)
		}
	}
	for _, group := range pubAdded {
		_, groupNet, err := net.ParseCIDR(fmt.Sprintf("%s/32", group))
		if err != nil {
			return fmt.Errorf("error parsing multicast group address: %v", err)
		}
		mroute := &routing.Route{Dst: groupNet, NextHop: s.Tunnel.RemoteOverlay, Table: syscall.RT_TABLE_MAIN, Src: s.DoubleZeroAddr, Protocol: unix.RTPROT_STATIC}
		if err := s.nl.RouteAdd(mroute); err != nil {
			return fmt.Errorf("error adding publisher multicast route: %v", err)
		}
	}

	// Apply publisher group changes to the heartbeat. If the readiness watcher
	// hasn't started the sender yet (BGP not up), just record the new group set —
	// the watcher will start with it; otherwise update the running sender in place.
	if isPublisher && (len(pubAdded) > 0 || len(pubRemoved) > 0) {
		s.mu.Lock()
		s.heartbeatGroups = newPR.MulticastPubGroups
		started := s.heartbeatStarted
		s.mu.Unlock()
		if started {
			if err := s.heartbeat.UpdateGroups(newPR.MulticastPubGroups); err != nil {
				return fmt.Errorf("error updating heartbeat groups: %v", err)
			}
		}
	}

	// Update subscriber routes
	for _, group := range subRemoved {
		if isPublisher && containsIP(newPR.MulticastPubGroups, group) {
			continue
		}
		_, groupNet, err := net.ParseCIDR(fmt.Sprintf("%s/32", group))
		if err != nil {
			return fmt.Errorf("error parsing multicast group address: %v", err)
		}
		mroute := &routing.Route{Dst: groupNet, NextHop: s.Tunnel.RemoteOverlay, Table: syscall.RT_TABLE_MAIN, Protocol: unix.RTPROT_STATIC}
		if err := s.nl.RouteDelete(mroute); err != nil {
			return fmt.Errorf("error deleting subscriber multicast route: %v", err)
		}
	}
	for _, group := range subAdded {
		if isPublisher && containsIP(newPR.MulticastPubGroups, group) {
			continue
		}
		_, groupNet, err := net.ParseCIDR(fmt.Sprintf("%s/32", group))
		if err != nil {
			return fmt.Errorf("error parsing multicast group address: %v", err)
		}
		mroute := &routing.Route{Dst: groupNet, NextHop: s.Tunnel.RemoteOverlay, Table: syscall.RT_TABLE_MAIN, Protocol: unix.RTPROT_STATIC}
		if err := s.nl.RouteAdd(mroute); err != nil {
			return fmt.Errorf("error adding subscriber multicast route: %v", err)
		}
	}

	// Stop PIM if losing subscriber role.
	if wasSubscriber && !isSubscriber {
		if err := s.pim.Close(); err != nil {
			slog.Error("error stopping pim FSM during group update", "error", err)
		}
	}

	// Start PIM if gaining subscriber role.
	if !wasSubscriber && isSubscriber {
		c, err := net.ListenPacket("ip4:103", "0.0.0.0")
		if err != nil {
			return fmt.Errorf("failed to listen: %v", err)
		}
		r, err := ipv4.NewRawConn(c)
		if err != nil {
			return fmt.Errorf("failed to create raw conn: %v", err)
		}

		if err := s.pim.Start(r, s.Tunnel.Name, s.Tunnel.RemoteOverlay, newPR.MulticastSubGroups); err != nil {
			return fmt.Errorf("error starting pim FSM: %v", err)
		}
	} else if isSubscriber && (len(subAdded) > 0 || len(subRemoved) > 0) {
		// Update PIM groups if subscriber groups changed.
		if err := s.pim.UpdateGroups(newPR.MulticastSubGroups); err != nil {
			return fmt.Errorf("error updating pim groups: %v", err)
		}
	}

	s.MulticastPubGroups = newPR.MulticastPubGroups
	s.MulticastSubGroups = newPR.MulticastSubGroups
	s.provisionReq = newPR
	return nil
}

func containsIP(ips []net.IP, target net.IP) bool {
	for _, ip := range ips {
		if ip.Equal(target) {
			return true
		}
	}
	return false
}
