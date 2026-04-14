package serviceability

import "log"

func DeserializeGlobalState(reader *ByteReader, gs *GlobalState) {
	gs.AccountType = AccountType(reader.ReadU8())
	gs.BumpSeed = reader.ReadU8()
	gs.AccountIndex = reader.ReadU128()
	gs.FoundationAllowlist = reader.ReadPubkeySlice()
	_ = reader.ReadPubkeySlice() // deprecated device_allowlist
	_ = reader.ReadPubkeySlice() // deprecated user_allowlist
	gs.ActivatorAuthorityPK = reader.ReadPubkey()
	gs.SentinelAuthorityPK = reader.ReadPubkey()
	gs.ContributorAirdropLamports = reader.ReadU64()
	gs.UserAirdropLamports = reader.ReadU64()
	gs.HealthOraclePK = reader.ReadPubkey()
	gs.QAAllowlist = reader.ReadPubkeySlice()
	gs.FeatureFlags = reader.ReadU128()
	gs.FeedAuthorityPK = reader.ReadPubkey()
}

func DeserializeGlobalConfig(reader *ByteReader, cfg *GlobalConfig) {
	cfg.AccountType = AccountType(reader.ReadU8())
	cfg.Owner = reader.ReadPubkey()
	cfg.BumpSeed = reader.ReadU8()
	cfg.LocalASN = reader.ReadU32()
	cfg.RemoteASN = reader.ReadU32()
	cfg.DeviceTunnelBlock = reader.ReadNetworkV4()
	cfg.UserTunnelBlock = reader.ReadNetworkV4()
	cfg.MulticastGroupBlock = reader.ReadNetworkV4()
	cfg.NextBGPCommunity = reader.ReadU16()
	cfg.MulticastPublisherBlock = reader.ReadNetworkV4()
}

// DeserializeConfig is a backward-compatible alias for DeserializeGlobalConfig.
func DeserializeConfig(reader *ByteReader, cfg *GlobalConfig) {
	DeserializeGlobalConfig(reader, cfg)
}

func DeserializeLocation(reader *ByteReader, loc *Location) {
	loc.AccountType = AccountType(reader.ReadU8())
	loc.Owner = reader.ReadPubkey()
	loc.Index = reader.ReadU128()
	loc.BumpSeed = reader.ReadU8()
	loc.Lat = reader.ReadF64()
	loc.Lng = reader.ReadF64()
	loc.LocId = reader.ReadU32()
	loc.Status = LocationStatus(reader.ReadU8())
	loc.Code = reader.ReadString()
	loc.Name = reader.ReadString()
	loc.Country = reader.ReadString()
	loc.ReferenceCount = reader.ReadU32()
}

func DeserializeExchange(reader *ByteReader, exchange *Exchange) {
	exchange.AccountType = AccountType(reader.ReadU8())
	exchange.Owner = reader.ReadPubkey()
	exchange.Index = reader.ReadU128()
	exchange.BumpSeed = reader.ReadU8()
	exchange.Lat = reader.ReadF64()
	exchange.Lng = reader.ReadF64()
	exchange.BgpCommunity = reader.ReadU16()
	_ = reader.ReadU16() // unused padding
	exchange.Status = ExchangeStatus(reader.ReadU8())
	exchange.Code = reader.ReadString()
	exchange.Name = reader.ReadString()
	exchange.ReferenceCount = reader.ReadU32()
	exchange.Device1PK = reader.ReadPubkey()
	exchange.Device2PK = reader.ReadPubkey()
}

func DeserializeContributor(reader *ByteReader, contributor *Contributor) {
	contributor.AccountType = AccountType(reader.ReadU8())
	contributor.Owner = reader.ReadPubkey()
	contributor.Index = reader.ReadU128()
	contributor.BumpSeed = reader.ReadU8()
	contributor.Status = ContributorStatus(reader.ReadU8())
	contributor.Code = reader.ReadString()
	contributor.ReferenceCount = reader.ReadU32()
	contributor.OpsManagerPK = reader.ReadPubkey()
}

