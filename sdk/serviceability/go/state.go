package serviceability

import (
	"encoding/json"
	"fmt"
	"net"

	"github.com/mr-tron/base58"
)

type AccountType uint8

const (
	GlobalStateType    AccountType = 1
	GlobalConfigType   AccountType = 2
	LocationType       AccountType = 3
	ExchangeType       AccountType = 4
	DeviceType         AccountType = 5
	LinkType           AccountType = 6
	UserType           AccountType = 7
	MulticastGroupType AccountType = 8
	ProgramConfigType  AccountType = 9
	ContributorType    AccountType = 10
	AccessPassType     AccountType = 11
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

type DeviceDeviceType uint8

const (
	DeviceDeviceTypeHybrid  DeviceDeviceType = 0
	DeviceDeviceTypeTransit DeviceDeviceType = 1
	DeviceDeviceTypeEdge    DeviceDeviceType = 2
)

func (d DeviceDeviceType) String() string {
	switch d {
	case DeviceDeviceTypeHybrid:
		return "hybrid"
	case DeviceDeviceTypeTransit:
		return "transit"
	case DeviceDeviceTypeEdge:
		return "edge"
	default:
		return "unknown"
	}
}

type DeviceStatus uint8

const (
	DeviceStatusPending            DeviceStatus = 0
	DeviceStatusActivated          DeviceStatus = 1
	DeviceStatusDeleting           DeviceStatus = 2
	DeviceStatusRejected           DeviceStatus = 3
	DeviceStatusDrained            DeviceStatus = 4
	DeviceStatusDeviceProvisioning DeviceStatus = 5
	DeviceStatusLinkProvisioning   DeviceStatus = 6
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
		return "unknown"
	}
}

func (d DeviceHealth) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

type DeviceDesiredStatus uint8

const (
	DeviceDesiredStatusPending   DeviceDesiredStatus = 0
	DeviceDesiredStatusActivated DeviceDesiredStatus = 1
	DeviceDesiredStatusDrained   DeviceDesiredStatus = 6
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
	InterfaceStatusInvalid   InterfaceStatus = 0
	InterfaceStatusUnmanaged InterfaceStatus = 1
	InterfaceStatusPending   InterfaceStatus = 2
	InterfaceStatusActivated InterfaceStatus = 3
	InterfaceStatusDeleting  InterfaceStatus = 4
	InterfaceStatusRejecting InterfaceStatus = 5
	InterfaceStatusUnlinked  InterfaceStatus = 6
)

func (i InterfaceStatus) String() string {
	switch i {
	case InterfaceStatusInvalid:
		return "invalid"
	case InterfaceStatusUnmanaged:
		return "unmanaged"
	case InterfaceStatusPending:
		return "pending"
	case InterfaceStatusActivated:
		return "activated"
	case InterfaceStatusDeleting:
		return "deleting"
	case InterfaceStatusRejecting:
		return "rejecting"
	case InterfaceStatusUnlinked:
		return "unlinked"
	default:
		return "unknown"
	}
}

func (i InterfaceStatus) MarshalJSON() ([]byte, error) {
	return json.Marshal(i.String())
}

type InterfaceType uint8

const (
	InterfaceTypeInvalid  InterfaceType = 0
	InterfaceTypeLoopback InterfaceType = 1
	InterfaceTypePhysical InterfaceType = 2
)

func (i InterfaceType) String() string {
	switch i {
	case InterfaceTypeInvalid:
		return "invalid"
	case InterfaceTypeLoopback:
		return "loopback"
	case InterfaceTypePhysical:
		return "physical"
	default:
		return "unknown"
	}
}

func (i InterfaceType) MarshalJSON() ([]byte, error) {
	return json.Marshal(i.String())
}

type LoopbackType uint8

const (
	LoopbackTypeNone      LoopbackType = 0
	LoopbackTypeVpnv4     LoopbackType = 1
	LoopbackTypeIpv4      LoopbackType = 2
	LoopbackTypePimRpAddr LoopbackType = 3
	LoopbackTypeReserved  LoopbackType = 4
)

func (l LoopbackType) String() string {
	switch l {
	case LoopbackTypeNone:
		return "none"
	case LoopbackTypeVpnv4:
		return "vpnv4"
	case LoopbackTypeIpv4:
		return "ipv4"
	case LoopbackTypePimRpAddr:
		return "pim_rp_addr"
	case LoopbackTypeReserved:
		return "reserved"
	default:
		return "unknown"
	}
}

func (l LoopbackType) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type InterfaceCYOA uint8

