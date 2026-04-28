package serviceability

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/mr-tron/base58"
)

type AccountType uint8

const (
	GlobalStateType AccountType = iota + 1
	GlobalConfigType
	LocationType
	ExchangeType
	DeviceType
	LinkType
	UserType
	MulticastGroupType
	ProgramConfigType
	ContributorType
	AccessPassType
	ResourceExtensionType // 12
	TenantType            // 13
	// 14 is reserved
	PermissionType AccountType = 15
	IndexType      AccountType = 16
	TopologyType   AccountType = 17
)

type LocationStatus uint8

const (
	LocationStatusPending   LocationStatus = 0
	LocationStatusActivated LocationStatus = 1
	LocationStatusSuspended LocationStatus = 2
)

func (s LocationStatus) String() string {
	switch s {
	case LocationStatusPending:
		return "pending"
	case LocationStatusActivated:
		return "activated"
	case LocationStatusSuspended:
		return "suspended"
	default:
		return "unknown"
	}
}

type Uint128 struct {
	High uint64
	Low  uint64
}

type GlobalConfig struct {
	AccountType             AccountType
	Owner                   [32]byte
	BumpSeed                uint8
	LocalASN                uint32
	RemoteASN               uint32
	DeviceTunnelBlock       [5]uint8
	UserTunnelBlock         [5]uint8
	MulticastGroupBlock     [5]uint8
	NextBGPCommunity        uint16
	MulticastPublisherBlock [5]uint8
	PubKey                  [32]byte
}

type GlobalState struct {
	AccountType                AccountType
	BumpSeed                   uint8
	AccountIndex               Uint128
	FoundationAllowlist        [][32]byte
	ActivatorAuthorityPK       [32]byte
	SentinelAuthorityPK        [32]byte
	ContributorAirdropLamports uint64
	UserAirdropLamports        uint64
	HealthOraclePK             [32]byte
	QAAllowlist                [][32]byte
	FeatureFlags               Uint128
	FeedAuthorityPK            [32]byte
	PubKey                     [32]byte
}

type Location struct {
	AccountType    AccountType
	Owner          [32]uint8
	Index          Uint128
	BumpSeed       uint8
	Lat            float64
	Lng            float64
	LocId          uint32
	Status         LocationStatus
	Code           string
	Name           string
	Country        string
	ReferenceCount uint32
	PubKey         [32]byte
}

type ExchangeStatus uint8

const (
	ExchangeStatusPending   ExchangeStatus = 0
	ExchangeStatusActivated ExchangeStatus = 1
	ExchangeStatusSuspended ExchangeStatus = 2
)

func (e ExchangeStatus) String() string {
	switch e {
	case ExchangeStatusPending:
		return "pending"
	case ExchangeStatusActivated:
		return "activated"
	case ExchangeStatusSuspended:
		return "suspended"
	default:
		return "unknown"
	}
}

type Exchange struct {
	AccountType    AccountType
	Owner          [32]uint8      `influx:"tag,owner,pubkey"`
	Index          Uint128        `influx:"-"`
	BumpSeed       uint8          `influx:"-"`
	Lat            float64        `influx:"field,lat"`
	Lng            float64        `influx:"field,lng"`
	BgpCommunity   uint16         `influx:"field,bgp_community"`
	ReferenceCount uint32         `influx:"field,reference_count"`
	Status         ExchangeStatus `influx:"tag,status"`
	Code           string         `influx:"tag,code"`
	Name           string         `influx:"tag,name"`
	Device1PK      [32]byte       `influx:"tag,device1_pk,pubkey"`
	Device2PK      [32]byte       `influx:"tag,device2_pk,pubkey"`
	PubKey         [32]byte       `influx:"tag,pubkey,pubkey"`
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
	DeviceStatusPending            DeviceStatus = 0
	DeviceStatusActivated          DeviceStatus = 1
	DeviceStatusDeleting           DeviceStatus = 3
	DeviceStatusRejected           DeviceStatus = 4
	DeviceStatusDrained            DeviceStatus = 5
	DeviceStatusDeviceProvisioning DeviceStatus = 6
	DeviceStatusLinkProvisioning   DeviceStatus = 7
)

