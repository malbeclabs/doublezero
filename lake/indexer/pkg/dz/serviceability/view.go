package dzsvc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/gagliardetto/solana-go"
	"github.com/jonboulle/clockwork"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/clickhouse"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/metrics"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type Contributor struct {
	PK   string
	Code string
	Name string
}

var (
	contributorNamesByCode = map[string]string{
		"jump_":    "Jump Crypto",
		"dgt":      "Distributed Global",
		"cherry":   "Cherry Servers",
		"cdrw":     "Cumberland/DRW",
		"glxy":     "Galaxy",
		"latitude": "Latitude",
		"rox":      "RockawayX",
		"s3v":      "S3V",
		"stakefac": "Staking Facilities",
		"tsw":      "Terraswitch",
	}
)

type Interface struct {
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Status string `json:"status"`
}

type Device struct {
	PK            string
	Status        string
	DeviceType    string
	Code          string
	PublicIP      string
	ContributorPK string
	MetroPK       string
	MaxUsers      uint16
	Interfaces    []Interface
}

type Metro struct {
	PK        string
	Code      string
	Name      string
	Longitude float64
	Latitude  float64
}

type Link struct {
	PK                  string
	Status              string
	Code                string
	TunnelNet           string
	ContributorPK       string
	SideAPK             string
	SideZPK             string
	SideAIfaceName      string
	SideZIfaceName      string
	SideAIP             string
	SideZIP             string
	LinkType            string
	CommittedRTTNs      uint64
	CommittedJitterNs   uint64
	Bandwidth           uint64
	ISISDelayOverrideNs uint64
}

type User struct {
	PK          string
	OwnerPubkey string
	Status      string
	Kind        string
	ClientIP    net.IP
	DZIP        net.IP
	DevicePK    string
	TunnelID    uint16
	Publishers  []string // multicast group PKs this user publishes to
	Subscribers []string // multicast group PKs this user subscribes to
}

type MulticastGroup struct {
	PK              string
	OwnerPubkey     string
	Code            string
	MulticastIP     net.IP
	MaxBandwidth    uint64
	Status          string
	PublisherCount  uint32
	SubscriberCount uint32
}

type ServiceabilityRPC interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

type ViewConfig struct {
	Logger            *slog.Logger
	Clock             clockwork.Clock
	ServiceabilityRPC ServiceabilityRPC
	RefreshInterval   time.Duration
	ClickHouse        clickhouse.Client
}

func (cfg *ViewConfig) Validate() error {
	if cfg.Logger == nil {
		return errors.New("logger is required")
	}
	if cfg.ServiceabilityRPC == nil {
		return errors.New("serviceability rpc is required")
	}
	if cfg.ClickHouse == nil {
		return errors.New("clickhouse connection is required")
	}
	if cfg.RefreshInterval <= 0 {
		return errors.New("refresh interval must be greater than 0")
	}

	if cfg.Clock == nil {
		cfg.Clock = clockwork.NewRealClock()
	}
	return nil
}

type View struct {
	log       *slog.Logger
	cfg       ViewConfig
	store     *Store
	refreshMu sync.Mutex // prevents concurrent refreshes

	fetchedAt time.Time
	readyOnce sync.Once
	readyCh   chan struct{}
}

func NewView(cfg ViewConfig) (*View, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	store, err := NewStore(StoreConfig{
		Logger:     cfg.Logger,
		ClickHouse: cfg.ClickHouse,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create store: %w", err)
	}

	v := &View{
		log:     cfg.Logger,
		cfg:     cfg,
		store:   store,
		readyCh: make(chan struct{}),
	}

	// Tables are created automatically by SCDTableBatch on first refresh
	return v, nil
}

func (v *View) Store() *Store {
	return v.store
}

// Ready returns true if the view has completed at least one successful refresh
func (v *View) Ready() bool {
	select {
	case <-v.readyCh:
		return true
	default:
		return false
	}
}

// WaitReady waits for the view to be ready (has completed at least one successful refresh)
// It returns immediately if already ready, or blocks until ready or context is cancelled.
func (v *View) WaitReady(ctx context.Context) error {
	select {
	case <-v.readyCh:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("context cancelled while waiting for serviceability view: %w", ctx.Err())
	}
}

func (v *View) Start(ctx context.Context) {
	go func() {
		v.log.Info("serviceability: starting refresh loop", "interval", v.cfg.RefreshInterval)

		v.safeRefresh(ctx)

		ticker := v.cfg.Clock.NewTicker(v.cfg.RefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.Chan():
				v.safeRefresh(ctx)
			}
		}
	}()
}

// safeRefresh wraps Refresh with panic recovery to prevent the refresh loop from dying
func (v *View) safeRefresh(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			v.log.Error("serviceability: refresh panicked", "panic", r)
			metrics.ViewRefreshTotal.WithLabelValues("serviceability", "panic").Inc()
		}
	}()

	if err := v.Refresh(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		v.log.Error("serviceability: refresh failed", "error", err)
	}
}

