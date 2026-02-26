package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/bgp"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/latency"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/routing"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/services"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

const (
	defaultPollInterval = 10 * time.Second
	fetchTimeout        = 20 * time.Second
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

// Fetcher is the interface for fetching onchain program data.
type Fetcher interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

// LatencyProvider is the interface for retrieving cached latency results.
type LatencyProvider interface {
	GetResultsCache() []latency.LatencyResult
}

// Option configures a NetlinkManager.
type Option func(*NetlinkManager)

// WithClientIP sets the client IP for the reconciler.
func WithClientIP(ip net.IP) Option {
	return func(n *NetlinkManager) {
		n.clientIP = ip
	}
}

// WithFetcher sets the onchain data fetcher for the reconciler.
func WithFetcher(f Fetcher) Option {
	return func(n *NetlinkManager) {
		n.fetcher = f
	}
}

// WithPollInterval sets the reconciler poll interval.
func WithPollInterval(d time.Duration) Option {
	return func(n *NetlinkManager) {
		n.pollInterval = d
	}
}

// WithEnabled sets the initial reconciler enabled state.
func WithEnabled(enabled bool) Option {
	return func(n *NetlinkManager) {
		n.enabled.Store(enabled)
	}
}

// WithStateDir sets the directory for reconciler state persistence.
func WithStateDir(dir string) Option {
	return func(n *NetlinkManager) {
		n.stateDir = dir
	}
}

// WithLatencyProvider sets the latency provider for status enrichment.
func WithLatencyProvider(lp LatencyProvider) Option {
	return func(n *NetlinkManager) {
		n.latencyProvider = lp
	}
}

// WithNetwork sets the network moniker (e.g. "mainnet", "testnet").
func WithNetwork(network string) Option {
	return func(n *NetlinkManager) {
		n.network = network
	}
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

	// Reconciler fields
	clientIP       net.IP
	fetcher        Fetcher
	pollInterval   time.Duration
	enabled        atomic.Bool
	enableCh       chan bool
	stateDir       string
	tunnelSrcCache map[string]net.IP // cached resolved tunnel src keyed by dst IP string

	// Status enrichment fields
	latencyProvider LatencyProvider
	network         string
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

func NewNetlinkManager(netlink routing.Netlinker, bgp BGPServer, pim services.PIMWriter, heartbeat services.HeartbeatWriter, opts ...Option) *NetlinkManager {
	n := &NetlinkManager{
		netlink:        netlink,
		bgp:            bgp,
		pim:            pim,
		heartbeat:      heartbeat,
		pollInterval:   defaultPollInterval,
		enableCh:       make(chan bool, 1),
		tunnelSrcCache: make(map[string]net.IP),
	}
	for _, o := range opts {
		o(n)
	}
	return n
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
func (n *NetlinkManager) HasUnicastService() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.UnicastService != nil
}

// HasMulticastService returns true if a multicast service is currently provisioned.
func (n *NetlinkManager) HasMulticastService() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.MulticastService != nil
}

// ResolveTunnelSrc performs a kernel route lookup to determine the source IP
// that would be used to reach the given destination.
func (n *NetlinkManager) ResolveTunnelSrc(dst net.IP) (net.IP, error) {
	routes, err := n.netlink.RouteGet(dst)
	if err != nil {
		return nil, fmt.Errorf("route lookup failed: %w", err)
	}
	var firstSrc net.IP
	for _, route := range routes {
		if route.Src != nil && firstSrc == nil {
			firstSrc = route.Src
		}
		if route.Dst != nil && route.Dst.IP.Equal(dst) && route.Src != nil {
			return route.Src, nil
		}
	}
	if firstSrc != nil {
		return firstSrc, nil
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

// SetEnabled sends an enable/disable signal to the reconciler loop.
// Non-blocking: drains any pending value so back-to-back calls don't hang.
// No-op if the reconciler is already in the requested state.
func (n *NetlinkManager) SetEnabled(enabled bool) {
	if n.enabled.Load() == enabled {
		return
	}
	// Drain any pending value so we don't block.
	select {
	case <-n.enableCh:
	default:
	}
	n.enableCh <- enabled
}

// Enabled returns the current reconciler enabled state.
func (n *NetlinkManager) Enabled() bool {
	return n.enabled.Load()
}

// StartReconciler runs the reconciliation loop until the context is cancelled.
func (n *NetlinkManager) StartReconciler(ctx context.Context) error {
	if n.enabled.Load() {
		n.reconcile(ctx)
	}

	ticker := time.NewTicker(n.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("reconciler: stopping")
			return nil
		case enabled := <-n.enableCh:
			n.enabled.Store(enabled)
			if enabled {
				slog.Info("reconciler: enabled")
				n.reconcile(ctx)
			} else {
				slog.Info("reconciler: disabled, tearing down services")
				n.reconcilerTeardown()
			}
		case <-ticker.C:
			if n.enabled.Load() {
				n.reconcile(ctx)
			}
		}
	}
}

func (n *NetlinkManager) reconcilerTeardown() {
	if n.HasUnicastService() {
		if err := n.Remove(api.UserTypeIBRL); err != nil {
			slog.Error("reconciler: error removing unicast service during teardown", "error", err)
		}
	}
	if n.HasMulticastService() {
		if err := n.Remove(api.UserTypeMulticast); err != nil {
			slog.Error("reconciler: error removing multicast service during teardown", "error", err)
		}
	}
	// Clear cached tunnel src so a fresh lookup is done on next enable.
	n.tunnelSrcCache = make(map[string]net.IP)
}

func (n *NetlinkManager) reconcile(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	data, err := n.fetcher.GetProgramData(ctx)
	if err != nil {
		metricPollsTotal.WithLabelValues(statusError).Inc()
		slog.Error("reconciler: error fetching program data", "error", err)
		return
	}

	// Build lookup maps
	devicesByPK := make(map[[32]byte]serviceability.Device, len(data.Devices))
	for _, d := range data.Devices {
		devicesByPK[d.PubKey] = d
	}

	mcastGroupsByPK := make(map[[32]byte]serviceability.MulticastGroup, len(data.MulticastGroups))
	for _, mg := range data.MulticastGroups {
		mcastGroupsByPK[mg.PubKey] = mg
	}

	// Collect all DZ prefixes from all devices
	var allPrefixes []*net.IPNet
	for _, d := range data.Devices {
		for _, p := range d.DzPrefixes {
			if prefix := parseOnchainNet(p); prefix != nil {
				allPrefixes = append(allPrefixes, prefix)
			}
		}
	}

	// Filter users matching our client IP and Activated status.
	// Only one unicast and one multicast user should match per client IP;
	// if multiple are found we log a warning and use the first.
	var wantUnicast, wantMulticast []serviceability.User
	for _, u := range data.Users {
		userIP := net.IP(u.ClientIp[:])
		if !userIP.Equal(n.clientIP) {
			continue
		}
		if u.Status != serviceability.UserStatusActivated {
			continue
		}

		daemonUserType := mapUserType(u.UserType)
		if daemonUserType == api.UserTypeUnknown {
			continue
		}
		if services.IsUnicastUser(daemonUserType) {
			if len(wantUnicast) > 0 {
				slog.Warn("reconciler: multiple activated unicast users for this client IP, ignoring extra", "user_type", daemonUserType)
				continue
			}
			wantUnicast = append(wantUnicast, u)
		}
		if services.IsMulticastUser(daemonUserType) {
			if len(wantMulticast) > 0 {
				slog.Warn("reconciler: multiple activated multicast users for this client IP, ignoring extra", "user_type", daemonUserType)
				continue
			}
			wantMulticast = append(wantMulticast, u)
		}
	}

	metricPollsTotal.WithLabelValues(statusSuccess).Inc()
	metricMatchedUsers.WithLabelValues(serviceUnicast).Set(float64(len(wantUnicast)))
	metricMatchedUsers.WithLabelValues(serviceMulticast).Set(float64(len(wantMulticast)))

	// Reconcile unicast and multicast services
	n.reconcileService(wantUnicast, n.HasUnicastService(), serviceUnicast, api.UserTypeIBRL, devicesByPK, mcastGroupsByPK, allPrefixes, data.Config)
	n.reconcileService(wantMulticast, n.HasMulticastService(), serviceMulticast, api.UserTypeMulticast, devicesByPK, mcastGroupsByPK, allPrefixes, data.Config)
}

func (n *NetlinkManager) reconcileService(
	wantUsers []serviceability.User,
	hasService bool,
	serviceType string,
	removeAsType api.UserType,
	devicesByPK map[[32]byte]serviceability.Device,
	mcastGroupsByPK map[[32]byte]serviceability.MulticastGroup,
	allPrefixes []*net.IPNet,
	cfg serviceability.Config,
) {
	if len(wantUsers) > 0 {
		u := wantUsers[0]
		pr, err := n.buildProvisionRequest(u, devicesByPK, mcastGroupsByPK, allPrefixes, cfg)
		if err != nil {
			slog.Error("reconciler: error building provision request", "service", serviceType, "error", err)
			metricProvisionsTotal.WithLabelValues(serviceType, statusError).Inc()
			return
		}

		if hasService {
			// Service already provisioned — check if onchain state has drifted
			// (e.g. multicast groups added/removed). If identical, nothing to do.
			currentPR := n.currentProvisionRequest(serviceType)
			if currentPR != nil && currentPR.Equal(&pr) {
				return
			}
			slog.Info("reconciler: onchain state changed, re-provisioning service", "service", serviceType)
			if err := n.Remove(removeAsType); err != nil {
				slog.Error("reconciler: error removing service for re-provision", "service", serviceType, "error", err)
				metricRemovalsTotal.WithLabelValues(serviceType, statusError).Inc()
				return
			}
			metricRemovalsTotal.WithLabelValues(serviceType, statusSuccess).Inc()
		}

		slog.Info("reconciler: provisioning service", "service", serviceType, "user_type", pr.UserType)
		if err := n.Provision(pr); err != nil {
			slog.Error("reconciler: error provisioning service", "service", serviceType, "error", err)
			metricProvisionsTotal.WithLabelValues(serviceType, statusError).Inc()
		} else {
			metricProvisionsTotal.WithLabelValues(serviceType, statusSuccess).Inc()
		}
	} else if hasService {
		slog.Info("reconciler: removing service", "service", serviceType)
		if err := n.Remove(removeAsType); err != nil {
			slog.Error("reconciler: error removing service", "service", serviceType, "error", err)
			metricRemovalsTotal.WithLabelValues(serviceType, statusError).Inc()
		} else {
			metricRemovalsTotal.WithLabelValues(serviceType, statusSuccess).Inc()
		}
	}
}

// currentProvisionRequest returns the ProvisionRequest for the currently
// provisioned service of the given type, or nil if none is provisioned.
func (n *NetlinkManager) currentProvisionRequest(serviceType string) *api.ProvisionRequest {
	switch serviceType {
	case serviceUnicast:
		if n.UnicastService != nil {
			return n.UnicastService.ProvisionRequest()
		}
	case serviceMulticast:
		if n.MulticastService != nil {
			return n.MulticastService.ProvisionRequest()
		}
	}
	return nil
}

func (n *NetlinkManager) buildProvisionRequest(
	u serviceability.User,
	devicesByPK map[[32]byte]serviceability.Device,
	mcastGroupsByPK map[[32]byte]serviceability.MulticastGroup,
	allPrefixes []*net.IPNet,
	cfg serviceability.Config,
) (api.ProvisionRequest, error) {
	// Resolve device
	devPK := [32]byte(u.DevicePubKey)
	device, ok := devicesByPK[devPK]
	if !ok {
		return api.ProvisionRequest{}, fmt.Errorf("device not found for user")
	}

	tunnelNet := parseOnchainNet(u.TunnelNet)
	if tunnelNet == nil {
		return api.ProvisionRequest{}, fmt.Errorf("invalid tunnel net: %v", u.TunnelNet)
	}

	// Use the user's assigned tunnel endpoint; fall back to device public IP
	// when unset (0.0.0.0) for backwards compatibility.
	tunnelDst := net.IP(u.TunnelEndpoint[:])
	if tunnelDst.Equal(net.IPv4zero) || tunnelDst.IsUnspecified() {
		tunnelDst = net.IP(device.PublicIp[:])
	}
	// Resolve the tunnel source from the routing table for user types that
	// need it. IBRLWithAllocatedIP and Multicast users may be behind NAT
	// where the public IP isn't bound to a local interface, so we look up
	// the actual outgoing interface IP. Regular IBRL uses client_ip directly,
	// matching the CLI behavior.
	//
	// The result is cached per destination IP so we don't repeat the kernel
	// route lookup every reconcile cycle.
	tunnelSrc := n.clientIP
	if u.UserType == serviceability.UserTypeIBRLWithAllocatedIP || u.UserType == serviceability.UserTypeMulticast {
		dstKey := tunnelDst.String()
		if cached, ok := n.tunnelSrcCache[dstKey]; ok {
			tunnelSrc = cached
		} else if resolved, err := n.ResolveTunnelSrc(tunnelDst); err == nil && resolved != nil {
			slog.Info("reconciler: resolved tunnel src", "dst", tunnelDst, "src", resolved)
			tunnelSrc = resolved
			n.tunnelSrcCache[dstKey] = resolved
		} else if err != nil {
			slog.Warn("reconciler: failed to resolve tunnel src, using client IP", "dst", tunnelDst, "error", err)
		} else {
			slog.Debug("reconciler: no route-based src found, using client IP", "dst", tunnelDst, "src", tunnelSrc)
		}
	}

	// Resolve multicast groups
	var pubGroups, subGroups []net.IP
	for _, pubPK := range u.Publishers {
		pk := [32]byte(pubPK)
		if mg, ok := mcastGroupsByPK[pk]; ok {
			pubGroups = append(pubGroups, net.IP(mg.MulticastIp[:]))
		}
	}
	for _, subPK := range u.Subscribers {
		pk := [32]byte(subPK)
		if mg, ok := mcastGroupsByPK[pk]; ok {
			subGroups = append(subGroups, net.IP(mg.MulticastIp[:]))
		}
	}

	return api.ProvisionRequest{
		UserType:           mapUserType(u.UserType),
		TunnelSrc:          tunnelSrc,
		TunnelDst:          tunnelDst,
		TunnelNet:          tunnelNet,
		DoubleZeroIP:       net.IP(u.DzIp[:]),
		DoubleZeroPrefixes: allPrefixes,
		BgpLocalAsn:        cfg.Local_asn,
		BgpRemoteAsn:       cfg.Remote_asn,
		MulticastPubGroups: pubGroups,
		MulticastSubGroups: subGroups,
	}, nil
}

// mapUserType maps onchain UserUserType to daemon api.UserType.
func mapUserType(ut serviceability.UserUserType) api.UserType {
	switch ut {
	case serviceability.UserTypeIBRL:
		return api.UserTypeIBRL
	case serviceability.UserTypeIBRLWithAllocatedIP:
		return api.UserTypeIBRLWithAllocatedIP
	case serviceability.UserTypeEdgeFiltering:
		return api.UserTypeEdgeFiltering
	case serviceability.UserTypeMulticast:
		return api.UserTypeMulticast
	default:
		return api.UserTypeUnknown
	}
}

// parseOnchainNet converts a [5]uint8 (4 bytes IP + 1 byte prefix length) to *net.IPNet.
func parseOnchainNet(raw [5]uint8) *net.IPNet {
	ip := net.IPv4(raw[0], raw[1], raw[2], raw[3])
	prefixLen := int(raw[4])
	if prefixLen > 32 {
		return nil
	}
	mask := net.CIDRMask(prefixLen, 32)
	return &net.IPNet{
		IP:   ip.Mask(mask),
		Mask: mask,
	}
}
