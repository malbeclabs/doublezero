package controller

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/gogo/protobuf/proto"

	"github.com/malbeclabs/doublezero/config"
	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

const (
	ISISAreaID          = "49"
	ISISAreaNumber      = "0000"
	ISISSystemIDPadding = "0000"
	ISISNSelector       = "00"

	bgpCommunityMinValid = 10000
	bgpCommunityMaxValid = 10999
)

var (
	ErrServiceabilityRequired = errors.New("serviceability program client is required")
	ErrLoggerRequired         = errors.New("logger is required")
)

type ServiceabilityProgramClient interface {
	GetProgramData(context.Context) (*serviceability.ProgramData, error)
	ProgramID() solana.PublicKey
}

type stateCache struct {
	Config          serviceability.Config
	Devices         map[string]*Device
	MulticastGroups map[string]serviceability.MulticastGroup
	Tenants         map[string]serviceability.Tenant
	UnicastVrfs     []uint16
	Vpnv4BgpPeers   []BgpPeer
	Ipv4BgpPeers    []BgpPeer
}

type Controller struct {
	pb.UnimplementedControllerServer

	log            *slog.Logger
	cache          stateCache
	mu             sync.RWMutex
	serviceability ServiceabilityProgramClient
	listener       net.Listener
	noHardware     bool
	updateDone     chan struct{}
	tlsConfig      *tls.Config
	environment    string
	deviceLocalASN uint32
	clickhouse     *ClickhouseWriter
}

type Option func(*Controller)

func NewController(options ...Option) (*Controller, error) {
	controller := &Controller{
		cache: stateCache{},
	}
	for _, o := range options {
		o(controller)
	}
	if controller.listener == nil {
		lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", 443))
		if err != nil {
			return nil, fmt.Errorf("failed to listen: %v", err)
		}
		controller.listener = lis
	}
	if controller.serviceability == nil {
		return nil, ErrServiceabilityRequired
	}
	if controller.log == nil {
		return nil, ErrLoggerRequired
	}
	return controller, nil
}

func WithLogger(log *slog.Logger) Option {
	return func(c *Controller) {
		c.log = log
	}
}

func WithServiceabilityProgramClient(s ServiceabilityProgramClient) Option {
	return func(c *Controller) {
		c.serviceability = s
	}
}

// WithListener provides a way to assign a custom listener for the gRPC server.
// If no listener is passed, the controller will listen on localhost.
func WithListener(listener net.Listener) Option {
	return func(c *Controller) {
		c.listener = listener
	}
}

// WithTLSConfig provides a way to assign a custom tls config for the gRPC server.
func WithTLSConfig(tlsConfig *tls.Config) Option {
	return func(c *Controller) {
		c.tlsConfig = tlsConfig
	}
}

// WithSignalChan provides a way to be signaled when the local state cache
// has been updated. This is used for testing.
func WithSignalChan(ch chan struct{}) Option {
	return func(c *Controller) {
		c.updateDone = ch
	}
}

// WithNoHardware provides a way to exclude rendering config commands that will fail when not
// running on the real hardware.
func WithNoHardware() Option {
	return func(c *Controller) {
		c.noHardware = true
	}
}

func WithEnvironment(env string) Option {
	return func(c *Controller) {
		c.environment = env
	}
}

func WithDeviceLocalASN(asn uint32) Option {
	return func(c *Controller) {
		c.deviceLocalASN = asn
	}
}

func WithClickhouse(cw *ClickhouseWriter) Option {
	return func(c *Controller) {
		c.clickhouse = cw
	}
}