func (v *View) Refresh(ctx context.Context) error {
	v.refreshMu.Lock()
	defer v.refreshMu.Unlock()

	refreshStart := time.Now()
	v.log.Debug("serviceability: refresh started", "start_time", refreshStart)
	defer func() {
		duration := time.Since(refreshStart)
		v.log.Info("serviceability: refresh completed", "duration", duration.String())
		metrics.ViewRefreshDuration.WithLabelValues("serviceability").Observe(duration.Seconds())
	}()

	v.log.Debug("serviceability: starting refresh")

	pd, err := v.cfg.ServiceabilityRPC.GetProgramData(ctx)
	if err != nil {
		metrics.ViewRefreshTotal.WithLabelValues("serviceability", "error").Inc()
		return err
	}

	v.log.Debug("serviceability: fetched program data",
		"contributors", len(pd.Contributors),
		"devices", len(pd.Devices),
		"users", len(pd.Users),
		"links", len(pd.Links),
		"metros", len(pd.Exchanges),
		"multicast_groups", len(pd.MulticastGroups))

	// Validate that we received data for each entity type - empty responses would tombstone all existing entities.
	// Check each independently since they're written separately with MissingMeansDeleted=true.
	if len(pd.Contributors) == 0 {
		metrics.ViewRefreshTotal.WithLabelValues("serviceability", "error").Inc()
		return fmt.Errorf("refusing to write snapshot: RPC returned no contributors (possible RPC issue)")
	}
	if len(pd.Devices) == 0 {
		metrics.ViewRefreshTotal.WithLabelValues("serviceability", "error").Inc()
		return fmt.Errorf("refusing to write snapshot: RPC returned no devices (possible RPC issue)")
	}
	if len(pd.Exchanges) == 0 {
		metrics.ViewRefreshTotal.WithLabelValues("serviceability", "error").Inc()
		return fmt.Errorf("refusing to write snapshot: RPC returned no metros (possible RPC issue)")
	}

	contributors := convertContributors(pd.Contributors)
	devices := convertDevices(pd.Devices)
	users := convertUsers(pd.Users)
	links := convertLinks(pd.Links, pd.Devices)
	metros := convertMetros(pd.Exchanges)
	multicastGroups := convertMulticastGroups(pd.MulticastGroups)

	fetchedAt := time.Now().UTC()

	if err := v.store.ReplaceContributors(ctx, contributors); err != nil {
		return fmt.Errorf("failed to replace contributors: %w", err)
	}

	if err := v.store.ReplaceDevices(ctx, devices); err != nil {
		return fmt.Errorf("failed to replace devices: %w", err)
	}

	if err := v.store.ReplaceUsers(ctx, users); err != nil {
		return fmt.Errorf("failed to replace users: %w", err)
	}

	if err := v.store.ReplaceMetros(ctx, metros); err != nil {
		return fmt.Errorf("failed to replace metros: %w", err)
	}

	if err := v.store.ReplaceLinks(ctx, links); err != nil {
		return fmt.Errorf("failed to replace links: %w", err)
	}

	if err := v.store.ReplaceMulticastGroups(ctx, multicastGroups); err != nil {
		return fmt.Errorf("failed to replace multicast groups: %w", err)
	}

	v.fetchedAt = fetchedAt
	v.readyOnce.Do(func() {
		close(v.readyCh)
		v.log.Info("serviceability: view is now ready")
	})

	v.log.Debug("serviceability: refresh completed", "fetched_at", fetchedAt)
	metrics.ViewRefreshTotal.WithLabelValues("serviceability", "success").Inc()
	return nil
}

func convertContributors(onchain []serviceability.Contributor) []Contributor {
	result := make([]Contributor, len(onchain))
	for i, contributor := range onchain {
		name := contributorNamesByCode[contributor.Code]
		result[i] = Contributor{
			PK:   solana.PublicKeyFromBytes(contributor.PubKey[:]).String(),
			Code: contributor.Code,
			Name: name,
		}
	}
	return result
}

func convertDevices(onchain []serviceability.Device) []Device {
	result := make([]Device, len(onchain))
	for i, device := range onchain {
		// Convert interfaces
		interfaces := make([]Interface, 0, len(device.Interfaces))
		for _, iface := range device.Interfaces {
			var ip string
			if iface.IpNet[4] > 0 && iface.IpNet[4] <= 32 {
				ip = fmt.Sprintf("%s/%d", net.IP(iface.IpNet[:4]).String(), iface.IpNet[4])
			}
			interfaces = append(interfaces, Interface{
				Name:   iface.Name,
				IP:     ip,
				Status: iface.Status.String(),
			})
		}

		result[i] = Device{
			PK:            solana.PublicKeyFromBytes(device.PubKey[:]).String(),
			Status:        device.Status.String(),
			DeviceType:    device.DeviceType.String(),
			Code:          device.Code,
			PublicIP:      net.IP(device.PublicIp[:]).String(),
			ContributorPK: solana.PublicKeyFromBytes(device.ContributorPubKey[:]).String(),
			MetroPK:       solana.PublicKeyFromBytes(device.ExchangePubKey[:]).String(),
			MaxUsers:      device.MaxUsers,
			Interfaces:    interfaces,
		}
	}
	return result
}

