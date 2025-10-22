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
}

// BgpReaderWriter is an interface for the handling of per
// service bgp sessions.
type BGPServer interface {
	Serve([]net.Listener) error
	AddPeer(*bgp.PeerConfig, []bgp.NLRI) error
	DeletePeer(net.IP) error
	GetPeerStatus(net.IP) bgp.Session
}

// DbReaderWriter is an interface for managing the state of
// services. This is used to persist the last provisioned state
// to disk so we can recover from it on restart/crash.
type DbReaderWriter interface {
	GetState(userTypes ...api.UserType) []*api.ProvisionRequest
	DeleteState(u api.UserType) error
	SaveState(p *api.ProvisionRequest) error
}

type NetlinkManager struct {
	netlink          routing.Netlinker
	Routes           []*routing.Route
	Rules            []*routing.IPRule
	UnicastService   Provisioner
	MulticastService Provisioner
	DoubleZeroAddr   net.IP
	bgp              BGPServer
	db               services.DBReaderWriter
	services         map[api.UserType]Provisioner
	mu               sync.Mutex
}

func NewNetlinkManager(netlink routing.Netlinker, bgp BGPServer, db services.DBReaderWriter, services map[api.UserType]Provisioner) *NetlinkManager {
	manager := &NetlinkManager{netlink: netlink, bgp: bgp, db: db, services: services}
	return manager
}

// Provision is the entry point for all user tunnel provisioning. This currently
// contains logic for IBRL, edge filtering and multicast use cases. After the user
// tunnel is provisioned, the original request is saved to disk so we're able to
// handle service restarts.
func (n *NetlinkManager) Provision(pr api.ProvisionRequest) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	svc, ok := n.services[pr.UserType]
	if !ok {
		return fmt.Errorf("error provisioning: unsupported user type: %s", pr.UserType.String())
	}

	if n.UnicastService != nil || n.MulticastService != nil {
		return fmt.Errorf("cannot provision multiple tunnels at the same time")
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

	if err := n.db.SaveState(&pr); err != nil {
		return fmt.Errorf("db: error saving state file: %v", err)
	}
	return nil
}

// Remove is the entry point for service deprovisioning.
func (n *NetlinkManager) Remove(u api.UserType) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	// We've never been provisioned
	if n.db.GetState() == nil {
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

	// Delete state so we don't reprovision ourselves on restart
	if err := n.db.DeleteState(u); err != nil {
		return fmt.Errorf("db: error deleting state file: %v", err)
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

// Serve starts the manager and attempts to recover from the last provisioned state.
func (n *NetlinkManager) Serve(ctx context.Context) error {
	errCh := make(chan error)
	slog.Info("bgp: starting bgp fsm")

	go func() {
		err := n.bgp.Serve([]net.Listener{})
		errCh <- err
	}()

	// attempt to recover from last provisioned state
	if err := n.Recover(); err != nil {
		slog.Error("netlink: error recovering provisioned state", "error", err)
	}

	select {
	case <-ctx.Done():
		slog.Info("teardown: closing server")
		return nil
	case err := <-errCh:
		return fmt.Errorf("netlink: error from manager: %v", err)
	}
}

// Recover attempts to recover from the last provisioned state.
func (n *NetlinkManager) Recover() error {
	// check last provisioned state and attempt to recover
	state := n.db.GetState()
	if state == nil {
		return nil
	}
	slog.Info("netlink: restoring previous provisioned state")
	// TODO: check state to make sure we adhere to our service iteraction rules
	for _, p := range state {
		if err := n.Provision(*p); err != nil {
			return fmt.Errorf("netlink: error while recovering provisioned state: %v", err)
		}
	}
	return nil
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