// DeserializeInterface reads an on-chain Interface account from reader.
//
// Interface version history (discriminant byte):
//
//	0 — V1: original format (no CYOA/DIA/Bandwidth fields)
//	1 — V2: adds CYOA, DIA, Bandwidth, Cir, Mtu, RoutingMode, and flex_algo_node_segments (RFC-18)
//	       Pre-RFC-18 mainnet accounts also use discriminant 1 but lack the flex_algo bytes.
//	       MigrateDeviceInterfaces must be run on all existing accounts before this SDK is
//	       deployed, so that every V2 account on-chain has the flex_algo bytes present.
//	2 — reserved, never written
//
// Design note: discriminant 3 (V3) was considered during RFC-18 implementation to distinguish
// old V2 (no flex_algo bytes) from new V2 (with flex_algo bytes) in a shared Borsh buffer
// reader. It was rejected in favour of a one-time migration (MigrateDeviceInterfaces) that
// rewrites all pre-RFC-18 accounts into the new V2 layout, avoiding the need for a new
// discriminant value entirely.
func DeserializeInterface(reader *ByteReader, iface *Interface) {
	iface.Version = reader.ReadU8()

	switch iface.Version {
	case 0: // V1
		DeserializeInterfaceV1(reader, iface)
	case 1: // V2: includes flex_algo_node_segments (RFC-18); requires MigrateDeviceInterfaces to have run
		DeserializeInterfaceV2(reader, iface)
	default:
		log.Println("DeserializeInterface: Unsupported interface version", iface.Version)
	}
}

func DeserializeInterfaceV1(reader *ByteReader, iface *Interface) {
	iface.Status = InterfaceStatus(reader.ReadU8())
	iface.Name = reader.ReadString()
	iface.InterfaceType = InterfaceType(reader.ReadU8())
	iface.LoopbackType = LoopbackType(reader.ReadU8())
	iface.VlanId = reader.ReadU16()
	iface.IpNet = reader.ReadNetworkV4()
	iface.NodeSegmentIdx = reader.ReadU16()
	iface.UserTunnelEndpoint = (reader.ReadU8() != 0)
}

func DeserializeInterfaceV2(reader *ByteReader, iface *Interface) {
	iface.Status = InterfaceStatus(reader.ReadU8())
	iface.Name = reader.ReadString()
	iface.InterfaceType = InterfaceType(reader.ReadU8())
	iface.InterfaceCYOA = InterfaceCYOA(reader.ReadU8())
	iface.InterfaceDIA = InterfaceDIA(reader.ReadU8())
	iface.LoopbackType = LoopbackType(reader.ReadU8())
	iface.Bandwidth = reader.ReadU64()
	iface.Cir = reader.ReadU64()
	iface.Mtu = reader.ReadU16()
	iface.RoutingMode = RoutingMode(reader.ReadU8())
	iface.VlanId = reader.ReadU16()
	iface.IpNet = reader.ReadNetworkV4()
	iface.NodeSegmentIdx = reader.ReadU16()
	iface.UserTunnelEndpoint = (reader.ReadU8() != 0)
	// flex_algo_node_segments (RFC-18): present in all V2 accounts after MigrateDeviceInterfaces.
	length := reader.ReadU32()
	iface.FlexAlgoNodeSegments = make([]FlexAlgoNodeSegment, length)
	for i := uint32(0); i < length; i++ {
		iface.FlexAlgoNodeSegments[i].Topology = reader.ReadPubkey()
		iface.FlexAlgoNodeSegments[i].NodeSegmentIdx = reader.ReadU16()
	}
}

