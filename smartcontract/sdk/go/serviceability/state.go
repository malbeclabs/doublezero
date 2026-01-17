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

func (e ExchangeStatus) String() string {
	return [...]string{
		"pending",
		"activated",
		"suspended",
		"deleted",
	}[e]
}

type Exchange struct {
	AccountType  AccountType
	Owner        [32]uint8      `influx:"tag,owner,pubkey"`
	Index        Uint128        `influx:"-"`
	Bump_seed    uint8          `influx:"-"`
	Lat          float64        `influx:"field,lat"`
	Lng          float64        `influx:"field,lng"`
	BgpCommunity uint16         `influx:"field,bgp_community"`
	Unused       uint16         `influx:"-"`
	Status       ExchangeStatus `influx:"tag,status"`
	Code         string         `influx:"tag,code"`
	Name         string         `influx:"tag,name"`
	PubKey       [32]byte       `influx:"tag,pubkey,pubkey"`
}

type DeviceDeviceType uint8

const (
	DeviceDeviceTypeHybrid DeviceDeviceType = iota
	DeviceDeviceTypeTransit
	DeviceDeviceTypeEdge
)

func (d DeviceDeviceType) String() string {
	return [...]string{
		"hybrid",
		"transit",
		"edge",
	}[d]
}

type DeviceStatus uint8

const (
	DeviceStatusPending DeviceStatus = iota
	DeviceStatusActivated
	DeviceStatusSuspended
	DeviceStatusDeleted
	DeviceStatusRejected
	DeviceStatusDrained
)

func (d DeviceStatus) String() string {
	return [...]string{
		"pending",
		"activated",
		"suspended",
		"deleted",
		"rejected",
		"drained",
	}[d]
}

