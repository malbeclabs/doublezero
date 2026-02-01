package enricher

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"time"

	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

type ServiceabilityAnnotator struct {
	name           string
	getProgramData func() serviceability.ProgramData
	programData    serviceability.ProgramData
	devices        map[[32]byte]serviceability.Device   // keyed by PubKey
	users          map[netip.Addr]serviceability.User   // keyed by DzIp
	locations      map[[32]byte]serviceability.Location // keyed by PubKey
	exchanges      map[[32]byte]serviceability.Exchange // keyed by PubKey
	mu             sync.RWMutex
}

func NewServiceabilityAnnotator(getProgramData func() serviceability.ProgramData) *ServiceabilityAnnotator {
	return &ServiceabilityAnnotator{
		name:           "serviceability annotator",
		getProgramData: getProgramData,
	}
}

func (s *ServiceabilityAnnotator) Init(ctx context.Context) error {
	s.updateServiceabilityCache()

	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.updateServiceabilityCache()
			case <-ctx.Done():
				return
			}
		}
	}()
	return nil
}

func (s *ServiceabilityAnnotator) Annotate(flow *FlowSample) error {
	flow.SrcDeviceCode, flow.SrcLocation, flow.SrcExchange = s.lookupByIP(flow.SrcAddress)
	flow.DstDeviceCode, flow.DstLocation, flow.DstExchange = s.lookupByIP(flow.DstAddress)
	return nil
}

func (s *ServiceabilityAnnotator) lookupByIP(ip net.IP) (deviceCode, locationCode, exchangeCode string) {
	addr, ok := netip.AddrFromSlice(ip.To4())
	if !ok {
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	user, found := s.users[addr]
	if !found {
		return
	}

	device, found := s.devices[user.DevicePubKey]
	if !found {
		return
	}
	deviceCode = device.Code

	if location, found := s.locations[device.LocationPubKey]; found {
		locationCode = location.Code
	}

	if exchange, found := s.exchanges[device.ExchangePubKey]; found {
		exchangeCode = exchange.Code
	}

	return
}

func (s *ServiceabilityAnnotator) String() string {
	return s.name
}

func (s *ServiceabilityAnnotator) updateServiceabilityCache() {
	programData := s.getProgramData()
	users := BuildUserMap(&programData)
	devices := BuildDeviceMap(&programData)
	locations := BuildLocationMap(&programData)
	exchanges := BuildExchangeMap(&programData)

	s.mu.Lock()
	s.programData = programData
	s.users = users
	s.devices = devices
	s.locations = locations
	s.exchanges = exchanges
	s.mu.Unlock()
}

// BuildUserMap creates a map of serviceability.User keyed by their DzIp address.
func BuildUserMap(data *serviceability.ProgramData) map[netip.Addr]serviceability.User {
	users := make(map[netip.Addr]serviceability.User, len(data.Users))
	for _, user := range data.Users {
		dzIP := netip.AddrFrom4(user.DzIp)
		users[dzIP] = user
	}
	return users
}

// BuildDeviceMap creates a map of serviceability.Device keyed by their PubKey.
func BuildDeviceMap(data *serviceability.ProgramData) map[[32]byte]serviceability.Device {
	devices := make(map[[32]byte]serviceability.Device, len(data.Devices))
	for _, device := range data.Devices {
		devices[device.PubKey] = device
	}
	return devices
}

// BuildLocationMap creates a map of serviceability.Location keyed by their PubKey.
func BuildLocationMap(data *serviceability.ProgramData) map[[32]byte]serviceability.Location {
	locations := make(map[[32]byte]serviceability.Location, len(data.Locations))
	for _, location := range data.Locations {
		locations[location.PubKey] = location
	}
	return locations
}

// BuildExchangeMap creates a map of serviceability.Exchange keyed by their PubKey.
func BuildExchangeMap(data *serviceability.ProgramData) map[[32]byte]serviceability.Exchange {
	exchanges := make(map[[32]byte]serviceability.Exchange, len(data.Exchanges))
	for _, exchange := range data.Exchanges {
		exchanges[exchange.PubKey] = exchange
	}
	return exchanges
}