const (
	InterfaceCYOANone               InterfaceCYOA = 0
	InterfaceCYOAGREOverDIA         InterfaceCYOA = 1
	InterfaceCYOAGREOverFabric      InterfaceCYOA = 2
	InterfaceCYOAGREOverPrivatePeer InterfaceCYOA = 3
	InterfaceCYOAGREOverPublicPeer  InterfaceCYOA = 4
	InterfaceCYOAGREOverCable       InterfaceCYOA = 5
)

type InterfaceDIA uint8

const (
	InterfaceDIANone InterfaceDIA = 0
	InterfaceDIADIA  InterfaceDIA = 1
)

type RoutingMode uint8

const (
	RoutingModeStatic RoutingMode = 0
	RoutingModeBGP    RoutingMode = 1
)

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

const CurrentInterfaceVersion = 2

type Uint128 struct {
	Low  uint64
	High uint64
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
	PubKey                     [32]byte
}

type GlobalConfig struct {
	AccountType         AccountType
	Owner               [32]byte
	BumpSeed            uint8
	LocalASN            uint32
	RemoteASN           uint32
	DeviceTunnelBlock   [5]uint8
	UserTunnelBlock     [5]uint8
	MulticastGroupBlock [5]uint8
	NextBGPCommunity    uint16
	PubKey              [32]byte
}

type Location struct {
	AccountType    AccountType
	Owner          [32]byte
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

type Exchange struct {
	AccountType    AccountType
	Owner          [32]byte
	Index          Uint128
	BumpSeed       uint8
	Lat            float64
	Lng            float64
	BgpCommunity   uint16
	Status         ExchangeStatus
	Code           string
	Name           string
	ReferenceCount uint32
	Device1PK      [32]byte
	Device2PK      [32]byte
	PubKey         [32]byte
}

type Device struct {
	AccountType            AccountType
	Owner                  [32]byte
	Index                  Uint128
	BumpSeed               uint8
	LocationPubKey         [32]byte
	ExchangePubKey         [32]byte
	DeviceType             DeviceDeviceType
	PublicIp               [4]uint8
	Status                 DeviceStatus
	Code                   string
	DzPrefixes             [][5]uint8
	MetricsPublisherPubKey [32]byte
	ContributorPubKey      [32]byte
	MgmtVrf                string
	Interfaces             []Interface
	ReferenceCount         uint32
	UsersCount             uint16
	MaxUsers               uint16
	DeviceHealth           DeviceHealth
	DeviceDesiredStatus    DeviceDesiredStatus
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
		prefixes[i] = networkV4ToString(p)
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
	LinkStatusDeleting     LinkStatus = 2
	LinkStatusRejected     LinkStatus = 3
	LinkStatusRequested    LinkStatus = 4
	LinkStatusHardDrained  LinkStatus = 5
	LinkStatusSoftDrained  LinkStatus = 6
	LinkStatusProvisioning LinkStatus = 7
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
		return "unknown"
	}
}

func (l LinkHealth) MarshalJSON() ([]byte, error) {
	return json.Marshal(l.String())
}

type LinkDesiredStatus uint8

const (
	LinkDesiredStatusPending     LinkDesiredStatus = 0
	LinkDesiredStatusActivated   LinkDesiredStatus = 1
	LinkDesiredStatusHardDrained LinkDesiredStatus = 2
	LinkDesiredStatusSoftDrained LinkDesiredStatus = 3
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
	Owner             [32]byte
	Index             Uint128
	BumpSeed          uint8
	SideAPubKey       [32]byte
	SideZPubKey       [32]byte
	LinkType          LinkLinkType
	Bandwidth         uint64
	Mtu               uint32
	DelayNs           uint64
	JitterNs          uint64
	TunnelId          uint16
	TunnelNet         [5]uint8
	Status            LinkStatus
	Code              string
	ContributorPubKey [32]byte
	SideAIfaceName    string
	SideZIfaceName    string
	DelayOverrideNs   uint64
	LinkHealth        LinkHealth
	LinkDesiredStatus LinkDesiredStatus
	PubKey            [32]byte
}

type ContributorStatus uint8

const (
	ContributorStatusNone      ContributorStatus = 0
	ContributorStatusActivated ContributorStatus = 1
	ContributorStatusSuspended ContributorStatus = 2
	ContributorStatusDeleting  ContributorStatus = 3
)

