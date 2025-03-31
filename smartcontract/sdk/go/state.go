package dzsdk

type AccountType uint8

const (
	GlobalState AccountType = iota + 1
	ConfigType
	LocationType
	ExchangeType
	DeviceType
	TunnelType
	UserType
)

type LocationStatus uint8

const (
	LocationStatusPending LocationStatus = iota
	LocationStatusActivated
	LocationStatusSuspended
	LocationStatusDeleted
)

type Uint128 struct {
	High uint64 // Parte alta del número
	Low  uint64 // Parte baja del número
}

type Config struct {
	AccountType       AccountType
	Owner             [32]byte
	Local_asn         uint32
	Remote_asn        uint32
	TunnelTunnelBlock [5]uint8
	UserTunnelBlock   [5]uint8
	PubKey            [32]byte
}

type Location struct {
	AccountType AccountType
	Owner       [32]uint8
	Index       Uint128
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

type Device struct {
	AccountType    AccountType
	Owner          [32]uint8
	Index          Uint128
	LocationPubKey [32]uint8
	ExchangePubKey [32]uint8
	DeviceType     uint8
	PublicIp       [4]uint8
	Status         DeviceStatus
	Code           string
	DzPrefixes     [][5]uint8
	PubKey         [32]byte
}

type TunnelTunnelType uint8

const (
	TunnelTunnelTypeMPLSoverGRE TunnelTunnelType = iota + 1
)

type TunnelStatus uint8

const (
	TunnelStatusPending TunnelStatus = iota
	TunnelStatusActivated
	TunnelStatusSuspended
	TunnelStatusDeleted
)

type Tunnel struct {
	AccountType AccountType
	Owner       [32]uint8
	Index       Uint128
	SideAPubKey [32]uint8
	SideZPubKey [32]uint8
	TunnelType  TunnelTunnelType
	Bandwidth   uint64
	Mtu         uint32
	DelayNs     uint64
	JitterNs    uint64
	TunnelId    uint16
	TunnelNet   [5]uint8
	Status      TunnelStatus
	Code        string
	PubKey      [32]byte
}

type UserUserType uint8

const (
	UserTypeServer UserUserType = iota + 1
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
	UserType     UserUserType
	TenantPubKey [32]uint8
	DevicePubKey [32]uint8
	CyoaType     CyoaType
	ClientIp     [4]uint8
	DzIp         [4]uint8
	TunnelId     uint16
	TunnelNet    [5]uint8
	Status       UserStatus
	PubKey       [32]byte
}
