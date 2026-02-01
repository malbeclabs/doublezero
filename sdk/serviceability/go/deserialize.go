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

func DeserializeInterface(reader *ByteReader, iface *Interface) {
	iface.Version = reader.ReadU8()

	if iface.Version > (CurrentInterfaceVersion - 1) {
		log.Println("DeserializeInterface: Unsupported interface version", iface.Version)
		return
	}

	switch iface.Version {
	case 0: // version 1
		DeserializeInterfaceV1(reader, iface)
	case 1: // version 2
		DeserializeInterfaceV2(reader, iface)
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
	iface.UserTunnelEndpoint = reader.ReadBool()
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
	iface.UserTunnelEndpoint = reader.ReadBool()
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
	if ap.AccessPassTypeTag == AccessPassTypeSolanaValidator {
		ap.ValidatorPubKey = reader.ReadPubkey()
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
