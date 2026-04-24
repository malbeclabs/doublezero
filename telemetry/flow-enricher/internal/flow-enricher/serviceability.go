package enricher

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type ServiceabilityAnnotator struct {
	name           string
	getProgramData func() serviceability.ProgramData
	programData    serviceability.ProgramData
	devices        map[[32]byte]serviceability.Device   // keyed by PubKey
	users          map[netip.Addr]serviceability.User   // keyed by DzIp
	facilities     map[[32]byte]serviceability.Facility // keyed by PubKey
	metros         map[[32]byte]serviceability.Metro    // keyed by PubKey
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
	flow.SrcDeviceCode, flow.SrcFacility, flow.SrcMetro = s.lookupByIP(flow.SrcAddress)
	flow.DstDeviceCode, flow.DstFacility, flow.DstMetro = s.lookupByIP(flow.DstAddress)
	return nil
}

func (s *ServiceabilityAnnotator) lookupByIP(ip net.IP) (deviceCode, facilityCode, metroCode string) {
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

	if facility, found := s.facilities[device.FacilityPubKey]; found {
		facilityCode = facility.Code
	}

	if metro, found := s.metros[device.MetroPubKey]; found {
		metroCode = metro.Code
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
	facilities := BuildFacilityMap(&programData)
	metros := BuildMetroMap(&programData)

	s.mu.Lock()
	s.programData = programData
	s.users = users
	s.devices = devices
	s.facilities = facilities
	s.metros = metros
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

// BuildFacilityMap creates a map of serviceability.Facility keyed by their PubKey.
func BuildFacilityMap(data *serviceability.ProgramData) map[[32]byte]serviceability.Facility {
	facilities := make(map[[32]byte]serviceability.Facility, len(data.Facilities))
	for _, facility := range data.Facilities {
		facilities[facility.PubKey] = facility
	}
	return facilities
}

// BuildMetroMap creates a map of serviceability.Metro keyed by their PubKey.
func BuildMetroMap(data *serviceability.ProgramData) map[[32]byte]serviceability.Metro {
	metros := make(map[[32]byte]serviceability.Metro, len(data.Metros))
	for _, metro := range data.Metros {
		metros[metro.PubKey] = metro
	}
	return metros
}
