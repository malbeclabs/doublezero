package serviceability

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
)

type InterfaceType uint8

const (
	InterfaceTypeInvalid InterfaceType = iota
	InterfaceTypeLoopback
	InterfaceTypePhysical
)

type LoopbackType uint8

const (
	LoopbackTypeNone LoopbackType = iota
	LoopbackTypeVpnv4
	LoopbackTypeIpv4
	LoopbackTypePimRpAddr
	LoopbackTypeReserved
)

type Interface struct {
	Version            uint8
	Name               string
	InterfaceType      InterfaceType
	LoopbackType       LoopbackType
	VlanId             uint16
	IpNet              [5]uint8
	NodeSegmentIdx     uint16
	UserTunnelEndpoint bool
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
	BgpAsn                 uint32
	DiaBgpAsn              uint32
	MgmtVrf                string
	DnsServers             [][4]uint8
	NtpServers             [][4]uint8
	Interfaces             []Interface
	PubKey                 [32]byte
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
)

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
