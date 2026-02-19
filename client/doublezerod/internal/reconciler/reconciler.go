package reconciler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
	"github.com/malbeclabs/doublezero/client/doublezerod/internal/services"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

const (
	defaultPollInterval = 10 * time.Second
	fetchTimeout        = 20 * time.Second
)

// Manager is the interface for provisioning and deprovisioning services.
type Manager interface {
	Provision(api.ProvisionRequest) error
	Remove(api.UserType) error
	HasUnicastService() bool
	HasMulticastService() bool
	ResolveTunnelSrc(dst net.IP) (net.IP, error)
	Status() ([]*api.StatusResponse, error)
}

// Fetcher is the interface for fetching onchain program data.
type Fetcher interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

// Reconciler watches onchain User state and reconciles it against
// in-memory service state, automatically provisioning or removing
// tunnels as users are activated or deactivated onchain.
type Reconciler struct {
	clientIP     net.IP
	manager      Manager
	fetcher      Fetcher
	pollInterval time.Duration
	enabled      atomic.Bool
	enableCh     chan bool
	stateDir     string
}

type Option func(*Reconciler)

func WithPollInterval(d time.Duration) Option {
	return func(r *Reconciler) {
		r.pollInterval = d
	}
}

func WithEnabled(enabled bool) Option {
	return func(r *Reconciler) {
		r.enabled.Store(enabled)
	}
}

func WithStateDir(dir string) Option {
	return func(r *Reconciler) {
		r.stateDir = dir
	}
}

// NewReconciler creates a new Reconciler.
func NewReconciler(clientIP net.IP, mgr Manager, fetcher Fetcher, opts ...Option) *Reconciler {
	r := &Reconciler{
		clientIP:     clientIP,
		manager:      mgr,
		fetcher:      fetcher,
		pollInterval: defaultPollInterval,
		enableCh:     make(chan bool, 1),
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// SetEnabled sends an enable/disable signal to the reconciler loop.
// Non-blocking: drains any pending value so back-to-back calls don't hang.
func (r *Reconciler) SetEnabled(enabled bool) {
	// Drain any pending value so we don't block.
	select {
	case <-r.enableCh:
	default:
	}
	r.enableCh <- enabled
}

// Enabled returns the current enabled state.
func (r *Reconciler) Enabled() bool {
	return r.enabled.Load()
}

// Start runs the reconciliation loop until the context is cancelled.
func (r *Reconciler) Start(ctx context.Context) error {
	if r.enabled.Load() {
		r.reconcile(ctx)
	}

	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("reconciler: stopping")
			return nil
		case enabled := <-r.enableCh:
			r.enabled.Store(enabled)
			if enabled {
				slog.Info("reconciler: enabled")
				r.reconcile(ctx)
			} else {
				slog.Info("reconciler: disabled, tearing down services")
				r.teardown()
			}
		case <-ticker.C:
			if r.enabled.Load() {
				r.reconcile(ctx)
			}
		}
	}
}

func (r *Reconciler) teardown() {
	if r.manager.HasUnicastService() {
		if err := r.manager.Remove(api.UserTypeIBRL); err != nil {
			slog.Error("reconciler: error removing unicast service during teardown", "error", err)
		}
	}
	if r.manager.HasMulticastService() {
		if err := r.manager.Remove(api.UserTypeMulticast); err != nil {
			slog.Error("reconciler: error removing multicast service during teardown", "error", err)
		}
	}
}

func (r *Reconciler) reconcile(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()

	data, err := r.fetcher.GetProgramData(ctx)
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
		if !userIP.Equal(r.clientIP) {
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
	r.reconcileService(wantUnicast, r.manager.HasUnicastService(), serviceUnicast, api.UserTypeIBRL, devicesByPK, mcastGroupsByPK, allPrefixes, data.Config)
	r.reconcileService(wantMulticast, r.manager.HasMulticastService(), serviceMulticast, api.UserTypeMulticast, devicesByPK, mcastGroupsByPK, allPrefixes, data.Config)
}

func (r *Reconciler) reconcileService(
	wantUsers []serviceability.User,
	hasService bool,
	serviceType string,
	removeAsType api.UserType,
	devicesByPK map[[32]byte]serviceability.Device,
	mcastGroupsByPK map[[32]byte]serviceability.MulticastGroup,
	allPrefixes []*net.IPNet,
	cfg serviceability.Config,
) {
	if len(wantUsers) > 0 && !hasService {
		u := wantUsers[0]
		pr, err := r.buildProvisionRequest(u, devicesByPK, mcastGroupsByPK, allPrefixes, cfg)
		if err != nil {
			slog.Error("reconciler: error building provision request", "service", serviceType, "error", err)
			metricProvisionsTotal.WithLabelValues(serviceType, statusError).Inc()
		} else {
			slog.Info("reconciler: provisioning service", "service", serviceType, "user_type", pr.UserType)
			if err := r.manager.Provision(pr); err != nil {
				slog.Error("reconciler: error provisioning service", "service", serviceType, "error", err)
				metricProvisionsTotal.WithLabelValues(serviceType, statusError).Inc()
			} else {
				metricProvisionsTotal.WithLabelValues(serviceType, statusSuccess).Inc()
			}
		}
	} else if len(wantUsers) == 0 && hasService {
		slog.Info("reconciler: removing service", "service", serviceType)
		if err := r.manager.Remove(removeAsType); err != nil {
			slog.Error("reconciler: error removing service", "service", serviceType, "error", err)
			metricRemovalsTotal.WithLabelValues(serviceType, statusError).Inc()
		} else {
			metricRemovalsTotal.WithLabelValues(serviceType, statusSuccess).Inc()
		}
	}
}

func (r *Reconciler) buildProvisionRequest(
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
	tunnelSrc := r.clientIP
	if resolved, err := r.manager.ResolveTunnelSrc(tunnelDst); err == nil && resolved != nil {
		slog.Info("reconciler: resolved tunnel src", "dst", tunnelDst, "src", resolved)
		tunnelSrc = resolved
	} else if err != nil {
		slog.Warn("reconciler: failed to resolve tunnel src, using client IP", "dst", tunnelDst, "error", err)
	} else {
		slog.Debug("reconciler: no route-based src found, using client IP", "dst", tunnelDst, "src", tunnelSrc)
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

// V2StatusResponse is the response for the /v2/status endpoint.
type V2StatusResponse struct {
	ReconcilerEnabled bool                  `json:"reconciler_enabled"`
	Services          []*api.StatusResponse `json:"services"`
}

// ServeEnable handles POST /enable requests.
func (r *Reconciler) ServeEnable(w http.ResponseWriter, _ *http.Request) {
	if err := WriteState(r.stateDir, true); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "description": err.Error()}) //nolint:errcheck
		return
	}
	r.SetEnabled(true)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// ServeDisable handles POST /disable requests.
func (r *Reconciler) ServeDisable(w http.ResponseWriter, _ *http.Request) {
	if err := WriteState(r.stateDir, false); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "description": err.Error()}) //nolint:errcheck
		return
	}
	r.SetEnabled(false)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// ServeV2Status handles GET /v2/status requests.
func (r *Reconciler) ServeV2Status(w http.ResponseWriter, _ *http.Request) {
	services, err := r.manager.Status()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"status": "error", "description": err.Error()}) //nolint:errcheck
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(V2StatusResponse{ //nolint:errcheck
		ReconcilerEnabled: r.enabled.Load(),
		Services:          services,
	})
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
