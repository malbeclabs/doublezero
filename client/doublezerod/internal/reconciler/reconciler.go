package reconciler

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"time"

	"github.com/malbeclabs/doublezero/client/doublezerod/internal/api"
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
}

type Option func(*Reconciler)

func WithPollInterval(d time.Duration) Option {
	return func(r *Reconciler) {
		r.pollInterval = d
	}
}

// NewReconciler creates a new Reconciler.
func NewReconciler(clientIP net.IP, mgr Manager, fetcher Fetcher, opts ...Option) *Reconciler {
	r := &Reconciler{
		clientIP:     clientIP,
		manager:      mgr,
		fetcher:      fetcher,
		pollInterval: defaultPollInterval,
	}
	for _, o := range opts {
		o(r)
	}
	return r
}

// Start runs the reconciliation loop until the context is cancelled.
func (r *Reconciler) Start(ctx context.Context) error {
	r.reconcile(ctx)

	ticker := time.NewTicker(r.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			slog.Info("reconciler: stopping")
			return nil
		case <-ticker.C:
			r.reconcile(ctx)
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

	// Filter users matching our client IP and Activated status
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
		if isUnicastUser(daemonUserType) {
			wantUnicast = append(wantUnicast, u)
		}
		if isMulticastUser(daemonUserType) {
			wantMulticast = append(wantMulticast, u)
		}
	}

	metricPollsTotal.WithLabelValues(statusSuccess).Inc()
	metricMatchedUsers.WithLabelValues(serviceUnicast).Set(float64(len(wantUnicast)))
	metricMatchedUsers.WithLabelValues(serviceMulticast).Set(float64(len(wantMulticast)))

	// Reconcile unicast
	if len(wantUnicast) > 0 && !r.manager.HasUnicastService() {
		u := wantUnicast[0]
		pr, err := r.buildProvisionRequest(u, devicesByPK, mcastGroupsByPK, allPrefixes, data.Config)
		if err != nil {
			slog.Error("reconciler: error building unicast provision request", "error", err)
			metricProvisionsTotal.WithLabelValues(serviceUnicast, statusError).Inc()
		} else {
			slog.Info("reconciler: provisioning unicast service", "user_type", pr.UserType)
			if err := r.manager.Provision(pr); err != nil {
				slog.Error("reconciler: error provisioning unicast service", "error", err)
				metricProvisionsTotal.WithLabelValues(serviceUnicast, statusError).Inc()
			} else {
				metricProvisionsTotal.WithLabelValues(serviceUnicast, statusSuccess).Inc()
			}
		}
	} else if len(wantUnicast) == 0 && r.manager.HasUnicastService() {
		slog.Info("reconciler: removing unicast service")
		if err := r.manager.Remove(api.UserTypeIBRL); err != nil {
			slog.Error("reconciler: error removing unicast service", "error", err)
			metricRemovalsTotal.WithLabelValues(serviceUnicast, statusError).Inc()
		} else {
			metricRemovalsTotal.WithLabelValues(serviceUnicast, statusSuccess).Inc()
		}
	}

	// Reconcile multicast
	if len(wantMulticast) > 0 && !r.manager.HasMulticastService() {
		u := wantMulticast[0]
		pr, err := r.buildProvisionRequest(u, devicesByPK, mcastGroupsByPK, allPrefixes, data.Config)
		if err != nil {
			slog.Error("reconciler: error building multicast provision request", "error", err)
			metricProvisionsTotal.WithLabelValues(serviceMulticast, statusError).Inc()
		} else {
			slog.Info("reconciler: provisioning multicast service", "user_type", pr.UserType)
			if err := r.manager.Provision(pr); err != nil {
				slog.Error("reconciler: error provisioning multicast service", "error", err)
				metricProvisionsTotal.WithLabelValues(serviceMulticast, statusError).Inc()
			} else {
				metricProvisionsTotal.WithLabelValues(serviceMulticast, statusSuccess).Inc()
			}
		}
	} else if len(wantMulticast) == 0 && r.manager.HasMulticastService() {
		slog.Info("reconciler: removing multicast service")
		if err := r.manager.Remove(api.UserTypeMulticast); err != nil {
			slog.Error("reconciler: error removing multicast service", "error", err)
			metricRemovalsTotal.WithLabelValues(serviceMulticast, statusError).Inc()
		} else {
			metricRemovalsTotal.WithLabelValues(serviceMulticast, statusSuccess).Inc()
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

	// Resolve tunnel source IP via kernel route lookup, fall back to clientIP
	tunnelDst := net.IP(device.PublicIp[:])
	tunnelSrc := r.clientIP
	if resolved, err := r.manager.ResolveTunnelSrc(tunnelDst); err == nil && resolved != nil {
		slog.Info("reconciler: resolved tunnel src", "dst", tunnelDst, "src", resolved)
		tunnelSrc = resolved
	} else if err != nil {
		slog.Warn("reconciler: failed to resolve tunnel src, using client IP", "dst", tunnelDst, "error", err)
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

func isUnicastUser(u api.UserType) bool {
	return u == api.UserTypeIBRL || u == api.UserTypeIBRLWithAllocatedIP || u == api.UserTypeEdgeFiltering
}

func isMulticastUser(u api.UserType) bool {
	return u == api.UserTypeMulticast
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