func (s ContributorStatus) String() string {
	switch s {
	case ContributorStatusNone:
		return "none"
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
	Owner          [32]byte
	Index          Uint128
	BumpSeed       uint8
	Status         ContributorStatus
	Code           string
	ReferenceCount uint32
	OpsManagerPK   [32]byte
	PubKey         [32]byte
}

type UserUserType uint8

const (
	UserTypeIBRL            UserUserType = 0
	UserTypeIBRLWithAllocIP UserUserType = 1
	UserTypeEdgeFiltering   UserUserType = 2
	UserTypeMulticast       UserUserType = 3
)

func (u UserUserType) String() string {
	switch u {
	case UserTypeIBRL:
		return "ibrl"
	case UserTypeIBRLWithAllocIP:
		return "ibrl_with_allocated_ip"
	case UserTypeEdgeFiltering:
		return "edge_filtering"
	case UserTypeMulticast:
		return "multicast"
	default:
		return "unknown"
	}
}

type CyoaType uint8

const (
	CyoaTypeNone               CyoaType = 0
	CyoaTypeGREOverDIA         CyoaType = 1
	CyoaTypeGREOverFabric      CyoaType = 2
	CyoaTypeGREOverPrivatePeer CyoaType = 3
	CyoaTypeGREOverPublicPeer  CyoaType = 4
	CyoaTypeGREOverCable       CyoaType = 5
)

func (c CyoaType) String() string {
	switch c {
	case CyoaTypeNone:
		return "none"
	case CyoaTypeGREOverDIA:
		return "gre_over_dia"
	case CyoaTypeGREOverFabric:
		return "gre_over_fabric"
	case CyoaTypeGREOverPrivatePeer:
		return "gre_over_private_peering"
	case CyoaTypeGREOverPublicPeer:
		return "gre_over_public_peering"
	case CyoaTypeGREOverCable:
		return "gre_over_cable"
	default:
		return "unknown"
	}
}

type UserStatus uint8

const (
	UserStatusPending      UserStatus = 0
	UserStatusActivated    UserStatus = 1
	UserStatusDeleting     UserStatus = 3
	UserStatusRejected     UserStatus = 4
	UserStatusPendingBan   UserStatus = 5
	UserStatusBanned       UserStatus = 6
	UserStatusUpdating     UserStatus = 7
	UserStatusOutOfCredits UserStatus = 8
)

func (u UserStatus) String() string {
	switch u {
	case UserStatusPending:
		return "pending"
	case UserStatusActivated:
		return "activated"
	case UserStatusDeleting:
		return "deleting"
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
	Owner           [32]byte
	Index           Uint128
	BumpSeed        uint8
	UserType        UserUserType
	TenantPubKey    [32]byte
	DevicePubKey    [32]byte
	CyoaType        CyoaType
	ClientIp        [4]uint8
	DzIp            [4]uint8
	TunnelId        uint16
	TunnelNet       [5]uint8
	Status          UserStatus
	Publishers      [][32]byte
	Subscribers     [][32]byte
	ValidatorPubKey [32]byte
	PubKey          [32]byte
}

type MulticastGroupStatus uint8

const (
	MulticastGroupStatusPending   MulticastGroupStatus = 0
	MulticastGroupStatusActivated MulticastGroupStatus = 1
	MulticastGroupStatusSuspended MulticastGroupStatus = 2
	MulticastGroupStatusDeleting  MulticastGroupStatus = 3
	MulticastGroupStatusRejected  MulticastGroupStatus = 4
)

func (s MulticastGroupStatus) String() string {
	switch s {
	case MulticastGroupStatusPending:
		return "pending"
	case MulticastGroupStatusActivated:
		return "activated"
	case MulticastGroupStatusSuspended:
		return "suspended"
	case MulticastGroupStatusDeleting:
		return "deleting"
	case MulticastGroupStatusRejected:
		return "rejected"
	default:
		return "unknown"
	}
}

type MulticastGroup struct {
	AccountType     AccountType
	Owner           [32]byte
	Index           Uint128
	BumpSeed        uint8
	TenantPubKey    [32]byte
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
	AccessPassTypePrepaid                 AccessPassTypeTag = 0
	AccessPassTypeSolanaValidator         AccessPassTypeTag = 1
	AccessPassTypeSolanaRPC               AccessPassTypeTag = 2
	AccessPassTypeSolanaMulticastPub      AccessPassTypeTag = 3
	AccessPassTypeSolanaMulticastSub      AccessPassTypeTag = 4
	AccessPassTypeOthers                  AccessPassTypeTag = 5
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

func networkV4ToString(n [5]uint8) string {
	prefixLen := n[4]
	if prefixLen > 0 && prefixLen <= 32 {
		ip := net.IP(n[:4])
		return fmt.Sprintf("%s/%d", ip.String(), prefixLen)
	}
	return ""
}
