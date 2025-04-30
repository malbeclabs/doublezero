package dzsdk

func DeserializeConfig(reader *ByteReader, cfg *Config) {
	cfg.AccountType = AccountType(reader.ReadU8())
	cfg.Owner = reader.ReadPubkey()
	cfg.Bump_seed = reader.ReadU8()
	cfg.Local_asn = reader.ReadU32()
	cfg.Remote_asn = reader.ReadU32()
	cfg.TunnelTunnelBlock = reader.ReadNetworkV4()
	cfg.UserTunnelBlock = reader.ReadNetworkV4()
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
	dev.PubKey = reader.ReadPubkey()
}

func DeserializeTunnel(reader *ByteReader, tunnel *Tunnel) {
	tunnel.AccountType = AccountType(reader.ReadU8())
	tunnel.Owner = reader.ReadPubkey()
	tunnel.Index = reader.ReadU128()
	tunnel.Bump_seed = reader.ReadU8()
	tunnel.SideAPubKey = reader.ReadPubkey()
	tunnel.SideZPubKey = reader.ReadPubkey()
	tunnel.TunnelType = TunnelTunnelType(reader.ReadU8())
	tunnel.Bandwidth = reader.ReadU64()
	tunnel.Mtu = reader.ReadU32()
	tunnel.DelayNs = reader.ReadU64()
	tunnel.JitterNs = reader.ReadU64()
	tunnel.TunnelId = reader.ReadU16()
	tunnel.TunnelNet = reader.ReadNetworkV4()
	tunnel.Status = TunnelStatus(reader.ReadU8())
	tunnel.Code = reader.ReadString()
	tunnel.PubKey = reader.ReadPubkey()
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
	user.PubKey = reader.ReadPubkey()
}