// processDeviceInterfacesAndPeers processes a device's interfaces and extracts BGP peer information.
// It returns the candidate VPNv4 and IPv4 BGP peers found from the device's loopback interfaces.
func (c *Controller) processDeviceInterfacesAndPeers(device serviceability.Device, d *Device, devicePubKey string) (candidateVpnv4BgpPeer, candidateIpv4BgpPeer BgpPeer) {
	for _, iface := range device.Interfaces {
		intf, err := toInterface(iface)
		if err != nil {
			// TODO: metric
			c.log.Error("failed to convert serviceability interface to controller interface", "device pubkey", devicePubKey, "iface", iface, "error", err)
			continue
		}
		// Create a parent interface since they're not defined on-chain.
		if intf.IsSubInterface {
			parent, err := intf.GetParent()
			if err != nil {
				c.log.Error("failed to get parent interface for subinterface", "device pubkey", devicePubKey, "iface", intf.Name, "error", err)
				continue
			}
			d.Interfaces = append(d.Interfaces, parent)
		}
		d.Interfaces = append(d.Interfaces, intf)
	}
	sort.Slice(d.Interfaces, func(i, j int) bool {
		return d.Interfaces[i].Name < d.Interfaces[j].Name
	})

	// Build list of peers from device interfaces
	for _, iface := range d.Interfaces {
		if iface.InterfaceType == InterfaceTypeLoopback &&
			iface.LoopbackType == LoopbackTypeVpnv4 {
			d.Vpn4vLoopbackIP = iface.Ip.Addr().AsSlice()
			d.Vpn4vLoopbackIntfName = iface.Name
			// Generate ISIS NET from the Vpn4vLoopbackIP. Format: <AreaID>.<AreaNumber>.<SystemID>.<NSelector>
			// SystemID is derived from the IP address in hex format. Example SystemId: 172.3.2.1 -> AC03.0201
			vpnIP := d.Vpn4vLoopbackIP
			if len(vpnIP) >= 4 {
				systemID := fmt.Sprintf("%02x%02x.%02x%02x", vpnIP[0], vpnIP[1], vpnIP[2], vpnIP[3])
				d.IsisNet = fmt.Sprintf("%s.%s.%s.%s.%s", ISISAreaID, ISISAreaNumber, systemID, ISISSystemIDPadding, ISISNSelector)
			} else {
				c.log.Error("Can't assign ISIS NET because VPNv4 loopback IP is invalid or empty", "device pubkey", devicePubKey, "interface", iface.Name, "ip length", len(vpnIP))
			}
			candidateVpnv4BgpPeer = BgpPeer{
				PeerIP:   d.Vpn4vLoopbackIP,
				PeerName: device.Code,
			}
		} else if iface.InterfaceType == InterfaceTypeLoopback &&
			iface.LoopbackType == LoopbackTypeIpv4 {
			d.Ipv4LoopbackIP = iface.Ip.Addr().AsSlice()
			d.Ipv4LoopbackIntfName = iface.Name
			candidateIpv4BgpPeer = BgpPeer{
				PeerIP:   d.Ipv4LoopbackIP,
				PeerName: device.Code,
			}
		}
	}
	return candidateVpnv4BgpPeer, candidateIpv4BgpPeer
}