func DeserializeDevice(reader *ByteReader, dev *Device) {
	dev.AccountType = AccountType(reader.ReadU8())
	dev.Owner = reader.ReadPubkey()
	dev.Index = reader.ReadU128()
	dev.BumpSeed = reader.ReadU8()
	dev.LocationPubKey = reader.ReadPubkey()
	dev.ExchangePubKey = reader.ReadPubkey()
	dev.DeviceType = DeviceDeviceType(reader.ReadU8())
	dev.PublicIp = reader.ReadIPv4()
	dev.Status = DeviceStatus(reader.ReadU8())
	dev.Code = reader.ReadString()
	dev.DzPrefixes = reader.ReadNetworkV4Slice()
	dev.MetricsPublisherPubKey = reader.ReadPubkey()
	dev.ContributorPubKey = reader.ReadPubkey()
	dev.MgmtVrf = reader.ReadString()
	dev.Interfaces = make([]Interface, 0)
	length := reader.ReadU32()
	if length > 0 && (length*18) > reader.Remaining() {
		log.Println("DeserializeDevice: Not enough data for interfaces (# of interfaces = ", length, ")")
		return
	}
	for i := uint32(0); i < length; i++ {
		var iface Interface
		DeserializeInterface(reader, &iface)
		dev.Interfaces = append(dev.Interfaces, iface)
	}
	dev.ReferenceCount = reader.ReadU32()
	dev.UsersCount = reader.ReadU16()
	dev.MaxUsers = reader.ReadU16()
	dev.DeviceHealth = DeviceHealth(reader.ReadU8())
	dev.DeviceDesiredStatus = DeviceDesiredStatus(reader.ReadU8())
	dev.UnicastUsersCount = reader.ReadU16()
	dev.MulticastSubscribersCount = reader.ReadU16()
	dev.MaxUnicastUsers = reader.ReadU16()
	dev.MaxMulticastSubscribers = reader.ReadU16()
	dev.ReservedSeats = reader.ReadU16()
	dev.MulticastPublishersCount = reader.ReadU16()
	dev.MaxMulticastPublishers = reader.ReadU16()
	// Note: dev.PubKey is set separately in client.go after deserialization
}

func DeserializeLink(reader *ByteReader, link *Link) {
	link.AccountType = AccountType(reader.ReadU8())
	link.Owner = reader.ReadPubkey()
	link.Index = reader.ReadU128()
	link.BumpSeed = reader.ReadU8()
	link.SideAPubKey = reader.ReadPubkey()
	link.SideZPubKey = reader.ReadPubkey()
	link.LinkType = LinkLinkType(reader.ReadU8())
	link.Bandwidth = reader.ReadU64()
	link.Mtu = reader.ReadU32()
	link.DelayNs = reader.ReadU64()
	link.JitterNs = reader.ReadU64()
	link.TunnelId = reader.ReadU16()
	link.TunnelNet = reader.ReadNetworkV4()
	link.Status = LinkStatus(reader.ReadU8())
	link.Code = reader.ReadString()
	link.ContributorPubKey = reader.ReadPubkey()
	link.SideAIfaceName = reader.ReadString()
	link.SideZIfaceName = reader.ReadString()
	link.DelayOverrideNs = reader.ReadU64()
	link.LinkHealth = LinkHealth(reader.ReadU8())
	link.LinkDesiredStatus = LinkDesiredStatus(reader.ReadU8())
	link.LinkTopologies = reader.ReadPubkeySlice()
	link.LinkFlags = reader.ReadU8()
}

func DeserializeUser(reader *ByteReader, user *User) {
	user.AccountType = AccountType(reader.ReadU8())
	user.Owner = reader.ReadPubkey()
	user.Index = reader.ReadU128()
	user.BumpSeed = reader.ReadU8()
	user.UserType = UserUserType(reader.ReadU8())
	user.TenantPubKey = reader.ReadPubkey()
	user.DevicePubKey = reader.ReadPubkey()
	user.CyoaType = CyoaType(reader.ReadU8())
	user.ClientIp = reader.ReadIPv4()
	user.DzIp = reader.ReadIPv4()
	user.TunnelId = reader.ReadU16()
	user.TunnelNet = reader.ReadNetworkV4()
	user.Status = UserStatus(reader.ReadU8())
	user.Publishers = reader.ReadPubkeySlice()
	user.Subscribers = reader.ReadPubkeySlice()
	user.ValidatorPubKey = reader.ReadPubkey()
	user.TunnelEndpoint = reader.ReadIPv4()
	user.TunnelFlags = reader.ReadU8()
	user.BgpStatus = reader.ReadU8()
	user.LastBgpUpAt = reader.ReadU64()
	user.LastBgpReportedAt = reader.ReadU64()
	// Note: user.PubKey is set separately in client.go after deserialization
}