func convertUsers(onchain []serviceability.User) []User {
	result := make([]User, len(onchain))
	for i, user := range onchain {
		// Convert publisher group PKs
		publishers := make([]string, len(user.Publishers))
		for j, pub := range user.Publishers {
			publishers[j] = solana.PublicKeyFromBytes(pub[:]).String()
		}
		// Convert subscriber group PKs
		subscribers := make([]string, len(user.Subscribers))
		for j, sub := range user.Subscribers {
			subscribers[j] = solana.PublicKeyFromBytes(sub[:]).String()
		}

		result[i] = User{
			PK:          solana.PublicKeyFromBytes(user.PubKey[:]).String(),
			OwnerPubkey: solana.PublicKeyFromBytes(user.Owner[:]).String(),
			Status:      user.Status.String(),
			Kind:        user.UserType.String(),
			ClientIP:    net.IP(user.ClientIp[:]),
			DZIP:        net.IP(user.DzIp[:]),
			DevicePK:    solana.PublicKeyFromBytes(user.DevicePubKey[:]).String(),
			TunnelID:    user.TunnelId,
			Publishers:  publishers,
			Subscribers: subscribers,
		}
	}
	return result
}

func convertLinks(onchain []serviceability.Link, devices []serviceability.Device) []Link {
	// Build a map of device pubkey -> interface name -> IP for quick lookup
	deviceIfaceIPs := make(map[string]map[string]string)
	for _, device := range devices {
		devicePK := solana.PublicKeyFromBytes(device.PubKey[:]).String()
		ifaceMap := make(map[string]string)
		for _, iface := range device.Interfaces {
			if iface.IpNet[4] > 0 && iface.IpNet[4] <= 32 {
				ip := net.IP(iface.IpNet[:4]).String()
				ifaceMap[iface.Name] = ip
			}
		}
		deviceIfaceIPs[devicePK] = ifaceMap
	}

	result := make([]Link, len(onchain))
	for i, link := range onchain {
		tunnelNet := net.IPNet{
			IP:   net.IP(link.TunnelNet[:4]),
			Mask: net.CIDRMask(int(link.TunnelNet[4]), 32),
		}
		sideAPK := solana.PublicKeyFromBytes(link.SideAPubKey[:]).String()
		sideZPK := solana.PublicKeyFromBytes(link.SideZPubKey[:]).String()

		// Look up interface IPs
		var sideAIP, sideZIP string
		if ifaceMap, ok := deviceIfaceIPs[sideAPK]; ok {
			sideAIP = ifaceMap[link.SideAIfaceName]
		}
		if ifaceMap, ok := deviceIfaceIPs[sideZPK]; ok {
			sideZIP = ifaceMap[link.SideZIfaceName]
		}

		result[i] = Link{
			PK:                  solana.PublicKeyFromBytes(link.PubKey[:]).String(),
			Status:              link.Status.String(),
			Code:                link.Code,
			SideAPK:             sideAPK,
			SideZPK:             sideZPK,
			ContributorPK:       solana.PublicKeyFromBytes(link.ContributorPubKey[:]).String(),
			SideAIfaceName:      link.SideAIfaceName,
			SideZIfaceName:      link.SideZIfaceName,
			SideAIP:             sideAIP,
			SideZIP:             sideZIP,
			TunnelNet:           tunnelNet.String(),
			LinkType:            link.LinkType.String(),
			CommittedRTTNs:      link.DelayNs,
			CommittedJitterNs:   link.JitterNs,
			Bandwidth:           link.Bandwidth,
			ISISDelayOverrideNs: link.DelayOverrideNs,
		}
	}
	return result
}

func convertMetros(onchain []serviceability.Exchange) []Metro {
	result := make([]Metro, len(onchain))
	for i, exchange := range onchain {
		result[i] = Metro{
			PK:        solana.PublicKeyFromBytes(exchange.PubKey[:]).String(),
			Code:      exchange.Code,
			Name:      exchange.Name,
			Longitude: float64(exchange.Lng),
			Latitude:  float64(exchange.Lat),
		}
	}
	return result
}

func convertMulticastGroups(onchain []serviceability.MulticastGroup) []MulticastGroup {
	result := make([]MulticastGroup, len(onchain))
	for i, group := range onchain {
		var status string
		switch group.Status {
		case 0:
			status = "pending"
		case 1:
			status = "activated"
		case 2:
			status = "suspended"
		case 3:
			status = "deleted"
		default:
			status = "unknown"
		}
		result[i] = MulticastGroup{
			PK:              solana.PublicKeyFromBytes(group.PubKey[:]).String(),
			OwnerPubkey:     solana.PublicKeyFromBytes(group.Owner[:]).String(),
			Code:            group.Code,
			MulticastIP:     net.IP(group.MulticastIp[:]),
			MaxBandwidth:    group.MaxBandwidth,
			Status:          status,
			PublisherCount:  group.PublisherCount,
			SubscriberCount: group.SubscriberCount,
		}
	}
	return result
}
