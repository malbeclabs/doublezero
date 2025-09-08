package serviceability

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/mr-tron/base58"
)

type AccountType uint8

const (
	GlobalState AccountType = iota + 1
	ConfigType
	LocationType
	ExchangeType
	DeviceType
	LinkType
	UserType
	MulticastGroupType
	ProgramConfigType
	ContributorType
	AccessPassType
)

type LocationStatus uint8

const (
	LocationStatusPending LocationStatus = iota
	LocationStatusActivated
	LocationStatusSuspended
	LocationStatusDeleted
)

type Uint128 struct {
	High uint64
	Low  uint64
}

type Config struct {
	AccountType         AccountType
	Owner               [32]byte
	Bump_seed           uint8
	Local_asn           uint32
	Remote_asn          uint32
	TunnelTunnelBlock   [5]uint8
	UserTunnelBlock     [5]uint8
	MulticastGroupBlock [5]uint8
	PubKey              [32]byte
}

type Location struct {
	AccountType AccountType
	Owner       [32]uint8
	Index       Uint128
	Bump_seed   uint8
	Lat         float64
	Lng         float64
	LocId       uint32
	Status      LocationStatus
	Code        string
	Name        string
	Country     string
	PubKey      [32]byte
}

type ExchangeStatus uint8

const (
	ExchangeStatusPending ExchangeStatus = iota
	ExchangeStatusActivated
	ExchangeStatusSuspended
	ExchangeStatusDeleted
)

type Exchange struct {
	AccountType AccountType
	Owner       [32]uint8
	Index       Uint128
	Bump_seed   uint8
	Lat         float64
	Lng         float64
	LocId       uint32
	Status      ExchangeStatus
	Code        string
	Name        string
	PubKey      [32]byte
}

type DeviceStatus uint8

const (
	DeviceStatusPending DeviceStatus = iota
	DeviceStatusActivated
	DeviceStatusSuspended
	DeviceStatusDeleted
	DeviceStatusRejected
)

func (d DeviceStatus) String() string {
	return [...]string{
		"pending",
		"activated",
		"suspended",
		"deleted",
		"rejected",
	}[d]
}