func DeserializeMulticastGroup(reader *ByteReader, mg *MulticastGroup) {
	mg.AccountType = AccountType(reader.ReadU8())
	mg.Owner = reader.ReadPubkey()
	mg.Index = reader.ReadU128()
	mg.BumpSeed = reader.ReadU8()
	mg.TenantPubKey = reader.ReadPubkey()
	mg.MulticastIp = reader.ReadIPv4()
	mg.MaxBandwidth = reader.ReadU64()
	mg.Status = MulticastGroupStatus(reader.ReadU8())
	mg.Code = reader.ReadString()
	mg.PublisherCount = reader.ReadU32()
	mg.SubscriberCount = reader.ReadU32()
}

func DeserializeTenant(reader *ByteReader, tenant *Tenant) {
	tenant.AccountType = AccountType(reader.ReadU8())
	tenant.Owner = reader.ReadPubkey()
	tenant.BumpSeed = reader.ReadU8()
	tenant.Code = reader.ReadString()
	tenant.VrfId = reader.ReadU16()
	tenant.ReferenceCount = reader.ReadU32()
	tenant.Administrators = reader.ReadPubkeySlice()
	tenant.PaymentStatus = TenantPaymentStatus(reader.ReadU8())
	tenant.TokenAccount = reader.ReadPubkey()
	tenant.MetroRouting = (reader.ReadU8() != 0)
	tenant.RouteLiveness = (reader.ReadU8() != 0)
	tenant.BillingDiscriminant = reader.ReadU8()
	tenant.BillingRate = reader.ReadU64()
	tenant.BillingLastDeductionDzEpoch = reader.ReadU64()
	tenant.IncludeTopologies = reader.ReadPubkeySlice()
	// Note: tenant.PubKey is set separately in client.go after deserialization
}

func DeserializeProgramConfig(reader *ByteReader, pc *ProgramConfig) {
	pc.AccountType = AccountType(reader.ReadU8())
	pc.BumpSeed = reader.ReadU8()
	DeserializeProgramVersion(reader, &pc.Version)
	DeserializeProgramVersion(reader, &pc.MinCompatVersion)
}

func DeserializeProgramVersion(reader *ByteReader, pv *ProgramVersion) {
	pv.Major = reader.ReadU32()
	pv.Minor = reader.ReadU32()
	pv.Patch = reader.ReadU32()
}

func DeserializeAccessPass(reader *ByteReader, ap *AccessPass) {
	ap.AccountType = AccountType(reader.ReadU8())
	ap.Owner = reader.ReadPubkey()
	ap.BumpSeed = reader.ReadU8()
	// AccessPassType is a Borsh enum: 1-byte discriminant + optional data
	ap.AccessPassTypeTag = AccessPassTypeTag(reader.ReadU8())
	// Variants 1-4 have an associated pubkey
	if ap.AccessPassTypeTag >= 1 && ap.AccessPassTypeTag <= 4 {
		ap.AssociatedPubkey = reader.ReadPubkey()
	} else if ap.AccessPassTypeTag == AccessPassTypeOthers {
		// Variant 5 (Others) has two strings
		ap.OthersTypeName = reader.ReadString()
		ap.OthersKey = reader.ReadString()
	}
	ap.ClientIp = reader.ReadIPv4()
	ap.UserPayer = reader.ReadPubkey()
	ap.LastAccessEpoch = reader.ReadU64()
	ap.ConnectionCount = reader.ReadU16()
	ap.Status = AccessPassStatus(reader.ReadU8())
	ap.MGroupPubAllowlist = reader.ReadPubkeySlice()
	ap.MGroupSubAllowlist = reader.ReadPubkeySlice()
	ap.Flags = reader.ReadU8()
}

