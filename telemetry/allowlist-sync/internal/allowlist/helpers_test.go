package allowlist

import (
	"net/netip"
	"testing"

	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
)

func key(n byte) [32]byte {
	var k [32]byte
	k[0] = n
	k[31] = 0xAA
	return k
}

func pk(n byte) string {
	k := key(n)
	return base58.Encode(k[:])
}

func ip4(s string) [4]byte { return netip.MustParseAddr(s).As4() }

func ipNet(s string) [5]byte {
	p := netip.MustParsePrefix(s)
	a := p.Addr().As4()
	return [5]byte{a[0], a[1], a[2], a[3], byte(p.Bits())}
}

func addrs(ss ...string) []netip.Addr {
	out := make([]netip.Addr, len(ss))
	for i, s := range ss {
		out[i] = netip.MustParseAddr(s)
	}
	return out
}

func dev(n byte, code string, status serviceability.DeviceStatus, publicIP string, ifaces ...string) serviceability.Device {
	d := serviceability.Device{PubKey: key(n), Code: code, Status: status, PublicIp: ip4(publicIP)}
	for _, i := range ifaces {
		d.Interfaces = append(d.Interfaces, serviceability.Interface{IpNet: ipNet(i)})
	}
	return d
}

func mustBuild(t *testing.T, devices []serviceability.Device, exc Exceptions) Built {
	t.Helper()
	return Build(devices, exc)
}