func (d DeviceStatus) String() string {
	switch d {
	case DeviceStatusPending:
		return "pending"
	case DeviceStatusActivated:
		return "activated"
	case DeviceStatusDeleting:
		return "deleting"
	case DeviceStatusRejected:
		return "rejected"
	case DeviceStatusDrained:
		return "drained"
	case DeviceStatusDeviceProvisioning:
		return "device-provisioning"
	case DeviceStatusLinkProvisioning:
		return "link-provisioning"
	default:
		return "unknown"
	}
}

func (d DeviceStatus) IsDrained() bool {
	return d == DeviceStatusDrained
}

func (d DeviceStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

type DeviceHealth uint8

const (
	DeviceHealthUnknown       DeviceHealth = 0
	DeviceHealthPending       DeviceHealth = 1
	DeviceHealthReadyForLinks DeviceHealth = 2
	DeviceHealthReadyForUsers DeviceHealth = 3
	DeviceHealthImpaired      DeviceHealth = 4
)

func (d DeviceHealth) String() string {
	switch d {
	case DeviceHealthUnknown:
		return "unknown"
	case DeviceHealthPending:
		return "pending"
	case DeviceHealthReadyForLinks:
		return "ready_for_links"
	case DeviceHealthReadyForUsers:
		return "ready_for_users"
	case DeviceHealthImpaired:
		return "impaired"
	default:
		return fmt.Sprintf("DeviceHealth(%d)", d)
	}
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

// FlexAlgoNodeSegment is a flex-algo node segment assigned to an interface.
// Each entry pairs a TopologyInfo PDA with the segment-routing index allocated
// for this device within that topology. Written as part of Interface V2 (RFC-18).
type FlexAlgoNodeSegment struct {
	Topology       [32]byte // TopologyInfo PDA pubkey
	NodeSegmentIdx uint16   // allocated from SegmentRoutingIds ResourceExtension
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
	// FlexAlgoNodeSegments holds flex-algo node segment assignments for this interface (RFC-18).
	// Present in all V2 accounts after MigrateDeviceInterfaces has been run (empty vec for
	// interfaces not yet assigned to any topology). Nil for V1 interfaces.
	FlexAlgoNodeSegments []FlexAlgoNodeSegment `json:",omitempty"`
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

const CurrentInterfaceVersion = 3

type Device struct {
	AccountType               AccountType
	Owner                     [32]uint8           `influx:"tag,owner,pubkey"`
	Index                     Uint128             `influx:"-"`
	BumpSeed                  uint8               `influx:"-"`
	LocationPubKey            [32]uint8           `influx:"tag,location_pubkey,pubkey"`
	ExchangePubKey            [32]uint8           `influx:"tag,exchange_pubkey,pubkey"`
	DeviceType                DeviceDeviceType    `influx:"tag,device_type"`
	PublicIp                  [4]uint8            `influx:"tag,public_ip,ip"`
	Status                    DeviceStatus        `influx:"tag,status"`
	Code                      string              `influx:"tag,code"`
	DzPrefixes                [][5]uint8          `influx:"field,dz_prefixes,cidr"`
	MetricsPublisherPubKey    [32]uint8           `influx:"tag,metrics_publisher_pubkey,pubkey"`
	ContributorPubKey         [32]byte            `influx:"tag,contributor_pubkey,pubkey"`
	MgmtVrf                   string              `influx:"field,mgmt_vrf"`
	Interfaces                []Interface         `influx:"-"`
	ReferenceCount            uint32              `influx:"field,reference_count"`
	UsersCount                uint16              `influx:"field,users_count"`
	MaxUsers                  uint16              `influx:"field,max_users"`
	DeviceHealth              DeviceHealth        `influx:"field,device_health"`
	DeviceDesiredStatus       DeviceDesiredStatus `influx:"tag,device_desired_status"`
	UnicastUsersCount         uint16              `influx:"field,unicast_users_count"`
	MulticastSubscribersCount uint16              `influx:"field,multicast_subscribers_count"`
	MaxUnicastUsers           uint16              `influx:"field,max_unicast_users"`
	MaxMulticastSubscribers   uint16              `influx:"field,max_multicast_subscribers"`
	ReservedSeats             uint16              `influx:"field,reserved_seats"`
	MulticastPublishersCount  uint16              `influx:"field,multicast_publishers_count"`
	MaxMulticastPublishers    uint16              `influx:"field,max_multicast_publishers"`
	PubKey                    [32]byte            `influx:"tag,pubkey,pubkey"`
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
	LinkStatusPending      LinkStatus = 0
	LinkStatusActivated    LinkStatus = 1
	LinkStatusDeleting     LinkStatus = 3
	LinkStatusRejected     LinkStatus = 4
	LinkStatusRequested    LinkStatus = 5
	LinkStatusHardDrained  LinkStatus = 6
	LinkStatusSoftDrained  LinkStatus = 7
	LinkStatusProvisioning LinkStatus = 8
)

func (l LinkStatus) String() string {
	switch l {
	case LinkStatusPending:
		return "pending"
	case LinkStatusActivated:
		return "activated"
	case LinkStatusDeleting:
		return "deleting"
	case LinkStatusRejected:
		return "rejected"
	case LinkStatusRequested:
		return "requested"
	case LinkStatusHardDrained:
		return "hard-drained"
	case LinkStatusSoftDrained:
		return "soft-drained"
	case LinkStatusProvisioning:
		return "provisioning"
	default:
		return "unknown"
	}
}

// IsHardDrained returns true if the link status is hard-drained
func (l LinkStatus) IsHardDrained() bool {
	return l == LinkStatusHardDrained
}

func (l LinkStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type LinkHealth uint8

const (
	LinkHealthUnknown         LinkHealth = 0
	LinkHealthPending         LinkHealth = 1
	LinkHealthReadyForService LinkHealth = 2
	LinkHealthImpaired        LinkHealth = 3
)

func (l LinkHealth) String() string {
	switch l {
	case LinkHealthUnknown:
		return "unknown"
	case LinkHealthPending:
		return "pending"
	case LinkHealthReadyForService:
		return "ready_for_service"
	case LinkHealthImpaired:
		return "impaired"
	default:
		return fmt.Sprintf("LinkHealth(%d)", l)
	}
}

func (l LinkHealth) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type BGPStatus uint8

const (
	BGPStatusUnknown BGPStatus = 0
	BGPStatusUp      BGPStatus = 1
	BGPStatusDown    BGPStatus = 2
)

func (b BGPStatus) String() string {
	switch b {
	case BGPStatusUnknown:
		return "unknown"
	case BGPStatusUp:
		return "up"
	case BGPStatusDown:
		return "down"
	default:
		return fmt.Sprintf("BGPStatus(%d)", b)
	}
}

func (b BGPStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(b.String())
}

type LinkDesiredStatus uint8

const (
	LinkDesiredStatusPending     LinkDesiredStatus = 0
	LinkDesiredStatusActivated   LinkDesiredStatus = 1
	LinkDesiredStatusHardDrained LinkDesiredStatus = 6
	LinkDesiredStatusSoftDrained LinkDesiredStatus = 7
)

func (l LinkDesiredStatus) String() string {
	switch l {
	case LinkDesiredStatusPending:
		return "pending"
	case LinkDesiredStatusActivated:
		return "activated"
	case LinkDesiredStatusHardDrained:
		return "hard-drained"
	case LinkDesiredStatusSoftDrained:
		return "soft-drained"
	default:
		return "unknown"
	}
}

func (l LinkDesiredStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type Link struct {
	AccountType       AccountType
	Owner             [32]uint8         `influx:"tag,owner,pubkey"`
	Index             Uint128           `influx:"-"`
	BumpSeed          uint8             `influx:"-"`
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
	LinkTopologies    [][32]byte        `json:",omitempty"`
	LinkFlags         uint32            `json:",omitempty"`
	PubKey            [32]byte          `influx:"tag,pubkey,pubkey"`
}

// LinkFlagUnicastDrained is set in LinkFlags when the link is marked as unicast-drained.
const LinkFlagUnicastDrained uint32 = 0x01

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
	ContributorStatusPending   ContributorStatus = 0
	ContributorStatusActivated ContributorStatus = 1
	ContributorStatusSuspended ContributorStatus = 2
	ContributorStatusDeleting  ContributorStatus = 3
)

func (s ContributorStatus) String() string {
	switch s {
	case ContributorStatusPending:
		return "pending"
	case ContributorStatusActivated:
		return "activated"
	case ContributorStatusSuspended:
		return "suspended"
	case ContributorStatusDeleting:
		return "deleting"
	default:
		return "unknown"
	}
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
	OpsManagerPK   [32]byte          `influx:"tag,ops_manager_pk,pubkey"`
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

type TenantPaymentStatus uint8

const (
	TenantPaymentStatusDelinquent TenantPaymentStatus = iota
	TenantPaymentStatusPaid
)

func (s TenantPaymentStatus) String() string {
	return [...]string{
		"delinquent",
		"paid",
	}[s]
}

func (s TenantPaymentStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

type Tenant struct {
	AccountType                 AccountType
	Owner                       [32]uint8 `influx:"tag,owner,pubkey"`
	BumpSeed                    uint8     `influx:"-"`
	Code                        string    `influx:"tag,code"`
	VrfId                       uint16    `influx:"field,vrf_id"`
	ReferenceCount              uint32    `influx:"field,reference_count"`
	Administrators              [][32]byte
	PaymentStatus               TenantPaymentStatus `influx:"tag,payment_status"`
	TokenAccount                [32]byte            `influx:"tag,token_account,pubkey"`
	MetroRouting                bool                `influx:"field,metro_routing"`
	RouteLiveness               bool                `influx:"field,route_liveness"`
	BillingDiscriminant         uint8               `influx:"-"`
	BillingRate                 uint64              `influx:"field,billing_rate"`
	BillingLastDeductionDzEpoch uint64              `influx:"field,billing_last_deduction_dz_epoch"`
	IncludeTopologies           [][32]byte          `json:",omitempty"`
	PubKey                      [32]byte            `influx:"tag,pubkey,pubkey"`
}

func (t Tenant) MarshalJSON() ([]byte, error) {
	type TenantAlias Tenant

	adminStrings := make([]string, len(t.Administrators))
	for i, admin := range t.Administrators {
		adminStrings[i] = base58.Encode(admin[:])
	}

	jsonTenant := &struct {
		TenantAlias
		Owner          string   `json:"Owner"`
		PubKey         string   `json:"PubKey"`
		Administrators []string `json:"Administrators"`
		PaymentStatus  string   `json:"PaymentStatus"`
		TokenAccount   string   `json:"TokenAccount"`
	}{
		TenantAlias:    TenantAlias(t),
		Administrators: adminStrings,
	}

	jsonTenant.Owner = base58.Encode(t.Owner[:])
	jsonTenant.PubKey = base58.Encode(t.PubKey[:])
	jsonTenant.PaymentStatus = t.PaymentStatus.String()
	jsonTenant.TokenAccount = base58.Encode(t.TokenAccount[:])

	return json.Marshal(jsonTenant)
}

type UserUserType uint8

const (
	UserTypeIBRL = iota
	UserTypeIBRLWithAllocatedIP
	UserTypeEdgeFiltering
	UserTypeMulticast
)

// UserTypeIBRLWithAllocIP is an alias for UserTypeIBRLWithAllocatedIP.
const UserTypeIBRLWithAllocIP = UserTypeIBRLWithAllocatedIP

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
	UserStatusPending             UserStatus = 0
	UserStatusActivated           UserStatus = 1
	UserStatusSuspendedDeprecated UserStatus = 2
	UserStatusDeleted             UserStatus = 3
	UserStatusRejected            UserStatus = 4
	UserStatusPendingBan          UserStatus = 5
	UserStatusBanned              UserStatus = 6
	UserStatusUpdating            UserStatus = 7
	UserStatusOutOfCredits        UserStatus = 8
)

func (u UserStatus) String() string {
	switch u {
	case UserStatusPending:
		return "pending"
	case UserStatusActivated:
		return "activated"
	case UserStatusSuspendedDeprecated:
		return "suspended"
	case UserStatusDeleted:
		return "deleted"
	case UserStatusRejected:
		return "rejected"
	case UserStatusPendingBan:
		return "pending_ban"
	case UserStatusBanned:
		return "banned"
	case UserStatusUpdating:
		return "updating"
	case UserStatusOutOfCredits:
		return "out_of_credits"
	default:
		return "unknown"
	}
}

func (u UserStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.String())
}

type User struct {
	AccountType     AccountType
	Owner           [32]uint8
	Index           Uint128
	BumpSeed        uint8
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
	// Tunnel endpoint IP (device-side GRE endpoint). 0.0.0.0 means use device.public_ip for backwards compatibility.
	TunnelEndpoint    [4]uint8
	TunnelFlags       uint8
	BgpStatus         uint8
	LastBgpUpAt       uint64
	LastBgpReportedAt uint64
	PubKey            [32]byte
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
		TunnelEndpoint  string   `json:"TunnelEndpoint"`
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
		TunnelEndpoint:  net.IP(u.TunnelEndpoint[:]).String(),
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
	MulticastGroupStatusDeleting
	MulticastGroupStatusRejected
)

type MulticastGroup struct {
	AccountType     AccountType
	Owner           [32]uint8
	Index           Uint128
	BumpSeed        uint8
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
	AccountType      AccountType
	BumpSeed         uint8
	Version          ProgramVersion
	MinCompatVersion ProgramVersion
}

type AccessPassTypeTag uint8

const (
	AccessPassTypePrepaid            AccessPassTypeTag = 0
	AccessPassTypeSolanaValidator    AccessPassTypeTag = 1
	AccessPassTypeSolanaRPC          AccessPassTypeTag = 2
	AccessPassTypeSolanaMulticastPub AccessPassTypeTag = 3
	AccessPassTypeSolanaMulticastSub AccessPassTypeTag = 4
	AccessPassTypeOthers             AccessPassTypeTag = 5
)

type AccessPassStatus uint8

const (
	AccessPassStatusRequested    AccessPassStatus = 0
	AccessPassStatusConnected    AccessPassStatus = 1
	AccessPassStatusDisconnected AccessPassStatus = 2
	AccessPassStatusExpired      AccessPassStatus = 3
)

func (s AccessPassStatus) String() string {
	switch s {
	case AccessPassStatusRequested:
		return "requested"
	case AccessPassStatusConnected:
		return "connected"
	case AccessPassStatusDisconnected:
		return "disconnected"
	case AccessPassStatusExpired:
		return "expired"
	default:
		return "unknown"
	}
}

type AccessPass struct {
	AccountType        AccountType
	Owner              [32]byte
	BumpSeed           uint8
	AccessPassTypeTag  AccessPassTypeTag
	AssociatedPubkey   [32]byte // for SolanaValidator, SolanaRPC, SolanaMulticast*
	OthersTypeName     string   // for Others variant
	OthersKey          string   // for Others variant
	ClientIp           [4]uint8
	UserPayer          [32]byte
	LastAccessEpoch    uint64
	ConnectionCount    uint16
	Status             AccessPassStatus
	MGroupPubAllowlist [][32]byte
	MGroupSubAllowlist [][32]byte
	Flags              uint8
	PubKey             [32]byte
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

// AllocatorType represents the type of allocator in a ResourceExtension
type AllocatorType uint8

const (
	AllocatorTypeIp AllocatorType = 0
	AllocatorTypeId AllocatorType = 1
)

func (a AllocatorType) String() string {
	switch a {
	case AllocatorTypeIp:
		return "ip"
	case AllocatorTypeId:
		return "id"
	default:
		return "unknown"
	}
}

// IpAllocator manages IP address allocation from a CIDR block
type IpAllocator struct {
	BaseNet        [5]byte // NetworkV4: 4 bytes IP + 1 byte prefix length
	FirstFreeIndex uint64  // usize in Rust (8 bytes on 64-bit)
}

// IdAllocator manages ID allocation from a numeric range
type IdAllocator struct {
	RangeStart     uint16 // Start of the range (inclusive)
	RangeEnd       uint16 // End of the range (exclusive)
	FirstFreeIndex uint64 // usize in Rust (8 bytes on 64-bit)
}

// Allocator represents either an IP or ID allocator
type Allocator struct {
	Type        AllocatorType
	IpAllocator *IpAllocator
	IdAllocator *IdAllocator
}

// ResourceExtension represents an on-chain resource pool (IP block or ID range)
type ResourceExtension struct {
	AccountType    AccountType
	Owner          [32]byte
	BumpSeed       uint8
	AssociatedWith [32]byte // Device pubkey for device-specific pools, or zero for global pools
	Allocator      Allocator
	Storage        []byte // Bitmap of allocated resources
	PubKey         [32]byte
}

// TotalCapacity returns the total number of resources in the pool
func (r *ResourceExtension) TotalCapacity() int {
	switch r.Allocator.Type {
	case AllocatorTypeIp:
		if r.Allocator.IpAllocator == nil {
			return 0
		}
		prefixLen := r.Allocator.IpAllocator.BaseNet[4]
		if prefixLen > 32 {
			return 0
		}
		return 1 << (32 - prefixLen)
	case AllocatorTypeId:
		if r.Allocator.IdAllocator == nil {
			return 0
		}
		return int(r.Allocator.IdAllocator.RangeEnd - r.Allocator.IdAllocator.RangeStart)
	default:
		return 0
	}
}

// AllocatedCount returns the number of currently allocated resources
func (r *ResourceExtension) AllocatedCount() int {
	count := 0
	for _, b := range r.Storage {
		// Count set bits in each byte
		for b != 0 {
			count += int(b & 1)
			b >>= 1
		}
	}
	return count
}

// AvailableCount returns the number of available (unallocated) resources
func (r *ResourceExtension) AvailableCount() int {
	return r.TotalCapacity() - r.AllocatedCount()
}

// BaseNetString returns the base network as a CIDR string for IP allocators
func (r *ResourceExtension) BaseNetString() string {
	if r.Allocator.Type != AllocatorTypeIp || r.Allocator.IpAllocator == nil {
		return ""
	}
	return onChainNetToString(r.Allocator.IpAllocator.BaseNet)
}

// RangeString returns the ID range as a string for ID allocators
func (r *ResourceExtension) RangeString() string {
	if r.Allocator.Type != AllocatorTypeId || r.Allocator.IdAllocator == nil {
		return ""
	}
	return fmt.Sprintf("[%d, %d)", r.Allocator.IdAllocator.RangeStart, r.Allocator.IdAllocator.RangeEnd)
}

type PermissionStatus uint8

const (
	PermissionStatusNone      PermissionStatus = 0
	PermissionStatusActivated PermissionStatus = 1
	PermissionStatusSuspended PermissionStatus = 2
	PermissionStatusDeleting  PermissionStatus = 3
)

func (s PermissionStatus) String() string {
	switch s {
	case PermissionStatusNone:
		return "none"
	case PermissionStatusActivated:
		return "activated"
	case PermissionStatusSuspended:
		return "suspended"
	case PermissionStatusDeleting:
		return "deleting"
	default:
		return "unknown"
	}
}

func (s PermissionStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// Permission flag bit positions (bitmask stored as u128, split into Lo/Hi uint64).
const (
	PermissionFlagFoundation       uint64 = 1 << 0
	PermissionFlagPermissionAdmin  uint64 = 1 << 1
	PermissionFlagInfraAdmin       uint64 = 1 << 2
	PermissionFlagNetworkAdmin     uint64 = 1 << 3
	PermissionFlagTenantAdmin      uint64 = 1 << 4
	PermissionFlagMulticastAdmin   uint64 = 1 << 5
	PermissionFlagFeedAuthority    uint64 = 1 << 6
	PermissionFlagActivator        uint64 = 1 << 7
	PermissionFlagSentinel         uint64 = 1 << 8
	PermissionFlagUserAdmin        uint64 = 1 << 9
	PermissionFlagAccessPassAdmin  uint64 = 1 << 10
	PermissionFlagHealthOracle     uint64 = 1 << 11
	PermissionFlagQA               uint64 = 1 << 12
	PermissionFlagGlobalstateAdmin uint64 = 1 << 13
	PermissionFlagContributorAdmin uint64 = 1 << 14
)

type Permission struct {
	AccountType   AccountType
	Owner         [32]byte
	BumpSeed      uint8
	Status        PermissionStatus
	UserPayer     [32]byte
	PermissionsLo uint64
	PermissionsHi uint64
	PubKey        [32]byte
}

func (p Permission) MarshalJSON() ([]byte, error) {
	type PermissionAlias Permission

	return json.Marshal(&struct {
		PermissionAlias
		Owner     string `json:"Owner"`
		UserPayer string `json:"UserPayer"`
		PubKey    string `json:"PubKey"`
		Status    string `json:"Status"`
	}{
		PermissionAlias: PermissionAlias(p),
		Owner:           base58.Encode(p.Owner[:]),
		UserPayer:       base58.Encode(p.UserPayer[:]),
		PubKey:          base58.Encode(p.PubKey[:]),
		Status:          p.Status.String(),
	})
}

func (r ResourceExtension) MarshalJSON() ([]byte, error) {
	type ResourceExtensionAlias ResourceExtension

	jsonExt := &struct {
		ResourceExtensionAlias
		Owner          string `json:"Owner"`
		AssociatedWith string `json:"AssociatedWith"`
		PubKey         string `json:"PubKey"`
		AllocatorType  string `json:"AllocatorType"`
		BaseNet        string `json:"BaseNet,omitempty"`
		Range          string `json:"Range,omitempty"`
		TotalCapacity  int    `json:"TotalCapacity"`
		AllocatedCount int    `json:"AllocatedCount"`
		AvailableCount int    `json:"AvailableCount"`
	}{
		ResourceExtensionAlias: ResourceExtensionAlias(r),
		Owner:                  base58.Encode(r.Owner[:]),
		AssociatedWith:         base58.Encode(r.AssociatedWith[:]),
		PubKey:                 base58.Encode(r.PubKey[:]),
		AllocatorType:          r.Allocator.Type.String(),
		BaseNet:                r.BaseNetString(),
		Range:                  r.RangeString(),
		TotalCapacity:          r.TotalCapacity(),
		AllocatedCount:         r.AllocatedCount(),
		AvailableCount:         r.AvailableCount(),
	}

	return json.Marshal(jsonExt)
}

type TopologyConstraint uint8

const (
	TopologyConstraintIncludeAny TopologyConstraint = 0
	TopologyConstraintExclude    TopologyConstraint = 1
)

func (c TopologyConstraint) String() string {
	switch c {
	case TopologyConstraintIncludeAny:
		return "include-any"
	case TopologyConstraintExclude:
		return "exclude"
	default:
		return "unknown"
	}
}

type TopologyInfo struct {
	AccountType    AccountType
	Owner          [32]byte
	BumpSeed       uint8
	Name           string
	AdminGroupBit  uint8
	FlexAlgoNumber uint8
	Constraint     TopologyConstraint
	ReferenceCount uint32
	PubKey         [32]byte
}