func (d DeviceStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

type DeviceHealth uint8

const (
	DeviceHealthPending DeviceHealth = iota
	DeviceHealthReadyForLinks
	DeviceHealthReadyForUsers
	DeviceHealthImpaired
)

func (d DeviceHealth) String() string {
	return [...]string{
		"pending",
		"ready_for_links",
		"ready_for_users",
		"impaired",
	}[d]
}

func (d DeviceHealth) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

type DeviceDesiredStatus uint8

const (
	DeviceDesiredStatusPending   DeviceDesiredStatus = iota
	DeviceDesiredStatusActivated                     = 1
	DeviceDesiredStatusDrained                       = 6
)

func (d DeviceDesiredStatus) String() string {
	switch d {
	case DeviceDesiredStatusPending:
		return "pending"
	case DeviceDesiredStatusActivated:
		return "activated"
	case DeviceDesiredStatusDrained:
		return "drained"
	default:
		return "unknown"
	}
}

func (d DeviceDesiredStatus) MarshalJSON() ([]byte, error) {
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

type InterfaceCYOA uint8

const (
	InterfaceCYOANone InterfaceCYOA = iota
	InterfaceCYOAGREOverDIA
	InterfaceCYOAGREOverFabric
	InterfaceCYOAGREOverPrivatePeering
	InterfaceCYOAGREOverPublicPeering
	InterfaceCYOAGREOverCable
)

func (l InterfaceCYOA) String() string {
	return [...]string{
		"none",
		"gre_over_dia",
		"gre_over_fabric",
		"gre_over_private_peering",
		"gre_over_public_peering",
		"gre_over_cable",
	}[l]
}

func (l InterfaceCYOA) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type InterfaceDIA uint8

const (
	InterfaceDIANone InterfaceDIA = iota
	InterfaceDIADIA
)

func (l InterfaceDIA) String() string {
	return [...]string{
		"none",
		"dia",
	}[l]
}

func (l InterfaceDIA) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type RoutingMode uint8

const (
	RoutingModeStatic RoutingMode = iota
	RoutingModeBGP
)

func (l RoutingMode) String() string {
	return [...]string{
		"static",
		"bgp",
	}[l]
}

func (l RoutingMode) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type Interface struct {
	Version            uint8
	Status             InterfaceStatus
	Name               string
	InterfaceType      InterfaceType
	InterfaceCYOA      InterfaceCYOA
	InterfaceDIA       InterfaceDIA
	LoopbackType       LoopbackType
	Bandwidth          uint64
	Cir                uint64
	Mtu                uint16
	RoutingMode        RoutingMode
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

const CurrentInterfaceVersion = 2

type Device struct {
	AccountType            AccountType
	Owner                  [32]uint8           `influx:"tag,owner,pubkey"`
	Index                  Uint128             `influx:"-"`
	Bump_seed              uint8               `influx:"-"`
	LocationPubKey         [32]uint8           `influx:"tag,location_pubkey,pubkey"`
	ExchangePubKey         [32]uint8           `influx:"tag,exchange_pubkey,pubkey"`
	DeviceType             DeviceDeviceType    `influx:"tag,device_type"`
	PublicIp               [4]uint8            `influx:"tag,public_ip,ip"`
	Status                 DeviceStatus        `influx:"tag,status"`
	Code                   string              `influx:"tag,code"`
	DzPrefixes             [][5]uint8          `influx:"field,dz_prefixes,cidr"`
	MetricsPublisherPubKey [32]uint8           `influx:"tag,metrics_publisher_pubkey,pubkey"`
	ContributorPubKey      [32]byte            `influx:"tag,contributor_pubkey,pubkey"`
	MgmtVrf                string              `influx:"field,mgmt_vrf"`
	Interfaces             []Interface         `influx:"-"`
	ReferenceCount         uint32              `influx:"field,reference_count"`
	UsersCount             uint16              `influx:"field,users_count"`
	MaxUsers               uint16              `influx:"field,max_users"`
	DeviceHealth           DeviceHealth        `influx:"field,device_health"`
	DeviceDesiredStatus    DeviceDesiredStatus `influx:"tag,device_desired_status"`
	PubKey                 [32]byte            `influx:"tag,pubkey,pubkey"`
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
		DeviceHealth           string   `json:"DeviceHealth"`
		DeviceDesiredStatus    string   `json:"DeviceDesiredStatus"`
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
	jsonDevice.DeviceHealth = d.DeviceHealth.String()
	jsonDevice.DeviceDesiredStatus = d.DeviceDesiredStatus.String()

	return json.Marshal(jsonDevice)
}

type LinkLinkType uint8

const (
	LinkLinkTypeWAN LinkLinkType = 1
	LinkLinkTypeDZX LinkLinkType = 127
)

func (l LinkLinkType) String() string {
	switch l {
	case LinkLinkTypeWAN:
		return "WAN"
	case LinkLinkTypeDZX:
		return "DZX"
	default:
		return ""
	}
}

func (l LinkLinkType) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type LinkStatus uint8

const (
	LinkStatusPending LinkStatus = iota
	LinkStatusActivated
	LinkStatusSuspended
	LinkStatusDeleted
	LinkStatusRejected
	LinkStatusRequested
	LinkStatusHardDrained
	LinkStatusSoftDrained
	LinkStatusProvisioning
)

func (l LinkStatus) String() string {
	return [...]string{
		"pending",
		"activated",
		"suspended",
		"deleted",
		"rejected",
		"requested",
		"hard-drained",
		"soft-drained",
		"provisioning",
	}[l]
}

func (l LinkStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type LinkHealth uint8

const (
	LinkHealthPending LinkHealth = iota
	LinkHealthReadyForService
	LinkHealthImpaired
)

func (l LinkHealth) String() string {
	return [...]string{
		"pending",
		"ready_for_service",
		"impaired",
	}[l]
}

func (l LinkHealth) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type LinkDesiredStatus uint8

const (
	LinkDesiredStatusPending LinkDesiredStatus = iota
	LinkDesiredStatusActivated
	LinkDesiredStatusHardDrained
	LinkDesiredStatusSoftDrained
)

func (l LinkDesiredStatus) String() string {
	return [...]string{
		"pending",
		"activated",
		"hard-drained",
		"soft-drained",
	}[l]
}

func (l LinkDesiredStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type Link struct {
	AccountType       AccountType
	Owner             [32]uint8         `influx:"tag,owner,pubkey"`
	Index             Uint128           `influx:"-"`
	Bump_seed         uint8             `influx:"-"`
	SideAPubKey       [32]uint8         `influx:"tag,side_a_pubkey,pubkey"`
	SideZPubKey       [32]uint8         `influx:"tag,side_z_pubkey,pubkey"`
	LinkType          LinkLinkType      `influx:"tag,link_type"`
	Bandwidth         uint64            `influx:"field,bandwidth"`
	Mtu               uint32            `influx:"field,mtu"`
	DelayNs           uint64            `influx:"field,delay_ns"`
	JitterNs          uint64            `influx:"field,jitter_ns"`
	TunnelId          uint16            `influx:"tag,tunnel_id"`
	TunnelNet         [5]uint8          `influx:"tag,tunnel_net,cidr"`
	Status            LinkStatus        `influx:"tag,status"`
	Code              string            `influx:"tag,code"`
	ContributorPubKey [32]uint8         `influx:"tag,contributor_pubkey,pubkey"`
	SideAIfaceName    string            `influx:"tag,side_a_iface_name"`
	SideZIfaceName    string            `influx:"tag,side_z_iface_name"`
	DelayOverrideNs   uint64            `influx:"field,delay_override_ns"`
	LinkHealth        LinkHealth        `influx:"field,link_health"`
	LinkDesiredStatus LinkDesiredStatus `influx:"tag,link_desired_status"`
	PubKey            [32]byte          `influx:"tag,pubkey,pubkey"`
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
		LinkHealth        string `json:"LinkHealth"`
		LinkDesiredStatus string `json:"LinkDesiredStatus"`
	}{
		LinkAlias: LinkAlias(l),
	}

	jsonLink.Owner = base58.Encode(l.Owner[:])
	jsonLink.SideAPubKey = base58.Encode(l.SideAPubKey[:])
	jsonLink.SideZPubKey = base58.Encode(l.SideZPubKey[:])
	jsonLink.ContributorPubKey = base58.Encode(l.ContributorPubKey[:])
	jsonLink.PubKey = base58.Encode(l.PubKey[:])
	jsonLink.Status = l.Status.String()
	jsonLink.LinkHealth = l.LinkHealth.String()
	jsonLink.LinkDesiredStatus = l.LinkDesiredStatus.String()

	jsonLink.TunnelNet = onChainNetToString(l.TunnelNet)

	return json.Marshal(jsonLink)
}

type ContributorStatus uint8

const (
	ContributorStatusPending ContributorStatus = iota
	ContributorStatusActivated
	ContributorStatusSuspended
	ContributorStatusDeleted
)

func (s ContributorStatus) String() string {
	return [...]string{
		"pending",
		"activated",
		"suspended",
		"deleted",
	}[s]
}

func (s ContributorStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

type Contributor struct {
	AccountType    AccountType
	Owner          [32]uint8         `influx:"tag,owner,pubkey"`
	Index          Uint128           `influx:"-"`
	BumpSeed       uint8             `influx:"-"`
	Status         ContributorStatus `influx:"tag,status"`
	Code           string            `influx:"tag,code"`
	ReferenceCount uint32            `influx:"field,reference_count"`
	PubKey         [32]byte          `influx:"tag,pubkey,pubkey"`
}

func (c Contributor) MarshalJSON() ([]byte, error) {
	type ContributorAlias Contributor

	jsonContributor := &struct {
		ContributorAlias
		Owner  string `json:"Owner"`
		PubKey string `json:"PubKey"`
		Status string `json:"Status"`
	}{
		ContributorAlias: ContributorAlias(c),
	}

	jsonContributor.Owner = base58.Encode(c.Owner[:])
	jsonContributor.PubKey = base58.Encode(c.PubKey[:])
	jsonContributor.Status = c.Status.String()

	return json.Marshal(jsonContributor)
}

type UserUserType uint8

const (
	UserTypeIBRL = iota
	UserTypeIBRLWithAllocatedIP
	UserTypeEdgeFiltering
	UserTypeMulticast
)

func (u UserUserType) String() string {
	return [...]string{
		"ibrl",
		"ibrl_with_allocated_ip",
		"edge_filtering",
		"multicast",
	}[u]
}

func (u UserUserType) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

type CyoaType uint8

const (
	CyoaTypeGREOverDIA CyoaType = iota + 1
	CyoaTypeGREOverFabric
	CyoaTypeGREOverPrivatePeering
	CyoaTypeGREOverPublicPeering
	CyoaTypeGREOverCable
)

func (c CyoaType) String() string {
	return [...]string{
		"unknown",
		"gre_over_dia",
		"gre_over_fabric",
		"gre_over_private_peering",
		"gre_over_public_peering",
		"gre_over_cable",
	}[c]
}

func (c CyoaType) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.String())
}

type UserStatus uint8

const (
	UserStatusPending UserStatus = iota
	UserStatusActivated
	UserStatusSuspendedDeprecated
	UserStatusDeleted
	UserStatusRejected
	UserStatusPendingBan
	UserStatusBanned
	UserStatusUpdating
)

func (u UserStatus) String() string {
	return [...]string{
		"pending",
		"activated",
		"suspended",
		"deleted",
		"rejected",
		"pending_ban",
		"banned",
		"updating",
	}[u]
}

func (u UserStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

type User struct {
	AccountType     AccountType
	Owner           [32]uint8
	Index           Uint128
	Bump_seed       uint8
	UserType        UserUserType
	TenantPubKey    [32]uint8
	DevicePubKey    [32]uint8
	CyoaType        CyoaType
	ClientIp        [4]uint8
	DzIp            [4]uint8
	TunnelId        uint16
	TunnelNet       [5]uint8
	Status          UserStatus
	Publishers      [][32]uint8
	Subscribers     [][32]uint8
	ValidatorPubKey [32]uint8
	PubKey          [32]byte
}

func (u User) MarshalJSON() ([]byte, error) {
	type UserAlias User

	publishers := make([]string, len(u.Publishers))
	for i, p := range u.Publishers {
		publishers[i] = base58.Encode(p[:])
	}

	subscribers := make([]string, len(u.Subscribers))
	for i, s := range u.Subscribers {
		subscribers[i] = base58.Encode(s[:])
	}

	jsonUser := &struct {
		UserAlias
		Owner           string   `json:"Owner"`
		TenantPubKey    string   `json:"TenantPubKey"`
		DevicePubKey    string   `json:"DevicePubKey"`
		ClientIp        string   `json:"ClientIp"`
		DzIp            string   `json:"DzIp"`
		TunnelNet       string   `json:"TunnelNet"`
		Publishers      []string `json:"Publishers"`
		Subscribers     []string `json:"Subscribers"`
		ValidatorPubKey string   `json:"ValidatorPubKey"`
		Status          string   `json:"Status"`
		CyoaType        string   `json:"CyoaType"`
		UserType        string   `json:"UserType"`
		PubKey          string   `json:"PubKey"`
	}{
		UserAlias:       UserAlias(u),
		Owner:           base58.Encode(u.Owner[:]),
		TenantPubKey:    base58.Encode(u.TenantPubKey[:]),
		DevicePubKey:    base58.Encode(u.DevicePubKey[:]),
		ClientIp:        net.IP(u.ClientIp[:]).String(),
		DzIp:            net.IP(u.DzIp[:]).String(),
		TunnelNet:       onChainNetToString(u.TunnelNet),
		Publishers:      publishers,
		Subscribers:     subscribers,
		ValidatorPubKey: base58.Encode(u.ValidatorPubKey[:]),
		Status:          u.Status.String(),
		CyoaType:        u.CyoaType.String(),
		UserType:        u.UserType.String(),
		PubKey:          base58.Encode(u.PubKey[:]),
	}

	return json.Marshal(jsonUser)
}

type MulticastGroupStatus uint8

const (
	MulticastGroupStatusPending MulticastGroupStatus = iota
	MulticastGroupStatusActivated
	MulticastGroupStatusSuspended
	MulticastGroupStatusDeleted
)

type MulticastGroup struct {
	AccountType     AccountType
	Owner           [32]uint8
	Index           Uint128
	Bump_seed       uint8
	TenantPubKey    [32]uint8
	MulticastIp     [4]uint8
	MaxBandwidth    uint64
	Status          MulticastGroupStatus
	Code            string
	PublisherCount  uint32
	SubscriberCount uint32
	PubKey          [32]byte
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