// ResourceExtension binary layout (from Rust):
// Header (84 bytes for IP allocator, 83 bytes for ID allocator):
//
//	[0]       account_type (u8) = 12
//	[1-32]    owner (Pubkey/[32]byte)
//	[33]      bump_seed (u8)
//	[34-65]   associated_with (Pubkey/[32]byte)
//	[66]      allocator discriminant (u8): 0=Ip, 1=Id
//	For Ip allocator (17 bytes):
//	  [67-71]   base_net IP (4 bytes)
//	  [72]      base_net prefix (1 byte)
//	  [73-80]   first_free_index (u64/usize)
//	For Id allocator (12 bytes):
//	  [67-68]   range_start (u16)
//	  [69-70]   range_end (u16)
//	  [71-78]   first_free_index (u64/usize)
//
// Bitmap starts at offset 88 (aligned to 8 bytes)
const resourceExtensionBitmapOffset = 88

func DeserializeResourceExtension(reader *ByteReader, ext *ResourceExtension) {
	ext.AccountType = AccountType(reader.ReadU8())
	ext.Owner = reader.ReadPubkey()
	ext.BumpSeed = reader.ReadU8()
	ext.AssociatedWith = reader.ReadPubkey()

	// Read allocator discriminant
	allocatorType := AllocatorType(reader.ReadU8())
	ext.Allocator.Type = allocatorType

	switch allocatorType {
	case AllocatorTypeIp:
		ext.Allocator.IpAllocator = &IpAllocator{
			BaseNet:        reader.ReadNetworkV4(),
			FirstFreeIndex: reader.ReadU64(),
		}
	case AllocatorTypeId:
		ext.Allocator.IdAllocator = &IdAllocator{
			RangeStart:     reader.ReadU16(),
			RangeEnd:       reader.ReadU16(),
			FirstFreeIndex: reader.ReadU64(),
		}
	default:
		log.Println("DeserializeResourceExtension: Unknown allocator type", allocatorType)
		return
	}

	// Skip to bitmap offset (header is padded to 88 bytes for alignment)
	currentOffset := reader.GetOffset()
	if currentOffset < resourceExtensionBitmapOffset {
		reader.Skip(resourceExtensionBitmapOffset - currentOffset)
	}

	// Read remaining bytes as storage bitmap
	remaining := int(reader.Remaining())
	if remaining > 0 {
		ext.Storage = reader.ReadBytes(remaining)
	}
}

func DeserializePermission(reader *ByteReader, perm *Permission) {
	perm.AccountType = AccountType(reader.ReadU8())
	perm.Owner = reader.ReadPubkey()
	perm.BumpSeed = reader.ReadU8()
	perm.Status = PermissionStatus(reader.ReadU8())
	perm.UserPayer = reader.ReadPubkey()
	perm.PermissionsLo = reader.ReadU64() // bits 0-63 (low u64 of u128)
	perm.PermissionsHi = reader.ReadU64() // bits 64-127 (high u64 of u128)
}

func DeserializeTopologyInfo(reader *ByteReader, t *TopologyInfo) {
	t.AccountType = AccountType(reader.ReadU8())
	t.Owner = reader.ReadPubkey()
	t.BumpSeed = reader.ReadU8()
	t.Name = reader.ReadString()
	t.AdminGroupBit = reader.ReadU8()
	t.FlexAlgoNumber = reader.ReadU8()
	t.Constraint = TopologyConstraint(reader.ReadU8())
	// Note: t.PubKey is set from the account address in client.go after deserialization
}