// updateStateCache fetches the latest on-chain data for devices/users and config. User accounts
// are converted into a list of tunnels, stored under their respective device, and placed in a map,
// keyed by the device's public key.
func (c *Controller) updateStateCache(ctx context.Context) error {
	data, err := c.serviceability.GetProgramData(ctx)
	if err != nil {
		cacheUpdateFetchErrors.Inc()
		return fmt.Errorf("error while loading program data: %v", err)
	}

	devices := data.Devices
	if len(devices) == 0 {
		return fmt.Errorf("0 devices found on-chain")
	}
	users := data.Users
	if len(users) == 0 {
		c.log.Debug("0 users found on-chain")
	}
	links := data.Links
	if len(links) == 0 {
		c.log.Debug("0 links found on-chain")
	}
	exchanges := data.Exchanges
	contributors := data.Contributors
	locations := data.Locations

	// Create lookup maps for contributors, exchanges, and locations
	contributorMap := make(map[[32]byte]serviceability.Contributor)
	for _, contributor := range contributors {
		contributorMap[contributor.PubKey] = contributor
	}
	exchangeMap := make(map[[32]byte]serviceability.Exchange)
	for _, exchange := range exchanges {
		exchangeMap[exchange.PubKey] = exchange
	}
	locationMap := make(map[[32]byte]serviceability.Location)
	for _, location := range locations {
		locationMap[location.PubKey] = location
	}
	cache := stateCache{
		Config:          data.Config,
		Devices:         make(map[string]*Device),
		MulticastGroups: make(map[string]serviceability.MulticastGroup),
	}

	// TODO: valid interface checks:
	// - subinterface parent can't be defined onchain
	// - node segments can only be defined on vpnv4 loopback interfaces

	// build cache of devices
	for _, device := range devices {
		ip := net.IP(device.PublicIp[:])
		if ip == nil {
			// TODO: metric
			c.log.Error("invalid public ip for device", "device pubkey", device.PubKey)
			continue
		}

		devicePubKey := base58.Encode(device.PubKey[:])
		d := NewDevice(ip, devicePubKey)

		d.MgmtVrf = device.MgmtVrf
		d.Code = device.Code
		d.Status = device.Status

		if contributor, ok := contributorMap[device.ContributorPubKey]; ok {
			d.ContributorCode = contributor.Code
		} else {
			d.ContributorCode = "unknown"
		}

		if exchange, ok := exchangeMap[device.ExchangePubKey]; ok {
			d.ExchangeCode = exchange.Code
			d.BgpCommunity = exchange.BgpCommunity

			if exchange.BgpCommunity < bgpCommunityMinValid || exchange.BgpCommunity > bgpCommunityMaxValid {
				c.log.Warn("device has pathology", "device_pubkey", devicePubKey, "pathology", fmt.Sprintf("exchange BGP community %d is out of valid range (%d-%d)", exchange.BgpCommunity, bgpCommunityMinValid, bgpCommunityMaxValid), "exchange_code", exchange.Code)
				d.DevicePathologies = append(d.DevicePathologies, fmt.Sprintf("exchange BGP community %d is out of valid range (%d-%d)", exchange.BgpCommunity, bgpCommunityMinValid, bgpCommunityMaxValid))
			}
		} else {
			d.ExchangeCode = "unknown"
			d.BgpCommunity = 0
		}

		if location, ok := locationMap[device.LocationPubKey]; ok {
			d.LocationCode = location.Code
		} else {
			d.LocationCode = "unknown"
		}

		candidateVpnv4BgpPeer, candidateIpv4BgpPeer := c.processDeviceInterfacesAndPeers(device, d, devicePubKey)

		if len(d.Vpn4vLoopbackIP) == 0 {
			c.log.Warn("device has pathology", "device_pubkey", devicePubKey, "pathology", "no or invalid VPNv4 loopback interface found for device")
			d.DevicePathologies = append(d.DevicePathologies, "no or invalid VPNv4 loopback interface found for device")
		}

		if len(d.Ipv4LoopbackIP) == 0 {
			c.log.Warn("device has pathology", "device_pubkey", devicePubKey, "pathology", "no or invalid IPv4 loopback interface found for device")
			d.DevicePathologies = append(d.DevicePathologies, "no or invalid IPv4 loopback interface found for device")
		}

		if d.Vpn4vLoopbackIP.Equal(net.IPv4(0, 0, 0, 0)) {
			c.log.Warn("device has pathology", "device_pubkey", devicePubKey, "pathology", "VPNv4 loopback interface is unassigned (0.0.0.0)")
			d.DevicePathologies = append(d.DevicePathologies, "VPNv4 loopback interface is unassigned (0.0.0.0)")
		}

		if d.Ipv4LoopbackIP.Equal(net.IPv4(0, 0, 0, 0)) {
			c.log.Warn("device has pathology", "device_pubkey", devicePubKey, "pathology", "IPv4 loopback interface is unassigned (0.0.0.0)")
			d.DevicePathologies = append(d.DevicePathologies, "IPv4 loopback interface is unassigned (0.0.0.0)")
		}

		if d.IsisNet == "" {
			c.log.Warn("device has pathology", "device_pubkey", devicePubKey, "pathology", "ISIS NET could not be generated")
			d.DevicePathologies = append(d.DevicePathologies, "ISIS NET could not be generated")
		}

		if d.BgpCommunity == 0 {
			c.log.Warn("device has pathology", "device_pubkey", devicePubKey, "pathology", "device exchange bgp_community=0")
			d.DevicePathologies = append(d.DevicePathologies, "device exchange bgp_community=0")
		}

		if len(d.DevicePathologies) > 0 {
			cache.Devices[devicePubKey] = d
			continue
		}

		cache.Vpnv4BgpPeers = append(cache.Vpnv4BgpPeers, candidateVpnv4BgpPeer)
		cache.Ipv4BgpPeers = append(cache.Ipv4BgpPeers, candidateIpv4BgpPeer)

		// determine if interface is in an onchain link and assign metrics
		findLink := func(intf Interface) serviceability.Link {
			for _, link := range links {
				if d.PubKey == base58.Encode(link.SideAPubKey[:]) && intf.Name == link.SideAIfaceName {
					return link
				}
				if d.PubKey == base58.Encode(link.SideZPubKey[:]) && intf.Name == link.SideZIfaceName {
					return link
				}
			}
			return serviceability.Link{}
		}

		for i, iface := range d.Interfaces {
			link := findLink(iface)

			if link == (serviceability.Link{}) || (link.Status != serviceability.LinkStatusActivated && link.Status != serviceability.LinkStatusSoftDrained && link.Status != serviceability.LinkStatusHardDrained) {
				d.Interfaces[i].IsLink = false
				d.Interfaces[i].Metric = 0
				d.Interfaces[i].LinkStatus = serviceability.LinkStatusPending
				continue
			}

			if link.DelayNs <= 0 {
				linkMetricInvalid.WithLabelValues(base58.Encode(link.PubKey[:]), device.Code, iface.Name).Inc()
				continue
			}

			microseconds := math.Ceil(float64(link.DelayNs) / 1000.0)

			// apply delay override if set
			if link.DelayOverrideNs != 0 {
				if link.DelayOverrideNs < 10000 || link.DelayOverrideNs > 1_000_000_000 {
					c.log.Warn("link delay override is outside valid range (10us - 1s), ignoring", "link_pubkey", base58.Encode(link.PubKey[:]), "device_code", device.Code, "interface", iface.Name, "delay_override_ns", link.DelayOverrideNs)
				} else {
					microseconds = math.Ceil(float64(link.DelayOverrideNs) / 1000.0)
				}
			}

			// override all delay values if link is soft drained
			if link.Status == serviceability.LinkStatusSoftDrained {
				microseconds = 1000000
			}

			d.Interfaces[i].Metric = uint32(microseconds)
			d.Interfaces[i].IsLink = true
			d.Interfaces[i].LinkStatus = link.Status
			linkMetrics.WithLabelValues(device.Code, iface.Name, d.PubKey).Set(float64(d.Interfaces[i].Metric))
		}

		cache.Devices[devicePubKey] = d
	}

	// Build cache of multicast groups.
	for _, group := range data.MulticastGroups {
		cache.MulticastGroups[base58.Encode(group.PubKey[:])] = group
	}

	// Build cache of tenants keyed by base58-encoded pubkey and collect all VRF IDs.
	cache.Tenants = make(map[string]serviceability.Tenant)
	vrfSet := map[uint16]struct{}{1: {}} // always include VRF 1 as default
	for _, tenant := range data.Tenants {
		cache.Tenants[base58.Encode(tenant.PubKey[:])] = tenant
		vrfSet[tenant.VrfId] = struct{}{}
	}
	vrfs := make([]uint16, 0, len(vrfSet))
	for v := range vrfSet {
		vrfs = append(vrfs, v)
	}
	sort.Slice(vrfs, func(i, j int) bool { return vrfs[i] < vrfs[j] })
	cache.UnicastVrfs = vrfs

	// create user tunnels and add to the appropriate device
	for _, user := range users {
		if user.Status != serviceability.UserStatusActivated {
			continue
		}
		devicePubKey := base58.Encode(user.DevicePubKey[:])
		userPubKey := base58.Encode(user.PubKey[:])

		// rules for validating on-chain user data
		validUser := func() bool {
			if _, ok := cache.Devices[devicePubKey]; !ok {
				// TODO: add metric
				c.log.Error("device pubkey could be found for activated user pubkey", "device pubkey", devicePubKey, "user pubkey", userPubKey)
				return false
			}
			if user.TunnelId == 0 {
				c.log.Error("tunnel id is not set for user", "user pubkey", userPubKey)
				return false
			}
			if user.ClientIp == [4]byte{} {
				c.log.Error("client ip is not set for user", "user pubkey", userPubKey)
				return false
			}
			if user.ClientIp == [4]byte{0, 0, 0, 0} {
				slog.Error("client ip is set to 0.0.0.0 for user", "user pubkey", userPubKey)
				return false
			}
			if user.DzIp == [4]byte{0, 0, 0, 0} {
				slog.Error("DZ IP is set to 0.0.0.0 for user", "user pubkey", userPubKey)
				return false
			}
			if isBgpMartian(net.IP(user.DzIp[:])) {
				slog.Error("DZ IP is a BGP martian address", "dz_ip", net.IP(user.DzIp[:]), "user pubkey", userPubKey)
				return false
			}
			if user.TunnelNet[4] != 31 {
				c.log.Error("tunnel network mask is not 31\n", "tunnel network mask", user.TunnelNet[4])
				return false
			}
			return true
		}()

		if !validUser {
			continue
		}

		tunnel := cache.Devices[devicePubKey].findTunnel(int(user.TunnelId))
		if tunnel == nil {
			c.log.Error("unable to find tunnel slot on device for user",
				"tunnel slot", user.TunnelId,
				"device pubkey", devicePubKey,
				"user pubkey", userPubKey)
			continue
		}
		tunnel.UnderlayDstIP = net.IP(user.ClientIp[:])

		// Use user's tunnel_endpoint if set, otherwise fall back to device PublicIP.
		// This allows multiple tunnels from the same client IP to terminate on the same device
		// using different tunnel endpoint IPs (avoiding GRE key conflicts).
		tunnelEndpoint := net.IP(user.TunnelEndpoint[:])
		if tunnelEndpoint.IsUnspecified() {
			tunnel.UnderlaySrcIP = cache.Devices[devicePubKey].PublicIP
		} else {
			tunnel.UnderlaySrcIP = tunnelEndpoint
		}

		// OverlaySrcIP is the device/link side of the tunnel.
		var overlaySrc [4]byte
		copy(overlaySrc[:], user.TunnelNet[:4])
		tunnel.OverlaySrcIP = net.IP(overlaySrc[:])

		// OverlayDstIP is the client side of the tunnel.
		var overlayDst [4]byte
		copy(overlayDst[:], user.TunnelNet[:4])
		tunnel.OverlayDstIP = net.IP(overlayDst[:])
		// the client is always the odd last octet; this should really be moved into the IP allocation logic
		tunnel.OverlayDstIP[3]++

		tunnel.DzIp = net.IP(user.DzIp[:])
		tunnel.PubKey = userPubKey
		tunnel.Allocated = true

		if user.UserType != serviceability.UserTypeMulticast {
			// Set the VRF ID and metro routing from the user's tenant. Default to VRF 1 with metro routing enabled.
			tunnel.VrfId = 1
			tunnel.MetroRouting = true
			if tenant, ok := cache.Tenants[base58.Encode(user.TenantPubKey[:])]; ok {
				tunnel.VrfId = tenant.VrfId
				tunnel.MetroRouting = tenant.MetroRouting
			}
		}

		if user.UserType == serviceability.UserTypeMulticast {
			tunnel.IsMulticast = true

			boundaryList := make(map[string]struct{})

			// Set multicast subscribers for the tunnel.
			for _, subscriber := range user.Subscribers {
				if subscriberIP, ok := cache.MulticastGroups[base58.Encode(subscriber[:])]; ok {
					tunnel.MulticastSubscribers = append(tunnel.MulticastSubscribers, net.IP(subscriberIP.MulticastIp[:]))

					boundaryList[net.IP(subscriberIP.MulticastIp[:]).String()] = struct{}{}
				}
			}

			// Set multicast publishers for the tunnel.
			for _, publisher := range user.Publishers {
				if publisherIP, ok := cache.MulticastGroups[base58.Encode(publisher[:])]; ok {
					tunnel.MulticastPublishers = append(tunnel.MulticastPublishers, net.IP(publisherIP.MulticastIp[:]))

					boundaryList[net.IP(publisherIP.MulticastIp[:]).String()] = struct{}{}
				}
			}

			// Set multicast boundary list for the tunnel.
			// This is the combined and deduplicated list of subscribers and publishers.
			for ip := range boundaryList {
				tunnel.MulticastBoundaryList = append(tunnel.MulticastBoundaryList, net.ParseIP(ip))
			}
			sort.Slice(tunnel.MulticastBoundaryList, func(i, j int) bool {
				return tunnel.MulticastBoundaryList[i].String() < tunnel.MulticastBoundaryList[j].String()
			})
		}
	}

	// swap out state cache with new version
	c.log.Debug("updating state cache", "state cache", cache)
	c.swapCache(cache)
	return nil
}