func (d DeviceStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

type InterfaceStatus uint8

const (
	InterfaceStatusInvalid InterfaceStatus = iota
	InterfaceStatusUnmanaged
	InterfaceStatusPending
	InterfaceStatusActivated
	InterfaceStatusDeleting
	InterfaceStatusRejecting
	InterfaceStatusUnlinked
)

func (i InterfaceStatus) String() string {
	return [...]string{
		"invalid",
		"unmanaged",
		"pending",
		"activated",
		"deleting",
		"rejecting",
		"unlinked",
	}[i]
}

func (i InterfaceStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(i.String())
}

type InterfaceType uint8

const (
	InterfaceTypeInvalid InterfaceType = iota
	InterfaceTypeLoopback
	InterfaceTypePhysical
)

func (i InterfaceType) String() string {
	return [...]string{
		"invalid",
		"loopback",
		"physical",
	}[i]
}

func (i InterfaceType) MarshalJSON() ([]byte, error) {
	return json.Marshal(i.String())
}

type LoopbackType uint8

const (
	LoopbackTypeNone LoopbackType = iota
	LoopbackTypeVpnv4
	LoopbackTypeIpv4
	LoopbackTypePimRpAddr
	LoopbackTypeReserved
)

func (l LoopbackType) String() string {
	return [...]string{
		"none",
		"vpnv4",
		"ipv4",
		"pim_rp_addr",
		"reserved",
	}[l]
}

func (l LoopbackType) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type Interface struct {
	Version            uint8
	Status             InterfaceStatus
	Name               string
	InterfaceType      InterfaceType
	LoopbackType       LoopbackType
	VlanId             uint16
	IpNet              [5]uint8
	NodeSegmentIdx     uint16
	UserTunnelEndpoint bool
}

func (i Interface) MarshalJSON() ([]byte, error) {
	type InterfaceAlias Interface

	jsonIface := &struct {
		InterfaceAlias
		Status        string `json:"Status"`
		InterfaceType string `json:"InterfaceType"`
		LoopbackType  string `json:"LoopbackType"`
		IpNet         string `json:"IpNet"`
	}{
		InterfaceAlias: InterfaceAlias(i),
	}

	jsonIface.Status = i.Status.String()
	jsonIface.InterfaceType = i.InterfaceType.String()
	jsonIface.LoopbackType = i.LoopbackType.String()

	jsonIface.IpNet = onChainNetToString(i.IpNet)

	return json.Marshal(jsonIface)
}

const CurrentInterfaceVersion = 1

type Device struct {
	AccountType            AccountType
	Owner                  [32]uint8
	Index                  Uint128
	Bump_seed              uint8
	LocationPubKey         [32]uint8
	ExchangePubKey         [32]uint8
	DeviceType             uint8
	PublicIp               [4]uint8
	Status                 DeviceStatus
	Code                   string
	DzPrefixes             [][5]uint8
	MetricsPublisherPubKey [32]uint8
	ContributorPubKey      [32]byte
	MgmtVrf                string
	Interfaces             []Interface
	ReferenceCount         uint32
	UsersCount             uint16
	MaxUsers               uint16
	PubKey                 [32]byte
}

func (d Device) MarshalJSON() ([]byte, error) {
	type DeviceAlias Device

	jsonDevice := &struct {
		DeviceAlias
		Owner                  string   `json:"Owner"`
		LocationPubKey         string   `json:"LocationPubKey"`
		ExchangePubKey         string   `json:"ExchangePubKey"`
		PublicIp               string   `json:"PublicIp"`
		DzPrefixes             []string `json:"DzPrefixes"`
		MetricsPublisherPubKey string   `json:"MetricsPublisherPubKey"`
		ContributorPubKey      string   `json:"ContributorPubKey"`
		PubKey                 string   `json:"PubKey"`
		Status                 string   `json:"Status"`
	}{
		DeviceAlias: DeviceAlias(d),
	}

	jsonDevice.Owner = base58.Encode(d.Owner[:])
	jsonDevice.LocationPubKey = base58.Encode(d.LocationPubKey[:])
	jsonDevice.ExchangePubKey = base58.Encode(d.ExchangePubKey[:])
	jsonDevice.MetricsPublisherPubKey = base58.Encode(d.MetricsPublisherPubKey[:])
	jsonDevice.ContributorPubKey = base58.Encode(d.ContributorPubKey[:])
	jsonDevice.PubKey = base58.Encode(d.PubKey[:])

	jsonDevice.PublicIp = net.IP(d.PublicIp[:]).String()

	prefixes := make([]string, len(d.DzPrefixes))
	for i, p := range d.DzPrefixes {
		prefixes[i] = onChainNetToString(p)
	}
	jsonDevice.DzPrefixes = prefixes
	jsonDevice.Status = d.Status.String()

	return json.Marshal(jsonDevice)
}

type LinkLinkType uint8

const (
	LinkLinkTypeMPLSoverGRE LinkLinkType = iota + 1
)

type LinkStatus uint8

const (
	LinkStatusPending LinkStatus = iota
	LinkStatusActivated
	LinkStatusSuspended
	LinkStatusDeleted
	LinkStatusRejected
	LinkStatusRequested
)

func (l LinkStatus) String() string {
	return [...]string{
		"pending",
		"activated",
		"suspended",
		"deleted",
		"rejected",
		"requested",
	}[l]
}

func (l LinkStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type Link struct {
	AccountType       AccountType
	Owner             [32]uint8
	Index             Uint128
	Bump_seed         uint8
	SideAPubKey       [32]uint8
	SideZPubKey       [32]uint8
	LinkType          LinkLinkType
	Bandwidth         uint64
	Mtu               uint32
	DelayNs           uint64
	JitterNs          uint64
	TunnelId          uint16
	TunnelNet         [5]uint8
	Status            LinkStatus
	Code              string
	ContributorPubKey [32]uint8
	SideAIfaceName    string
	SideZIfaceName    string
	PubKey            [32]byte
}

func (l Link) MarshalJSON() ([]byte, error) {
	type LinkAlias Link

	jsonLink := &struct {
		LinkAlias
		TunnelNet         string `json:"TunnelNet"`
		Owner             string `json:"Owner"`
		SideAPubKey       string `json:"SideAPubKey"`
		SideZPubKey       string `json:"SideZPubKey"`
		ContributorPubKey string `json:"ContributorPubKey"`
		PubKey            string `json:"PubKey"`
		Status            string `json:"Status"`
	}{
		LinkAlias: LinkAlias(l),
	}

	jsonLink.Owner = base58.Encode(l.Owner[:])
	jsonLink.SideAPubKey = base58.Encode(l.SideAPubKey[:])
	jsonLink.SideZPubKey = base58.Encode(l.SideZPubKey[:])
	jsonLink.ContributorPubKey = base58.Encode(l.ContributorPubKey[:])
	jsonLink.PubKey = base58.Encode(l.PubKey[:])
	jsonLink.Status = l.Status.String()

	jsonLink.TunnelNet = onChainNetToString(l.TunnelNet)

	return json.Marshal(jsonLink)
}

type UserUserType uint8

const (
	UserTypeIBRL = iota
	UserTypeIBRLWithAllocatedIP
	UserTypeEdgeFiltering
	UserTypeMulticast
)

type CyoaType uint8

const (
	CyoaTypeGREOverDIA CyoaType = iota + 1
	CyoaTypeGREOverFabric
	CyoaTypeGREOverPrivatePeering
	CyoaTypeGREOverPublicPeering
	CyoaTypeGREOverCable
)

type UserStatus uint8

const (
	UserStatusPending UserStatus = iota
	UserStatusActivated
	UserStatusSuspended
	UserStatusDeleted
	UserStatusRejected
	UserStatusPendingBan
	UserStatusBanned
	UserStatusUpdating
)

type User struct {
	AccountType  AccountType
	Owner        [32]uint8
	Index        Uint128
	Bump_seed    uint8
	UserType     UserUserType
	TenantPubKey [32]uint8
	DevicePubKey [32]uint8
	CyoaType     CyoaType
	ClientIp     [4]uint8
	DzIp         [4]uint8
	TunnelId     uint16
	TunnelNet    [5]uint8
	Status       UserStatus
	Publishers   [][32]uint8
	Subscribers  [][32]uint8
	PubKey       [32]byte
}

type MulticastGroupStatus uint8

const (
	MulticastGroupStatusPending MulticastGroupStatus = iota
	MulticastGroupStatusActivated
	MulticastGroupStatusSuspended
	MulticastGroupStatusDeleted
)

type MulticastGroup struct {
	AccountType  AccountType
	Owner        [32]uint8
	Index        Uint128
	Bump_seed    uint8
	TenantPubKey [32]uint8
	MulticastIp  [4]uint8
	MaxBandwidth uint64
	Status       MulticastGroupStatus
	Code         string
	PubAllowList [][32]uint8
	SubAllowList [][32]uint8
	Publishers   [][32]uint8
	Subscribers  [][32]uint8
	PubKey       [32]byte
}

type ProgramVersion struct {
	Major uint32
	Minor uint32
	Patch uint32
}

type ProgramConfig struct {
	AccountType AccountType
	BumpSeed    uint8
	Version     ProgramVersion
}

func onChainNetToString(n [5]uint8) string {
	prefixLen := n[4]
	if prefixLen > 0 && prefixLen <= 32 {
		ipBytes := n[:4]
		ip := net.IP(ipBytes)
		return fmt.Sprintf("%s/%d", ip.String(), prefixLen)
	}
	return ""
}
