package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/services"
)

// Provisioner is an interface for all services that can be provisioned by the
// manager. All new services must implement this interface.
type Provisioner interface {
	Setup(*api.ProvisionRequest) error
	Teardown() error
	Status() (*api.StatusResponse, error) // TODO: what do we return here?
	ServiceType() services.ServiceType
	ProvisionRequest() *api.ProvisionRequest
}

// BgpReaderWriter is an interface for the handling of per
// service bgp sessions.
type BGPServer interface {
	Serve([]net.Listener) error
	AddPeer(*bgp.PeerConfig, []bgp.NLRI) error
	DeletePeer(net.IP) error
	GetPeerStatus(net.IP) bgp.Session
}

type NetlinkManager struct {
	netlink          routing.Netlinker
	Routes           []*routing.Route
	Rules            []*routing.IPRule
	UnicastService   Provisioner
	MulticastService Provisioner
	DoubleZeroAddr   net.IP
	bgp              BGPServer
	pim              services.PIMWriter
	heartbeat        services.HeartbeatWriter
	mu               sync.Mutex
}

// CreateService creates the appropriate service based on the provisioned
// user type.
func CreateService(u api.UserType, bgp services.BGPReaderWriter, nl routing.Netlinker, pim services.PIMWriter, heartbeat services.HeartbeatWriter) (Provisioner, error) {
	switch u {
	case api.UserTypeIBRL:
		return services.NewIBRLService(bgp, nl), nil
	case api.UserTypeIBRLWithAllocatedIP:
		return services.NewIBRLServiceWithAllocatedAddress(bgp, nl), nil
	case api.UserTypeEdgeFiltering:
		return services.NewEdgeFilteringService(bgp, nl), nil
	case api.UserTypeMulticast:
		return services.NewMulticastService(bgp, nl, pim, heartbeat), nil
	default:
		return nil, fmt.Errorf("unsupported user type: %s", u)
	}
}

func NewNetlinkManager(netlink routing.Netlinker, bgp BGPServer, pim services.PIMWriter, heartbeat services.HeartbeatWriter) *NetlinkManager {
	manager := &NetlinkManager{netlink: netlink, bgp: bgp, pim: pim, heartbeat: heartbeat}
	return manager
}

// Provision is the entry point for all user tunnel provisioning. This currently
// contains logic for IBRL, edge filtering and multicast use cases.
func (n *NetlinkManager) Provision(pr api.ProvisionRequest) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	svc, err := CreateService(pr.UserType, n.bgp, n.netlink, n.pim, n.heartbeat)
	if err != nil {
		return fmt.Errorf("error creating service: %v", err)
	}

	if n.UnicastService != nil && svc.ServiceType() == services.ServiceTypeUnicast {
		return fmt.Errorf("unicast service already provisioned")
	}

	if n.MulticastService != nil && svc.ServiceType() == services.ServiceTypeMulticast {
		return fmt.Errorf("multicast service already provisioned")
	}

	if err := svc.Setup(&pr); err != nil {
		return fmt.Errorf("error provisioning service: %v", err)
	}
	if svc.ServiceType() == services.ServiceTypeUnicast {
		n.UnicastService = svc
	}
	if svc.ServiceType() == services.ServiceTypeMulticast {
		n.MulticastService = svc
	}

	return nil
}

// Remove is the entry point for service deprovisioning.
func (n *NetlinkManager) Remove(u api.UserType) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// We've never been provisioned
	if n.UnicastService == nil && n.MulticastService == nil {
		return nil
	}

	if !services.IsUnicastUser(u) && !services.IsMulticastUser(u) {
		return fmt.Errorf("unsupported user type: %s", u)
	}

	if services.IsUnicastUser(u) && n.UnicastService != nil {
		if err := n.UnicastService.Teardown(); err != nil {
			return fmt.Errorf("error tearing down unicast service: %v", err)
		}
		n.UnicastService = nil
	}

	if services.IsMulticastUser(u) && n.MulticastService != nil {
		if err := n.MulticastService.Teardown(); err != nil {
			return fmt.Errorf("error tearing down multicast service: %v", err)
		}
		n.MulticastService = nil
	}

	return nil
}

// Close tears down any active services. This is typically called when
// manager is shutting down. Per-service state is not deleted from the db.
func (n *NetlinkManager) Close() error {
	n.mu.Lock()
	defer n.mu.Unlock()

	var teardownErr error
	if n.UnicastService == nil && n.MulticastService == nil {
		return nil
	}

	if n.UnicastService != nil {
		if err := n.UnicastService.Teardown(); err != nil {
			teardownErr = errors.Join(teardownErr, fmt.Errorf("error tearing down unicast service: %v", err))
		}
	}
	if n.MulticastService != nil {
		if err := n.MulticastService.Teardown(); err != nil {
			teardownErr = errors.Join(teardownErr, fmt.Errorf("error tearing down multicast service: %v", err))
		}
	}
	return teardownErr
}

// Serve starts the manager.
func (n *NetlinkManager) Serve(ctx context.Context) error {
	errCh := make(chan error)
	slog.Info("bgp: starting bgp fsm")

	go func() {
		err := n.bgp.Serve([]net.Listener{})
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		slog.Info("teardown: closing server")
		return nil
	case err := <-errCh:
		return fmt.Errorf("netlink: error from manager: %v", err)
	}
}

// HasUnicastService returns true if a unicast service is currently provisioned.
func (n *NetlinkManager) HasUnicastService() bool { return n.UnicastService != nil }

// HasMulticastService returns true if a multicast service is currently provisioned.
func (n *NetlinkManager) HasMulticastService() bool { return n.MulticastService != nil }

// ResolveTunnelSrc performs a kernel route lookup to determine the source IP
// that would be used to reach the given destination.
func (n *NetlinkManager) ResolveTunnelSrc(dst net.IP) (net.IP, error) {
	routes, err := n.netlink.RouteGet(dst)
	if err != nil {
		return nil, fmt.Errorf("route lookup failed: %w", err)
	}
	for _, route := range routes {
		if route.Dst != nil && route.Dst.IP.Equal(dst) {
			return route.Src, nil
		}
	}
	return nil, fmt.Errorf("no route found to %s", dst)
}

// TODO: this contains some workarounds that will be removed when multicast
// is fully implemented. For now, we only return the status of the unicast
// service.
//
// Status returns the status of all provisioned services.
func (n *NetlinkManager) Status() ([]*api.StatusResponse, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	resp := []*api.StatusResponse{}
	if n.UnicastService != nil {
		status, err := n.UnicastService.Status()
		if err != nil {
			return nil, fmt.Errorf("error getting unicast service status: %v", err)
		}
		// TODO: remove this during multicast work
		resp = append(resp, status)
	}
	if n.MulticastService != nil {
		status, err := n.MulticastService.Status()
		if err != nil {
			return nil, fmt.Errorf("error getting multicast service status: %v", err)
		}
		resp = append(resp, status)
	}
	return resp, nil
}

// GetProvisionedServices returns the provision requests for all active services.
func (n *NetlinkManager) GetProvisionedServices() []*api.ProvisionRequest {
	n.mu.Lock()
	defer n.mu.Unlock()

	var reqs []*api.ProvisionRequest
	if n.UnicastService != nil {
		if pr := n.UnicastService.ProvisionRequest(); pr != nil {
			reqs = append(reqs, pr)
		}
	}
	if n.MulticastService != nil {
		if pr := n.MulticastService.ProvisionRequest(); pr != nil {
			reqs = append(reqs, pr)
		}
	}
	return reqs
}