// swapCache atomically updates the local state cache with the latest copy and
// if a signal channel is present, sends a notification.
func (c *Controller) swapCache(cache stateCache) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = cache
	if c.updateDone != nil {
		c.updateDone <- struct{}{}
	}
}

// Run starts a goroutine for updating the local state cache with on-chain
// data and another for a gRPC server to service devices.
func (c *Controller) Run(ctx context.Context) error {
	// start prometheus
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		http.ListenAndServe("127.0.0.1:2112", mux) //nolint
	}()

	if c.clickhouse != nil {
		if err := c.clickhouse.CreateTable(ctx); err != nil {
			c.log.Warn("error creating clickhouse table, continuing without clickhouse", "error", err)
			c.clickhouse = nil
		} else {
			go c.clickhouse.Run(ctx)
		}
	}

	// start on-chain fetcher
	go func() {
		c.log.Info("starting fetch of on-chain data", "program-id", c.serviceability.ProgramID())
		if err := c.updateStateCache(ctx); err != nil {
			cacheUpdateErrors.Inc()
			c.log.Error("error fetching accounts", "error", err)
		}
		cacheUpdateOps.Inc()
		ticker := time.NewTicker(10 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.log.Debug("updating state cache on clock tick")
				if err := c.updateStateCache(ctx); err != nil {
					cacheUpdateErrors.Inc()
					c.log.Error("error fetching accounts", "error", err)
				}
				cacheUpdateOps.Inc()
			}
		}
	}()

	// start gRPC server
	opts := []grpc.ServerOption{
		grpc.UnaryInterceptor(srvMetrics.UnaryServerInterceptor()),
	}
	if c.tlsConfig != nil {
		opts = append(opts, grpc.Creds(credentials.NewTLS(c.tlsConfig)))
	}
	server := grpc.NewServer(opts...)
	pb.RegisterControllerServer(server, c)

	errChan := make(chan error)
	go func() {
		if err := server.Serve(c.listener); err != nil {
			errChan <- err
		}
	}()

	select {
	case <-ctx.Done():
		server.GracefulStop()
		return nil
	case err := <-errChan:
		return err
	}
}

