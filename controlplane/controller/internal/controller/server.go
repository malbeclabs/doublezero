package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	telemetryconfig "github.com/malbeclabs/doublezero/controlplane/telemetry/pkg/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	ISISAreaID          = "49"
	ISISAreaNumber      = "0000"
	ISISSystemIDPadding = "0000"
	ISISNSelector       = "00"
)

var (
	ErrServiceabilityRequired = errors.New("serviceability program client is required")
)

type ServiceabilityProgramClient interface {
	GetProgramData(context.Context) (*serviceability.ProgramData, error)
	ProgramID() solana.PublicKey
}

type stateCache struct {
	Config          serviceability.Config
	Devices         map[string]*Device
	MulticastGroups map[string]serviceability.MulticastGroup
	Vpnv4BgpPeers   []BgpPeer
	Ipv4BgpPeers    []BgpPeer
}

type Controller struct {
	pb.UnimplementedControllerServer

	cache          stateCache
	mu             sync.RWMutex
	serviceability ServiceabilityProgramClient
	listener       net.Listener
	noHardware     bool
	updateDone     chan struct{}
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
	return controller, nil
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
		slog.Debug("0 users found on-chain")
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
			slog.Error("invalid public ip for device", "device pubkey", device.PubKey)
			continue
		}

		devicePubKey := base58.Encode(device.PubKey[:])
		d := NewDevice(ip, devicePubKey)
		for _, iface := range device.Interfaces {
			intf, err := toInterface(iface)
			if err != nil {
				// TODO: metric
				slog.Error("failed to convert serviceability interface to controller interface", "device pubkey", devicePubKey, "iface", iface, "error", err)
				continue
			}
			// Create a parent interface since they're not defined on-chain.
			if intf.IsSubInterface {
				parent, err := intf.GetParent()
				if err != nil {
					slog.Error("failed to get parent interface for subinterface", "device pubkey", devicePubKey, "iface", intf.Name, "error", err)
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
		candidateVpnv4BgpPeer := BgpPeer{}
		candidateIpv4BgpPeer := BgpPeer{}

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
					slog.Error("Can't assign ISIS NET because VPNv4 loopback IP is invalid or empty", "device pubkey", devicePubKey, "interface", iface.Name, "ip length", len(vpnIP))
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

		if d.Vpn4vLoopbackIP == nil || len(d.Vpn4vLoopbackIP) == 0 {
			slog.Error("not adding device to cache", "device pubkey", devicePubKey, "reason", "no or invalid VPNv4 loopback interface found for device")
			continue
		}

		if d.Ipv4LoopbackIP == nil || len(d.Ipv4LoopbackIP) == 0 {
			slog.Error("not adding device to cache", "device pubkey", devicePubKey, "reason", "no or invalid IPv4 loopback interface found for device")
			continue
		}

		if d.Vpn4vLoopbackIP.Equal(net.IPv4(0, 0, 0, 0)) {
			slog.Error("not adding device to cache", "device pubkey", devicePubKey, "reason", "VPNv4 loopback interface is unassigned (0.0.0.0)")
			continue
		}

		if d.Vpn4vLoopbackIP.Equal(net.IPv4(0, 0, 0, 0)) {
			slog.Error("not adding device to cache", "device pubkey", devicePubKey, "reason", "VPNv4 loopback interface is unassigned (0.0.0.0)")
			continue
		}

		if d.Ipv4LoopbackIP.Equal(net.IPv4(0, 0, 0, 0)) {
			slog.Error("not adding device to cache", "device pubkey", devicePubKey, "reason", "IPv4 loopback interface is unassigned (0.0.0.0)")
			continue
		}

		if d.IsisNet == "" {
			slog.Error("not adding device to cache", "device pubkey", devicePubKey, "reason", "ISIS NET could not be generated")
			continue
		}

		d.MgmtVrf = device.MgmtVrf

		cache.Vpnv4BgpPeers = append(cache.Vpnv4BgpPeers, candidateVpnv4BgpPeer)
		cache.Ipv4BgpPeers = append(cache.Ipv4BgpPeers, candidateIpv4BgpPeer)
		cache.Devices[devicePubKey] = d
	}

	// Build cache of multicast groups.
	for _, group := range data.MulticastGroups {
		cache.MulticastGroups[base58.Encode(group.PubKey[:])] = group
	}

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
				slog.Error("device pubkey could be found for activated user pubkey", "device pubkey", devicePubKey, "user pubkey", userPubKey)
				return false
			}
			if user.TunnelId == 0 {
				slog.Error("tunnel id is not set for user", "user pubkey", userPubKey)
				return false
			}
			if user.ClientIp == [4]byte{} {
				slog.Error("client ip is not set for user", "user pubkey", userPubKey)
				return false
			}
			if user.TunnelNet[4] != 31 {
				slog.Error("tunnel network mask is not 31\n", "tunnel network mask", user.TunnelNet[4])
				return false
			}
			return true
		}()

		if !validUser {
			continue
		}

		tunnel := cache.Devices[devicePubKey].findTunnel(int(user.TunnelId))
		if tunnel == nil {
			slog.Error("unable to find tunnel slot %d on device %s for user %s\n", "tunnel slot", user.TunnelId, "device pubkey", devicePubKey, "user pubkey", userPubKey)
			continue
		}
		tunnel.UnderlayDstIP = net.IP(user.ClientIp[:])
		tunnel.UnderlaySrcIP = cache.Devices[devicePubKey].PublicIP

		var overlaySrc [4]byte
		copy(overlaySrc[:], user.TunnelNet[:4])
		tunnel.OverlaySrcIP = net.IP(overlaySrc[:])

		var overlayDst [4]byte
		copy(overlayDst[:], user.TunnelNet[:4])
		tunnel.OverlayDstIP = net.IP(overlayDst[:])
		// the client is always the odd last octet; this should really be moved into the IP allocation logic
		tunnel.OverlayDstIP[3]++

		tunnel.DzIp = net.IP(user.DzIp[:])
		tunnel.PubKey = userPubKey
		tunnel.Allocated = true

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
	slog.Debug("updating state cache", "state cache", cache)
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

	// start on-chain fetcher
	go func() {
		slog.Info("starting fetch of on-chain data", "program-id", c.serviceability.ProgramID())
		if err := c.updateStateCache(ctx); err != nil {
			cacheUpdateErrors.Inc()
			slog.Error("error fetching accounts", "error", err)
		}
		cacheUpdateOps.Inc()
		ticker := time.NewTicker(10 * time.Second)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				slog.Debug("updating state cache on clock tick")
				if err := c.updateStateCache(ctx); err != nil {
					cacheUpdateErrors.Inc()
					slog.Error("error fetching accounts", "error", err)
				}
				cacheUpdateOps.Inc()
			}
		}
	}()

	// start gRPC server
	server := grpc.NewServer()
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

