package controller

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strings"

	"github.com/malbeclabs/doublezero/controlplane/controller/config"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
)

type InterfaceType uint8

const (
	InterfaceTypeUnknown InterfaceType = iota
	InterfaceTypeLoopback
	InterfaceTypePhysical
)

type LoopbackType uint8

const (
	LoopbackTypeUnknown LoopbackType = iota
	LoopbackTypeVpnv4
	LoopbackTypeIpv4
	LoopbackTypePimRpAddr
	LoopbackTypeReserved
)

type Interface struct {
	Name                 string
	VlanId               uint16
	Ip                   netip.Prefix
	NodeSegmentIdx       uint16
	IsSubInterface       bool
	IsSubInterfaceParent bool
	InterfaceType        InterfaceType
	LoopbackType         LoopbackType
	Metric               uint32
	IsLink               bool
	LinkStatus           serviceability.LinkStatus
}

// toInterface validates onchain data for a serviceability interface and converts it to a controller interface.
func toInterface(iface serviceability.Interface) (Interface, error) {
	if iface == (serviceability.Interface{}) {
		return Interface{}, errors.New("serviceability interface cannot be nil")
	}

	addr := netip.AddrFrom4([4]byte(iface.IpNet[:4]))
	prefixLen := int(iface.IpNet[4])
	prefix := netip.PrefixFrom(addr, prefixLen)

	if !prefix.IsValid() {
		return Interface{}, fmt.Errorf("invalid IP prefix assigned to interface")
	}

	// onchain data uses [5]byte so 0/0 is an unallocated address
	if prefix == netip.MustParsePrefix("0.0.0.0/0") {
		prefix = netip.Prefix{}
	}
	_, subIntf := isSubInterface(iface.Name)

	var ifType InterfaceType = InterfaceTypeUnknown
	switch iface.InterfaceType {
	case serviceability.InterfaceTypeLoopback:
		ifType = InterfaceTypeLoopback
	case serviceability.InterfaceTypePhysical:
		ifType = InterfaceTypePhysical
	}

	var loopbackType LoopbackType = LoopbackTypeUnknown
	switch iface.LoopbackType {
	case serviceability.LoopbackTypeVpnv4:
		loopbackType = LoopbackTypeVpnv4
	case serviceability.LoopbackTypeIpv4:
		loopbackType = LoopbackTypeIpv4
	}

	if loopbackType != LoopbackTypeVpnv4 && iface.NodeSegmentIdx != 0 {
		return Interface{}, fmt.Errorf("node segment cannot be defined on non-vpnv4 loopbacks")
	}

	return Interface{
		Name:                 iface.Name,
		VlanId:               iface.VlanId,
		Ip:                   prefix,
		NodeSegmentIdx:       iface.NodeSegmentIdx,
		IsSubInterface:       subIntf,
		IsSubInterfaceParent: false,
		InterfaceType:        ifType,
		LoopbackType:         loopbackType,
		IsLink:               false,
	}, nil

}

func NewInterface(
	name string,
	vlanId uint16,
	ip netip.Prefix,
	nodeSegmentIdx uint16,
	isSubInterface bool,
	isSubInterfaceParent bool,
	interfaceType InterfaceType,
	loopbackType LoopbackType,
) Interface {
	return Interface{
		Name:                 name,
		VlanId:               vlanId,
		Ip:                   ip,
		NodeSegmentIdx:       nodeSegmentIdx,
		IsSubInterface:       isSubInterface,
		IsSubInterfaceParent: isSubInterfaceParent,
		InterfaceType:        interfaceType,
		LoopbackType:         loopbackType,
		IsLink:               false,
	}
}

func (i Interface) IsLoopback() bool {
	return i.InterfaceType == InterfaceTypeLoopback
}

func (i Interface) IsPhysical() bool {
	return i.InterfaceType == InterfaceTypePhysical
}

func (i Interface) IsVpnv4Loopback() bool {
	return i.IsLoopback() && i.LoopbackType == LoopbackTypeVpnv4
}