// deduplicateTunnels removes tunnels with duplicate (UnderlaySrcIP, UnderlayDstIP) pairs.
// Returns a new slice containing only unique tunnel pairs. Keeps the first occurrence of
// each pair. Logs an error and increments metrics when duplicates are found.
func (c *Controller) deduplicateTunnels(device *Device) []*Tunnel {
	if len(device.Tunnels) == 0 {
		return device.Tunnels
	}

	// Track seen (UnderlaySrcIP, UnderlayDstIP) pairs
	seen := make(map[string]*Tunnel)
	unique := make([]*Tunnel, 0, len(device.Tunnels))
	duplicateCount := 0

	for _, tunnel := range device.Tunnels {
		// Only check allocated tunnels (unallocated slots don't render config)
		if !tunnel.Allocated {
			unique = append(unique, tunnel)
			continue
		}

		// Create unique key from underlay IP pair
		key := fmt.Sprintf("%s:%s", tunnel.UnderlaySrcIP.String(), tunnel.UnderlayDstIP.String())

		if existing, found := seen[key]; found {
			// Duplicate detected - log and skip
			duplicateCount++
			c.log.Error("duplicate tunnel pair detected, removing from config",
				"device_pubkey", device.PubKey,
				"device_code", device.Code,
				"kept_tunnel_id", existing.Id,
				"kept_user_pubkey", existing.PubKey,
				"removed_tunnel_id", tunnel.Id,
				"removed_user_pubkey", tunnel.PubKey,
				"underlay_src_ip", tunnel.UnderlaySrcIP.String(),
				"underlay_dst_ip", tunnel.UnderlayDstIP.String(),
				"vrf_id", tunnel.VrfId,
			)
		} else {
			// First occurrence - keep it
			seen[key] = tunnel
			unique = append(unique, tunnel)
		}
	}

	// Increment metric if duplicates found
	if duplicateCount > 0 {
		duplicateTunnelPairs.WithLabelValues(device.PubKey, device.Code).Add(float64(duplicateCount))
	}

	return unique
}

