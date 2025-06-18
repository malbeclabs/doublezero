package controller

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	pb "github.com/malbeclabs/doublezero/controlplane/proto/controller/gen/pb-go"
	dzsdk "github.com/malbeclabs/doublezero/smartcontract/sdk/go"
	"github.com/mr-tron/base58"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type accountFetcher interface {
	Load(context.Context) error
	GetDevices() []dzsdk.Device
	GetUsers() []dzsdk.User
	GetMulticastGroups() []dzsdk.MulticastGroup
	GetConfig() dzsdk.Config
}

type stateCache struct {
	Config          dzsdk.Config
	Devices         map[string]*Device
	MulticastGroups map[string]dzsdk.MulticastGroup
}

type Controller struct {
	pb.UnimplementedControllerServer

	cache stateCache
	mu    sync.RWMutex
	accountFetcher
	listener    net.Listener
	programId   string
	rpcEndpoint string
	noHardware  bool
	updateDone  chan struct{}
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
	if controller.accountFetcher == nil {
		options := []dzsdk.Option{}
		if controller.programId != "" {
			_, err := solana.PublicKeyFromBase58(controller.programId)
			if err != nil {
				return nil, fmt.Errorf("invalid program id %s: %v", controller.programId, err)
			}
			options = append(options, dzsdk.WithProgramId(controller.programId))
		}
		if controller.rpcEndpoint == "" {
			controller.rpcEndpoint = dzsdk.URL_DOUBLEZERO
		}
		if controller.programId == "" {
			controller.programId = dzsdk.PROGRAM_ID_TESTNET
		}
		controller.accountFetcher = dzsdk.New(controller.rpcEndpoint, options...)
	}
	return controller, nil
}

func WithAccountFetcher(f accountFetcher) Option {
	return func(c *Controller) {
		c.accountFetcher = f
	}
}

func WithProgramId(programId string) Option {
	return func(c *Controller) {
		c.programId = programId
	}
}

func WithRpcEndpoint(rpcEndpoint string) Option {
	return func(c *Controller) {
		c.rpcEndpoint = rpcEndpoint
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
	if err := c.accountFetcher.Load(ctx); err != nil {
		cacheUpdateFetchErrors.Inc()
		return fmt.Errorf("error while loading accounts: %v", err)
	}

	devices := c.accountFetcher.GetDevices()
	if len(devices) == 0 {
		return fmt.Errorf("0 devices found on-chain")
	}
	users := c.accountFetcher.GetUsers()
	if len(users) == 0 {
		slog.Debug("0 users found on-chain")
	}
	cache := stateCache{
		Config:          c.accountFetcher.GetConfig(),
		Devices:         make(map[string]*Device),
		MulticastGroups: make(map[string]dzsdk.MulticastGroup),
	}

	// build cache of devices
	for _, device := range devices {
		ip := net.IP(device.PublicIp[:])
		if ip == nil {
			// TODO: metric
			slog.Error("invalid public ip for device", "device pubkey", device.PubKey)
			continue
		}
		devicePubKey := base58.Encode(device.PubKey[:])
		cache.Devices[devicePubKey] = NewDevice(ip, devicePubKey)
	}

	// Build cache of multicast groups.
	for _, group := range c.accountFetcher.GetMulticastGroups() {
		cache.MulticastGroups[base58.Encode(group.PubKey[:])] = group
	}

	// create user tunnels and add to the appropriate device
	for _, user := range users {
		if user.Status != dzsdk.UserStatusActivated {
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

		if user.UserType == dzsdk.UserTypeMulticast {
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
		slog.Info("starting fetch of on-chain data", "program-id", c.programId, "rpc-endpoint", c.rpcEndpoint)
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

	err := validatePubkey(req.GetPubkey())
	if err != nil {
		return nil, err
	}

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
		return false
	}

	unknownPeers := []net.IP{}
	for _, peer := range req.GetBgpPeers() {
		ip := net.ParseIP(peer)
		if ip == nil {
			slog.Error("malformed peer ip", "peer", peer)
			continue
		}
		if !ip.IsLinkLocalUnicast() || peerFound(ip) {
			continue
		}
		unknownPeers = append(unknownPeers, ip)
	}

	if len(unknownPeers) != 0 {
		slog.Error("device returned unknown peers", "device pubkey", req.GetPubkey(), "number of unknown peers", len(unknownPeers), "peers", unknownPeers)
	}

	multicastGroupBlock := formatCIDR(&c.cache.Config.MulticastGroupBlock)

	data := templateData{
		MulticastGroupBlock: multicastGroupBlock,
		Device:              device,
		UnknownBgpPeers:     unknownPeers,
		NoHardware:          c.noHardware,
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

func validatePubkey(pubkey string) error {
	decoded, err := base58.Decode(pubkey)
	if err != nil {
		err := status.Errorf(codes.InvalidArgument, "pubkey %s is not a valid base58 string: %v", pubkey, err)
		return err
	}

	if len(decoded) != 32 {
		err := status.Errorf(codes.InvalidArgument, "pubkey %s is length %d, expected 32", pubkey, len(decoded))
		return err
	}

	var publicKey solana.PublicKey
	copy(publicKey[:], decoded)
	if !publicKey.IsOnCurve() {
		err := status.Errorf(codes.InvalidArgument, "pubkey %s is not a valid public key", pubkey)
		return err

	}
	return nil
}
