package allowlist

import (
	"net/netip"
	"reflect"
)

// isGlobalIPv4 reports whether addr may go on the list: a global IPv4 address.
// It follows is_global in the serviceability program
// (smartcontract/programs/doublezero-serviceability/src/helper.rs), which is
// what the program enforces on agent_ip, so an address the program accepts is
// never dropped here.
func isGlobalIPv4(addr netip.Addr) bool {
	if !addr.Is4() {
		return false
	}
	if addr.IsUnspecified() || addr.IsPrivate() || addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() || addr.IsMulticast() {
		return false
	}
	for _, p := range nonGlobalIPv4 {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

var nonGlobalIPv4 = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "this" network
	netip.MustParsePrefix("100.64.0.0/10"),   // shared address space (CGNAT)
	netip.MustParsePrefix("192.0.0.0/24"),    // IETF protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved, which holds broadcast
}

// agentIP returns the device's agent_ip, the address its agents connect from
// when that is neither public_ip nor an interface address (spec section 6).
//
// The serviceability Go SDK on main has no AgentIp field yet. It arrives with
// the agent_ip change to the Device account, as `AgentIp [4]uint8`. Until then
// this reads the field by name, so the sync picks it up without a code change
// whichever lands first. Once the SDK has it, replace the body with
// `netip.AddrFrom4(d.AgentIp)` and take a serviceability.Device.
func agentIP(device any) netip.Addr {
	v := reflect.ValueOf(device)
	if v.Kind() != reflect.Struct {
		return netip.Addr{}
	}
	f := v.FieldByName("AgentIp")
	if !f.IsValid() {
		return netip.Addr{}
	}
	ip, ok := f.Interface().([4]uint8)
	if !ok {
		return netip.Addr{}
	}
	return netip.AddrFrom4(ip)
}