// generateConfig renders the device configuration. It must be called with c.mu held
// because it reads from c.cache, which is updated by a background goroutine.
func (c *Controller) generateConfig(pubkey string, device *Device, unknownPeers []net.IP) (string, error) {
	// Create shallow copy of device with deduplicated tunnels
	deviceForRender := *device
	deviceForRender.Tunnels = c.deduplicateTunnels(device)

	multicastGroupBlock := formatCIDR(&c.cache.Config.MulticastGroupBlock)

	// This check avoids the situation where the template produces the following useless output, which happens in any test case with a single DZD.
	// ```
	// no router msdp
	// router msdp
	// ```
	ipv4Peers := c.cache.Ipv4BgpPeers
	if len(ipv4Peers) == 1 && ipv4Peers[0].PeerIP.Equal(deviceForRender.Ipv4LoopbackIP) {
		ipv4Peers = nil
	}

	var localASN uint32
	if c.deviceLocalASN != 0 {
		localASN = c.deviceLocalASN
	} else if c.environment != "" {
		networkConfig, err := config.NetworkConfigForEnv(c.environment)
		if err != nil {
			getConfigRenderErrors.WithLabelValues(pubkey).Inc()
			return "", status.Errorf(codes.Internal, "failed to get network config for environment %s: %v", c.environment, err)
		}
		localASN = networkConfig.DeviceLocalASN
	} else {
		getConfigRenderErrors.WithLabelValues(pubkey).Inc()
		return "", status.Errorf(codes.Internal, "device local ASN not configured")
	}

	data := templateData{
		MulticastGroupBlock:      multicastGroupBlock,
		Device:                   &deviceForRender,
		Vpnv4BgpPeers:            c.cache.Vpnv4BgpPeers,
		Ipv4BgpPeers:             ipv4Peers,
		UnknownBgpPeers:          unknownPeers,
		NoHardware:               c.noHardware,
		TelemetryTWAMPListenPort: telemetryconfig.TWAMPListenPort,
		LocalASN:                 localASN,
		UnicastVrfs:              c.cache.UnicastVrfs,
		Strings:                  StringsHelper{},
	}

	configStr, err := renderConfig(data)
	if err != nil {
		getConfigRenderErrors.WithLabelValues(pubkey).Inc()
		return "", status.Errorf(codes.Aborted, "config rendering for pubkey %s failed: %v", pubkey, err)
	}

	return configStr, nil
}