func (i Interface) IsIpv4Loopback() bool {
	return i.IsLoopback() && i.LoopbackType == LoopbackTypeIpv4
}

func (i Interface) GetParent() (Interface, error) {
	if !i.IsSubInterface {
		return Interface{}, fmt.Errorf("interface %s is not a sub-interface", i.Name)
	}
	parentName, ok := isSubInterface(i.Name)
	if !ok {
		return Interface{}, fmt.Errorf("interface %s is not a valid sub-interface", i.Name)
	}
	return Interface{
		Name:                 parentName,
		IsSubInterface:       false,
		IsSubInterfaceParent: true,
		InterfaceType:        i.InterfaceType,
		LoopbackType:         i.LoopbackType,
	}, nil
}

func (i Interface) IsVlanInterface() bool {
	return strings.HasPrefix(i.Name, "Vlan")
}

// isSubInterface determines whether an interface is considered a subinterface by the name.
// Switch1/1/1.100 is an example of a subinterface, where Switch1/1/1 is the parent interface.
func isSubInterface(name string) (parent string, ok bool) {
	re := regexp.MustCompile(`^(.+)\.(\d+)$`)
	matches := re.FindStringSubmatch(name)
	if len(matches) == 3 {
		return matches[1], true
	}
	return "", false
}

type Device struct {
	PubKey                string
	PublicIP              net.IP
	Vpn4vLoopbackIP       net.IP
	Ipv4LoopbackIP        net.IP
	Tunnels               []*Tunnel
	TunnelSlots           int
	Vpn4vLoopbackIntfName string
	Ipv4LoopbackIntfName  string
	Interfaces            []Interface
	MgmtVrf               string
	IsisNet               string
	DevicePathologies     []string
	BgpCommunity          uint16
	ExchangeCode          string
	Status serviceability.DeviceStatus
	// Additional fields for metric labels
	Code            string
	ContributorCode string
	LocationCode    string
}

func NewDevice(ip net.IP, publicKey string) *Device {
	tunnels := []*Tunnel{}
	devicePathologies := []string{}
	for i := 0; i < config.MaxUserTunnelSlots; i++ {
		id := config.StartUserTunnelNum + i
		tunnel := &Tunnel{
			Id:        id,
			Allocated: false,
		}
		tunnels = append(tunnels, tunnel)
	}
	return &Device{
		PublicIP:          ip,
		PubKey:            publicKey,
		Tunnels:           tunnels,
		TunnelSlots:       config.MaxUserTunnelSlots,
		DevicePathologies: devicePathologies,
	}
}

func (d *Device) findTunnel(id int) *Tunnel {
	for _, tunnel := range d.Tunnels {
		if tunnel.Id == id {
			return tunnel
		}
	}
	return nil
}

type Tunnel struct {
	Id                    int
	UnderlaySrcIP         net.IP
	UnderlayDstIP         net.IP
	OverlaySrcIP          net.IP // This needs to be derived based on the tunnel net
	OverlayDstIP          net.IP // This needs to be derived based on the tunnel net
	DzIp                  net.IP
	PubKey                string
	Allocated             bool
	IsMulticast           bool
	VrfId                 uint16
	MulticastBoundaryList []net.IP
	MulticastSubscribers  []net.IP
	MulticastPublishers   []net.IP
}

type BgpPeer struct {
	PeerIP   net.IP
	PeerName string
}

type StringsHelper struct{}

func (StringsHelper) ToUpper(s string) string {
	return strings.ToUpper(s)
}

type templateData struct {
	Device                   *Device
	Vpnv4BgpPeers            []BgpPeer
	Ipv4BgpPeers             []BgpPeer
	UnknownBgpPeers          []net.IP
	MulticastGroupBlock      string
	NoHardware               bool
	TelemetryTWAMPListenPort int
	LocalASN                 uint32
	UnicastVrfs              []uint16
	Strings                  StringsHelper
}
