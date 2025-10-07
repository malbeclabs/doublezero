package serviceability

import "log"

func DeserializeConfig(reader *ByteReader, cfg *Config) {
	cfg.AccountType = AccountType(reader.ReadU8())
	cfg.Owner = reader.ReadPubkey()
	cfg.Bump_seed = reader.ReadU8()
	cfg.Local_asn = reader.ReadU32()
	cfg.Remote_asn = reader.ReadU32()
	cfg.TunnelTunnelBlock = reader.ReadNetworkV4()
	cfg.UserTunnelBlock = reader.ReadNetworkV4()
	cfg.MulticastGroupBlock = reader.ReadNetworkV4()
	cfg.PubKey = reader.ReadPubkey()
}

func DeserializeLocation(reader *ByteReader, loc *Location) {
	loc.AccountType = AccountType(reader.ReadU8())
	loc.Owner = reader.ReadPubkey()
	loc.Index = reader.ReadU128()
	loc.Bump_seed = reader.ReadU8()
	loc.Lat = reader.ReadF64()
	loc.Lng = reader.ReadF64()
	loc.LocId = reader.ReadU32()
	loc.Status = LocationStatus(reader.ReadU8())
	loc.Code = reader.ReadString()
	loc.Name = reader.ReadString()
	loc.Country = reader.ReadString()
	loc.PubKey = reader.ReadPubkey()
}

func DeserializeExchange(reader *ByteReader, exchange *Exchange) {
	exchange.AccountType = AccountType(reader.ReadU8())
	exchange.Owner = reader.ReadPubkey()
	exchange.Index = reader.ReadU128()
	exchange.Bump_seed = reader.ReadU8()
	exchange.Lat = reader.ReadF64()
	exchange.Lng = reader.ReadF64()
	exchange.LocId = reader.ReadU32()
	exchange.Status = ExchangeStatus(reader.ReadU8())
	exchange.Code = reader.ReadString()
	exchange.Name = reader.ReadString()
	exchange.PubKey = reader.ReadPubkey()
}

func DeserializeContributor(reader *ByteReader, contributor *Contributor) {
	contributor.AccountType = AccountType(reader.ReadU8())
	contributor.Owner = reader.ReadPubkey()
	contributor.Index = reader.ReadU128()
	contributor.BumpSeed = reader.ReadU8()
	contributor.Status = ContributorStatus(reader.ReadU8())
	contributor.Code = reader.ReadString()
	contributor.Name = reader.ReadString()
	contributor.PubKey = reader.ReadPubkey()
}

func DeserializeInterface(reader *ByteReader, iface *Interface) {
	iface.Version = reader.ReadU8()

	if iface.Version != (CurrentInterfaceVersion - 1) { // subtract 1 because the discriminant starts from 0
		log.Println("DeserializeInterface: Unsupported interface version", iface.Version)
		return
	}

	iface.Status = InterfaceStatus(reader.ReadU8())
	iface.Name = reader.ReadString()
	iface.InterfaceType = InterfaceType(reader.ReadU8())
	iface.LoopbackType = LoopbackType(reader.ReadU8())
	iface.VlanId = reader.ReadU16()
	iface.IpNet = reader.ReadNetworkV4()
	iface.NodeSegmentIdx = reader.ReadU16()
	iface.UserTunnelEndpoint = (reader.ReadU8() != 0)
}

func DeserializeDevice(reader *ByteReader, dev *Device) {
	dev.AccountType = AccountType(reader.ReadU8())
	dev.Owner = reader.ReadPubkey()
	dev.Index = reader.ReadU128()
	dev.Bump_seed = reader.ReadU8()
	dev.LocationPubKey = reader.ReadPubkey()
	dev.ExchangePubKey = reader.ReadPubkey()
	dev.DeviceType = reader.ReadU8()
	dev.PublicIp = reader.ReadIPv4()
	dev.Status = DeviceStatus(reader.ReadU8())
	dev.Code = reader.ReadString()
	dev.DzPrefixes = reader.ReadNetworkV4Slice()
	dev.MetricsPublisherPubKey = reader.ReadPubkey()
	dev.ContributorPubKey = reader.ReadPubkey()
	dev.MgmtVrf = reader.ReadString()
	dev.Interfaces = make([]Interface, 0)
	var length = reader.ReadU32()
	if (length * 18) > reader.Remaining() {
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
	// Note: dev.PubKey is set separately in client.go after deserialization
}

func DeserializeLink(reader *ByteReader, link *Link) {
	link.AccountType = AccountType(reader.ReadU8())
	link.Owner = reader.ReadPubkey()
	link.Index = reader.ReadU128()
	link.Bump_seed = reader.ReadU8()
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
	link.PubKey = reader.ReadPubkey()
}

func DeserializeUser(reader *ByteReader, user *User) {
	user.AccountType = AccountType(reader.ReadU8())
	user.Owner = reader.ReadPubkey()
	user.Index = reader.ReadU128()
	user.Bump_seed = reader.ReadU8()
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
	user.PubKey = reader.ReadPubkey()
}

func DeserializeMulticastGroup(reader *ByteReader, multicastgroup *MulticastGroup) {
	multicastgroup.AccountType = AccountType(reader.ReadU8())
	multicastgroup.Owner = reader.ReadPubkey()
	multicastgroup.Index = reader.ReadU128()
	multicastgroup.Bump_seed = reader.ReadU8()
	multicastgroup.TenantPubKey = reader.ReadPubkey()
	multicastgroup.MulticastIp = reader.ReadIPv4()
	multicastgroup.MaxBandwidth = reader.ReadU64()
	multicastgroup.Status = MulticastGroupStatus(reader.ReadU8())
	multicastgroup.Code = reader.ReadString()
	multicastgroup.PubKey = reader.ReadPubkey()
}

func DeserializeProgramConfig(reader *ByteReader, programconfig *ProgramConfig) {
	programconfig.AccountType = AccountType(reader.ReadU8())
	programconfig.BumpSeed = reader.ReadU8()
	DeserializeProgramVersion(reader, &programconfig.Version)
}

func DeserializeProgramVersion(reader *ByteReader, programversion *ProgramVersion) {
	programversion.Major = reader.ReadU32()
	programversion.Minor = reader.ReadU32()
	programversion.Patch = reader.ReadU32()
}