// processConfigRequest validates the request and generates the config.
// It must be called with c.mu held.
// Returns the config string and the device (for metric labeling by the caller).
func (c *Controller) processConfigRequest(req *pb.ConfigRequest) (string, *Device, error) {
	pubkey := req.GetPubkey()
	device, ok := c.cache.Devices[pubkey]
	if !ok {
		getConfigPubkeyErrors.WithLabelValues(pubkey).Inc()
		return "", nil, status.Errorf(codes.NotFound, "pubkey %s not found", pubkey)
	}
	if len(device.DevicePathologies) > 0 {
		return "", nil, status.Errorf(codes.FailedPrecondition, "cannot render config for device %s: %v", pubkey, device.DevicePathologies)
	}

	// Find unknown BGP peers that need to be removed
	peerFound := func(peer net.IP) bool {
		for _, tun := range device.Tunnels {
			if tun.OverlayDstIP.Equal(peer) {
				return true
			}
		}
		for _, bgpPeer := range c.cache.Vpnv4BgpPeers {
			if bgpPeer.PeerIP.Equal(peer) {
				return true
			}
		}
		for _, bgpPeer := range c.cache.Ipv4BgpPeers {
			if bgpPeer.PeerIP.Equal(peer) {
				return true
			}
		}
		return false
	}

	var unknownPeers []net.IP
	for _, peer := range req.GetBgpPeers() {
		ip := net.ParseIP(peer)
		if ip == nil {
			continue
		}
		if peerFound(ip) {
			continue
		}
		if isIPInBlock(ip, c.cache.Config.UserTunnelBlock) || isIPInBlock(ip, c.cache.Config.TunnelTunnelBlock) {
			unknownPeers = append(unknownPeers, ip)
		}
	}

	configStr, err := c.generateConfig(pubkey, device, unknownPeers)
	if err != nil {
		return "", nil, err
	}
	return configStr, device, nil
}