// GetConfig renders the latest device configuration based on cached device data
func (c *Controller) GetConfig(ctx context.Context, req *pb.ConfigRequest) (*pb.ConfigResponse, error) {
	getConfigOps.WithLabelValues(req.GetPubkey()).Inc()
	c.mu.RLock()
	defer c.mu.RUnlock()
	device, ok := c.cache.Devices[req.GetPubkey()]
	if !ok {
		getConfigPubkeyErrors.WithLabelValues(req.GetPubkey()).Inc()
		err := status.Errorf(codes.NotFound, "pubkey %s not found", req.Pubkey)
		return nil, err
	}

	// compare peers from device to on-chain
	peerFound := func(peer net.IP) bool {
		for _, tun := range device.Tunnels {
			if tun.OverlayDstIP.Equal(peer) {
				return true
			}
		}
		for _, bgpPeer := range c.cache.Vpnv4BgpPeers { // TODO: write a test that proves we don't remove ipv4/vpnv4 BGP peers
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

	unknownPeers := []net.IP{}
	for _, peer := range req.GetBgpPeers() {
		ip := net.ParseIP(peer)
		if ip == nil {
			continue
		}
		if peerFound(ip) {
			continue
		}
		// Only remove peers with addresses that DZ has assigned. This will avoid removal of contributor-configured peers like DIA.
		if isIPInBlock(ip, c.cache.Config.UserTunnelBlock) || isIPInBlock(ip, c.cache.Config.TunnelTunnelBlock) {
			unknownPeers = append(unknownPeers, ip)
		}
	}

	if len(unknownPeers) != 0 {
		slog.Error("device returned unknown peers", "device pubkey", req.GetPubkey(), "number of unknown peers", len(unknownPeers), "peers", unknownPeers)
	}

	multicastGroupBlock := formatCIDR(&c.cache.Config.MulticastGroupBlock)

	// This check avoids the situation where the template produces the following useless output, which happens in any test case with a single DZD.
	// ```
	// no router msdp
	// router msdp
	// ```
	ipv4Peers := c.cache.Ipv4BgpPeers
	if len(ipv4Peers) == 1 && ipv4Peers[0].PeerIP.Equal(device.Ipv4LoopbackIP) {
		ipv4Peers = nil
	}

	data := templateData{
		MulticastGroupBlock:      multicastGroupBlock,
		Device:                   device,
		Vpnv4BgpPeers:            c.cache.Vpnv4BgpPeers,
		Ipv4BgpPeers:             ipv4Peers,
		UnknownBgpPeers:          unknownPeers,
		NoHardware:               c.noHardware,
		TelemetryTWAMPListenPort: telemetryconfig.TWAMPListenPort,
	}

	config, err := renderConfig(data)
	if err != nil {
		getConfigRenderErrors.WithLabelValues(req.GetPubkey()).Inc()
		err := status.Errorf(codes.Aborted, "config rendering for pubkey %s failed: %v", req.Pubkey, err)
		return nil, err
	}
	return &pb.ConfigResponse{Config: config}, nil
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