// GetConfig renders the latest device configuration based on cached device data
func (c *Controller) GetConfig(ctx context.Context, req *pb.ConfigRequest) (*pb.ConfigResponse, error) {
	reqStart := time.Now()
	c.mu.RLock()
	defer c.mu.RUnlock()

	configStr, device, err := c.processConfigRequest(req)
	if err != nil {
		return nil, err
	}

	getConfigOps.WithLabelValues(
		req.GetPubkey(),
		device.Code,
		device.ContributorCode,
		device.ExchangeCode,
		device.LocationCode,
		device.Status.String(),
		req.GetAgentVersion(),
		req.GetAgentCommit(),
		req.GetAgentDate(),
	).Inc()

	resp := &pb.ConfigResponse{Config: configStr}
	getConfigMsgSize.Observe(float64(proto.Size(resp)))
	getConfigDuration.Observe(float64(time.Since(reqStart).Seconds()))
	if c.clickhouse != nil {
		c.clickhouse.Record(getConfigEvent{
			Timestamp:    reqStart,
			DevicePubkey: req.GetPubkey(),
		})
	}
	return resp, nil
}

// GetConfigHash returns only the hash of the configuration for change detection
func (c *Controller) GetConfigHash(ctx context.Context, req *pb.ConfigRequest) (*pb.ConfigHashResponse, error) {
	reqStart := time.Now()
	c.mu.RLock()
	defer c.mu.RUnlock()

	configStr, device, err := c.processConfigRequest(req)
	if err != nil {
		return nil, err
	}

	getConfigHashOps.WithLabelValues(
		req.GetPubkey(),
		device.Code,
		device.ContributorCode,
		device.ExchangeCode,
		device.LocationCode,
		device.Status.String(),
		req.GetAgentVersion(),
		req.GetAgentCommit(),
		req.GetAgentDate(),
	).Inc()

	hash := sha256.Sum256([]byte(configStr))
	getConfigDuration.Observe(float64(time.Since(reqStart).Seconds()))
	return &pb.ConfigHashResponse{Hash: hex.EncodeToString(hash[:])}, nil
}

// formatCIDR formats a 5-byte network block into CIDR notation
func formatCIDR(b *[5]byte) string {
	ip := net.IPv4(b[0], b[1], b[2], b[3])
	mask := net.CIDRMask(int(b[4]), 32)
	return (&net.IPNet{IP: ip, Mask: mask}).String()
}

// isIPInBlock checks if an IP address is within a 5-byte network block
func isIPInBlock(ip net.IP, block [5]uint8) bool {
	network := net.IPv4(block[0], block[1], block[2], block[3])
	mask := net.CIDRMask(int(block[4]), 32)
	ipNet := &net.IPNet{IP: network, Mask: mask}
	return ipNet.Contains(ip)
}
